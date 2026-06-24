package seed

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strings"

	"github.com/edlundin/edvat/internal/baseatlas"

	"ariga.io/atlas/sql/schema"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/lib/pq"
	"github.com/zclconf/go-cty/cty"
)

type Mode string

const (
	ModeInsert Mode = "INSERT"
	ModeUpsert Mode = "UPSERT"
	ModeSync   Mode = "SYNC"
)

type DataSet struct {
	Table      string
	KeyColumns []string
	Rows       []Row
}

type Row map[string]any

type CurrentRows func(ctx context.Context, table string, columns []string) ([]Row, error)

type PlanConfig struct {
	SchemaPaths []string
	SQLPaths    []string
	Mode        Mode
	Desired     *schema.Realm
	CurrentRows CurrentRows
}

func Plan(ctx context.Context, cfg PlanConfig) ([]baseatlas.Statement, error) {
	mode := cfg.Mode
	if mode == "" {
		mode = ModeInsert
	}

	var datasets []DataSet
	for _, path := range cfg.SchemaPaths {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read seed source %q: %w", path, err)
		}
		parsed, err := ParseHCL(src, path)
		if err != nil {
			return nil, err
		}
		datasets = append(datasets, parsed...)
	}

	var statements []baseatlas.Statement
	for _, path := range cfg.SQLPaths {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read SQL seed source %q: %w", path, err)
		}
		for _, stmt := range SplitSQL(src) {
			statements = append(statements, baseatlas.Statement{Comment: "SQL seed data from " + path, SQL: stmt})
		}
	}

	for _, data := range datasets {
		sql, err := sqlForMode(ctx, mode, data, cfg.Desired, cfg.CurrentRows)
		if err != nil {
			return nil, err
		}
		for _, stmt := range sql {
			statements = append(statements, baseatlas.Statement{Comment: "seed data for " + data.Table, SQL: stmt})
		}
	}
	return statements, nil
}

func sqlForMode(ctx context.Context, mode Mode, data DataSet, desired *schema.Realm, currentRows CurrentRows) ([]string, error) {
	keyColumns := seedKeyColumns(data, desired)
	if (mode == ModeUpsert || mode == ModeSync) && currentRows != nil {
		columns := DiffColumns(data.Rows, keyColumns)
		current, err := currentRows(ctx, data.Table, columns)
		if err != nil {
			return nil, err
		}
		if mode == ModeUpsert {
			return UpsertDiffSQL(data.Table, current, data.Rows, keyColumns)
		}
		return DiffSQL(data.Table, current, data.Rows, keyColumns)
	}

	switch mode {
	case ModeInsert:
		return InsertSQL(data, keyColumns)
	case ModeUpsert:
		return UpsertSQL(data, keyColumns)
	case ModeSync:
		return SyncSQL(data, keyColumns)
	default:
		return nil, fmt.Errorf("unsupported seed mode %s", mode)
	}
}

func ParseHCL(src []byte, filename string) ([]DataSet, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse seed hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse seed hcl: unexpected body type %T", file.Body)
	}

	var datasets []DataSet
	for _, block := range body.Blocks {
		if block.Type != "data" || len(block.Labels) != 0 {
			continue
		}
		attrs := block.Body.Attributes
		var diags hcl.Diagnostics
		if diags.HasErrors() {
			return nil, fmt.Errorf("decode data block: %s", diags.Error())
		}
		tableAttr, ok := attrs["table"]
		if !ok {
			return nil, fmt.Errorf("data block requires table")
		}
		rowsAttr, ok := attrs["rows"]
		if !ok {
			return nil, fmt.Errorf("data block for table %s requires rows", exprName(tableAttr.Expr, src))
		}
		table, err := tableName(tableAttr.Expr, src)
		if err != nil {
			return nil, err
		}
		var keyColumns []string
		if keyAttr, ok := attrs["key"]; ok {
			keyColumns, err = keyColumnsExpr(keyAttr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode seed key for table %s: %w", table, err)
			}
		}
		rowsValue, diags := rowsAttr.Expr.Value(nil)
		if diags.HasErrors() {
			return nil, fmt.Errorf("evaluate rows for table %s: %s", table, diags.Error())
		}
		rows, err := rowsFromValue(rowsValue)
		if err != nil {
			return nil, fmt.Errorf("decode rows for table %s: %w", table, err)
		}
		datasets = append(datasets, DataSet{Table: table, KeyColumns: keyColumns, Rows: rows})
	}
	return datasets, nil
}

