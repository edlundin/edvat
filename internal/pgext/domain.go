package pgext

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/edlundin/edvat/internal/baseatlas"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

type Domain struct {
	Name    string
	Schema  string
	Type    string
	NotNull bool
	Default string
	Checks  []DomainCheck
	Comment string
}

type DomainCheck struct {
	Name string
	Expr string
}

type DomainState map[string]Domain

func ParseDomainFiles(paths []string) (DomainState, error) {
	return parseStateFiles(paths, "domain", ParseDomainsHCL)
}

func ParseDomainsHCL(src []byte, filename string) (DomainState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse domain hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse domain hcl: unexpected body type %T", file.Body)
	}

	state := DomainState{}
	for _, block := range body.Blocks {
		if block.Type != "domain" || len(block.Labels) != 1 {
			continue
		}
		domain := Domain{Name: block.Labels[0]}
		attrs := block.Body.Attributes
		if attr, ok := attrs["schema"]; ok {
			schemaName, err := schemaExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode domain.%s.schema: %w", domain.Name, err)
			}
			domain.Schema = schemaName
		}
		if attr, ok := attrs["type"]; ok {
			domain.Type = typeExpr(attr.Expr, src)
		}
		if attr, ok := attrs["null"]; ok {
			null, err := boolExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode domain.%s.null: %w", domain.Name, err)
			}
			domain.NotNull = !null
		}
		if attr, ok := attrs["default"]; ok {
			domain.Default = typeExpr(attr.Expr, src)
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode domain.%s.comment: %w", domain.Name, err)
			}
			domain.Comment = comment
		}
		for _, nested := range block.Body.Blocks {
			if nested.Type != "check" {
				continue
			}
			check := DomainCheck{}
			if len(nested.Labels) > 0 {
				check.Name = nested.Labels[0]
			}
			if attr, ok := nested.Body.Attributes["expr"]; ok {
				expr, err := stringExpr(attr.Expr)
				if err != nil {
					return nil, fmt.Errorf("decode domain.%s.check.expr: %w", domain.Name, err)
				}
				check.Expr = strings.TrimSpace(expr)
			}
			if check.Expr == "" {
				return nil, fmt.Errorf("domain.%s check requires expr", domain.Name)
			}
			domain.Checks = append(domain.Checks, check)
		}
		if domain.Type == "" {
			return nil, fmt.Errorf("domain.%s requires type", domain.Name)
		}
		state[domainID(domain)] = domain
	}
	return state, nil
}

func InspectDomainsURL(ctx context.Context, url string) (DomainState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectDomains(ctx, db)
}

func InspectDomains(ctx context.Context, db *sql.DB) (DomainState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT n.nspname,
       t.typname,
       format_type(t.typbasetype, t.typtypmod),
       t.typnotnull,
       COALESCE(pg_get_expr(t.typdefaultbin, 0), t.typdefault, ''),
       COALESCE(d.description, '')
FROM pg_type t
JOIN pg_namespace n ON n.oid = t.typnamespace
LEFT JOIN pg_description d ON d.objoid = t.oid AND d.classoid = 'pg_type'::regclass AND d.objsubid = 0
WHERE t.typtype = 'd'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY n.nspname, t.typname`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres domains: %w", err)
	}
	defer rows.Close()
	state := DomainState{}
	for rows.Next() {
		var domain Domain
		if err := rows.Scan(&domain.Schema, &domain.Name, &domain.Type, &domain.NotNull, &domain.Default, &domain.Comment); err != nil {
			return nil, fmt.Errorf("scan postgres domain: %w", err)
		}
		state[domainID(domain)] = domain
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres domains: %w", err)
	}
	return state, nil
}

func DiffDomains(current, desired DomainState) []baseatlas.Statement {
	if current == nil {
		current = DomainState{}
	}
	if desired == nil {
		desired = DomainState{}
	}
	ids := stateIDs(current, desired)

	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createDomainStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, baseatlas.Statement{Comment: "drop domain " + domainID(cur) + " (destructive)", SQL: "DROP DOMAIN " + qualifiedIdent(cur.Schema, cur.Name), Reverse: strings.Join(domainSQL(cur), ";\n")})
		case hasCurrent && hasDesired:
			if !sameDomainDefinition(cur, des) {
				statements = append(statements,
					baseatlas.Statement{Comment: "drop domain " + domainID(cur) + " for replacement (destructive)", SQL: "DROP DOMAIN " + qualifiedIdent(cur.Schema, cur.Name)},
				)
				statements = append(statements, createDomainStatements(des)...)
				continue
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on domain " + domainID(des), SQL: "COMMENT ON DOMAIN " + qualifiedIdent(des.Schema, des.Name) + " IS " + nullableLiteral(des.Comment), Reverse: "COMMENT ON DOMAIN " + qualifiedIdent(cur.Schema, cur.Name) + " IS " + nullableLiteral(cur.Comment)})
			}
		}
	}
	return statements
}

func createDomainStatements(domain Domain) []baseatlas.Statement {
	statements := []baseatlas.Statement{{Comment: "create domain " + domainID(domain), SQL: createDomainSQL(domain), Reverse: "DROP DOMAIN " + qualifiedIdent(domain.Schema, domain.Name)}}
	if domain.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on domain " + domainID(domain), SQL: "COMMENT ON DOMAIN " + qualifiedIdent(domain.Schema, domain.Name) + " IS " + literal(domain.Comment), Reverse: "COMMENT ON DOMAIN " + qualifiedIdent(domain.Schema, domain.Name) + " IS NULL"})
	}
	return statements
}

func domainSQL(domain Domain) []string {
	statements := []string{createDomainSQL(domain)}
	if domain.Comment != "" {
		statements = append(statements, "COMMENT ON DOMAIN "+qualifiedIdent(domain.Schema, domain.Name)+" IS "+literal(domain.Comment))
	}
	return statements
}

func createDomainSQL(domain Domain) string {
	var b strings.Builder
	b.WriteString("CREATE DOMAIN ")
	b.WriteString(qualifiedIdent(domain.Schema, domain.Name))
	b.WriteString(" AS ")
	b.WriteString(domain.Type)
	if domain.Default != "" {
		b.WriteString(" DEFAULT ")
		b.WriteString(domain.Default)
	}
	if domain.NotNull {
		b.WriteString(" NOT NULL")
	}
	for _, check := range domain.Checks {
		b.WriteString(" CONSTRAINT ")
		if check.Name != "" {
			b.WriteString(quoteIdent(check.Name))
		} else {
			b.WriteString(quoteIdent(domain.Name + "_check"))
		}
		b.WriteString(" CHECK (")
		b.WriteString(check.Expr)
		b.WriteString(")")
	}
	return b.String()
}

func sameDomainDefinition(a, b Domain) bool {
	return normalizeSQL(a.Type) == normalizeSQL(b.Type) && a.NotNull == b.NotNull && normalizeSQL(a.Default) == normalizeSQL(b.Default) && sameDomainChecks(a.Checks, b.Checks)
}

func sameDomainChecks(a, b []DomainCheck) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || normalizeSQL(a[i].Expr) != normalizeSQL(b[i].Expr) {
			return false
		}
	}
	return true
}

func domainID(domain Domain) string { return viewID(domain.Schema, domain.Name) }

func boolExpr(expr hclsyntax.Expression) (bool, error) {
	value, diags := expr.Value(nil)
	if diags.HasErrors() {
		return false, fmt.Errorf("%s", diags.Error())
	}
	if value.Type().FriendlyName() != "bool" {
		return false, fmt.Errorf("expected bool, got %s", value.Type().FriendlyName())
	}
	return value.True(), nil
}
