package baseatlas

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"ariga.io/atlas/sql/migrate"
	"ariga.io/atlas/sql/postgres"
	"ariga.io/atlas/sql/schema"
	"ariga.io/atlas/sql/sqlclient"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	_ "github.com/lib/pq"
)

var (
	ErrNoSchemaSources    = errors.New("baseatlas: no schema sources configured")
	ErrInspectUnavailable = errors.New("baseatlas: current database inspection is not wired yet")
)

type ProjectConfig struct {
	SchemaPaths []string
}

type Statement struct {
	Comment string
	SQL     string
	Reverse string
}

type CurrentInspector func(context.Context, string) (*schema.Realm, error)

type BaseEngine interface {
	LoadDesired(context.Context, ProjectConfig) (*schema.Realm, error)
	InspectCurrent(context.Context, string) (*schema.Realm, error)
	Diff(context.Context, *schema.Realm, *schema.Realm) ([]schema.Change, error)
	PlanSQL(context.Context, string, []schema.Change) ([]Statement, error)
}

type Engine struct {
	Differ    schema.Differ
	Planner   migrate.PlanApplier
	Inspector CurrentInspector
}

func New() *Engine {
	return &Engine{
		Differ:  postgres.DefaultDiff,
		Planner: postgres.DefaultPlan,
	}
}

func (e *Engine) LoadDesired(ctx context.Context, cfg ProjectConfig) (*schema.Realm, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(cfg.SchemaPaths) == 0 {
		return nil, ErrNoSchemaSources
	}

	var src strings.Builder
	for _, path := range cfg.SchemaPaths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read schema source %q: %w", path, err)
		}
		b = stripEdvatOwnedBlocks(b, path)
		src.Write(b)
		src.WriteString("\n")
	}

	var desired schema.Realm
	if err := postgres.EvalHCLBytes([]byte(src.String()), &desired, nil); err != nil {
		return nil, fmt.Errorf("evaluate postgres hcl: %w", err)
	}
	normalizePublicSchema(&desired)
	return &desired, nil
}

func (e *Engine) InspectCurrent(ctx context.Context, url string) (*schema.Realm, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e != nil && e.Inspector != nil {
		return e.Inspector(ctx, url)
	}
	if strings.TrimSpace(url) == "" {
		return nil, ErrInspectUnavailable
	}
	return InspectURL(ctx, url)
}

func InspectURL(ctx context.Context, url string) (*schema.Realm, error) {
	client, err := sqlclient.Open(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open postgres atlas client: %w", err)
	}
	defer client.Close()
	inspector, ok := client.Driver.(schema.Inspector)
	if !ok {
		return nil, fmt.Errorf("postgres atlas driver does not inspect realms")
	}
	realm, err := inspector.InspectRealm(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("inspect current realm: %w", err)
	}
	normalizePublicSchema(realm)
	return realm, nil
}

func (e *Engine) Diff(ctx context.Context, current, desired *schema.Realm) ([]schema.Change, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if current == nil {
		current = schema.NewRealm()
	}
	if desired == nil {
		desired = schema.NewRealm()
	}
	normalizePublicSchema(current)
	normalizePublicSchema(desired)
	differ := postgres.DefaultDiff
	if e != nil && e.Differ != nil {
		differ = e.Differ
	}
	changes, err := differ.RealmDiff(current, desired)
	if err != nil {
		return nil, fmt.Errorf("atlas realm diff: %w", err)
	}
	return dropNoopPublicTypeChanges(changes), nil
}

func (e *Engine) PlanSQL(ctx context.Context, name string, changes []schema.Change) ([]Statement, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	planner := postgres.DefaultPlan
	if e != nil && e.Planner != nil {
		planner = e.Planner
	}
	plan, err := planner.PlanChanges(ctx, name, changes)
	if err != nil {
		return nil, fmt.Errorf("atlas plan sql: %w", err)
	}
	statements := make([]Statement, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		statements = append(statements, Statement{
			Comment: change.Comment,
			SQL:     normalizePlannedSQL(change.Cmd),
			Reverse: normalizePlannedSQL(reverseSQL(change.Reverse)),
		})
	}
	return statements, nil
}