func SplitSQL(src []byte) []string {
	var statements []string
	var current strings.Builder
	inSingleQuote := false
	inDollarQuote := false
	text := string(src)
	for i := 0; i < len(text); i++ {
		if !inSingleQuote && i+1 < len(text) && text[i:i+2] == "$$" {
			inDollarQuote = !inDollarQuote
			current.WriteString("$$")
			i++
			continue
		}
		ch := text[i]
		if ch == '\'' && !inDollarQuote {
			inSingleQuote = !inSingleQuote
			current.WriteByte(ch)
			if inSingleQuote && i+1 < len(text) && text[i+1] == '\'' {
				i++
				current.WriteByte(text[i])
			}
			continue
		}
		if ch == ';' && !inSingleQuote && !inDollarQuote {
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			current.Reset()
			continue
		}
		current.WriteByte(ch)
	}
	stmt := strings.TrimSpace(current.String())
	if stmt != "" {
		statements = append(statements, stmt)
	}
	return statements
}

func InsertSQL(data DataSet, keyColumns []string) ([]string, error) {
	return conflictSQL(data, keyColumns, false)
}

func InsertSQLForRealm(data DataSet, realm *schema.Realm) ([]string, error) {
	return InsertSQL(data, seedKeyColumns(data, realm))
}

func UpsertSQL(data DataSet, keyColumns []string) ([]string, error) {
	return conflictSQL(data, keyColumns, true)
}

func UpsertSQLForRealm(data DataSet, realm *schema.Realm) ([]string, error) {
	return UpsertSQL(data, seedKeyColumns(data, realm))
}

func SyncSQL(data DataSet, keyColumns []string) ([]string, error) {
	statements, err := UpsertSQL(data, keyColumns)
	if err != nil {
		return nil, err
	}
	if len(data.Rows) == 0 {
		statements = append(statements, fmt.Sprintf("DELETE FROM %s", quoteIdent(data.Table)))
		return statements, nil
	}
	values := make([]string, 0, len(data.Rows))
	for _, row := range data.Rows {
		values = append(values, keyTupleLiteral(row, keyColumns))
	}
	statements = append(statements, fmt.Sprintf("DELETE FROM %s WHERE %s NOT IN (%s)", quoteIdent(data.Table), syncKeyExpression(keyColumns), strings.Join(values, ", ")))
	return statements, nil
}

func SyncSQLForRealm(data DataSet, realm *schema.Realm) ([]string, error) {
	return SyncSQL(data, seedKeyColumns(data, realm))
}

func InspectRowsURL(ctx context.Context, url, table string, columns []string) ([]Row, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectRows(ctx, db, table, columns)
}

func InspectRows(ctx context.Context, db *sql.DB, table string, columns []string) ([]Row, error) {
	if table == "" {
		return nil, fmt.Errorf("seed data requires table")
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("seed data for table %s requires columns to inspect", table)
	}
	query := fmt.Sprintf("SELECT %s FROM %s", quoteIdentList(columns), quoteIdent(table))
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		if pgErr, ok := err.(*pq.Error); ok && (pgErr.Code == "42P01" || pgErr.Code == "3F000") {
			return nil, nil
		}
		return nil, fmt.Errorf("inspect seed rows for table %s: %w", table, err)
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		values := make([]any, len(columns))
		scan := make([]any, len(columns))
		for i := range values {
			scan[i] = &values[i]
		}
		if err := rows.Scan(scan...); err != nil {
			return nil, fmt.Errorf("scan seed row for table %s: %w", table, err)
		}
		row := Row{}
		for i, column := range columns {
			row[column] = normalizeDBValue(values[i])
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect seed rows for table %s: %w", table, err)
	}
	return out, nil
}

