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

type Function struct {
	Name       string
	Schema     string
	Language   string
	Args       []FunctionArg
	ReturnType string
	Body       string
	Comment    string
}

type FunctionArg struct {
	Name string
	Type string
}

type FunctionState map[string]Function

func ParseFunctionFiles(paths []string) (FunctionState, error) {
	return parseStateFiles(paths, "function", ParseFunctionsHCL)
}

func ParseFunctionsHCL(src []byte, filename string) (FunctionState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse function hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse function hcl: unexpected body type %T", file.Body)
	}

	state := FunctionState{}
	for _, block := range body.Blocks {
		if block.Type != "function" || len(block.Labels) != 1 {
			continue
		}
		fn := Function{Name: block.Labels[0]}
		attrs := block.Body.Attributes
		if attr, ok := attrs["schema"]; ok {
			schemaName, err := schemaExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode function.%s.schema: %w", fn.Name, err)
			}
			fn.Schema = schemaName
		}
		if attr, ok := attrs["lang"]; ok {
			lang, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode function.%s.lang: %w", fn.Name, err)
			}
			fn.Language = lang
		}
		if attr, ok := attrs["return"]; ok {
			fn.ReturnType = typeExpr(attr.Expr, src)
		}
		if attr, ok := attrs["as"]; ok {
			body, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode function.%s.as: %w", fn.Name, err)
			}
			fn.Body = strings.TrimSpace(body)
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode function.%s.comment: %w", fn.Name, err)
			}
			fn.Comment = comment
		}
		for _, nested := range block.Body.Blocks {
			if nested.Type != "arg" || len(nested.Labels) != 1 {
				continue
			}
			arg := FunctionArg{Name: nested.Labels[0]}
			if attr, ok := nested.Body.Attributes["type"]; ok {
				arg.Type = typeExpr(attr.Expr, src)
			}
			if arg.Type == "" {
				return nil, fmt.Errorf("function.%s arg.%s requires type", fn.Name, arg.Name)
			}
			fn.Args = append(fn.Args, arg)
		}
		if fn.Language == "" {
			return nil, fmt.Errorf("function.%s requires lang", fn.Name)
		}
		if fn.ReturnType == "" {
			return nil, fmt.Errorf("function.%s requires return", fn.Name)
		}
		if fn.Body == "" {
			return nil, fmt.Errorf("function.%s requires as", fn.Name)
		}
		state[functionID(fn)] = fn
	}
	return state, nil
}

func InspectFunctionsURL(ctx context.Context, url string) (FunctionState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectFunctions(ctx, db)
}