func reverseSQL(reverse any) string {
	switch r := reverse.(type) {
	case nil:
		return ""
	case string:
		return r
	case []string:
		return strings.Join(r, ";\n")
	default:
		return fmt.Sprint(r)
	}
}

func normalizePlannedSQL(sql string) string {
	return strings.ReplaceAll(sql, `DEFAULT now() AT TIME ZONE 'utc'::text`, `DEFAULT (now() AT TIME ZONE 'utc'::text)`)
}

func dropNoopPublicTypeChanges(changes []schema.Change) []schema.Change {
	out := changes[:0]
	for _, change := range changes {
		mt, ok := change.(*schema.ModifyTable)
		if !ok {
			out = append(out, change)
			continue
		}
		kept := mt.Changes[:0]
		for _, tableChange := range mt.Changes {
			mc, ok := tableChange.(*schema.ModifyColumn)
			if !ok {
				kept = append(kept, tableChange)
				continue
			}
			if mc.Change.Is(schema.ChangeType) && samePublicColumnType(mc.From, mc.To) {
				mc.Change &^= schema.ChangeType
			}
			if mc.Change.Is(schema.ChangeDefault) && samePublicDefault(mc.From, mc.To) {
				mc.Change &^= schema.ChangeDefault
			}
			if mc.Change != schema.NoChange {
				kept = append(kept, mc)
			}
		}
		mt.Changes = kept
		if len(mt.Changes) > 0 {
			out = append(out, mt)
		}
	}
	return out
}

func samePublicColumnType(from, to *schema.Column) bool {
	fromName, fromOK := publicTypeName(from)
	toName, toOK := publicTypeName(to)
	return fromOK && toOK && fromName == toName
}

func publicTypeName(c *schema.Column) (string, bool) {
	if c == nil || c.Type == nil {
		return "", false
	}
	switch typ := c.Type.Type.(type) {
	case *schema.EnumType:
		if typ.Schema == nil || typ.Schema.Name == "public" {
			name := unqualifiedPublicTypeName(typ.T)
			return name, name != ""
		}
	case *postgres.UserDefinedType:
		name := unqualifiedPublicTypeName(typ.T)
		return name, name != ""
	}
	name := unqualifiedPublicTypeName(c.Type.Raw)
	return name, name != ""
}

func samePublicDefault(from, to *schema.Column) bool {
	return publicDefaultText(from) == publicDefaultText(to)
}

