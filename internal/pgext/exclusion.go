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

type ExclusionConstraint struct {
	Name    string
	Schema  string
	Table   string
	Type    string
	Columns []ExclusionColumn
	Include []string
	Where   string
}

type ExclusionColumn struct {
	Column     string
	Expression string
	Op         string
}

type ExclusionState map[string]ExclusionConstraint

func ParseExclusionFiles(paths []string) (ExclusionState, error) {
	return parseStateFiles(paths, "exclusion", ParseExclusionsHCL)
}

func ParseExclusionsHCL(src []byte, filename string) (ExclusionState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse exclusion hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse exclusion hcl: unexpected body type %T", file.Body)
	}
	state := ExclusionState{}
	for _, tableBlock := range body.Blocks {
		if tableBlock.Type != "table" || len(tableBlock.Labels) != 1 {
			continue
		}
		schemaName := "public"
		if attr, ok := tableBlock.Body.Attributes["schema"]; ok {
			decoded, err := schemaExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode table.%s.schema: %w", tableBlock.Labels[0], err)
			}
			schemaName = decoded
		}
		for _, block := range tableBlock.Body.Blocks {
			if block.Type != "exclude" || len(block.Labels) != 1 {
				continue
			}
			exclusion := ExclusionConstraint{Name: block.Labels[0], Schema: schemaName, Table: tableBlock.Labels[0], Type: "gist"}
			if attr, ok := block.Body.Attributes["type"]; ok {
				indexType, err := symbolOrString(attr.Expr, src)
				if err != nil {
					return nil, fmt.Errorf("decode table.%s.exclude.%s.type: %w", tableBlock.Labels[0], exclusion.Name, err)
				}
				exclusion.Type = strings.ToLower(indexType)
			}
			if attr, ok := block.Body.Attributes["include"]; ok {
				include, err := exclusionColumnListExpr(attr.Expr, src)
				if err != nil {
					return nil, fmt.Errorf("decode table.%s.exclude.%s.include: %w", tableBlock.Labels[0], exclusion.Name, err)
				}
				exclusion.Include = include
			}
			if attr, ok := block.Body.Attributes["where"]; ok {
				where, err := stringExpr(attr.Expr)
				if err != nil {
					return nil, fmt.Errorf("decode table.%s.exclude.%s.where: %w", tableBlock.Labels[0], exclusion.Name, err)
				}
				exclusion.Where = strings.TrimSpace(where)
			}
			for _, on := range block.Body.Blocks {
				if on.Type != "on" {
					continue
				}
				column := ExclusionColumn{}
				if attr, ok := on.Body.Attributes["column"]; ok {
					name, err := exclusionColumnExpr(attr.Expr, src)
					if err != nil {
						return nil, fmt.Errorf("decode table.%s.exclude.%s.on.column: %w", tableBlock.Labels[0], exclusion.Name, err)
					}
					column.Column = name
				}
				if attr, ok := on.Body.Attributes["expr"]; ok {
					expr, err := stringExpr(attr.Expr)
					if err != nil {
						return nil, fmt.Errorf("decode table.%s.exclude.%s.on.expr: %w", tableBlock.Labels[0], exclusion.Name, err)
					}
					column.Expression = strings.TrimSpace(expr)
				}
				if attr, ok := on.Body.Attributes["op"]; ok {
					op, err := stringExpr(attr.Expr)
					if err != nil {
						return nil, fmt.Errorf("decode table.%s.exclude.%s.on.op: %w", tableBlock.Labels[0], exclusion.Name, err)
					}
					column.Op = op
				}
				if (column.Column == "" && column.Expression == "") || column.Op == "" {
					return nil, fmt.Errorf("table.%s.exclude.%s on requires column or expr and op", tableBlock.Labels[0], exclusion.Name)
				}
				if column.Column != "" && column.Expression != "" {
					return nil, fmt.Errorf("table.%s.exclude.%s on cannot use both column and expr", tableBlock.Labels[0], exclusion.Name)
				}
				exclusion.Columns = append(exclusion.Columns, column)
			}
			if len(exclusion.Columns) == 0 {
				return nil, fmt.Errorf("table.%s.exclude.%s requires on blocks", tableBlock.Labels[0], exclusion.Name)
			}
			state[exclusionID(exclusion)] = exclusion
		}
	}
	return state, nil
}

