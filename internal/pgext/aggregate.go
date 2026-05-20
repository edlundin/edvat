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

type Aggregate struct {
	Name      string
	Schema    string
	Args      []string
	StateFunc string
	StateType string
	InitCond  string
	Parallel  string
	Comment   string
}

type AggregateState map[string]Aggregate

func ParseAggregateFiles(paths []string) (AggregateState, error) {
	return parseStateFiles(paths, "aggregate", ParseAggregatesHCL)
}

func ParseAggregatesHCL(src []byte, filename string) (AggregateState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse aggregate hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse aggregate hcl: unexpected body type %T", file.Body)
	}

	state := AggregateState{}
	for _, block := range body.Blocks {
		if block.Type != "aggregate" || len(block.Labels) != 1 {
			continue
		}
		aggregate := Aggregate{Name: block.Labels[0]}
		attrs := block.Body.Attributes
		if attr, ok := attrs["schema"]; ok {
			schemaName, err := schemaExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode aggregate.%s.schema: %w", aggregate.Name, err)
			}
			aggregate.Schema = schemaName
		}
		if attr, ok := attrs["args"]; ok {
			args, err := typeListExpr(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode aggregate.%s.args: %w", aggregate.Name, err)
			}
			aggregate.Args = args
		}
		if attr, ok := attrs["state_func"]; ok {
			fn, err := functionExpr(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode aggregate.%s.state_func: %w", aggregate.Name, err)
			}
			aggregate.StateFunc = fn
		}
		if attr, ok := attrs["state_type"]; ok {
			aggregate.StateType = typeExpr(attr.Expr, src)
		}
		if attr, ok := attrs["init_cond"]; ok {
			initCond, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode aggregate.%s.init_cond: %w", aggregate.Name, err)
			}
			aggregate.InitCond = initCond
		}
		if attr, ok := attrs["parallel"]; ok {
			parallel, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode aggregate.%s.parallel: %w", aggregate.Name, err)
			}
			aggregate.Parallel = strings.ToUpper(parallel)
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode aggregate.%s.comment: %w", aggregate.Name, err)
			}
			aggregate.Comment = comment
		}
		if aggregate.StateFunc == "" {
			return nil, fmt.Errorf("aggregate.%s requires state_func", aggregate.Name)
		}
		if aggregate.StateType == "" {
			return nil, fmt.Errorf("aggregate.%s requires state_type", aggregate.Name)
		}
		state[aggregateID(aggregate)] = aggregate
	}
	return state, nil
}

func InspectAggregatesURL(ctx context.Context, url string) (AggregateState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectAggregates(ctx, db)
}