func publicDefaultText(c *schema.Column) string {
	if c == nil {
		return ""
	}
	s := normalizePublicSQL(exprText(c.Default))
	if i := strings.Index(s, "::"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return strings.Trim(s, `"'`)
}

func normalizePublicSchema(realm *schema.Realm) {
	if realm == nil {
		return
	}
	publicTypes := map[string]bool{}
	for _, s := range realm.Schemas {
		if s.Name != "public" {
			continue
		}
		for _, o := range s.Objects {
			if enum, ok := o.(*schema.EnumType); ok {
				publicTypes[enum.T] = true
			}
		}
	}
	for _, s := range realm.Schemas {
		for _, t := range s.Tables {
			for _, c := range t.Columns {
				normalizePublicColumn(c, publicTypes)
			}
			for _, idx := range t.Indexes {
				normalizePublicIndex(idx, publicTypes)
			}
		}
	}
}

func normalizePublicColumn(c *schema.Column, publicTypes map[string]bool) {
	if c == nil || c.Type == nil {
		return
	}
	if enum, ok := c.Type.Type.(*schema.EnumType); ok && enum.Schema != nil && enum.Schema.Name == "public" {
		c.Type.Raw = qualifiedPublicIdent(enum.T)
		c.Default = normalizeEnumDefault(c.Default)
	} else if name := unqualifiedPublicTypeName(c.Type.Raw); publicTypes[name] {
		c.Type.Raw = qualifiedPublicIdent(name)
	}
	c.Default = normalizePublicDefault(c.Default)
}

func normalizePublicIndex(idx *schema.Index, publicTypes map[string]bool) {
	if idx == nil {
		return
	}
	for _, attr := range idx.Attrs {
		if pred, ok := attr.(*postgres.IndexPredicate); ok {
			pred.P = qualifyPublicTypeCasts(normalizePublicSQL(pred.P), publicTypes)
		}
	}
}

func normalizeEnumDefault(expr schema.Expr) schema.Expr {
	s := exprText(expr)
	if s == "" {
		return expr
	}
	base := strings.TrimSpace(stripPublicQualifiers(s))
	if i := strings.Index(base, "::"); i >= 0 {
		base = strings.TrimSpace(base[:i])
	}
	base = strings.Trim(base, "'")
	if base == "" {
		return expr
	}
	return &schema.Literal{V: base}
}

func normalizePublicDefault(expr schema.Expr) schema.Expr {
	s := exprText(expr)
	if s == "" {
		return expr
	}
	normalized := normalizePublicSQL(s)
	if strings.EqualFold(normalized, "uuid_generate_v7()") {
		normalized = "public.uuid_generate_v7()"
	}
	if normalized == s {
		return expr
	}
	return &schema.RawExpr{X: normalized}
}

func normalizePublicSQL(sql string) string {
	s := stripPublicQualifiers(strings.TrimSpace(sql))
	for strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") && balancedSQLParens(s[1:len(s)-1]) {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

func stripPublicQualifiers(s string) string {
	s = strings.ReplaceAll(s, `"public".`, "")
	s = strings.ReplaceAll(s, "public.", "")
	return s
}

func qualifyPublicTypeCasts(sql string, publicTypes map[string]bool) string {
	for name := range publicTypes {
		sql = strings.ReplaceAll(sql, "::"+name, "::"+qualifiedPublicIdent(name))
		sql = strings.ReplaceAll(sql, `::"`+name+`"`, "::"+qualifiedPublicIdent(name))
	}
	return sql
}

func unqualifiedPublicTypeName(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, `"public".`)
	raw = strings.TrimPrefix(raw, "public.")
	return strings.Trim(raw, `"`)
}

func qualifiedPublicIdent(name string) string {
	return `"public".` + quoteIdent(name)
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func exprText(expr schema.Expr) string {
	switch x := expr.(type) {
	case *schema.RawExpr:
		return x.X
	case *schema.Literal:
		return x.V
	case *schema.NamedDefault:
		return exprText(x.Expr)
	default:
		return ""
	}
}

func balancedSQLParens(s string) bool {
	depth := 0
	inSingle := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' {
			inSingle = !inSingle
			if inSingle && i+1 < len(s) && s[i+1] == '\'' {
				i++
			}
			continue
		}
		if inSingle {
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0 && !inSingle
}

func stripEdvatOwnedBlocks(src []byte, filename string) []byte {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return src
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return src
	}
	out := append([]byte(nil), src...)
	for _, block := range body.Blocks {
		if !edvatOwnsBlock(block) {
			continue
		}
		range_ := block.Range()
		start, end := range_.Start.Byte, range_.End.Byte
		if start < 0 || end > len(out) || start >= end {
			continue
		}
		for i := start; i < end; i++ {
			if out[i] != '\n' && out[i] != '\r' {
				out[i] = ' '
			}
		}
	}
	return out
}

func edvatOwnsBlock(block *hclsyntax.Block) bool {
	if block.Type == "data" {
		return len(block.Labels) == 0
	}
	switch block.Type {
	case "aggregate",
		"cast",
		"collation",
		"composite",
		"default_permission",
		"domain",
		"event_trigger",
		"extension",
		"foreign_table",
		"function",
		"materialized",
		"partition",
		"permission",
		"policy",
		"procedure",
		"range",
		"role",
		"sequence",
		"server",
		"trigger",
		"user",
		"user_mapping",
		"view":
		return true
	default:
		return false
	}
}