func InspectFunctions(ctx context.Context, db *sql.DB) (FunctionState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT n.nspname,
       p.proname,
       pg_get_function_identity_arguments(p.oid),
       pg_get_function_result(p.oid),
       l.lanname,
       p.prosrc,
       COALESCE(d.description, '')
FROM pg_proc p
JOIN pg_namespace n ON n.oid = p.pronamespace
JOIN pg_language l ON l.oid = p.prolang
LEFT JOIN pg_description d ON d.objoid = p.oid AND d.classoid = 'pg_proc'::regclass AND d.objsubid = 0
WHERE p.prokind = 'f'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND NOT EXISTS (
    SELECT 1
    FROM pg_depend dep
    WHERE dep.classid = 'pg_proc'::regclass
      AND dep.objid = p.oid
      AND dep.deptype = 'e'
  )
ORDER BY n.nspname, p.proname, pg_get_function_identity_arguments(p.oid)`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres functions: %w", err)
	}
	defer rows.Close()
	state := FunctionState{}
	for rows.Next() {
		var schemaName, name, args, returnType, lang, def, comment string
		if err := rows.Scan(&schemaName, &name, &args, &returnType, &lang, &def, &comment); err != nil {
			return nil, fmt.Errorf("scan postgres function: %w", err)
		}
		fn := Function{Name: name, Schema: schemaName, Language: lang, Args: parseIdentityArgs(args), ReturnType: returnType, Body: strings.TrimSpace(def), Comment: comment}
		state[functionID(fn)] = fn
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres functions: %w", err)
	}
	return state, nil
}

func DiffFunctions(current, desired FunctionState) []baseatlas.Statement {
	if current == nil {
		current = FunctionState{}
	}
	if desired == nil {
		desired = FunctionState{}
	}
	ids := stateIDs(current, desired)

	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createFunctionStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, baseatlas.Statement{
				Comment: "drop function " + functionLabel(cur) + " (destructive)",
				SQL:     "DROP FUNCTION " + qualifiedIdent(cur.Schema, cur.Name) + "(" + argTypeList(cur.Args) + ")",
			})
		case hasCurrent && hasDesired:
			if !sameFunction(cur, des) {
				statements = append(statements, createFunctionStatements(des)[0])
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on function " + functionLabel(des), SQL: commentFunctionSQL(des)})
			}
		}
	}
	return statements
}

func createFunctionStatements(fn Function) []baseatlas.Statement {
	stmt := baseatlas.Statement{Comment: "create or replace function " + functionLabel(fn), SQL: createFunctionSQL(fn), Reverse: "DROP FUNCTION " + qualifiedIdent(fn.Schema, fn.Name) + "(" + argTypeList(fn.Args) + ")"}
	statements := []baseatlas.Statement{stmt}
	if fn.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on function " + functionLabel(fn), SQL: commentFunctionSQL(fn), Reverse: "COMMENT ON FUNCTION " + qualifiedIdent(fn.Schema, fn.Name) + "(" + argTypeList(fn.Args) + ") IS NULL"})
	}
	return statements
}

func createFunctionSQL(fn Function) string {
	return "CREATE OR REPLACE FUNCTION " + qualifiedIdent(fn.Schema, fn.Name) + "(" + argDeclList(fn.Args) + ") RETURNS " + fn.ReturnType + " LANGUAGE " + fn.Language + " AS $$\n" + fn.Body + "\n$$"
}

func commentFunctionSQL(fn Function) string {
	return "COMMENT ON FUNCTION " + qualifiedIdent(fn.Schema, fn.Name) + "(" + argTypeList(fn.Args) + ") IS " + nullableLiteral(fn.Comment)
}

func sameFunction(a, b Function) bool {
	return strings.EqualFold(a.Language, b.Language) && normalizeRoutineType(a.ReturnType) == normalizeRoutineType(b.ReturnType) && normalizeRoutineBody(a.Body) == normalizeRoutineBody(b.Body)
}

func normalizeRoutineType(typeName string) string {
	normalized := normalizeSQL(typeName)
	switch normalized {
	case "character varying":
		return "varchar"
	case "integer":
		return "int"
	default:
		return normalized
	}
}

func normalizeRoutineBody(body string) string {
	body = strings.TrimSpace(body)
	start := strings.Index(body, "$$")
	if start == -1 {
		return normalizeSQL(body)
	}
	start += len("$$")
	end := strings.LastIndex(body[start:], "$$")
	if end == -1 {
		return normalizeSQL(body)
	}
	return normalizeSQL(body[start : start+end])
}

func functionID(fn Function) string {
	return viewID(fn.Schema, fn.Name) + "(" + argTypeList(fn.Args) + ")"
}

func functionLabel(fn Function) string { return functionID(fn) }

func argDeclList(args []FunctionArg) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if arg.Name == "" {
			parts = append(parts, arg.Type)
			continue
		}
		parts = append(parts, quoteIdent(arg.Name)+" "+arg.Type)
	}
	return strings.Join(parts, ", ")
}

func argTypeList(args []FunctionArg) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, arg.Type)
	}
	return strings.Join(parts, ", ")
}

func parseIdentityArgs(args string) []FunctionArg {
	args = strings.TrimSpace(args)
	if args == "" {
		return nil
	}
	parts := strings.Split(args, ",")
	out := make([]FunctionArg, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		fields := strings.Fields(part)
		if len(fields) > 2 && isArgMode(fields[0]) {
			out = append(out, FunctionArg{Name: fields[1], Type: strings.Join(fields[2:], " ")})
		} else if len(fields) > 1 {
			out = append(out, FunctionArg{Name: fields[0], Type: strings.Join(fields[1:], " ")})
		} else {
			out = append(out, FunctionArg{Type: part})
		}
	}
	return out
}

func isArgMode(value string) bool {
	switch strings.ToUpper(value) {
	case "IN", "OUT", "INOUT", "VARIADIC":
		return true
	default:
		return false
	}
}

func symbolOrString(expr hclsyntax.Expression, src []byte) (string, error) {
	if traversal, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		parts := traversalParts(traversal.Traversal)
		if len(parts) == 1 {
			return parts[0], nil
		}
	}
	return stringExpr(expr)
}

func typeExpr(expr hclsyntax.Expression, src []byte) string {
	if traversal, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		return strings.Join(traversalParts(traversal.Traversal), ".")
	}
	r := expr.Range()
	if r.Start.Byte >= 0 && r.End.Byte <= len(src) && r.Start.Byte < r.End.Byte {
		return strings.TrimSpace(string(src[r.Start.Byte:r.End.Byte]))
	}
	return ""
}