func InspectExclusionsURL(ctx context.Context, url string) (ExclusionState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectExclusions(ctx, db)
}

func InspectExclusions(ctx context.Context, db *sql.DB) (ExclusionState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT n.nspname, t.relname, c.conname, pg_get_constraintdef(c.oid, true)
FROM pg_constraint c
JOIN pg_class t ON t.oid = c.conrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
WHERE c.contype = 'x'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY n.nspname, t.relname, c.conname`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres exclusion constraints: %w", err)
	}
	defer rows.Close()
	state := ExclusionState{}
	for rows.Next() {
		var schemaName, tableName, name, def string
		if err := rows.Scan(&schemaName, &tableName, &name, &def); err != nil {
			return nil, fmt.Errorf("scan postgres exclusion constraint: %w", err)
		}
		exclusion := ExclusionConstraint{Name: name, Schema: schemaName, Table: tableName, Type: exclusionTypeFromDef(def), Columns: exclusionColumnsFromDef(def), Include: exclusionIncludeFromDef(def), Where: exclusionWhereFromDef(def)}
		state[exclusionID(exclusion)] = exclusion
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres exclusion constraints: %w", err)
	}
	return state, nil
}

func DiffExclusions(current, desired ExclusionState) []baseatlas.Statement {
	if current == nil {
		current = ExclusionState{}
	}
	if desired == nil {
		desired = ExclusionState{}
	}
	ids := stateIDs(current, desired)
	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createExclusionStatement(des))
		case hasCurrent && !hasDesired:
			statements = append(statements, dropExclusionStatement(cur, "drop exclusion constraint "+exclusionID(cur)+" (destructive)"))
		case hasCurrent && hasDesired:
			if !sameExclusion(cur, des) {
				statements = append(statements,
					dropExclusionStatement(cur, "drop exclusion constraint "+exclusionID(cur)+" for replacement (destructive)"),
					createExclusionStatement(des),
				)
			}
		}
	}
	return statements
}

func createExclusionStatement(exclusion ExclusionConstraint) baseatlas.Statement {
	parts := make([]string, 0, len(exclusion.Columns))
	for _, column := range exclusion.Columns {
		term := quoteIdent(column.Column)
		if column.Expression != "" {
			term = "(" + column.Expression + ")"
		}
		parts = append(parts, term+" WITH "+column.Op)
	}
	sql := "ALTER TABLE " + qualifiedIdent(exclusion.Schema, exclusion.Table) + " ADD CONSTRAINT " + quoteIdent(exclusion.Name) + " EXCLUDE USING " + strings.ToLower(exclusion.Type) + " (" + strings.Join(parts, ", ") + ")"
	if len(exclusion.Include) > 0 {
		include := make([]string, 0, len(exclusion.Include))
		for _, column := range exclusion.Include {
			include = append(include, quoteIdent(column))
		}
		sql += " INCLUDE (" + strings.Join(include, ", ") + ")"
	}
	if exclusion.Where != "" {
		sql += " WHERE (" + exclusion.Where + ")"
	}
	return baseatlas.Statement{Comment: "create exclusion constraint " + exclusionID(exclusion), SQL: sql, Reverse: dropExclusionSQL(exclusion)}
}

func dropExclusionStatement(exclusion ExclusionConstraint, comment string) baseatlas.Statement {
	return baseatlas.Statement{Comment: comment, SQL: dropExclusionSQL(exclusion), Reverse: createExclusionStatement(exclusion).SQL}
}

func dropExclusionSQL(exclusion ExclusionConstraint) string {
	return "ALTER TABLE " + qualifiedIdent(exclusion.Schema, exclusion.Table) + " DROP CONSTRAINT " + quoteIdent(exclusion.Name)
}

func sameExclusion(a, b ExclusionConstraint) bool {
	if strings.ToLower(a.Type) != strings.ToLower(b.Type) || len(a.Columns) != len(b.Columns) || !equalExclusionStringSlices(a.Include, b.Include) || normalizeSQL(a.Where) != normalizeSQL(b.Where) {
		return false
	}
	for i := range a.Columns {
		if a.Columns[i] != b.Columns[i] {
			return false
		}
	}
	return true
}

func exclusionID(exclusion ExclusionConstraint) string {
	return exclusion.Schema + "." + exclusion.Table + "." + exclusion.Name
}

func equalExclusionStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func exclusionColumnListExpr(expr hclsyntax.Expression, src []byte) ([]string, error) {
	if tuple, ok := expr.(*hclsyntax.TupleConsExpr); ok {
		out := make([]string, 0, len(tuple.Exprs))
		for _, elem := range tuple.Exprs {
			column, err := exclusionColumnExpr(elem, src)
			if err != nil {
				return nil, err
			}
			out = append(out, column)
		}
		return out, nil
	}
	column, err := exclusionColumnExpr(expr, src)
	if err != nil {
		return nil, err
	}
	return []string{column}, nil
}

func exclusionColumnExpr(expr hclsyntax.Expression, src []byte) (string, error) {
	if traversal, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		parts := traversalParts(traversal.Traversal)
		if len(parts) == 2 && parts[0] == "column" {
			return parts[1], nil
		}
	}
	return symbolOrString(expr, src)
}

func exclusionTypeFromDef(def string) string {
	fields := strings.Fields(def)
	for i := 0; i+1 < len(fields); i++ {
		if strings.EqualFold(fields[i], "USING") {
			return strings.ToLower(fields[i+1])
		}
	}
	return "gist"
}

func exclusionColumnsFromDef(def string) []ExclusionColumn {
	start := strings.Index(def, "(")
	end := endOfBalancedParens(def, start)
	if start < 0 || end <= start {
		return nil
	}
	items := splitTopLevelComma(def[start+1 : end])
	columns := make([]ExclusionColumn, 0, len(items))
	for _, item := range items {
		parts := strings.Split(strings.TrimSpace(item), " WITH ")
		if len(parts) != 2 {
			continue
		}
		term := strings.TrimSpace(parts[0])
		column := ExclusionColumn{Op: strings.TrimSpace(parts[1])}
		if strings.HasPrefix(term, "(") && strings.HasSuffix(term, ")") {
			column.Expression = strings.TrimSpace(term[1 : len(term)-1])
		} else {
			column.Column = strings.Trim(term, `"`)
		}
		columns = append(columns, column)
	}
	return columns
}

