package pgext

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/edlundin/edvat/internal/baseatlas"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

type View struct {
	Name    string
	Schema  string
	SQL     string
	Comment string
}

type ViewState map[string]View

func ParseViewFiles(paths []string) (ViewState, error) {
	return parseStateFiles(paths, "view", ParseViewsHCL)
}

func ParseViewsHCL(src []byte, filename string) (ViewState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse view hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse view hcl: unexpected body type %T", file.Body)
	}

	state := ViewState{}
	for _, block := range body.Blocks {
		if block.Type != "view" || len(block.Labels) != 1 {
			continue
		}
		view := View{Name: block.Labels[0]}
		attrs := block.Body.Attributes
		if attr, ok := attrs["schema"]; ok {
			schemaName, err := schemaExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode view.%s.schema: %w", view.Name, err)
			}
			view.Schema = schemaName
		}
		if attr, ok := attrs["as"]; ok {
			sql, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode view.%s.as: %w", view.Name, err)
			}
			view.SQL = strings.TrimSpace(sql)
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode view.%s.comment: %w", view.Name, err)
			}
			view.Comment = comment
		}
		if view.SQL == "" {
			return nil, fmt.Errorf("view.%s requires as", view.Name)
		}
		state[viewID(view.Schema, view.Name)] = view
	}
	return state, nil
}

func InspectViewsURL(ctx context.Context, url string) (ViewState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectViews(ctx, db)
}

func InspectViews(ctx context.Context, db *sql.DB) (ViewState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT n.nspname, c.relname, pg_get_viewdef(c.oid, true), COALESCE(d.description, '')
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_description d ON d.objoid = c.oid AND d.classoid = 'pg_class'::regclass AND d.objsubid = 0
WHERE c.relkind = 'v'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY n.nspname, c.relname`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres views: %w", err)
	}
	defer rows.Close()
	state := ViewState{}
	for rows.Next() {
		var view View
		if err := rows.Scan(&view.Schema, &view.Name, &view.SQL, &view.Comment); err != nil {
			return nil, fmt.Errorf("scan postgres view: %w", err)
		}
		view.SQL = strings.TrimSpace(view.SQL)
		state[viewID(view.Schema, view.Name)] = view
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres views: %w", err)
	}
	return state, nil
}

func DiffViews(current, desired ViewState) []baseatlas.Statement {
	if current == nil {
		current = ViewState{}
	}
	if desired == nil {
		desired = ViewState{}
	}
	ids := stateIDs(current, desired)

	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createViewStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, baseatlas.Statement{
				Comment: "drop view " + qualifiedViewName(cur) + " (destructive)",
				SQL:     "DROP VIEW " + qualifiedIdent(cur.Schema, cur.Name),
			})
		case hasCurrent && hasDesired:
			if normalizeViewSQL(cur.SQL) != normalizeViewSQL(des.SQL) {
				statements = append(statements, baseatlas.Statement{
					Comment: "replace view " + qualifiedViewName(des),
					SQL:     "CREATE OR REPLACE VIEW " + qualifiedIdent(des.Schema, des.Name) + " AS\n" + des.SQL,
				})
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{
					Comment: "set comment on view " + qualifiedViewName(des),
					SQL:     "COMMENT ON VIEW " + qualifiedIdent(des.Schema, des.Name) + " IS " + nullableLiteral(des.Comment),
				})
			}
		}
	}
	return statements
}

func createViewStatements(view View) []baseatlas.Statement {
	statements := []baseatlas.Statement{{
		Comment: "create view " + qualifiedViewName(view),
		SQL:     "CREATE VIEW " + qualifiedIdent(view.Schema, view.Name) + " AS\n" + view.SQL,
		Reverse: "DROP VIEW " + qualifiedIdent(view.Schema, view.Name),
	}}
	if view.Comment != "" {
		statements = append(statements, baseatlas.Statement{
			Comment: "set comment on view " + qualifiedViewName(view),
			SQL:     "COMMENT ON VIEW " + qualifiedIdent(view.Schema, view.Name) + " IS " + literal(view.Comment),
			Reverse: "COMMENT ON VIEW " + qualifiedIdent(view.Schema, view.Name) + " IS NULL",
		})
	}
	return statements
}

func viewID(schemaName, name string) string {
	if schemaName == "" {
		return name
	}
	return schemaName + "." + name
}

func qualifiedViewName(view View) string {
	return viewID(view.Schema, view.Name)
}

func qualifiedIdent(schemaName, name string) string {
	if schemaName == "" {
		return quoteIdent(name)
	}
	return quoteIdent(schemaName) + "." + quoteIdent(name)
}

func normalizeSQL(sql string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(sql)), " ")
	normalized = strings.TrimSuffix(normalized, ";")
	return strings.TrimSpace(normalized)
}

func normalizeViewSQL(sql string) string {
	normalized := normalizeSQL(sql)
	upper := strings.ToUpper(normalized)
	fromIndex := strings.Index(upper, " FROM ")
	if !strings.HasPrefix(upper, "SELECT ") || fromIndex == -1 {
		return normalized
	}
	selectList := normalized[len("SELECT "):fromIndex]
	selectList = relationQualifierRe.ReplaceAllString(selectList, "")
	return "SELECT " + selectList + normalized[fromIndex:]
}

var relationQualifierRe = regexp.MustCompile(`\b[a-zA-Z_][a-zA-Z0-9_]*\.`)