func UpsertDiffSQL(table string, currentRows, desiredRows []Row, keyColumns []string) ([]string, error) {
	if table == "" {
		return nil, fmt.Errorf("seed data requires table")
	}
	if len(keyColumns) == 0 {
		return nil, fmt.Errorf("data for table %s requires a primary key or explicit seed key", table)
	}
	currentByKey := map[string]Row{}
	for _, row := range currentRows {
		currentByKey[rowKey(row, keyColumns)] = row
	}
	var statements []string
	for _, desired := range desiredRows {
		current, exists := currentByKey[rowKey(desired, keyColumns)]
		if !exists {
			insert, err := UpsertSQL(DataSet{Table: table, Rows: []Row{desired}}, keyColumns)
			if err != nil {
				return nil, err
			}
			statements = append(statements, insert...)
			continue
		}
		if assignments := changedAssignments(current, desired, keyColumns); len(assignments) > 0 {
			statements = append(statements, fmt.Sprintf("UPDATE %s SET %s WHERE %s", quoteIdent(table), strings.Join(assignments, ", "), keyPredicate(desired, keyColumns)))
		}
	}
	return statements, nil
}

func DiffSQL(table string, currentRows, desiredRows []Row, keyColumns []string) ([]string, error) {
	if table == "" {
		return nil, fmt.Errorf("seed data requires table")
	}
	if len(keyColumns) == 0 {
		return nil, fmt.Errorf("data for table %s requires a primary key or explicit seed key", table)
	}
	desiredByKey := map[string]Row{}
	for _, row := range desiredRows {
		desiredByKey[rowKey(row, keyColumns)] = row
	}
	currentByKey := map[string]Row{}
	for _, row := range currentRows {
		currentByKey[rowKey(row, keyColumns)] = row
	}
	var statements []string
	for _, desired := range desiredRows {
		key := rowKey(desired, keyColumns)
		current, exists := currentByKey[key]
		if !exists {
			insert, err := InsertSQL(DataSet{Table: table, Rows: []Row{desired}}, keyColumns)
			if err != nil {
				return nil, err
			}
			statements = append(statements, insert...)
			continue
		}
		if assignments := changedAssignments(current, desired, keyColumns); len(assignments) > 0 {
			statements = append(statements, fmt.Sprintf("UPDATE %s SET %s WHERE %s", quoteIdent(table), strings.Join(assignments, ", "), keyPredicate(desired, keyColumns)))
		}
	}
	for _, current := range currentRows {
		if _, exists := desiredByKey[rowKey(current, keyColumns)]; !exists {
			statements = append(statements, fmt.Sprintf("DELETE FROM %s WHERE %s", quoteIdent(table), keyPredicate(current, keyColumns)))
		}
	}
	return statements, nil
}

func seedKeyColumns(data DataSet, realm *schema.Realm) []string {
	if len(data.KeyColumns) > 0 {
		return data.KeyColumns
	}
	return PrimaryKeyColumns(realm, data.Table)
}

func conflictSQL(data DataSet, keyColumns []string, update bool) ([]string, error) {
	if data.Table == "" {
		return nil, fmt.Errorf("seed data requires table")
	}
	if len(keyColumns) == 0 {
		return nil, fmt.Errorf("data for table %s requires a primary key or explicit seed key", data.Table)
	}
	statements := make([]string, 0, len(data.Rows))
	for _, row := range data.Rows {
		columns := orderedColumns(row, keyColumns)
		values := make([]string, 0, len(columns))
		for _, column := range columns {
			values = append(values, literal(row[column]))
		}
		statements = append(statements, fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) %s",
			quoteIdent(data.Table), quoteIdentList(columns), strings.Join(values, ", "), quoteIdentList(keyColumns), conflictAction(columns, keyColumns, update),
		))
	}
	return statements, nil
}

func syncKeyExpression(keyColumns []string) string {
	if len(keyColumns) == 1 {
		return quoteIdent(keyColumns[0])
	}
	return "(" + quoteIdentList(keyColumns) + ")"
}

