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

type MaterializedView struct {
	Name    string
	Schema  string
	SQL     string
	Comment string
}

type MaterializedViewState map[string]MaterializedView

func ParseMaterializedViewFiles(paths []string) (MaterializedViewState, error) {
	return parseStateFiles(paths, "materialized view", ParseMaterializedViewsHCL)
}

func ParseMaterializedViewsHCL(src []byte, filename string) (MaterializedViewState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse materialized view hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse materialized view hcl: unexpected body type %T", file.Body)
	}

	state := MaterializedViewState{}
	for _, block := range body.Blocks {
		if block.Type != "materialized" || len(block.Labels) != 1 {
			continue
		}
		view := MaterializedView{Name: block.Labels[0]}
		attrs := block.Body.Attributes
		if attr, ok := attrs["schema"]; ok {
			schemaName, err := schemaExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode materialized.%s.schema: %w", view.Name, err)
			}
			view.Schema = schemaName
		}
		if attr, ok := attrs["as"]; ok {
			sql, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode materialized.%s.as: %w", view.Name, err)
			}
			view.SQL = strings.TrimSpace(sql)
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode materialized.%s.comment: %w", view.Name, err)
			}
			view.Comment = comment
		}
		if view.SQL == "" {
			return nil, fmt.Errorf("materialized.%s requires as", view.Name)
		}
		state[materializedViewID(view)] = view
	}
	return state, nil
}

func InspectMaterializedViewsURL(ctx context.Context, url string) (MaterializedViewState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectMaterializedViews(ctx, db)
}

func InspectMaterializedViews(ctx context.Context, db *sql.DB) (MaterializedViewState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT n.nspname, c.relname, pg_get_viewdef(c.oid, true), COALESCE(d.description, '')
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_description d ON d.objoid = c.oid AND d.classoid = 'pg_class'::regclass AND d.objsubid = 0
WHERE c.relkind = 'm'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY n.nspname, c.relname`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres materialized views: %w", err)
	}
	defer rows.Close()
	state := MaterializedViewState{}
	for rows.Next() {
		var view MaterializedView
		if err := rows.Scan(&view.Schema, &view.Name, &view.SQL, &view.Comment); err != nil {
			return nil, fmt.Errorf("scan postgres materialized view: %w", err)
		}
		view.SQL = strings.TrimSpace(view.SQL)
		state[materializedViewID(view)] = view
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres materialized views: %w", err)
	}
	return state, nil
}

func DiffMaterializedViews(current, desired MaterializedViewState) []baseatlas.Statement {
	if current == nil {
		current = MaterializedViewState{}
	}
	if desired == nil {
		desired = MaterializedViewState{}
	}
	ids := stateIDs(current, desired)

	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createMaterializedViewStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, baseatlas.Statement{Comment: "drop materialized view " + materializedViewID(cur) + " (destructive)", SQL: "DROP MATERIALIZED VIEW " + qualifiedIdent(cur.Schema, cur.Name), Reverse: strings.Join(materializedViewSQL(cur), ";\n")})
		case hasCurrent && hasDesired:
			if normalizeSQL(cur.SQL) != normalizeSQL(des.SQL) {
				statements = append(statements, baseatlas.Statement{Comment: "drop materialized view " + materializedViewID(cur) + " for replacement (destructive)", SQL: "DROP MATERIALIZED VIEW " + qualifiedIdent(cur.Schema, cur.Name)})
				statements = append(statements, createMaterializedViewStatements(des)...)
				continue
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on materialized view " + materializedViewID(des), SQL: "COMMENT ON MATERIALIZED VIEW " + qualifiedIdent(des.Schema, des.Name) + " IS " + nullableLiteral(des.Comment), Reverse: "COMMENT ON MATERIALIZED VIEW " + qualifiedIdent(cur.Schema, cur.Name) + " IS " + nullableLiteral(cur.Comment)})
			}
		}
	}
	return statements
}

func createMaterializedViewStatements(view MaterializedView) []baseatlas.Statement {
	statements := []baseatlas.Statement{{Comment: "create materialized view " + materializedViewID(view), SQL: createMaterializedViewSQL(view), Reverse: "DROP MATERIALIZED VIEW " + qualifiedIdent(view.Schema, view.Name)}}
	if view.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on materialized view " + materializedViewID(view), SQL: "COMMENT ON MATERIALIZED VIEW " + qualifiedIdent(view.Schema, view.Name) + " IS " + literal(view.Comment), Reverse: "COMMENT ON MATERIALIZED VIEW " + qualifiedIdent(view.Schema, view.Name) + " IS NULL"})
	}
	return statements
}

func materializedViewSQL(view MaterializedView) []string {
	statements := []string{createMaterializedViewSQL(view)}
	if view.Comment != "" {
		statements = append(statements, "COMMENT ON MATERIALIZED VIEW "+qualifiedIdent(view.Schema, view.Name)+" IS "+literal(view.Comment))
	}
	return statements
}

func createMaterializedViewSQL(view MaterializedView) string {
	return "CREATE MATERIALIZED VIEW " + qualifiedIdent(view.Schema, view.Name) + " AS\n" + view.SQL
}

func materializedViewID(view MaterializedView) string { return viewID(view.Schema, view.Name) }
