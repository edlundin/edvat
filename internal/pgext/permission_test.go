package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParsePermissionsHCL(t *testing.T) {
	got, err := ParsePermissionsHCL([]byte(`
schema "public" {}

permission "read_users" {
  on = schema.public.users
  to = authenticated
  privileges = [SELECT, INSERT]
  grantable = true
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParsePermissionsHCL() error = %v", err)
	}
	want := PermissionState{`TABLE "public"."users" TO authenticated`: {Name: "read_users", Target: `TABLE "public"."users"`, Grantee: "authenticated", Privileges: []string{"SELECT", "INSERT"}, Grantable: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParsePermissionsHCL() = %#v, want %#v", got, want)
	}
}

func TestParsePermissionsHCLSchemaTarget(t *testing.T) {
	got, err := ParsePermissionsHCL([]byte(`
permission "schema_usage" {
  on = schema.app
  to = authenticated
  privileges = [USAGE]
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParsePermissionsHCL() error = %v", err)
	}
	want := PermissionState{`SCHEMA "app" TO authenticated`: {Name: "schema_usage", Target: `SCHEMA "app"`, Grantee: "authenticated", Privileges: []string{"USAGE"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParsePermissionsHCL() = %#v, want %#v", got, want)
	}
}

func TestParsePermissionsHCLSequenceTarget(t *testing.T) {
	got, err := ParsePermissionsHCL([]byte(`
permission "sequence_usage" {
  on = sequence.public.order_seq
  to = authenticated
  privileges = [USAGE, SELECT]
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParsePermissionsHCL() error = %v", err)
	}
	want := PermissionState{`SEQUENCE "public"."order_seq" TO authenticated`: {Name: "sequence_usage", Target: `SEQUENCE "public"."order_seq"`, Grantee: "authenticated", Privileges: []string{"USAGE", "SELECT"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParsePermissionsHCL() = %#v, want %#v", got, want)
	}
}

func TestParsePermissionsHCLTypeTarget(t *testing.T) {
	got, err := ParsePermissionsHCL([]byte(`
permission "type_usage" {
  on = type.public.email_domain
  to = authenticated
  privileges = [USAGE]
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParsePermissionsHCL() error = %v", err)
	}
	want := PermissionState{`TYPE "public"."email_domain" TO authenticated`: {Name: "type_usage", Target: `TYPE "public"."email_domain"`, Grantee: "authenticated", Privileges: []string{"USAGE"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParsePermissionsHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffPermissionsTypeGrant(t *testing.T) {
	statements := DiffPermissions(nil, PermissionState{`TYPE "public"."email_domain" TO authenticated`: {Target: `TYPE "public"."email_domain"`, Grantee: "authenticated", Privileges: []string{"USAGE"}}})
	got := joinSQL(statements)
	want := `GRANT USAGE ON TYPE "public"."email_domain" TO "authenticated"`
	if !strings.Contains(got, want) {
		t.Fatalf("DiffPermissions() missing %q:\n%s", want, got)
	}
}

func TestParsePermissionsHCLDatabaseTarget(t *testing.T) {
	got, err := ParsePermissionsHCL([]byte(`
permission "database_connect" {
  on = database.appdb
  to = authenticated
  privileges = [CONNECT]
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParsePermissionsHCL() error = %v", err)
	}
	want := PermissionState{`DATABASE "appdb" TO authenticated`: {Name: "database_connect", Target: `DATABASE "appdb"`, Grantee: "authenticated", Privileges: []string{"CONNECT"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParsePermissionsHCL() = %#v, want %#v", got, want)
	}
}

func TestParsePermissionsHCLForeignServerTarget(t *testing.T) {
	got, err := ParsePermissionsHCL([]byte(`
permission "server_usage" {
  on = server.analytics
  to = authenticated
  privileges = [USAGE]
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParsePermissionsHCL() error = %v", err)
	}
	want := PermissionState{`FOREIGN SERVER "analytics" TO authenticated`: {Name: "server_usage", Target: `FOREIGN SERVER "analytics"`, Grantee: "authenticated", Privileges: []string{"USAGE"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParsePermissionsHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffPermissionsDatabaseAndForeignServerGrant(t *testing.T) {
	statements := DiffPermissions(nil, PermissionState{
		`DATABASE "appdb" TO authenticated`:           {Target: `DATABASE "appdb"`, Grantee: "authenticated", Privileges: []string{"CONNECT"}},
		`FOREIGN SERVER "analytics" TO authenticated`: {Target: `FOREIGN SERVER "analytics"`, Grantee: "authenticated", Privileges: []string{"USAGE"}},
	})
	got := joinSQL(statements)
	for _, want := range []string{
		`GRANT CONNECT ON DATABASE "appdb" TO "authenticated"`,
		`GRANT USAGE ON FOREIGN SERVER "analytics" TO "authenticated"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffPermissions() missing %q:\n%s", want, got)
		}
	}
}

func TestAddInspectedPermissionMergesPrivileges(t *testing.T) {
	state := PermissionState{}
	addInspectedPermission(state, Permission{Target: `SCHEMA "app"`, Grantee: "reader", Privileges: []string{"CREATE"}})
	addInspectedPermission(state, Permission{Target: `SCHEMA "app"`, Grantee: "reader", Privileges: []string{"USAGE"}})
	got := state[`SCHEMA "app" TO reader`].Privileges
	want := []string{"CREATE", "USAGE"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("privileges = %#v, want %#v", got, want)
	}
}

func TestDiffPermissionsRoutineGrant(t *testing.T) {
	statements := DiffPermissions(nil, PermissionState{`FUNCTION "public"."touch"(integer) TO authenticated`: {Target: `FUNCTION "public"."touch"(integer)`, Grantee: "authenticated", Privileges: []string{"EXECUTE"}}})
	got := joinSQL(statements)
	want := `GRANT EXECUTE ON FUNCTION "public"."touch"(integer) TO "authenticated"`
	if !strings.Contains(got, want) {
		t.Fatalf("DiffPermissions() missing %q:\n%s", want, got)
	}
}

func TestDiffPermissionsGrant(t *testing.T) {
	statements := DiffPermissions(nil, PermissionState{`TABLE "public"."users" TO authenticated`: {Target: `TABLE "public"."users"`, Grantee: "authenticated", Privileges: []string{"SELECT"}, Grantable: true}})
	got := joinSQL(statements)
	want := `GRANT SELECT ON TABLE "public"."users" TO "authenticated" WITH GRANT OPTION`
	if !strings.Contains(got, want) {
		t.Fatalf("DiffPermissions() missing %q:\n%s", want, got)
	}
}

func TestDiffPermissionsGrantIncludesReverse(t *testing.T) {
	statements := DiffPermissions(nil, PermissionState{`TABLE "public"."users" TO authenticated`: {Target: `TABLE "public"."users"`, Grantee: "authenticated", Privileges: []string{"SELECT"}}})
	if len(statements) == 0 || statements[0].Reverse != `REVOKE SELECT ON TABLE "public"."users" FROM "authenticated"` {
		t.Fatalf("Reverse = %q, want revoke permission", statements[0].Reverse)
	}
}

func TestDiffPermissionsIgnoresPrivilegeOrder(t *testing.T) {
	statements := DiffPermissions(
		PermissionState{`SEQUENCE "public"."order_seq" TO authenticated`: {Target: `SEQUENCE "public"."order_seq"`, Grantee: "authenticated", Privileges: []string{"SELECT", "USAGE"}}},
		PermissionState{`SEQUENCE "public"."order_seq" TO authenticated`: {Target: `SEQUENCE "public"."order_seq"`, Grantee: "authenticated", Privileges: []string{"USAGE", "SELECT"}}},
	)
	if len(statements) != 0 {
		t.Fatalf("DiffPermissions() = %#v, want no churn for privilege order", statements)
	}
}

func TestDiffPermissionsReplace(t *testing.T) {
	statements := DiffPermissions(
		PermissionState{`TABLE "public"."users" TO authenticated`: {Target: `TABLE "public"."users"`, Grantee: "authenticated", Privileges: []string{"SELECT"}}},
		PermissionState{`TABLE "public"."users" TO authenticated`: {Target: `TABLE "public"."users"`, Grantee: "authenticated", Privileges: []string{"INSERT"}}},
	)
	got := joinSQL(statements)
	for _, want := range []string{
		`REVOKE SELECT ON TABLE "public"."users" FROM "authenticated"`,
		`GRANT INSERT ON TABLE "public"."users" TO "authenticated"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffPermissions() missing %q:\n%s", want, got)
		}
	}
}