func keyTupleLiteral(row Row, keyColumns []string) string {
	values := make([]string, 0, len(keyColumns))
	for _, key := range keyColumns {
		values = append(values, literal(row[key]))
	}
	if len(values) == 1 {
		return values[0]
	}
	return "(" + strings.Join(values, ", ") + ")"
}

func rowKey(row Row, keyColumns []string) string {
	parts := make([]string, 0, len(keyColumns))
	for _, key := range keyColumns {
		parts = append(parts, literal(row[key]))
	}
	return strings.Join(parts, "\x00")
}

func changedAssignments(current, desired Row, keyColumns []string) []string {
	keySet := map[string]bool{}
	for _, key := range keyColumns {
		keySet[key] = true
	}
	columns := orderedColumns(desired, nil)
	var assignments []string
	for _, column := range columns {
		if keySet[column] || valuesEqual(current[column], desired[column]) {
			continue
		}
		assignments = append(assignments, quoteIdent(column)+" = "+literal(desired[column]))
	}
	return assignments
}

func valuesEqual(a, b any) bool {
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func keyPredicate(row Row, keyColumns []string) string {
	parts := make([]string, 0, len(keyColumns))
	for _, key := range keyColumns {
		if row[key] == nil {
			parts = append(parts, quoteIdent(key)+" IS NULL")
			continue
		}
		parts = append(parts, quoteIdent(key)+" = "+literal(row[key]))
	}
	return strings.Join(parts, " AND ")
}

func conflictAction(columns, keyColumns []string, update bool) string {
	if !update {
		return "DO NOTHING"
	}
	keySet := map[string]bool{}
	for _, key := range keyColumns {
		keySet[key] = true
	}
	var assignments []string
	for _, column := range columns {
		if keySet[column] {
			continue
		}
		assignments = append(assignments, quoteIdent(column)+" = EXCLUDED."+quoteIdent(column))
	}
	if len(assignments) == 0 {
		return "DO NOTHING"
	}
	return "DO UPDATE SET " + strings.Join(assignments, ", ")
}

func PrimaryKeyColumns(realm *schema.Realm, tableName string) []string {
	if realm == nil {
		return nil
	}
	for _, s := range realm.Schemas {
		for _, t := range s.Tables {
			if !tableNameMatches(s.Name, t.Name, tableName) || t.PrimaryKey == nil {
				continue
			}
			columns := make([]string, 0, len(t.PrimaryKey.Parts))
			for _, part := range t.PrimaryKey.Parts {
				if part.C != nil {
					columns = append(columns, part.C.Name)
				}
			}
			return columns
		}
	}
	return nil
}

func tableNameMatches(schemaName, actual, desired string) bool {
	return desired == actual || desired == schemaName+"."+actual
}

func tableName(expr hcl.Expression, src []byte) (string, error) {
	if traversal, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		parts := traversalParts(traversal.Traversal)
		if len(parts) == 2 && parts[0] == "table" {
			return parts[1], nil
		}
		if len(parts) == 3 && parts[0] == "schema" {
			return parts[1] + "." + parts[2], nil
		}
		return strings.Join(parts, "."), nil
	}
	value, diags := expr.Value(nil)
	if diags.HasErrors() {
		return "", fmt.Errorf("evaluate data table: %s", diags.Error())
	}
	if value.Type() != cty.String || value.IsNull() {
		return "", fmt.Errorf("data table must be a table reference or string, got %s", exprName(expr, src))
	}
	return value.AsString(), nil
}

func keyColumnsExpr(expr hcl.Expression, src []byte) ([]string, error) {
	if tuple, ok := expr.(*hclsyntax.TupleConsExpr); ok {
		out := make([]string, 0, len(tuple.Exprs))
		for _, elem := range tuple.Exprs {
			value, err := symbolOrString(elem, src)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		return out, nil
	}
	value, err := symbolOrString(expr, src)
	if err != nil {
		return nil, err
	}
	return []string{value}, nil
}

func symbolOrString(expr hcl.Expression, src []byte) (string, error) {
	if traversal, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		parts := traversalParts(traversal.Traversal)
		if len(parts) == 1 {
			return parts[0], nil
		}
	}
	value, diags := expr.Value(nil)
	if diags.HasErrors() {
		return "", fmt.Errorf("evaluate expression %s: %s", exprName(expr, src), diags.Error())
	}
	if value.Type() != cty.String || value.IsNull() {
		return "", fmt.Errorf("expected symbol or string, got %s", exprName(expr, src))
	}
	return value.AsString(), nil
}

