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

type Composite struct {
	Name    string
	Schema  string
	Fields  []CompositeField
	Comment string
}

type CompositeField struct {
	Name      string
	Type      string
	Collation string
}

type CompositeState map[string]Composite

func ParseCompositeFiles(paths []string) (CompositeState, error) {
	return parseStateFiles(paths, "composite", ParseCompositesHCL)
}

func ParseCompositesHCL(src []byte, filename string) (CompositeState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse composite hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse composite hcl: unexpected body type %T", file.Body)
	}

	state := CompositeState{}
	for _, block := range body.Blocks {
		if block.Type != "composite" || len(block.Labels) != 1 {
			continue
		}
		composite := Composite{Name: block.Labels[0]}
		attrs := block.Body.Attributes
		if attr, ok := attrs["schema"]; ok {
			schemaName, err := schemaExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode composite.%s.schema: %w", composite.Name, err)
			}
			composite.Schema = schemaName
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode composite.%s.comment: %w", composite.Name, err)
			}
			composite.Comment = comment
		}
		for _, nested := range block.Body.Blocks {
			if nested.Type != "field" || len(nested.Labels) != 1 {
				continue
			}
			field := CompositeField{Name: nested.Labels[0]}
			if attr, ok := nested.Body.Attributes["type"]; ok {
				field.Type = typeExpr(attr.Expr, src)
			}
			if attr, ok := nested.Body.Attributes["collation"]; ok {
				collation, err := schemaQualifiedExpr(attr.Expr)
				if err != nil {
					return nil, fmt.Errorf("decode composite.%s.field.%s.collation: %w", composite.Name, field.Name, err)
				}
				field.Collation = collation
			}
			if field.Type == "" {
				return nil, fmt.Errorf("composite.%s field.%s requires type", composite.Name, field.Name)
			}
			composite.Fields = append(composite.Fields, field)
		}
		if len(composite.Fields) == 0 {
			return nil, fmt.Errorf("composite.%s requires at least one field", composite.Name)
		}
		state[compositeID(composite)] = composite
	}
	return state, nil
}

func InspectCompositesURL(ctx context.Context, url string) (CompositeState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectComposites(ctx, db)
}

func InspectComposites(ctx context.Context, db *sql.DB) (CompositeState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT n.nspname,
       t.typname,
       a.attname,
       format_type(a.atttypid, a.atttypmod),
       COALESCE(cn.nspname || '.' || co.collname, ''),
       COALESCE(d.description, '')
FROM pg_type t
JOIN pg_namespace n ON n.oid = t.typnamespace
JOIN pg_class c ON c.oid = t.typrelid
JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
LEFT JOIN pg_collation co ON co.oid = a.attcollation AND a.attcollation <> 0
LEFT JOIN pg_namespace cn ON cn.oid = co.collnamespace
LEFT JOIN pg_description d ON d.objoid = t.oid AND d.classoid = 'pg_type'::regclass AND d.objsubid = 0
WHERE t.typtype = 'c'
  AND c.relkind = 'c'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY n.nspname, t.typname, a.attnum`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres composites: %w", err)
	}
	defer rows.Close()
	state := CompositeState{}
	for rows.Next() {
		var schemaName, name, fieldName, fieldType, collation, comment string
		if err := rows.Scan(&schemaName, &name, &fieldName, &fieldType, &collation, &comment); err != nil {
			return nil, fmt.Errorf("scan postgres composite: %w", err)
		}
		id := viewID(schemaName, name)
		composite := state[id]
		composite.Name = name
		composite.Schema = schemaName
		composite.Comment = comment
		composite.Fields = append(composite.Fields, CompositeField{Name: fieldName, Type: fieldType, Collation: collation})
		state[id] = composite
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres composites: %w", err)
	}
	return state, nil
}

func DiffComposites(current, desired CompositeState) []baseatlas.Statement {
	if current == nil {
		current = CompositeState{}
	}
	if desired == nil {
		desired = CompositeState{}
	}
	ids := stateIDs(current, desired)

	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createCompositeStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, baseatlas.Statement{Comment: "drop composite " + compositeID(cur) + " (destructive)", SQL: "DROP TYPE " + qualifiedIdent(cur.Schema, cur.Name), Reverse: strings.Join(compositeSQL(cur), ";\n")})
		case hasCurrent && hasDesired:
			if !sameCompositeDefinition(cur, des) {
				statements = append(statements, baseatlas.Statement{Comment: "drop composite " + compositeID(cur) + " for replacement (destructive)", SQL: "DROP TYPE " + qualifiedIdent(cur.Schema, cur.Name)})
				statements = append(statements, createCompositeStatements(des)...)
				continue
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on composite " + compositeID(des), SQL: "COMMENT ON TYPE " + qualifiedIdent(des.Schema, des.Name) + " IS " + nullableLiteral(des.Comment), Reverse: "COMMENT ON TYPE " + qualifiedIdent(cur.Schema, cur.Name) + " IS " + nullableLiteral(cur.Comment)})
			}
		}
	}
	return statements
}

func createCompositeStatements(composite Composite) []baseatlas.Statement {
	statements := []baseatlas.Statement{{Comment: "create composite " + compositeID(composite), SQL: createCompositeSQL(composite), Reverse: "DROP TYPE " + qualifiedIdent(composite.Schema, composite.Name)}}
	if composite.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on composite " + compositeID(composite), SQL: "COMMENT ON TYPE " + qualifiedIdent(composite.Schema, composite.Name) + " IS " + literal(composite.Comment), Reverse: "COMMENT ON TYPE " + qualifiedIdent(composite.Schema, composite.Name) + " IS NULL"})
	}
	return statements
}

func compositeSQL(composite Composite) []string {
	statements := []string{createCompositeSQL(composite)}
	if composite.Comment != "" {
		statements = append(statements, "COMMENT ON TYPE "+qualifiedIdent(composite.Schema, composite.Name)+" IS "+literal(composite.Comment))
	}
	return statements
}

func createCompositeSQL(composite Composite) string {
	parts := make([]string, 0, len(composite.Fields))
	for _, field := range composite.Fields {
		part := quoteIdent(field.Name) + " " + field.Type
		if field.Collation != "" {
			part += " COLLATE " + qualifiedNameLiteral(field.Collation)
		}
		parts = append(parts, part)
	}
	return "CREATE TYPE " + qualifiedIdent(composite.Schema, composite.Name) + " AS (" + strings.Join(parts, ", ") + ")"
}

func sameCompositeDefinition(a, b Composite) bool {
	if len(a.Fields) != len(b.Fields) {
		return false
	}
	for i := range a.Fields {
		if a.Fields[i].Name != b.Fields[i].Name || normalizeSQL(a.Fields[i].Type) != normalizeSQL(b.Fields[i].Type) || a.Fields[i].Collation != b.Fields[i].Collation {
			return false
		}
	}
	return true
}

func compositeID(composite Composite) string { return viewID(composite.Schema, composite.Name) }

func schemaQualifiedExpr(expr hclsyntax.Expression) (string, error) {
	if traversal, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		parts := traversalParts(traversal.Traversal)
		if len(parts) == 2 {
			return parts[0] + "." + parts[1], nil
		}
		if len(parts) == 3 && parts[0] == "schema" {
			return parts[1] + "." + parts[2], nil
		}
	}
	return stringExpr(expr)
}

func qualifiedNameLiteral(name string) string {
	parts := strings.Split(name, ".")
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, quoteIdent(part))
	}
	return strings.Join(quoted, ".")
}
