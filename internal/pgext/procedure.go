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

type Procedure struct {
	Name     string
	Schema   string
	Language string
	Args     []FunctionArg
	Body     string
	Comment  string
}

type ProcedureState map[string]Procedure

func ParseProcedureFiles(paths []string) (ProcedureState, error) {
	return parseStateFiles(paths, "procedure", ParseProceduresHCL)
}

func ParseProceduresHCL(src []byte, filename string) (ProcedureState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse procedure hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse procedure hcl: unexpected body type %T", file.Body)
	}

	state := ProcedureState{}
	for _, block := range body.Blocks {
		if block.Type != "procedure" || len(block.Labels) != 1 {
			continue
		}
		procedure := Procedure{Name: block.Labels[0]}
		attrs := block.Body.Attributes
		if attr, ok := attrs["schema"]; ok {
			schemaName, err := schemaExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode procedure.%s.schema: %w", procedure.Name, err)
			}
			procedure.Schema = schemaName
		}
		if attr, ok := attrs["lang"]; ok {
			lang, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode procedure.%s.lang: %w", procedure.Name, err)
			}
			procedure.Language = lang
		}
		if attr, ok := attrs["as"]; ok {
			body, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode procedure.%s.as: %w", procedure.Name, err)
			}
			procedure.Body = strings.TrimSpace(body)
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode procedure.%s.comment: %w", procedure.Name, err)
			}
			procedure.Comment = comment
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
				return nil, fmt.Errorf("procedure.%s arg.%s requires type", procedure.Name, arg.Name)
			}
			procedure.Args = append(procedure.Args, arg)
		}
		if procedure.Language == "" {
			return nil, fmt.Errorf("procedure.%s requires lang", procedure.Name)
		}
		if procedure.Body == "" {
			return nil, fmt.Errorf("procedure.%s requires as", procedure.Name)
		}
		state[procedureID(procedure)] = procedure
	}
	return state, nil
}

func InspectProceduresURL(ctx context.Context, url string) (ProcedureState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectProcedures(ctx, db)
}

func InspectProcedures(ctx context.Context, db *sql.DB) (ProcedureState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT n.nspname,
       p.proname,
       pg_get_function_identity_arguments(p.oid),
       l.lanname,
       p.prosrc,
       COALESCE(d.description, '')
FROM pg_proc p
JOIN pg_namespace n ON n.oid = p.pronamespace
JOIN pg_language l ON l.oid = p.prolang
LEFT JOIN pg_description d ON d.objoid = p.oid AND d.classoid = 'pg_proc'::regclass AND d.objsubid = 0
WHERE p.prokind = 'p'
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
		return nil, fmt.Errorf("inspect postgres procedures: %w", err)
	}
	defer rows.Close()
	state := ProcedureState{}
	for rows.Next() {
		var schemaName, name, args, lang, def, comment string
		if err := rows.Scan(&schemaName, &name, &args, &lang, &def, &comment); err != nil {
			return nil, fmt.Errorf("scan postgres procedure: %w", err)
		}
		procedure := Procedure{Name: name, Schema: schemaName, Language: lang, Args: parseIdentityArgs(args), Body: strings.TrimSpace(def), Comment: comment}
		state[procedureID(procedure)] = procedure
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres procedures: %w", err)
	}
	return state, nil
}

func DiffProcedures(current, desired ProcedureState) []baseatlas.Statement {
	if current == nil {
		current = ProcedureState{}
	}
	if desired == nil {
		desired = ProcedureState{}
	}
	ids := stateIDs(current, desired)

	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createProcedureStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, baseatlas.Statement{Comment: "drop procedure " + procedureID(cur) + " (destructive)", SQL: dropProcedureSQL(cur), Reverse: strings.Join(procedureSQL(cur), ";\n")})
		case hasCurrent && hasDesired:
			if !sameProcedure(cur, des) {
				stmt := createProcedureStatements(des)[0]
				stmt.Reverse = createProcedureSQL(cur)
				statements = append(statements, stmt)
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on procedure " + procedureID(des), SQL: commentProcedureSQL(des), Reverse: commentProcedureSQL(cur)})
			}
		}
	}
	return statements
}

func createProcedureStatements(procedure Procedure) []baseatlas.Statement {
	statements := []baseatlas.Statement{{Comment: "create or replace procedure " + procedureID(procedure), SQL: createProcedureSQL(procedure), Reverse: dropProcedureSQL(procedure)}}
	if procedure.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on procedure " + procedureID(procedure), SQL: commentProcedureSQL(procedure), Reverse: "COMMENT ON PROCEDURE " + qualifiedIdent(procedure.Schema, procedure.Name) + "(" + argTypeList(procedure.Args) + ") IS NULL"})
	}
	return statements
}

func procedureSQL(procedure Procedure) []string {
	statements := []string{createProcedureSQL(procedure)}
	if procedure.Comment != "" {
		statements = append(statements, commentProcedureSQL(procedure))
	}
	return statements
}

func dropProcedureSQL(procedure Procedure) string {
	return "DROP PROCEDURE " + qualifiedIdent(procedure.Schema, procedure.Name) + "(" + argTypeList(procedure.Args) + ")"
}

func createProcedureSQL(procedure Procedure) string {
	return "CREATE OR REPLACE PROCEDURE " + qualifiedIdent(procedure.Schema, procedure.Name) + "(" + argDeclList(procedure.Args) + ") LANGUAGE " + procedure.Language + " AS $$\n" + procedure.Body + "\n$$"
}

func commentProcedureSQL(procedure Procedure) string {
	return "COMMENT ON PROCEDURE " + qualifiedIdent(procedure.Schema, procedure.Name) + "(" + argTypeList(procedure.Args) + ") IS " + nullableLiteral(procedure.Comment)
}

func sameProcedure(a, b Procedure) bool {
	return strings.EqualFold(a.Language, b.Language) && normalizeRoutineBody(a.Body) == normalizeRoutineBody(b.Body)
}

func procedureID(procedure Procedure) string {
	return viewID(procedure.Schema, procedure.Name) + "(" + argTypeList(procedure.Args) + ")"
}