func InspectAggregates(ctx context.Context, db *sql.DB) (AggregateState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT n.nspname,
       p.proname,
       pg_get_function_identity_arguments(p.oid),
       COALESCE(fn.nspname || '.' || sf.proname, ''),
       format_type(a.aggtranstype, NULL),
       COALESCE(a.agginitval, ''),
       CASE p.proparallel WHEN 's' THEN 'SAFE' WHEN 'r' THEN 'RESTRICTED' WHEN 'u' THEN 'UNSAFE' ELSE '' END,
       COALESCE(d.description, '')
FROM pg_aggregate a
JOIN pg_proc p ON p.oid = a.aggfnoid
JOIN pg_namespace n ON n.oid = p.pronamespace
LEFT JOIN pg_proc sf ON sf.oid = a.aggtransfn
LEFT JOIN pg_namespace fn ON fn.oid = sf.pronamespace
LEFT JOIN pg_description d ON d.objoid = p.oid AND d.classoid = 'pg_proc'::regclass AND d.objsubid = 0
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY n.nspname, p.proname, pg_get_function_identity_arguments(p.oid)`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres aggregates: %w", err)
	}
	defer rows.Close()
	state := AggregateState{}
	for rows.Next() {
		var aggregate Aggregate
		var args string
		if err := rows.Scan(&aggregate.Schema, &aggregate.Name, &args, &aggregate.StateFunc, &aggregate.StateType, &aggregate.InitCond, &aggregate.Parallel, &aggregate.Comment); err != nil {
			return nil, fmt.Errorf("scan postgres aggregate: %w", err)
		}
		aggregate.Args = splitTypeList(args)
		state[aggregateID(aggregate)] = aggregate
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres aggregates: %w", err)
	}
	return state, nil
}

func DiffAggregates(current, desired AggregateState) []baseatlas.Statement {
	if current == nil {
		current = AggregateState{}
	}
	if desired == nil {
		desired = AggregateState{}
	}
	ids := stateIDs(current, desired)

	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createAggregateStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, dropAggregateStatement(cur))
		case hasCurrent && hasDesired:
			if !sameAggregateDefinition(cur, des) {
				statements = append(statements, dropAggregateStatement(cur))
				statements = append(statements, createAggregateStatements(des)...)
				continue
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on aggregate " + aggregateID(des), SQL: commentAggregateSQL(des), Reverse: commentAggregateSQL(cur)})
			}
		}
	}
	return statements
}

func createAggregateStatements(aggregate Aggregate) []baseatlas.Statement {
	statements := []baseatlas.Statement{{Comment: "create aggregate " + aggregateID(aggregate), SQL: createAggregateSQL(aggregate), Reverse: dropAggregateSQL(aggregate)}}
	if aggregate.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on aggregate " + aggregateID(aggregate), SQL: commentAggregateSQL(aggregate), Reverse: "COMMENT ON AGGREGATE " + qualifiedIdent(aggregate.Schema, aggregate.Name) + "(" + strings.Join(aggregate.Args, ", ") + ") IS NULL"})
	}
	return statements
}

func dropAggregateStatement(aggregate Aggregate) baseatlas.Statement {
	return baseatlas.Statement{Comment: "drop aggregate " + aggregateID(aggregate) + " (destructive)", SQL: dropAggregateSQL(aggregate), Reverse: strings.Join(aggregateSQL(aggregate), ";\n")}
}

func aggregateSQL(aggregate Aggregate) []string {
	statements := []string{createAggregateSQL(aggregate)}
	if aggregate.Comment != "" {
		statements = append(statements, commentAggregateSQL(aggregate))
	}
	return statements
}

func dropAggregateSQL(aggregate Aggregate) string {
	return "DROP AGGREGATE " + qualifiedIdent(aggregate.Schema, aggregate.Name) + "(" + strings.Join(aggregate.Args, ", ") + ")"
}

func createAggregateSQL(aggregate Aggregate) string {
	opts := []string{"SFUNC = " + aggregate.StateFunc, "STYPE = " + aggregate.StateType}
	if aggregate.InitCond != "" {
		opts = append(opts, "INITCOND = "+literal(aggregate.InitCond))
	}
	if aggregate.Parallel != "" {
		opts = append(opts, "PARALLEL = "+aggregate.Parallel)
	}
	return "CREATE AGGREGATE " + qualifiedIdent(aggregate.Schema, aggregate.Name) + "(" + strings.Join(aggregate.Args, ", ") + ") (" + strings.Join(opts, ", ") + ")"
}

func commentAggregateSQL(aggregate Aggregate) string {
	return "COMMENT ON AGGREGATE " + qualifiedIdent(aggregate.Schema, aggregate.Name) + "(" + strings.Join(aggregate.Args, ", ") + ") IS " + nullableLiteral(aggregate.Comment)
}

func sameAggregateDefinition(a, b Aggregate) bool {
	return strings.Join(a.Args, ",") == strings.Join(b.Args, ",") && normalizeSQL(a.StateFunc) == normalizeSQL(b.StateFunc) && normalizeSQL(a.StateType) == normalizeSQL(b.StateType) && a.InitCond == b.InitCond && a.Parallel == b.Parallel
}

func aggregateID(aggregate Aggregate) string {
	return viewID(aggregate.Schema, aggregate.Name) + "(" + strings.Join(aggregate.Args, ", ") + ")"
}

func typeListExpr(expr hclsyntax.Expression, src []byte) ([]string, error) {
	if tuple, ok := expr.(*hclsyntax.TupleConsExpr); ok {
		out := make([]string, 0, len(tuple.Exprs))
		for _, elem := range tuple.Exprs {
			out = append(out, typeExpr(elem, src))
		}
		return out, nil
	}
	return []string{typeExpr(expr, src)}, nil
}

func splitTypeList(types string) []string {
	types = strings.TrimSpace(types)
	if types == "" {
		return nil
	}
	parts := strings.Split(types, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, strings.TrimSpace(part))
	}
	return out
}
