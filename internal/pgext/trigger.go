package pgext

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/edlundin/edvat/internal/baseatlas"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

type Trigger struct {
	Name          string
	Schema        string
	Table         string
	Timing        string
	Events        []string
	UpdateColumns []string
	Function      string
	Args          []string
	When          string
	ForEach       string
	OldTable      string
	NewTable      string
	Constraint    bool
	Deferrable    bool
	Initially     string
}

type TriggerState map[string]Trigger

func ParseTriggerFiles(paths []string) (TriggerState, error) {
	return parseStateFiles(paths, "trigger", ParseTriggersHCL)
}

func ParseTriggersHCL(src []byte, filename string) (TriggerState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse trigger hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse trigger hcl: unexpected body type %T", file.Body)
	}

	state := TriggerState{}
	for _, block := range body.Blocks {
		if block.Type != "trigger" || len(block.Labels) != 1 {
			continue
		}
		trigger := Trigger{Name: block.Labels[0]}
		attrs := block.Body.Attributes
		if attr, ok := attrs["on"]; ok {
			schemaName, tableName, err := tableExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode trigger.%s.on: %w", trigger.Name, err)
			}
			trigger.Schema = schemaName
			trigger.Table = tableName
		}
		if attr, ok := attrs["timing"]; ok {
			timing, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode trigger.%s.timing: %w", trigger.Name, err)
			}
			trigger.Timing = strings.ToUpper(timing)
		}
		if attr, ok := attrs["events"]; ok {
			events, err := symbolOrStringList(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode trigger.%s.events: %w", trigger.Name, err)
			}
			for _, event := range events {
				trigger.Events = append(trigger.Events, strings.ToUpper(event))
			}
		}
		if attr, ok := attrs["update_of"]; ok {
			columns, err := symbolOrStringList(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode trigger.%s.update_of: %w", trigger.Name, err)
			}
			trigger.UpdateColumns = columns
		}
		if attr, ok := attrs["execute"]; ok {
			execute, err := functionExpr(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode trigger.%s.execute: %w", trigger.Name, err)
			}
			trigger.Function = execute
		}
		if attr, ok := attrs["args"]; ok {
			args, err := stringListExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode trigger.%s.args: %w", trigger.Name, err)
			}
			trigger.Args = args
		}
		if attr, ok := attrs["when"]; ok {
			when, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode trigger.%s.when: %w", trigger.Name, err)
			}
			trigger.When = strings.TrimSpace(when)
		}
		if attr, ok := attrs["for_each"]; ok {
			forEach, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode trigger.%s.for_each: %w", trigger.Name, err)
			}
			trigger.ForEach = strings.ToUpper(forEach)
		}
		if attr, ok := attrs["old_table"]; ok {
			oldTable, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode trigger.%s.old_table: %w", trigger.Name, err)
			}
			trigger.OldTable = oldTable
		}
		if attr, ok := attrs["new_table"]; ok {
			newTable, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode trigger.%s.new_table: %w", trigger.Name, err)
			}
			trigger.NewTable = newTable
		}
		if attr, ok := attrs["constraint"]; ok {
			constraint, err := boolExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode trigger.%s.constraint: %w", trigger.Name, err)
			}
			trigger.Constraint = constraint
		}
		if attr, ok := attrs["deferrable"]; ok {
			deferrable, err := boolExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode trigger.%s.deferrable: %w", trigger.Name, err)
			}
			trigger.Deferrable = deferrable
		}
		if attr, ok := attrs["initially"]; ok {
			initially, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode trigger.%s.initially: %w", trigger.Name, err)
			}
			trigger.Initially = strings.ToUpper(initially)
		}
		if trigger.Table == "" {
			return nil, fmt.Errorf("trigger.%s requires on", trigger.Name)
		}
		if trigger.Timing == "" {
			return nil, fmt.Errorf("trigger.%s requires timing", trigger.Name)
		}
		if len(trigger.Events) == 0 {
			return nil, fmt.Errorf("trigger.%s requires events", trigger.Name)
		}
		if trigger.Function == "" {
			return nil, fmt.Errorf("trigger.%s requires execute", trigger.Name)
		}
		state[triggerID(trigger)] = trigger
	}
	return state, nil
}

func InspectTriggersURL(ctx context.Context, url string) (TriggerState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectTriggers(ctx, db)
}

