package migrationplan

import (
	"regexp"
	"strings"

	"github.com/edlundin/edvat/internal/baseatlas"
)

func orderMigrationStatements(statements []baseatlas.Statement) []baseatlas.Statement {
	ordered := append([]baseatlas.Statement(nil), statements...)
	stableStatementSort(ordered)
	ordered = orderCreateTablesByReferences(ordered)
	ordered = orderFunctionsBeforeReferencingTables(ordered)
	ordered = orderCreateStatementsByObjectReferences(ordered)
	return ordered
}

func stableStatementSort(statements []baseatlas.Statement) {
	for i := 1; i < len(statements); i++ {
		current := statements[i]
		j := i - 1
		for j >= 0 && statementOrder(current) < statementOrder(statements[j]) {
			statements[j+1] = statements[j]
			j--
		}
		statements[j+1] = current
	}
}

func statementOrder(statement baseatlas.Statement) int {
	sql := strings.ToUpper(strings.TrimSpace(statement.SQL))
	switch {
	case strings.HasPrefix(sql, "CREATE SCHEMA") || strings.HasPrefix(sql, "COMMENT ON SCHEMA"):
		return 10
	case strings.HasPrefix(sql, "CREATE EXTENSION") || strings.HasPrefix(sql, "ALTER EXTENSION") || strings.HasPrefix(sql, "COMMENT ON EXTENSION"):
		return 20
	case strings.HasPrefix(sql, "CREATE DOMAIN") || strings.HasPrefix(sql, "CREATE TYPE") || strings.HasPrefix(sql, "CREATE COLLATION") || strings.HasPrefix(sql, "CREATE SEQUENCE"):
		return 30
	case strings.HasPrefix(sql, "CREATE TABLE"):
		return 40
	case strings.HasPrefix(sql, "ALTER TABLE"):
		return 45
	case strings.HasPrefix(sql, "CREATE INDEX"):
		return 50
	case strings.HasPrefix(sql, "CREATE CAST"):
		return 55
	case strings.HasPrefix(sql, "CREATE OR REPLACE FUNCTION") || strings.HasPrefix(sql, "CREATE FUNCTION") || strings.HasPrefix(sql, "COMMENT ON FUNCTION"):
		return 60
	case strings.HasPrefix(sql, "CREATE OR REPLACE PROCEDURE") || strings.HasPrefix(sql, "CREATE PROCEDURE") || strings.HasPrefix(sql, "COMMENT ON PROCEDURE"):
		return 70
	case strings.HasPrefix(sql, "CREATE AGGREGATE"):
		return 80
	case strings.HasPrefix(sql, "CREATE SERVER") || strings.HasPrefix(sql, "COMMENT ON SERVER"):
		return 85
	case strings.HasPrefix(sql, "CREATE FOREIGN TABLE"):
		return 86
	case strings.HasPrefix(sql, "CREATE USER MAPPING"):
		return 87
	case strings.HasPrefix(sql, "CREATE TRIGGER") || strings.HasPrefix(sql, "CREATE CONSTRAINT TRIGGER") || strings.HasPrefix(sql, "CREATE EVENT TRIGGER"):
		return 90
	case strings.HasPrefix(sql, "CREATE POLICY") || strings.HasPrefix(sql, "GRANT ") || strings.HasPrefix(sql, "REVOKE "):
		return 100
	case strings.HasPrefix(sql, "CREATE VIEW") || strings.HasPrefix(sql, "CREATE MATERIALIZED VIEW"):
		return 110
	case strings.HasPrefix(sql, "INSERT "):
		return 120
	case strings.HasPrefix(sql, "DROP TRIGGER") || strings.HasPrefix(sql, "DROP EVENT TRIGGER"):
		return 800
	case strings.HasPrefix(sql, "DROP POLICY") || strings.HasPrefix(sql, "REVOKE "):
		return 810
	case strings.HasPrefix(sql, "DROP VIEW") || strings.HasPrefix(sql, "DROP MATERIALIZED VIEW"):
		return 820
	case strings.HasPrefix(sql, "DROP USER MAPPING"):
		return 825
	case strings.HasPrefix(sql, "DROP FOREIGN TABLE"):
		return 826
	case strings.HasPrefix(sql, "DROP SERVER"):
		return 827
	case strings.HasPrefix(sql, "DROP AGGREGATE"):
		return 830
	case strings.HasPrefix(sql, "DROP PROCEDURE"):
		return 840
	case strings.HasPrefix(sql, "DROP FUNCTION"):
		return 850
	case strings.HasPrefix(sql, "DROP CAST"):
		return 860
	case strings.HasPrefix(sql, "DROP TABLE") || strings.HasPrefix(sql, "DROP INDEX"):
		return 870
	case strings.HasPrefix(sql, "DROP SEQUENCE") || strings.HasPrefix(sql, "DROP DOMAIN") || strings.HasPrefix(sql, "DROP TYPE") || strings.HasPrefix(sql, "DROP COLLATION"):
		return 880
	case strings.HasPrefix(sql, "DROP EXTENSION"):
		return 890
	case strings.HasPrefix(sql, "DROP SCHEMA"):
		return 900
	case strings.HasPrefix(sql, "DROP "):
		return 895
	default:
		return 500
	}
}

