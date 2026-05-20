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

type EventTrigger struct {
	Name     string
	Event    string
	Tags     []string
	Function string
	Comment  string
}

type EventTriggerState map[string]EventTrigger

func ParseEventTriggerFiles(paths []string) (EventTriggerState, error) {
	return parseStateFiles(paths, "event trigger", ParseEventTriggersHCL)
}

func ParseEventTriggersHCL(src []byte, filename string) (EventTriggerState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse event trigger hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse event trigger hcl: unexpected body type %T", file.Body)
	}

	state := EventTriggerState{}
	for _, block := range body.Blocks {
		if block.Type != "event_trigger" || len(block.Labels) != 1 {
			continue
		}
		trigger := EventTrigger{Name: block.Labels[0]}
		attrs := block.Body.Attributes
		if attr, ok := attrs["on"]; ok {
			event, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode event_trigger.%s.on: %w", trigger.Name, err)
			}
			trigger.Event = event
		}
		if attr, ok := attrs["tags"]; ok {
			tags, err := stringListExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode event_trigger.%s.tags: %w", trigger.Name, err)
			}
			trigger.Tags = tags
		}
		if attr, ok := attrs["execute"]; ok {
			execute, err := functionExpr(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode event_trigger.%s.execute: %w", trigger.Name, err)
			}
			trigger.Function = execute
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode event_trigger.%s.comment: %w", trigger.Name, err)
			}
			trigger.Comment = comment
		}
		if trigger.Event == "" {
			return nil, fmt.Errorf("event_trigger.%s requires on", trigger.Name)
		}
		if trigger.Function == "" {
			return nil, fmt.Errorf("event_trigger.%s requires execute", trigger.Name)
		}
		state[eventTriggerID(trigger)] = trigger
	}
	return state, nil
}

func InspectEventTriggersURL(ctx context.Context, url string) (EventTriggerState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectEventTriggers(ctx, db)
}

func InspectEventTriggers(ctx context.Context, db *sql.DB) (EventTriggerState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT e.evtname,
       e.evtevent,
       COALESCE(array_to_string(e.evttags, ','), ''),
       pn.nspname || '.' || p.proname,
       COALESCE(d.description, '')
FROM pg_event_trigger e
JOIN pg_proc p ON p.oid = e.evtfoid
JOIN pg_namespace pn ON pn.oid = p.pronamespace
LEFT JOIN pg_description d ON d.objoid = e.oid AND d.classoid = 'pg_event_trigger'::regclass AND d.objsubid = 0
ORDER BY e.evtname`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres event triggers: %w", err)
	}
	defer rows.Close()
	state := EventTriggerState{}
	for rows.Next() {
		var trigger EventTrigger
		var tags string
		if err := rows.Scan(&trigger.Name, &trigger.Event, &tags, &trigger.Function, &trigger.Comment); err != nil {
			return nil, fmt.Errorf("scan postgres event trigger: %w", err)
		}
		trigger.Tags = splitCSV(tags)
		state[eventTriggerID(trigger)] = trigger
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres event triggers: %w", err)
	}
	return state, nil
}

func DiffEventTriggers(current, desired EventTriggerState) []baseatlas.Statement {
	if current == nil {
		current = EventTriggerState{}
	}
	if desired == nil {
		desired = EventTriggerState{}
	}
	ids := stateIDs(current, desired)

	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createEventTriggerStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, dropEventTriggerStatement(cur))
		case hasCurrent && hasDesired:
			if !sameEventTriggerDefinition(cur, des) {
				statements = append(statements, dropEventTriggerStatement(cur))
				statements = append(statements, createEventTriggerStatements(des)...)
				continue
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on event trigger " + des.Name, SQL: commentEventTriggerSQL(des), Reverse: commentEventTriggerSQL(cur)})
			}
		}
	}
	return statements
}

func createEventTriggerStatements(trigger EventTrigger) []baseatlas.Statement {
	statements := []baseatlas.Statement{{Comment: "create event trigger " + trigger.Name, SQL: createEventTriggerSQL(trigger), Reverse: "DROP EVENT TRIGGER " + quoteIdent(trigger.Name)}}
	if trigger.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on event trigger " + trigger.Name, SQL: commentEventTriggerSQL(trigger), Reverse: "COMMENT ON EVENT TRIGGER " + quoteIdent(trigger.Name) + " IS NULL"})
	}
	return statements
}

func dropEventTriggerStatement(trigger EventTrigger) baseatlas.Statement {
	return baseatlas.Statement{Comment: "drop event trigger " + trigger.Name + " (destructive)", SQL: "DROP EVENT TRIGGER " + quoteIdent(trigger.Name), Reverse: strings.Join(eventTriggerSQL(trigger), ";\n")}
}

func eventTriggerSQL(trigger EventTrigger) []string {
	statements := []string{createEventTriggerSQL(trigger)}
	if trigger.Comment != "" {
		statements = append(statements, commentEventTriggerSQL(trigger))
	}
	return statements
}

func createEventTriggerSQL(trigger EventTrigger) string {
	var b strings.Builder
	b.WriteString("CREATE EVENT TRIGGER ")
	b.WriteString(quoteIdent(trigger.Name))
	b.WriteString(" ON ")
	b.WriteString(trigger.Event)
	if len(trigger.Tags) > 0 {
		b.WriteString(" WHEN TAG IN (")
		for i, tag := range trigger.Tags {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(literal(tag))
		}
		b.WriteString(")")
	}
	b.WriteString(" EXECUTE FUNCTION ")
	b.WriteString(trigger.Function)
	b.WriteString("()")
	return b.String()
}

func commentEventTriggerSQL(trigger EventTrigger) string {
	return "COMMENT ON EVENT TRIGGER " + quoteIdent(trigger.Name) + " IS " + nullableLiteral(trigger.Comment)
}

func sameEventTriggerDefinition(a, b EventTrigger) bool {
	return a.Event == b.Event && strings.Join(a.Tags, ",") == strings.Join(b.Tags, ",") && normalizeSQL(a.Function) == normalizeSQL(b.Function)
}

func eventTriggerID(trigger EventTrigger) string { return trigger.Name }

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, strings.TrimSpace(part))
	}
	return out
}
