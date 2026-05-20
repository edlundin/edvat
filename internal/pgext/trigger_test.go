package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseTriggersHCL(t *testing.T) {
	got, err := ParseTriggersHCL([]byte(`
schema "public" {}

table "users" {
  schema = schema.public
}

trigger "set_updated_at" {
  on = schema.public.users
  timing = BEFORE
  events = [UPDATE]
  execute = schema.public.touch_updated_at
  for_each = ROW
  args = ["updated_at"]
  when = "OLD.* IS DISTINCT FROM NEW.*"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseTriggersHCL() error = %v", err)
	}
	want := TriggerState{"public.users.set_updated_at": {
		Name: "set_updated_at", Schema: "public", Table: "users", Timing: "BEFORE", Events: []string{"UPDATE"}, Function: `"public"."touch_updated_at"`, Args: []string{"updated_at"}, When: "OLD.* IS DISTINCT FROM NEW.*", ForEach: "ROW",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseTriggersHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffTriggersCreate(t *testing.T) {
	statements := DiffTriggers(nil, TriggerState{"public.users.set_updated_at": {
		Name: "set_updated_at", Schema: "public", Table: "users", Timing: "BEFORE", Events: []string{"INSERT", "UPDATE"}, Function: `"public"."touch_updated_at"`, Args: []string{"updated_at"},
	}})
	got := joinSQL(statements)
	want := `CREATE TRIGGER "set_updated_at" BEFORE INSERT OR UPDATE ON "public"."users" EXECUTE FUNCTION "public"."touch_updated_at"('updated_at')`
	if !strings.Contains(got, want) {
		t.Fatalf("DiffTriggers() missing %q:\n%s", want, got)
	}
}

func TestDiffTriggersCreateForEachRow(t *testing.T) {
	statements := DiffTriggers(nil, TriggerState{"public.users.set_updated_at": {
		Name: "set_updated_at", Schema: "public", Table: "users", Timing: "BEFORE", Events: []string{"UPDATE"}, Function: `"public"."touch_updated_at"`, ForEach: "ROW", When: "OLD.email IS DISTINCT FROM NEW.email",
	}})
	got := joinSQL(statements)
	want := `CREATE TRIGGER "set_updated_at" BEFORE UPDATE ON "public"."users" FOR EACH ROW WHEN (OLD.email IS DISTINCT FROM NEW.email) EXECUTE FUNCTION "public"."touch_updated_at"()`
	if !strings.Contains(got, want) {
		t.Fatalf("DiffTriggers() missing %q:\n%s", want, got)
	}
}

func TestParseTriggerDef(t *testing.T) {
	got, err := parseTriggerDef(Trigger{Schema: "public", Table: "users"}, `CREATE CONSTRAINT TRIGGER audit_users AFTER UPDATE OF name, email ON public.users DEFERRABLE INITIALLY DEFERRED REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows WHEN ((old.* IS DISTINCT FROM new.*)) EXECUTE FUNCTION public.audit_users('changed')`)
	if err != nil {
		t.Fatalf("parseTriggerDef() error = %v", err)
	}
	want := Trigger{Name: "audit_users", Schema: "public", Table: "users", Timing: "AFTER", Events: []string{"UPDATE"}, UpdateColumns: []string{"name", "email"}, Function: "public.audit_users", Args: []string{"changed"}, When: "(old.* IS DISTINCT FROM new.*)", OldTable: "old_rows", NewTable: "new_rows", Constraint: true, Deferrable: true, Initially: "DEFERRED"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseTriggerDef() = %#v, want %#v", got, want)
	}
}

func TestParseTriggerDefForEachRow(t *testing.T) {
	got, err := parseTriggerDef(Trigger{Schema: "public", Table: "users"}, `CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.users FOR EACH ROW WHEN ((old.email IS DISTINCT FROM new.email)) EXECUTE FUNCTION public.touch_updated_at('changed')`)
	if err != nil {
		t.Fatalf("parseTriggerDef() error = %v", err)
	}
	want := Trigger{Name: "set_updated_at", Schema: "public", Table: "users", Timing: "BEFORE", Events: []string{"UPDATE"}, Function: "public.touch_updated_at", Args: []string{"changed"}, When: "(old.email IS DISTINCT FROM new.email)", ForEach: "ROW"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseTriggerDef() = %#v, want %#v", got, want)
	}
}

func TestDiffTriggersCreateWithUpdateColumnsAndConstraintOptions(t *testing.T) {
	statements := DiffTriggers(nil, TriggerState{"public.users.audit_users": {
		Name: "audit_users", Schema: "public", Table: "users", Timing: "AFTER", Events: []string{"UPDATE"}, UpdateColumns: []string{"name", "email"}, Function: `"public"."audit_users"`, OldTable: "old_rows", NewTable: "new_rows", Constraint: true, Deferrable: true, Initially: "DEFERRED",
	}})
	got := joinSQL(statements)
	want := `CREATE CONSTRAINT TRIGGER "audit_users" AFTER UPDATE OF "name", "email" ON "public"."users" DEFERRABLE INITIALLY DEFERRED REFERENCING OLD TABLE AS "old_rows" NEW TABLE AS "new_rows" EXECUTE FUNCTION "public"."audit_users"()`
	if !strings.Contains(got, want) {
		t.Fatalf("DiffTriggers() missing %q:\n%s", want, got)
	}
}

func TestDiffTriggersTreatsInspectedOldNewTextCastsAsEquivalent(t *testing.T) {
	statements := DiffTriggers(
		TriggerState{"public.users.set_updated_at": {Name: "set_updated_at", Schema: "public", Table: "users", Timing: "BEFORE", Events: []string{"UPDATE"}, UpdateColumns: []string{"email"}, Function: `public.touch_updated_at`, Args: []string{"changed"}, When: "old.email::text IS DISTINCT FROM new.email::text", ForEach: "ROW"}},
		TriggerState{"public.users.set_updated_at": {Name: "set_updated_at", Schema: "public", Table: "users", Timing: "BEFORE", Events: []string{"UPDATE"}, UpdateColumns: []string{"email"}, Function: `"public"."touch_updated_at"`, Args: []string{"changed"}, When: "OLD.email IS DISTINCT FROM NEW.email", ForEach: "ROW"}},
	)
	if len(statements) != 0 {
		t.Fatalf("DiffTriggers() = %#v, want no statements", statements)
	}
}

func TestDiffTriggersCreateIncludesReverse(t *testing.T) {
	statements := DiffTriggers(nil, TriggerState{"public.users.set_updated_at": {Name: "set_updated_at", Schema: "public", Table: "users", Timing: "BEFORE", Events: []string{"UPDATE"}, Function: `"public"."touch_updated_at"`}})
	if len(statements) == 0 || statements[0].Reverse != `DROP TRIGGER "set_updated_at" ON "public"."users"` {
		t.Fatalf("Reverse = %q, want drop trigger", statements[0].Reverse)
	}
}

func TestDiffTriggersReplace(t *testing.T) {
	statements := DiffTriggers(
		TriggerState{"public.users.set_updated_at": {Name: "set_updated_at", Schema: "public", Table: "users", Timing: "AFTER", Events: []string{"UPDATE"}, Function: `"public"."touch_updated_at"`}},
		TriggerState{"public.users.set_updated_at": {Name: "set_updated_at", Schema: "public", Table: "users", Timing: "BEFORE", Events: []string{"UPDATE"}, Function: `"public"."touch_updated_at"`}},
	)
	got := joinSQL(statements)
	for _, want := range []string{
		`DROP TRIGGER "set_updated_at" ON "public"."users"`,
		`CREATE TRIGGER "set_updated_at" BEFORE UPDATE ON "public"."users" EXECUTE FUNCTION "public"."touch_updated_at"()`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffTriggers() missing %q:\n%s", want, got)
		}
	}
}
