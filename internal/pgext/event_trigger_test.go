package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseEventTriggersHCL(t *testing.T) {
	got, err := ParseEventTriggersHCL([]byte(`
schema "public" {}

event_trigger "audit_ddl" {
  on = ddl_command_end
  tags = ["CREATE TABLE", "ALTER TABLE"]
  execute = schema.public.audit_ddl
  comment = "audit ddl"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseEventTriggersHCL() error = %v", err)
	}
	want := EventTriggerState{"audit_ddl": {Name: "audit_ddl", Event: "ddl_command_end", Tags: []string{"CREATE TABLE", "ALTER TABLE"}, Function: `"public"."audit_ddl"`, Comment: "audit ddl"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseEventTriggersHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffEventTriggersCreate(t *testing.T) {
	statements := DiffEventTriggers(nil, EventTriggerState{"audit_ddl": {Name: "audit_ddl", Event: "ddl_command_end", Tags: []string{"CREATE TABLE"}, Function: `"public"."audit_ddl"`, Comment: "audit ddl"}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE EVENT TRIGGER "audit_ddl" ON ddl_command_end WHEN TAG IN ('CREATE TABLE') EXECUTE FUNCTION "public"."audit_ddl"()`,
		`COMMENT ON EVENT TRIGGER "audit_ddl" IS 'audit ddl'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffEventTriggers() missing %q:\n%s", want, got)
		}
	}
	if statements[0].Reverse != `DROP EVENT TRIGGER "audit_ddl"` {
		t.Fatalf("create reverse = %q", statements[0].Reverse)
	}
	if statements[1].Reverse != `COMMENT ON EVENT TRIGGER "audit_ddl" IS NULL` {
		t.Fatalf("comment reverse = %q", statements[1].Reverse)
	}
}

func TestDiffEventTriggersDropHasReverse(t *testing.T) {
	statements := DiffEventTriggers(EventTriggerState{"audit_ddl": {Name: "audit_ddl", Event: "ddl_command_end", Tags: []string{"CREATE TABLE"}, Function: `"public"."audit_ddl"`, Comment: "audit ddl"}}, nil)
	if len(statements) != 1 {
		t.Fatalf("DiffEventTriggers() got %d statements, want 1", len(statements))
	}
	want := `CREATE EVENT TRIGGER "audit_ddl" ON ddl_command_end WHEN TAG IN ('CREATE TABLE') EXECUTE FUNCTION "public"."audit_ddl"();
COMMENT ON EVENT TRIGGER "audit_ddl" IS 'audit ddl'`
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffEventTriggersCommentChangeHasReverse(t *testing.T) {
	statements := DiffEventTriggers(
		EventTriggerState{"audit_ddl": {Name: "audit_ddl", Event: "ddl_command_end", Function: `"public"."audit_ddl"`, Comment: "old"}},
		EventTriggerState{"audit_ddl": {Name: "audit_ddl", Event: "ddl_command_end", Function: `"public"."audit_ddl"`, Comment: "new"}},
	)
	if len(statements) != 1 {
		t.Fatalf("DiffEventTriggers() got %d statements, want 1", len(statements))
	}
	if statements[0].Reverse != `COMMENT ON EVENT TRIGGER "audit_ddl" IS 'old'` {
		t.Fatalf("comment reverse = %q", statements[0].Reverse)
	}
}

func TestDiffEventTriggersReplaceOnDefinitionChange(t *testing.T) {
	statements := DiffEventTriggers(
		EventTriggerState{"audit_ddl": {Name: "audit_ddl", Event: "ddl_command_end", Function: `"public"."old"`}},
		EventTriggerState{"audit_ddl": {Name: "audit_ddl", Event: "ddl_command_end", Function: `"public"."audit_ddl"`}},
	)
	got := joinSQL(statements)
	for _, want := range []string{
		`DROP EVENT TRIGGER "audit_ddl"`,
		`CREATE EVENT TRIGGER "audit_ddl" ON ddl_command_end EXECUTE FUNCTION "public"."audit_ddl"()`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffEventTriggers() missing %q:\n%s", want, got)
		}
	}
}