func InspectTriggers(ctx context.Context, db *sql.DB) (TriggerState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT n.nspname, c.relname, t.tgname, pg_get_triggerdef(t.oid, true)
FROM pg_trigger t
JOIN pg_class c ON c.oid = t.tgrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE NOT t.tgisinternal
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY n.nspname, c.relname, t.tgname`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres triggers: %w", err)
	}
	defer rows.Close()
	state := TriggerState{}
	for rows.Next() {
		var trigger Trigger
		var def string
		if err := rows.Scan(&trigger.Schema, &trigger.Table, &trigger.Name, &def); err != nil {
			return nil, fmt.Errorf("scan postgres trigger: %w", err)
		}
		parsed, err := parseTriggerDef(trigger, def)
		if err != nil {
			return nil, fmt.Errorf("parse postgres trigger %s.%s.%s: %w", trigger.Schema, trigger.Table, trigger.Name, err)
		}
		state[triggerID(parsed)] = parsed
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres triggers: %w", err)
	}
	return state, nil
}

func DiffTriggers(current, desired TriggerState) []baseatlas.Statement {
	if current == nil {
		current = TriggerState{}
	}
	if desired == nil {
		desired = TriggerState{}
	}
	ids := stateIDs(current, desired)

	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createTriggerStatement(des))
		case hasCurrent && !hasDesired:
			statements = append(statements, dropTriggerStatement(cur))
		case hasCurrent && hasDesired:
			if !sameTrigger(cur, des) {
				statements = append(statements, dropTriggerStatement(cur), createTriggerStatement(des))
			}
		}
	}
	return statements
}

func createTriggerStatement(trigger Trigger) baseatlas.Statement {
	return baseatlas.Statement{Comment: "create trigger " + triggerID(trigger), SQL: createTriggerSQL(trigger), Reverse: dropTriggerStatement(trigger).SQL}
}

func dropTriggerStatement(trigger Trigger) baseatlas.Statement {
	return baseatlas.Statement{Comment: "drop trigger " + triggerID(trigger) + " (destructive)", SQL: "DROP TRIGGER " + quoteIdent(trigger.Name) + " ON " + qualifiedIdent(trigger.Schema, trigger.Table)}
}

func createTriggerSQL(trigger Trigger) string {
	var b strings.Builder
	if trigger.Constraint {
		b.WriteString("CREATE CONSTRAINT TRIGGER ")
	} else {
		b.WriteString("CREATE TRIGGER ")
	}
	b.WriteString(quoteIdent(trigger.Name))
	b.WriteString(" ")
	b.WriteString(trigger.Timing)
	b.WriteString(" ")
	b.WriteString(triggerEventsSQL(trigger))
	b.WriteString(" ON ")
	b.WriteString(qualifiedIdent(trigger.Schema, trigger.Table))
	if trigger.Deferrable {
		b.WriteString(" DEFERRABLE")
	}
	if trigger.Initially != "" {
		b.WriteString(" INITIALLY ")
		b.WriteString(trigger.Initially)
	}
	if trigger.ForEach != "" {
		b.WriteString(" FOR EACH ")
		b.WriteString(trigger.ForEach)
	}
	if trigger.OldTable != "" || trigger.NewTable != "" {
		b.WriteString(" REFERENCING")
		if trigger.OldTable != "" {
			b.WriteString(" OLD TABLE AS ")
			b.WriteString(quoteIdent(trigger.OldTable))
		}
		if trigger.NewTable != "" {
			b.WriteString(" NEW TABLE AS ")
			b.WriteString(quoteIdent(trigger.NewTable))
		}
	}
	if trigger.When != "" {
		b.WriteString(" WHEN (")
		b.WriteString(trigger.When)
		b.WriteString(")")
	}
	b.WriteString(" EXECUTE FUNCTION ")
	b.WriteString(trigger.Function)
	b.WriteString("(")
	for i, arg := range trigger.Args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(literal(arg))
	}
	b.WriteString(")")
	return b.String()
}

func sameTrigger(a, b Trigger) bool {
	return a.Timing == b.Timing && strings.Join(a.Events, ",") == strings.Join(b.Events, ",") && strings.Join(a.UpdateColumns, ",") == strings.Join(b.UpdateColumns, ",") && normalizeQualifiedName(a.Function) == normalizeQualifiedName(b.Function) && strings.Join(a.Args, "\x00") == strings.Join(b.Args, "\x00") && normalizeTriggerWhen(a.When) == normalizeTriggerWhen(b.When) && normalizeTriggerForEach(a.ForEach) == normalizeTriggerForEach(b.ForEach) && a.OldTable == b.OldTable && a.NewTable == b.NewTable && a.Constraint == b.Constraint && a.Deferrable == b.Deferrable && a.Initially == b.Initially
}

func normalizeQualifiedName(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), `"`, ""))
}

func normalizeTriggerWhen(value string) string {
	normalized := strings.ToLower(normalizeSQL(value))
	normalized = oldNewTextCastRe.ReplaceAllString(normalized, `$1.$2`)
	return normalized
}

var oldNewTextCastRe = regexp.MustCompile(`\b(old|new)\.([a-zA-Z_][a-zA-Z0-9_]*)::text\b`)

