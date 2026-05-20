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

type ForeignTable struct {
	Name    string
	Schema  string
	Server  string
	Columns []ForeignColumn
	Options map[string]string
	Comment string
}

type ForeignColumn struct {
	Name string
	Type string
}

type ForeignTableState map[string]ForeignTable

func ParseForeignTableFiles(paths []string) (ForeignTableState, error) {
	return parseStateFiles(paths, "foreign table", ParseForeignTablesHCL)
}

func ParseForeignTablesHCL(src []byte, filename string) (ForeignTableState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse foreign table hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse foreign table hcl: unexpected body type %T", file.Body)
	}
	state := ForeignTableState{}
	for _, block := range body.Blocks {
		if block.Type != "foreign_table" || len(block.Labels) != 1 {
			continue
		}
		table := ForeignTable{Name: block.Labels[0], Options: map[string]string{}}
		attrs := block.Body.Attributes
		if attr, ok := attrs["schema"]; ok {
			schemaName, err := schemaExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode foreign_table.%s.schema: %w", table.Name, err)
			}
			table.Schema = schemaName
		}
		if attr, ok := attrs["server"]; ok {
			server, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode foreign_table.%s.server: %w", table.Name, err)
			}
			table.Server = server
		}
		if attr, ok := attrs["options"]; ok {
			options, err := stringMapExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode foreign_table.%s.options: %w", table.Name, err)
			}
			table.Options = options
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode foreign_table.%s.comment: %w", table.Name, err)
			}
			table.Comment = comment
		}
		for _, nested := range block.Body.Blocks {
			if nested.Type != "column" || len(nested.Labels) != 1 {
				continue
			}
			column := ForeignColumn{Name: nested.Labels[0]}
			if attr, ok := nested.Body.Attributes["type"]; ok {
				column.Type = typeExpr(attr.Expr, src)
			}
			if column.Type == "" {
				return nil, fmt.Errorf("foreign_table.%s column.%s requires type", table.Name, column.Name)
			}
			table.Columns = append(table.Columns, column)
		}
		if table.Server == "" {
			return nil, fmt.Errorf("foreign_table.%s requires server", table.Name)
		}
		if len(table.Columns) == 0 {
			return nil, fmt.Errorf("foreign_table.%s requires at least one column", table.Name)
		}
		state[foreignTableID(table)] = table
	}
	return state, nil
}

func InspectForeignTablesURL(ctx context.Context, url string) (ForeignTableState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectForeignTables(ctx, db)
}

func InspectForeignTables(ctx context.Context, db *sql.DB) (ForeignTableState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT n.nspname, c.relname, s.srvname, a.attname, format_type(a.atttypid, a.atttypmod), COALESCE(array_to_string(ft.ftoptions, ','), ''), COALESCE(d.description, '')
FROM pg_foreign_table ft
JOIN pg_class c ON c.oid = ft.ftrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_foreign_server s ON s.oid = ft.ftserver
JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
LEFT JOIN pg_description d ON d.objoid = c.oid AND d.classoid = 'pg_class'::regclass AND d.objsubid = 0
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY n.nspname, c.relname, a.attnum`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres foreign tables: %w", err)
	}
	defer rows.Close()
	state := ForeignTableState{}
	for rows.Next() {
		var schemaName, name, server, columnName, columnType, options, comment string
		if err := rows.Scan(&schemaName, &name, &server, &columnName, &columnType, &options, &comment); err != nil {
			return nil, fmt.Errorf("scan postgres foreign table: %w", err)
		}
		id := viewID(schemaName, name)
		table := state[id]
		table.Name = name
		table.Schema = schemaName
		table.Server = server
		table.Options = parseOptionCSV(options)
		table.Comment = comment
		table.Columns = append(table.Columns, ForeignColumn{Name: columnName, Type: columnType})
		state[id] = table
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres foreign tables: %w", err)
	}
	return state, nil
}

func DiffForeignTables(current, desired ForeignTableState) []baseatlas.Statement {
	if current == nil {
		current = ForeignTableState{}
	}
	if desired == nil {
		desired = ForeignTableState{}
	}
	ids := stateIDs(current, desired)
	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createForeignTableStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, baseatlas.Statement{Comment: "drop foreign table " + foreignTableID(cur) + " (destructive)", SQL: dropForeignTableSQL(cur), Reverse: strings.Join(foreignTableSQL(cur), ";\n")})
		case hasCurrent && hasDesired:
			if !sameForeignTableDefinition(cur, des) {
				statements = append(statements, baseatlas.Statement{Comment: "drop foreign table " + foreignTableID(cur) + " for replacement (destructive)", SQL: "DROP FOREIGN TABLE " + qualifiedIdent(cur.Schema, cur.Name)})
				statements = append(statements, createForeignTableStatements(des)...)
				continue
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on foreign table " + foreignTableID(des), SQL: "COMMENT ON FOREIGN TABLE " + qualifiedIdent(des.Schema, des.Name) + " IS " + nullableLiteral(des.Comment), Reverse: "COMMENT ON FOREIGN TABLE " + qualifiedIdent(cur.Schema, cur.Name) + " IS " + nullableLiteral(cur.Comment)})
			}
		}
	}
	return statements
}

func createForeignTableStatements(table ForeignTable) []baseatlas.Statement {
	statements := []baseatlas.Statement{{Comment: "create foreign table " + foreignTableID(table), SQL: createForeignTableSQL(table), Reverse: dropForeignTableSQL(table)}}
	if table.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on foreign table " + foreignTableID(table), SQL: "COMMENT ON FOREIGN TABLE " + qualifiedIdent(table.Schema, table.Name) + " IS " + literal(table.Comment), Reverse: "COMMENT ON FOREIGN TABLE " + qualifiedIdent(table.Schema, table.Name) + " IS NULL"})
	}
	return statements
}

func foreignTableSQL(table ForeignTable) []string {
	statements := []string{createForeignTableSQL(table)}
	if table.Comment != "" {
		statements = append(statements, "COMMENT ON FOREIGN TABLE "+qualifiedIdent(table.Schema, table.Name)+" IS "+literal(table.Comment))
	}
	return statements
}

func dropForeignTableSQL(table ForeignTable) string {
	return "DROP FOREIGN TABLE " + qualifiedIdent(table.Schema, table.Name)
}

func createForeignTableSQL(table ForeignTable) string {
	columns := make([]string, 0, len(table.Columns))
	for _, column := range table.Columns {
		columns = append(columns, quoteIdent(column.Name)+" "+column.Type)
	}
	var b strings.Builder
	b.WriteString("CREATE FOREIGN TABLE ")
	b.WriteString(qualifiedIdent(table.Schema, table.Name))
	b.WriteString(" (")
	b.WriteString(strings.Join(columns, ", "))
	b.WriteString(") SERVER ")
	b.WriteString(quoteIdent(table.Server))
	if len(table.Options) > 0 {
		b.WriteString(" OPTIONS (")
		b.WriteString(optionsSQL(table.Options))
		b.WriteString(")")
	}
	return b.String()
}

func sameForeignTableDefinition(a, b ForeignTable) bool {
	if a.Server != b.Server || !stringMapEqual(a.Options, b.Options) || len(a.Columns) != len(b.Columns) {
		return false
	}
	for i := range a.Columns {
		if a.Columns[i].Name != b.Columns[i].Name || normalizeSQL(a.Columns[i].Type) != normalizeSQL(b.Columns[i].Type) {
			return false
		}
	}
	return true
}

func foreignTableID(table ForeignTable) string { return viewID(table.Schema, table.Name) }
