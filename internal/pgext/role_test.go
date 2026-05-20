package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseRolesHCL(t *testing.T) {
	got, err := ParseRolesHCL([]byte(`
role "app_user" {
  login = true
  createdb = false
  createrole = false
  inherit = true
  comment = "application user"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseRolesHCL() error = %v", err)
	}
	want := RoleState{"app_user": {Name: "app_user", Login: true, Inherit: true, Comment: "application user"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseRolesHCL() = %#v, want %#v", got, want)
	}
}

func TestParseRolesHCLRejectsUnsupportedAttributes(t *testing.T) {
	_, err := ParseRolesHCL([]byte(`
role "app_user" {
  password = "secret"
}
`), "schema.pg.hcl")
	if err == nil || !strings.Contains(err.Error(), "role.app_user has unsupported attribute") {
		t.Fatalf("ParseRolesHCL() error = %v, want unsupported attribute", err)
	}
}

func TestDiffRolesCreate(t *testing.T) {
	statements := DiffRoles(nil, RoleState{"app_user": {Name: "app_user", Login: true, Inherit: true, Comment: "application user"}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE ROLE "app_user" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS`,
		`COMMENT ON ROLE "app_user" IS 'application user'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffRoles() missing %q:\n%s", want, got)
		}
	}
	if statements[0].Reverse != `DROP ROLE "app_user"` {
		t.Fatalf("create reverse = %q", statements[0].Reverse)
	}
	if statements[1].Reverse != `COMMENT ON ROLE "app_user" IS NULL` {
		t.Fatalf("comment reverse = %q", statements[1].Reverse)
	}
}

func TestDiffRolesDropHasReverse(t *testing.T) {
	statements := DiffRoles(RoleState{"app_user": {Name: "app_user", Login: true, Inherit: true, Comment: "application user"}}, nil)
	if len(statements) != 1 {
		t.Fatalf("DiffRoles() got %d statements, want 1", len(statements))
	}
	want := `CREATE ROLE "app_user" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS;
COMMENT ON ROLE "app_user" IS 'application user'`
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffRolesAlter(t *testing.T) {
	statements := DiffRoles(
		RoleState{"app_user": {Name: "app_user", Inherit: true}},
		RoleState{"app_user": {Name: "app_user", Login: true, Inherit: true}},
	)
	got := joinSQL(statements)
	want := `ALTER ROLE "app_user" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS`
	if !strings.Contains(got, want) {
		t.Fatalf("DiffRoles() missing %q:\n%s", want, got)
	}
	if statements[0].Reverse != `ALTER ROLE "app_user" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS` {
		t.Fatalf("alter reverse = %q", statements[0].Reverse)
	}
}

func TestDiffRolesCommentChangeHasReverse(t *testing.T) {
	statements := DiffRoles(
		RoleState{"app_user": {Name: "app_user", Inherit: true, Comment: "old"}},
		RoleState{"app_user": {Name: "app_user", Inherit: true, Comment: "new"}},
	)
	if len(statements) != 1 {
		t.Fatalf("DiffRoles() got %d statements, want 1", len(statements))
	}
	if statements[0].Reverse != `COMMENT ON ROLE "app_user" IS 'old'` {
		t.Fatalf("comment reverse = %q", statements[0].Reverse)
	}
}