func normalizeTriggerForEach(value string) string {
	if value == "" {
		return "STATEMENT"
	}
	return value
}

func triggerEventsSQL(trigger Trigger) string {
	parts := make([]string, 0, len(trigger.Events))
	for _, event := range trigger.Events {
		if event == "UPDATE" && len(trigger.UpdateColumns) > 0 {
			quoted := make([]string, 0, len(trigger.UpdateColumns))
			for _, column := range trigger.UpdateColumns {
				quoted = append(quoted, quoteIdent(column))
			}
			parts = append(parts, "UPDATE OF "+strings.Join(quoted, ", "))
			continue
		}
		parts = append(parts, event)
	}
	return strings.Join(parts, " OR ")
}

func parseTriggerDef(trigger Trigger, def string) (Trigger, error) {
	def = strings.TrimSpace(def)
	upper := strings.ToUpper(def)
	prefix := "CREATE TRIGGER "
	if strings.HasPrefix(upper, "CREATE CONSTRAINT TRIGGER ") {
		trigger.Constraint = true
		prefix = "CREATE CONSTRAINT TRIGGER "
	} else if !strings.HasPrefix(upper, prefix) {
		return trigger, fmt.Errorf("unsupported trigger definition %q", def)
	}
	rest := strings.TrimSpace(def[len(prefix):])
	name, rest := readSQLToken(rest)
	if name == "" {
		return trigger, fmt.Errorf("missing trigger name")
	}
	trigger.Name = unquoteIdent(name)
	upperRest := strings.ToUpper(rest)
	for _, timing := range []string{"INSTEAD OF", "BEFORE", "AFTER"} {
		if strings.HasPrefix(upperRest, timing+" ") {
			trigger.Timing = timing
			rest = strings.TrimSpace(rest[len(timing):])
			break
		}
	}
	if trigger.Timing == "" {
		return trigger, fmt.Errorf("missing trigger timing")
	}
	onIdx := strings.Index(strings.ToUpper(rest), " ON ")
	if onIdx == -1 {
		return trigger, fmt.Errorf("missing ON clause")
	}
	parseTriggerEvents(&trigger, strings.TrimSpace(rest[:onIdx]))
	rest = strings.TrimSpace(rest[onIdx+len(" ON "):])
	_, rest = readQualifiedName(rest)
	executeIdx := strings.Index(strings.ToUpper(rest), " EXECUTE ")
	if executeIdx == -1 {
		return trigger, fmt.Errorf("missing EXECUTE clause")
	}
	parseTriggerOptions(&trigger, strings.TrimSpace(rest[:executeIdx]))
	exec := strings.TrimSpace(rest[executeIdx+len(" EXECUTE "):])
	upperExec := strings.ToUpper(exec)
	if strings.HasPrefix(upperExec, "FUNCTION ") {
		exec = strings.TrimSpace(exec[len("FUNCTION "):])
	} else if strings.HasPrefix(upperExec, "PROCEDURE ") {
		exec = strings.TrimSpace(exec[len("PROCEDURE "):])
	} else {
		return trigger, fmt.Errorf("missing FUNCTION/PROCEDURE after EXECUTE")
	}
	open := strings.LastIndex(exec, "(")
	close := strings.LastIndex(exec, ")")
	if open == -1 || close < open {
		return trigger, fmt.Errorf("malformed EXECUTE arguments")
	}
	trigger.Function = strings.TrimSpace(exec[:open])
	trigger.Args = parseTriggerArgs(exec[open+1 : close])
	return trigger, nil
}

func parseTriggerEvents(trigger *Trigger, value string) {
	for _, part := range strings.Split(value, " OR ") {
		part = strings.TrimSpace(part)
		upper := strings.ToUpper(part)
		if strings.HasPrefix(upper, "UPDATE OF ") {
			trigger.Events = append(trigger.Events, "UPDATE")
			for _, column := range strings.Split(part[len("UPDATE OF "):], ",") {
				trigger.UpdateColumns = append(trigger.UpdateColumns, unquoteIdent(strings.TrimSpace(column)))
			}
			continue
		}
		trigger.Events = append(trigger.Events, upper)
	}
}