func exclusionIncludeFromDef(def string) []string {
	idx := strings.Index(strings.ToUpper(def), " INCLUDE ")
	if idx < 0 {
		return nil
	}
	start := strings.Index(def[idx:], "(")
	if start < 0 {
		return nil
	}
	start += idx
	end := endOfBalancedParens(def, start)
	if end <= start {
		return nil
	}
	items := splitTopLevelComma(def[start+1 : end])
	include := make([]string, 0, len(items))
	for _, item := range items {
		include = append(include, strings.Trim(strings.TrimSpace(item), `"`))
	}
	return include
}

func exclusionWhereFromDef(def string) string {
	idx := strings.Index(strings.ToUpper(def), " WHERE ")
	if idx < 0 {
		return ""
	}
	start := strings.Index(def[idx:], "(")
	if start < 0 {
		return ""
	}
	start += idx
	end := endOfBalancedParens(def, start)
	if end <= start {
		return ""
	}
	return strings.TrimSpace(def[start+1 : end])
}

func endOfBalancedParens(s string, start int) int {
	if start < 0 || start >= len(s) || s[start] != '(' {
		return -1
	}
	depth := 0
	inSingle := false
	inDouble := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func splitTopLevelComma(s string) []string {
	var out []string
	start := 0
	depth := 0
	inSingle := false
	inDouble := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}