func rowsFromValue(value cty.Value) ([]Row, error) {
	if value.IsNull() {
		return nil, fmt.Errorf("rows cannot be null")
	}
	if !value.CanIterateElements() {
		return nil, fmt.Errorf("rows must be a list of objects")
	}
	rows := []Row{}
	it := value.ElementIterator()
	for it.Next() {
		_, rowValue := it.Element()
		if rowValue.IsNull() || !rowValue.CanIterateElements() {
			return nil, fmt.Errorf("row must be an object")
		}
		row := Row{}
		rowIt := rowValue.ElementIterator()
		for rowIt.Next() {
			key, cell := rowIt.Element()
			row[key.AsString()] = goValue(cell)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func goValue(value cty.Value) any {
	if value.IsNull() {
		return nil
	}
	switch {
	case value.Type() == cty.String:
		return value.AsString()
	case value.Type() == cty.Bool:
		return value.True()
	case value.Type() == cty.Number:
		if i, exact := value.AsBigFloat().Int64(); exact == big.Exact {
			return i
		}
		f, _ := value.AsBigFloat().Float64()
		return f
	default:
		return value.GoString()
	}
}

func DiffColumns(rows []Row, keyColumns []string) []string {
	seen := map[string]bool{}
	var columns []string
	for _, key := range keyColumns {
		if !seen[key] {
			seen[key] = true
			columns = append(columns, key)
		}
	}
	var rest []string
	for _, row := range rows {
		for column := range row {
			if !seen[column] {
				seen[column] = true
				rest = append(rest, column)
			}
		}
	}
	sort.Strings(rest)
	return append(columns, rest...)
}

func normalizeDBValue(value any) any {
	switch v := value.(type) {
	case []byte:
		return string(v)
	default:
		return v
	}
}

func orderedColumns(row Row, keyColumns []string) []string {
	seen := map[string]bool{}
	columns := make([]string, 0, len(row))
	for _, key := range keyColumns {
		if _, ok := row[key]; ok {
			columns = append(columns, key)
			seen[key] = true
		}
	}
	rest := make([]string, 0, len(row))
	for column := range row {
		if !seen[column] {
			rest = append(rest, column)
		}
	}
	sort.Strings(rest)
	return append(columns, rest...)
}

func literal(value any) string {
	switch v := value.(type) {
	case nil:
		return "NULL"
	case string:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	case bool:
		if v {
			return "TRUE"
		}
		return "FALSE"
	default:
		return fmt.Sprint(v)
	}
}

func quoteIdentList(parts []string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, quoteIdent(part))
	}
	return strings.Join(quoted, ", ")
}

func quoteIdent(name string) string {
	parts := strings.Split(name, ".")
	for i, part := range parts {
		parts[i] = `"` + strings.ReplaceAll(part, `"`, `""`) + `"`
	}
	return strings.Join(parts, ".")
}

func traversalParts(traversal hcl.Traversal) []string {
	parts := make([]string, 0, len(traversal))
	for _, traverser := range traversal {
		switch t := traverser.(type) {
		case hcl.TraverseRoot:
			parts = append(parts, t.Name)
		case hcl.TraverseAttr:
			parts = append(parts, t.Name)
		}
	}
	return parts
}

func exprName(expr hcl.Expression, src []byte) string {
	range_ := expr.Range()
	if range_.Start.Byte >= 0 && range_.End.Byte <= len(src) && range_.Start.Byte < range_.End.Byte {
		return strings.TrimSpace(string(src[range_.Start.Byte:range_.End.Byte]))
	}
	return range_.String()
}
