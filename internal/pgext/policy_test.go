package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParsePoliciesHCL(t *testing.T) {
	got, err := ParsePoliciesHCL([]byte(`
schema "public" {}

policy "own_rows" {
  on = schema.public.users
  for = SELECT
  to = [authenticated, public]
  using = "tenant_id = current_setting('app.tenant_id')::uuid"
  check = "tenant_id = current_setting('app.tenant_id')::uuid"
  restrictive = true
  comment = "tenant isolation"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParsePoliciesHCL() error = %v", err)
	}
	want := PolicyState{"public.users.own_rows": {
		Name: "own_rows", Schema: "public", Table: "users", Command: "SELECT", Roles: []string{"authenticated", "public"}, Using: "tenant_id = current_setting('app.tenant_id')::uuid", Check: "tenant_id = current_setting('app.tenant_id')::uuid", Restrictive: true, Comment: "tenant isolation",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParsePoliciesHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffPoliciesCreate(t *testing.T) {
	statements := DiffPolicies(nil, PolicyState{"public.users.own_rows": {
		Name: "own_rows", Schema: "public", Table: "users", Command: "SELECT", Roles: []string{"authenticated", "public"}, Using: "tenant_id = current_setting('app.tenant_id')::uuid", Comment: "tenant isolation",
	}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE POLICY "own_rows" ON "public"."users" FOR SELECT TO "authenticated", PUBLIC USING (tenant_id = current_setting('app.tenant_id')::uuid)`,
		`COMMENT ON POLICY "own_rows" ON "public"."users" IS 'tenant isolation'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffPolicies() missing %q:\n%s", want, got)
		}
	}
}

func TestDiffPoliciesTreatsInspectedCurrentSettingCastAsEquivalent(t *testing.T) {
	statements := DiffPolicies(
		PolicyState{"public.users.tenant_select": {Name: "tenant_select", Schema: "public", Table: "users", Command: "SELECT", Roles: []string{"public"}, Using: "(tenant_id = (current_setting('app.tenant_id'::text))::integer)"}},
		PolicyState{"public.users.tenant_select": {Name: "tenant_select", Schema: "public", Table: "users", Command: "SELECT", Roles: []string{"public"}, Using: "tenant_id = current_setting('app.tenant_id')::integer"}},
	)
	if len(statements) != 0 {
		t.Fatalf("DiffPolicies() = %#v, want no statements", statements)
	}
}

func TestDiffPoliciesCreateIncludesReverse(t *testing.T) {
	statements := DiffPolicies(nil, PolicyState{"public.users.own_rows": {Name: "own_rows", Schema: "public", Table: "users", Command: "SELECT", Using: "true", Comment: "tenant isolation"}})
	if len(statements) < 1 || statements[0].Reverse != `DROP POLICY "own_rows" ON "public"."users"` {
		t.Fatalf("Reverse = %q, want drop policy", statements[0].Reverse)
	}
	if len(statements) < 2 || statements[1].Reverse != `COMMENT ON POLICY "own_rows" ON "public"."users" IS NULL` {
		t.Fatalf("Comment reverse = %q, want clear policy comment", statements[1].Reverse)
	}
}

func TestDiffPoliciesReplaceOnDefinitionChange(t *testing.T) {
	statements := DiffPolicies(
		PolicyState{"public.users.own_rows": {Name: "own_rows", Schema: "public", Table: "users", Command: "SELECT", Using: "true"}},
		PolicyState{"public.users.own_rows": {Name: "own_rows", Schema: "public", Table: "users", Command: "SELECT", Using: "id > 0"}},
	)
	got := joinSQL(statements)
	for _, want := range []string{
		`DROP POLICY "own_rows" ON "public"."users"`,
		`CREATE POLICY "own_rows" ON "public"."users" FOR SELECT USING (id > 0)`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffPolicies() missing %q:\n%s", want, got)
		}
	}
}
