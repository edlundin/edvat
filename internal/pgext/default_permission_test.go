package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseDefaultPermissionsHCL(t *testing.T) {
	got, err := ParseDefaultPermissionsHCL([]byte(`
schema "public" {}

default_permission "public_read_tables" {
  schema = schema.public
  for_role = app_owner
  on = TABLES
  to = public
  privileges = [SELECT]
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseDefaultPermissionsHCL() error = %v", err)
	}
	want := DefaultPermissionState{"public.app_owner.TABLES.public": {Name: "public_read_tables", Schema: "public", ForRole: "app_owner", On: "TABLES", Grantee: "public", Privileges: []string{"SELECT"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseDefaultPermissionsHCL() = %#v, want %#v", got, want)
	}
}

func TestDefaultPermissionIDUsesWildcardForCurrentRole(t *testing.T) {
	got := defaultPermissionID(DefaultPermission{Schema: "public", On: "TABLES", Grantee: "public"})
	want := "public.*.TABLES.public"
	if got != want {
		t.Fatalf("defaultPermissionID() = %q, want %q", got, want)
	}
}

func TestParseDefaultPermissionsHCLRejectsInvalidObjectType(t *testing.T) {
	_, err := ParseDefaultPermissionsHCL([]byte(`
schema "public" {}

default_permission "bad" {
  schema = schema.public
  on = "DATABASES"
  to = "reader"
  privileges = ["CONNECT"]
}
`), "schema.pg.hcl")
	if err == nil || !strings.Contains(err.Error(), "unsupported on") {
		t.Fatalf("ParseDefaultPermissionsHCL() error = %v, want unsupported on", err)
	}
}

func TestParseDefaultPermissionsHCLRejectsInvalidPrivilegeForObjectType(t *testing.T) {
	_, err := ParseDefaultPermissionsHCL([]byte(`
schema "public" {}

default_permission "bad" {
  schema = schema.public
  on = "TABLES"
  to = "reader"
  privileges = ["EXECUTE"]
}
`), "schema.pg.hcl")
	if err == nil || !strings.Contains(err.Error(), "unsupported privilege") {
		t.Fatalf("ParseDefaultPermissionsHCL() error = %v, want unsupported privilege", err)
	}
}

func TestParseDefaultPermissionsHCLFunctionsAndTypes(t *testing.T) {
	got, err := ParseDefaultPermissionsHCL([]byte(`
schema "public" {}

default_permission "execute_functions" {
  schema = schema.public
  for_role = app_owner
  on = FUNCTIONS
  to = reader
  privileges = [EXECUTE]
}

default_permission "use_types" {
  schema = schema.public
  for_role = app_owner
  on = TYPES
  to = reader
  privileges = [USAGE]
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseDefaultPermissionsHCL() error = %v", err)
	}
	for _, want := range []string{"public.app_owner.FUNCTIONS.reader", "public.app_owner.TYPES.reader"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("ParseDefaultPermissionsHCL() missing %s in %#v", want, got)
		}
	}
}

func TestDiffDefaultPermissionsCreateFunctionsAndTypes(t *testing.T) {
	statements := DiffDefaultPermissions(nil, DefaultPermissionState{
		"public.app_owner.FUNCTIONS.reader": {Schema: "public", ForRole: "app_owner", On: "FUNCTIONS", Grantee: "reader", Privileges: []string{"EXECUTE"}},
		"public.app_owner.TYPES.reader":     {Schema: "public", ForRole: "app_owner", On: "TYPES", Grantee: "reader", Privileges: []string{"USAGE"}},
	})
	got := joinSQL(statements)
	for _, want := range []string{
		`ALTER DEFAULT PRIVILEGES FOR ROLE "app_owner" IN SCHEMA "public" GRANT EXECUTE ON FUNCTIONS TO "reader"`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE "app_owner" IN SCHEMA "public" GRANT USAGE ON TYPES TO "reader"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffDefaultPermissions() missing %q:\n%s", want, got)
		}
	}
}

func TestDiffDefaultPermissionsCreateGrantOptionAndSequences(t *testing.T) {
	statements := DiffDefaultPermissions(nil, DefaultPermissionState{"public.app_owner.SEQUENCES.reader": {Schema: "public", ForRole: "app_owner", On: "SEQUENCES", Grantee: "reader", Privileges: []string{"USAGE", "SELECT"}, Grantable: true}})
	got := joinSQL(statements)
	want := `ALTER DEFAULT PRIVILEGES FOR ROLE "app_owner" IN SCHEMA "public" GRANT SELECT, USAGE ON SEQUENCES TO "reader" WITH GRANT OPTION`
	if !strings.Contains(got, want) {
		t.Fatalf("DiffDefaultPermissions() missing %q:\n%s", want, got)
	}
}

func TestDiffDefaultPermissionsCreate(t *testing.T) {
	statements := DiffDefaultPermissions(nil, DefaultPermissionState{"public.app_owner.TABLES.public": {Schema: "public", ForRole: "app_owner", On: "TABLES", Grantee: "public", Privileges: []string{"SELECT"}}})
	got := joinSQL(statements)
	want := `ALTER DEFAULT PRIVILEGES FOR ROLE "app_owner" IN SCHEMA "public" GRANT SELECT ON TABLES TO PUBLIC`
	if !strings.Contains(got, want) {
		t.Fatalf("DiffDefaultPermissions() missing %q:\n%s", want, got)
	}
}
