package pgext

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/edlundin/edvat/internal/baseatlas"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	_ "github.com/lib/pq"
	"github.com/zclconf/go-cty/cty"
)

type Extension struct {
	Name    string
	Schema  string
	Version string
	Comment string
}

type State map[string]Extension

func ParseFiles(paths []string) (State, error) {
	return parseStateFiles(paths, "extension", ParseHCL)
}

func ParseHCL(src []byte, filename string) (State, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse extension hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse extension hcl: unexpected body type %T", file.Body)
	}

	state := State{}
	for _, block := range body.Blocks {
		if block.Type != "extension" || len(block.Labels) != 1 {
			continue
		}
		ext := Extension{Name: block.Labels[0]}
		attrs := block.Body.Attributes
		if attr, ok := attrs["schema"]; ok {
			schemaName, err := schemaExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode extension.%s.schema: %w", ext.Name, err)
			}
			ext.Schema = schemaName
		}
		if attr, ok := attrs["version"]; ok {
			version, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode extension.%s.version: %w", ext.Name, err)
			}
			ext.Version = version
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode extension.%s.comment: %w", ext.Name, err)
			}
			ext.Comment = comment
		}
		state[ext.Name] = ext
	}
	return state, nil
}

func InspectURL(ctx context.Context, url string) (State, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return Inspect(ctx, db)
}

func Inspect(ctx context.Context, db *sql.DB) (State, error) {
	rows, err := db.QueryContext(ctx, `
SELECT e.extname, n.nspname, e.extversion, COALESCE(d.description, '')
FROM pg_extension e
JOIN pg_namespace n ON n.oid = e.extnamespace
LEFT JOIN pg_description d ON d.objoid = e.oid AND d.classoid = 'pg_extension'::regclass AND d.objsubid = 0
ORDER BY e.extname`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres extensions: %w", err)
	}
	defer rows.Close()
	state := State{}
	for rows.Next() {
		var ext Extension
		if err := rows.Scan(&ext.Name, &ext.Schema, &ext.Version, &ext.Comment); err != nil {
			return nil, fmt.Errorf("scan postgres extension: %w", err)
		}
		state[ext.Name] = ext
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres extensions: %w", err)
	}
	return state, nil
}

func Diff(current, desired State) []baseatlas.Statement {
	if current == nil {
		current = State{}
	}
	if desired == nil {
		desired = State{}
	}
	var names []string
	seen := map[string]bool{}
	for name := range desired {
		names = append(names, name)
		seen[name] = true
	}
	for name := range current {
		if !seen[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	var statements []baseatlas.Statement
	for _, name := range names {
		cur, hasCurrent := current[name]
		des, hasDesired := desired[name]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createStatements(des)...)
		case hasCurrent && !hasDesired:
			if isImplicitExtension(name) {
				continue
			}
			statements = append(statements, baseatlas.Statement{
				Comment: "drop extension " + name + " (destructive)",
				SQL:     "DROP EXTENSION " + quoteIdent(name),
			})
		case hasCurrent && hasDesired:
			if des.Version != "" && cur.Version != des.Version {
				statements = append(statements, baseatlas.Statement{
					Comment: "update extension " + name,
					SQL:     "ALTER EXTENSION " + quoteIdent(name) + " UPDATE TO " + literal(des.Version),
				})
			}
			if des.Comment != "" && cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{
					Comment: "set comment on extension " + name,
					SQL:     "COMMENT ON EXTENSION " + quoteIdent(name) + " IS " + nullableLiteral(des.Comment),
				})
			}
		}
	}
	return statements
}

func isImplicitExtension(name string) bool {
	return strings.EqualFold(name, "plpgsql")
}

func createStatements(ext Extension) []baseatlas.Statement {
	var b strings.Builder
	b.WriteString("CREATE EXTENSION ")
	b.WriteString(quoteIdent(ext.Name))
	if ext.Schema != "" {
		b.WriteString(" WITH SCHEMA ")
		b.WriteString(quoteIdent(ext.Schema))
	}
	if ext.Version != "" {
		b.WriteString(" VERSION ")
		b.WriteString(literal(ext.Version))
	}
	statements := []baseatlas.Statement{{Comment: "create extension " + ext.Name, SQL: b.String(), Reverse: "DROP EXTENSION " + quoteIdent(ext.Name)}}
	if ext.Comment != "" {
		statements = append(statements, baseatlas.Statement{
			Comment: "set comment on extension " + ext.Name,
			SQL:     "COMMENT ON EXTENSION " + quoteIdent(ext.Name) + " IS " + literal(ext.Comment),
			Reverse: "COMMENT ON EXTENSION " + quoteIdent(ext.Name) + " IS NULL",
		})
	}
	return statements
}

func schemaExpr(expr hclsyntax.Expression) (string, error) {
	if traversal, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		parts := traversalParts(traversal.Traversal)
		if len(parts) == 2 && parts[0] == "schema" {
			return parts[1], nil
		}
	}
	return stringExpr(expr)
}

func stringExpr(expr hclsyntax.Expression) (string, error) {
	value, diags := expr.Value(nil)
	if diags.HasErrors() {
		return "", fmt.Errorf("%s", diags.Error())
	}
	if value.Type() != cty.String || value.IsNull() {
		return "", fmt.Errorf("expected string, got %s", value.Type().FriendlyName())
	}
	return value.AsString(), nil
}

func rejectUnknownAttrs(scope string, attrs hclsyntax.Attributes, allowed map[string]bool) error {
	var names []string
	for name := range attrs {
		if !allowed[name] {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return fmt.Errorf("%s has unsupported attribute(s): %s", scope, strings.Join(names, ", "))
}

func traversalParts(traversal hcl.Traversal) []string {
	parts := make([]string, 0, len(traversal))
	for _, traverser := range traversal {
		switch t := traverser.(type) {
		case hcl.TraverseRoot:
			parts = append(parts, t.Name)
		case hcl.TraverseAttr:
			parts = append(parts, t.Name)
		}
	}
	return parts
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func literal(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func nullableLiteral(value string) string {
	if value == "" {
		return "NULL"
	}
	return literal(value)
}