var (
	createTableRe    = regexp.MustCompile(`(?is)^\s*CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?((?:"[^"]+"|[a-zA-Z_][a-zA-Z0-9_]*)(?:\s*\.\s*(?:"[^"]+"|[a-zA-Z_][a-zA-Z0-9_]*))?)`)
	createFunctionRe = regexp.MustCompile(`(?is)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+((?:"[^"]+"|[a-zA-Z_][a-zA-Z0-9_]*)(?:\s*\.\s*(?:"[^"]+"|[a-zA-Z_][a-zA-Z0-9_]*))?)`)
	createViewRe     = regexp.MustCompile(`(?is)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:MATERIALIZED\s+)?VIEW\s+((?:"[^"]+"|[a-zA-Z_][a-zA-Z0-9_]*)(?:\s*\.\s*(?:"[^"]+"|[a-zA-Z_][a-zA-Z0-9_]*))?)`)
	referencesRe     = regexp.MustCompile(`(?is)\bREFERENCES\s+((?:"[^"]+"|[a-zA-Z_][a-zA-Z0-9_]*)(?:\s*\.\s*(?:"[^"]+"|[a-zA-Z_][a-zA-Z0-9_]*))?)`)
)

func orderCreateTablesByReferences(statements []baseatlas.Statement) []baseatlas.Statement {
	nameToIndex := map[string]int{}
	for i, statement := range statements {
		if name := createTableName(statement.SQL); name != "" {
			nameToIndex[name] = i
		}
	}
	if len(nameToIndex) < 2 {
		return statements
	}
	ordered := append([]baseatlas.Statement(nil), statements...)
	visiting := map[int]bool{}
	visited := map[int]bool{}
	var out []baseatlas.Statement
	var visit func(int)
	visit = func(i int) {
		if visited[i] {
			return
		}
		if visiting[i] {
			return
		}
		visiting[i] = true
		for _, dep := range referencedTableNames(statements[i].SQL) {
			if depIndex, ok := nameToIndex[dep]; ok && statementOrder(statements[depIndex]) == statementOrder(statements[i]) {
				visit(depIndex)
			}
		}
		visiting[i] = false
		visited[i] = true
		out = append(out, statements[i])
	}
	for i, statement := range statements {
		if createTableName(statement.SQL) == "" {
			continue
		}
		visit(i)
	}
	j := 0
	for i, statement := range ordered {
		if createTableName(statement.SQL) != "" {
			ordered[i] = out[j]
			j++
		}
	}
	return ordered
}

func orderFunctionsBeforeReferencingTables(statements []baseatlas.Statement) []baseatlas.Statement {
	functionIndexes := map[string]int{}
	for i, statement := range statements {
		if name := createFunctionName(statement.SQL); name != "" {
			functionIndexes[name] = i
			functionIndexes[unqualifiedObjectName(name)] = i
		}
	}
	if len(functionIndexes) == 0 {
		return statements
	}
	ordered := append([]baseatlas.Statement(nil), statements...)
	for tableIndex := 0; tableIndex < len(ordered); tableIndex++ {
		if createTableName(ordered[tableIndex].SQL) == "" {
			continue
		}
		for _, functionName := range referencedFunctionNames(ordered[tableIndex].SQL, functionIndexes) {
			functionIndex := indexOfCreateFunction(ordered, functionName)
			if functionIndex == -1 || functionIndex < tableIndex {
				continue
			}
			fn := ordered[functionIndex]
			copy(ordered[tableIndex+1:functionIndex+1], ordered[tableIndex:functionIndex])
			ordered[tableIndex] = fn
			tableIndex++
		}
	}
	return ordered
}

func orderCreateStatementsByObjectReferences(statements []baseatlas.Statement) []baseatlas.Statement {
	nameToIndex := map[string]int{}
	for i, statement := range statements {
		if name := createdObjectName(statement.SQL); name != "" {
			nameToIndex[name] = i
			nameToIndex[unqualifiedObjectName(name)] = i
		}
	}
	if len(nameToIndex) == 0 {
		return statements
	}
	visiting := map[int]bool{}
	visited := map[int]bool{}
	var orderedIndexes []int
	var visit func(int)
	visit = func(i int) {
		if visited[i] || visiting[i] {
			return
		}
		visiting[i] = true
		for _, depName := range referencedCreatedObjectNames(statements[i], nameToIndex) {
			depIndex := nameToIndex[depName]
			if depIndex != i && statementOrder(statements[depIndex]) == statementOrder(statements[i]) {
				visit(depIndex)
			}
		}
		visiting[i] = false
		visited[i] = true
		orderedIndexes = append(orderedIndexes, i)
	}
	for i, statement := range statements {
		if createdObjectName(statement.SQL) != "" {
			visit(i)
		}
	}
	if len(orderedIndexes) == 0 {
		return statements
	}
	out := append([]baseatlas.Statement(nil), statements...)
	j := 0
	for i, statement := range statements {
		if createdObjectName(statement.SQL) != "" {
			out[i] = statements[orderedIndexes[j]]
			j++
		}
	}
	return out
}