func parseTriggerOptions(trigger *Trigger, options string) {
	for options != "" {
		upper := strings.ToUpper(options)
		switch {
		case strings.HasPrefix(upper, "DEFERRABLE"):
			trigger.Deferrable = true
			options = strings.TrimSpace(options[len("DEFERRABLE"):])
		case strings.HasPrefix(upper, "INITIALLY DEFERRED"):
			trigger.Initially = "DEFERRED"
			options = strings.TrimSpace(options[len("INITIALLY DEFERRED"):])
		case strings.HasPrefix(upper, "INITIALLY IMMEDIATE"):
			trigger.Initially = "IMMEDIATE"
			options = strings.TrimSpace(options[len("INITIALLY IMMEDIATE"):])
		case strings.HasPrefix(upper, "FOR EACH ROW"):
			trigger.ForEach = "ROW"
			options = strings.TrimSpace(options[len("FOR EACH ROW"):])
		case strings.HasPrefix(upper, "FOR EACH STATEMENT"):
			trigger.ForEach = "STATEMENT"
			options = strings.TrimSpace(options[len("FOR EACH STATEMENT"):])
		case strings.HasPrefix(upper, "REFERENCING "):
			end := strings.Index(strings.ToUpper(options), " WHEN ")
			ref := strings.TrimSpace(options[len("REFERENCING "):])
			if end != -1 {
				ref = strings.TrimSpace(options[len("REFERENCING "):end])
				options = strings.TrimSpace(options[end:])
			} else {
				options = ""
			}
			parseReferencing(trigger, ref)
		case strings.HasPrefix(upper, "WHEN ("):
			trigger.When = strings.TrimSuffix(strings.TrimSpace(options[len("WHEN ("):]), ")")
			options = ""
		default:
			options = ""
		}
	}
}

func parseReferencing(trigger *Trigger, ref string) {
	fields := strings.Fields(ref)
	for i := 0; i+3 < len(fields); i++ {
		if strings.EqualFold(fields[i], "OLD") && strings.EqualFold(fields[i+1], "TABLE") && strings.EqualFold(fields[i+2], "AS") {
			trigger.OldTable = unquoteIdent(fields[i+3])
		}
		if strings.EqualFold(fields[i], "NEW") && strings.EqualFold(fields[i+1], "TABLE") && strings.EqualFold(fields[i+2], "AS") {
			trigger.NewTable = unquoteIdent(fields[i+3])
		}
	}
}

func parseTriggerArgs(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var args []string
	for _, part := range strings.Split(value, ",") {
		args = append(args, strings.Trim(unquoteSQLLiteral(strings.TrimSpace(part)), " "))
	}
	return args
}

func readQualifiedName(s string) (string, string) {
	first, rest := readSQLToken(s)
	if strings.HasPrefix(rest, ".") {
		second, after := readSQLToken(strings.TrimSpace(rest[1:]))
		return first + "." + second, after
	}
	return first, rest
}

func readSQLToken(s string) (string, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if s[0] == '"' {
		for i := 1; i < len(s); i++ {
			if s[i] == '"' {
				if i+1 < len(s) && s[i+1] == '"' {
					i++
					continue
				}
				return s[:i+1], strings.TrimSpace(s[i+1:])
			}
		}
	}
	for i, r := range s {
		if unicode.IsSpace(r) || r == '(' || r == ')' || r == ',' || r == '.' {
			return s[:i], strings.TrimSpace(s[i:])
		}
	}
	return s, ""
}

func unquoteIdent(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		return strings.ReplaceAll(value[1:len(value)-1], `""`, `"`)
	}
	return value
}

func unquoteSQLLiteral(value string) string {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return strings.ReplaceAll(value[1:len(value)-1], `''`, `'`)
	}
	return value
}

func triggerID(trigger Trigger) string {
	return viewID(trigger.Schema, trigger.Table) + "." + trigger.Name
}

func tableExpr(expr hclsyntax.Expression) (string, string, error) {
	if traversal, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		parts := traversalParts(traversal.Traversal)
		if len(parts) == 2 && parts[0] == "table" {
			return "", parts[1], nil
		}
		if len(parts) == 3 && parts[0] == "schema" {
			return parts[1], parts[2], nil
		}
	}
	name, err := stringExpr(expr)
	return "", name, err
}

func functionExpr(expr hclsyntax.Expression, src []byte) (string, error) {
	if traversal, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		parts := traversalParts(traversal.Traversal)
		if len(parts) == 2 && parts[0] == "function" {
			return quoteIdent(parts[1]), nil
		}
		if len(parts) == 3 && parts[0] == "schema" {
			return qualifiedIdent(parts[1], parts[2]), nil
		}
	}
	return symbolOrString(expr, src)
}

func symbolOrStringList(expr hclsyntax.Expression, src []byte) ([]string, error) {
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

func stringListExpr(expr hclsyntax.Expression) ([]string, error) {
	value, diags := expr.Value(nil)
	if diags.HasErrors() {
		return nil, fmt.Errorf("%s", diags.Error())
	}
	if !value.CanIterateElements() {
		return nil, fmt.Errorf("expected list")
	}
	var out []string
	it := value.ElementIterator()
	for it.Next() {
		_, elem := it.Element()
		if elem.Type().FriendlyName() != "string" {
			return nil, fmt.Errorf("expected string, got %s", elem.Type().FriendlyName())
		}
		out = append(out, elem.AsString())
	}
	return out, nil
}
