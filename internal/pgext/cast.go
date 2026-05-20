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

type Cast struct {
	Source     string
	Target     string
	Function   string
	Method     string
	Assignment bool
	Implicit   bool
	Comment    string
}

type CastState map[string]Cast

func ParseCastFiles(paths []string) (CastState, error) {
	return parseStateFiles(paths, "cast", ParseCastsHCL)
}

func ParseCastsHCL(src []byte, filename string) (CastState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse cast hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse cast hcl: unexpected body type %T", file.Body)
	}

	state := CastState{}
	for _, block := range body.Blocks {
		if block.Type != "cast" {
			continue
		}
		cast := Cast{}
		attrs := block.Body.Attributes
		if attr, ok := attrs["source"]; ok {
			cast.Source = typeExpr(attr.Expr, src)
		}
		if attr, ok := attrs["target"]; ok {
			cast.Target = typeExpr(attr.Expr, src)
		}
		if attr, ok := attrs["with"]; ok {
			fn, err := functionExpr(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode cast.with: %w", err)
			}
			cast.Function = fn
		}
		if attr, ok := attrs["method"]; ok {
			method, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode cast.method: %w", err)
			}
			cast.Method = strings.ToUpper(method)
		}
		if attr, ok := attrs["assignment"]; ok {
			assignment, err := boolExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode cast.assignment: %w", err)
			}
			cast.Assignment = assignment
		}
		if attr, ok := attrs["implicit"]; ok {
			implicit, err := boolExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode cast.implicit: %w", err)
			}
			cast.Implicit = implicit
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode cast.comment: %w", err)
			}
			cast.Comment = comment
		}
		if cast.Source == "" || cast.Target == "" {
			return nil, fmt.Errorf("cast requires source and target")
		}
		if cast.Function == "" && cast.Method == "" {
			return nil, fmt.Errorf("cast %s requires with or method", castID(cast))
		}
		if cast.Assignment && cast.Implicit {
			return nil, fmt.Errorf("cast %s cannot be both assignment and implicit", castID(cast))
		}
		state[castID(cast)] = cast
	}
	return state, nil
}

func InspectCastsURL(ctx context.Context, url string) (CastState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectCasts(ctx, db)
}

func InspectCasts(ctx context.Context, db *sql.DB) (CastState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT format_type(c.castsource, NULL),
       format_type(c.casttarget, NULL),
       COALESCE(pn.nspname || '.' || p.proname, ''),
       CASE c.castmethod WHEN 'f' THEN 'FUNCTION' WHEN 'i' THEN 'INOUT' WHEN 'b' THEN 'BINARY' ELSE c.castmethod::text END,
       c.castcontext,
       COALESCE(d.description, '')
FROM pg_cast c
JOIN pg_type source_type ON source_type.oid = c.castsource
JOIN pg_namespace source_ns ON source_ns.oid = source_type.typnamespace
JOIN pg_type target_type ON target_type.oid = c.casttarget
JOIN pg_namespace target_ns ON target_ns.oid = target_type.typnamespace
LEFT JOIN pg_proc p ON p.oid = c.castfunc
LEFT JOIN pg_namespace pn ON pn.oid = p.pronamespace
LEFT JOIN pg_description d ON d.objoid = c.oid AND d.classoid = 'pg_cast'::regclass AND d.objsubid = 0
WHERE source_ns.nspname NOT IN ('pg_catalog', 'information_schema')
   OR target_ns.nspname NOT IN ('pg_catalog', 'information_schema')
   OR pn.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY 1, 2`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres casts: %w", err)
	}
	defer rows.Close()
	state := CastState{}
	for rows.Next() {
		var cast Cast
		var context string
		if err := rows.Scan(&cast.Source, &cast.Target, &cast.Function, &cast.Method, &context, &cast.Comment); err != nil {
			return nil, fmt.Errorf("scan postgres cast: %w", err)
		}
		cast.Assignment = context == "a"
		cast.Implicit = context == "i"
		state[castID(cast)] = cast
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres casts: %w", err)
	}
	return state, nil
}

func DiffCasts(current, desired CastState) []baseatlas.Statement {
	if current == nil {
		current = CastState{}
	}
	if desired == nil {
		desired = CastState{}
	}
	ids := stateIDs(current, desired)

	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createCastStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, dropCastStatement(cur))
		case hasCurrent && hasDesired:
			if !sameCastDefinition(cur, des) {
				statements = append(statements, dropCastStatement(cur))
				statements = append(statements, createCastStatements(des)...)
				continue
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on cast " + castID(des), SQL: commentCastSQL(des), Reverse: commentCastSQL(cur)})
			}
		}
	}
	return statements
}

func createCastStatements(cast Cast) []baseatlas.Statement {
	statements := []baseatlas.Statement{{Comment: "create cast " + castID(cast), SQL: createCastSQL(cast), Reverse: dropCastSQL(cast)}}
	if cast.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on cast " + castID(cast), SQL: commentCastSQL(cast), Reverse: "COMMENT ON CAST (" + cast.Source + " AS " + cast.Target + ") IS NULL"})
	}
	return statements
}

func dropCastStatement(cast Cast) baseatlas.Statement {
	return baseatlas.Statement{Comment: "drop cast " + castID(cast) + " (destructive)", SQL: dropCastSQL(cast), Reverse: strings.Join(castSQL(cast), ";\n")}
}

func castSQL(cast Cast) []string {
	statements := []string{createCastSQL(cast)}
	if cast.Comment != "" {
		statements = append(statements, commentCastSQL(cast))
	}
	return statements
}

func dropCastSQL(cast Cast) string {
	return "DROP CAST (" + cast.Source + " AS " + cast.Target + ")"
}

func createCastSQL(cast Cast) string {
	var b strings.Builder
	b.WriteString("CREATE CAST (")
	b.WriteString(cast.Source)
	b.WriteString(" AS ")
	b.WriteString(cast.Target)
	b.WriteString(") WITH ")
	if cast.Function != "" {
		b.WriteString("FUNCTION ")
		b.WriteString(cast.Function)
	} else {
		b.WriteString(cast.Method)
	}
	if cast.Assignment {
		b.WriteString(" AS ASSIGNMENT")
	}
	if cast.Implicit {
		b.WriteString(" AS IMPLICIT")
	}
	return b.String()
}

func commentCastSQL(cast Cast) string {
	return "COMMENT ON CAST (" + cast.Source + " AS " + cast.Target + ") IS " + nullableLiteral(cast.Comment)
}

func sameCastDefinition(a, b Cast) bool {
	return normalizeSQL(a.Function) == normalizeSQL(b.Function) && a.Method == b.Method && a.Assignment == b.Assignment && a.Implicit == b.Implicit
}

func castID(cast Cast) string { return cast.Source + " AS " + cast.Target }