func referencedCreatedObjectNames(statement baseatlas.Statement, nameToIndex map[string]int) []string {
	self := createdObjectName(statement.SQL)
	normalizedSQL := normalizeSQLSearchText(statement.SQL)
	var out []string
	seen := map[string]bool{}
	for name := range nameToIndex {
		if seen[name] || name == self || name == unqualifiedObjectName(self) {
			continue
		}
		if objectNameReferenced(normalizedSQL, name) {
			out = append(out, name)
			seen[name] = true
		}
	}
	return out
}

func objectNameReferenced(normalizedSQL, name string) bool {
	if name == "" {
		return false
	}
	if strings.Contains(name, ".") {
		return containsNormalizedObjectName(normalizedSQL, name)
	}
	return containsNormalizedObjectName(normalizedSQL, name+"(")
}

func containsNormalizedObjectName(sql, name string) bool {
	start := 0
	for {
		idx := strings.Index(sql[start:], name)
		if idx == -1 {
			return false
		}
		idx += start
		beforeOK := idx == 0 || !isSQLObjectNameChar(sql[idx-1])
		after := idx + len(name)
		afterOK := after == len(sql) || !isSQLObjectNameChar(sql[after])
		if beforeOK && afterOK {
			return true
		}
		start = idx + 1
	}
}

func isSQLObjectNameChar(ch byte) bool {
	return ch == '_' || ch == '.' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9'
}

func createdObjectName(sql string) string {
	if name := createTableName(sql); name != "" {
		return name
	}
	if name := createFunctionName(sql); name != "" {
		return name
	}
	if name := createViewName(sql); name != "" {
		return name
	}
	return ""
}

func indexOfCreateFunction(statements []baseatlas.Statement, name string) int {
	for i, statement := range statements {
		if createFunctionName(statement.SQL) == name || unqualifiedObjectName(createFunctionName(statement.SQL)) == name {
			return i
		}
	}
	return -1
}

func referencedFunctionNames(sql string, functionIndexes map[string]int) []string {
	normalizedSQL := normalizeSQLSearchText(sql)
	var out []string
	seen := map[string]bool{}
	for name := range functionIndexes {
		if seen[name] {
			continue
		}
		if strings.Contains(normalizedSQL, name+"(") {
			out = append(out, name)
			seen[name] = true
		}
	}
	return out
}

func createTableName(sql string) string {
	match := createTableRe.FindStringSubmatch(sql)
	if len(match) != 2 {
		return ""
	}
	return normalizeSQLObjectName(match[1])
}

func createFunctionName(sql string) string {
	match := createFunctionRe.FindStringSubmatch(sql)
	if len(match) != 2 {
		return ""
	}
	return normalizeSQLObjectName(match[1])
}

func createViewName(sql string) string {
	match := createViewRe.FindStringSubmatch(sql)
	if len(match) != 2 {
		return ""
	}
	return normalizeSQLObjectName(match[1])
}

func referencedTableNames(sql string) []string {
	matches := referencesRe.FindAllStringSubmatch(sql, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) == 2 {
			out = append(out, normalizeSQLObjectName(match[1]))
		}
	}
	return out
}

func normalizeSQLObjectName(name string) string {
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, " ", "")
	return strings.ToLower(strings.TrimSpace(name))
}

func normalizeSQLSearchText(sql string) string {
	sql = strings.ReplaceAll(sql, `"`, "")
	return strings.ToLower(sql)
}

func unqualifiedObjectName(name string) string {
	parts := strings.Split(name, ".")
	return parts[len(parts)-1]
}

func destructiveFindings(statements []baseatlas.Statement) []Finding {
	comments := destructiveStatementComments(statements)
	findings := make([]Finding, 0, len(comments))
	for _, comment := range comments {
		findings = append(findings, Finding{Kind: "destructive", Message: comment})
	}
	return findings
}

func destructiveStatementComments(statements []baseatlas.Statement) []string {
	var comments []string
	for _, statement := range statements {
		if strings.Contains(strings.ToLower(statement.Comment), "destructive") {
			comments = append(comments, statement.Comment)
			continue
		}
		if isDestructiveSQL(statement.SQL) {
			comments = append(comments, strings.TrimSpace(statement.SQL))
		}
	}
	return comments
}

func isDestructiveSQL(sql string) bool {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	return strings.HasPrefix(upper, "DROP ") || strings.HasPrefix(upper, "REVOKE ")
}
