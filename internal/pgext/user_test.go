package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseUsersHCL(t *testing.T) {
	got, err := ParseUsersHCL([]byte(`
user "app_user" {
  createdb = false
  createrole = false
  inherit = true
  valid_until = "infinity"
  comment = "application user"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseUsersHCL() error = %v", err)
	}
	want := UserState{"app_user": {Name: "app_user", Inherit: true, ValidUntil: "infinity", Comment: "application user"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseUsersHCL() = %#v, want %#v", got, want)
	}
}

func TestParseUsersHCLRejectsPassword(t *testing.T) {
	_, err := ParseUsersHCL([]byte(`user "app_user" { password = "secret" }`), "schema.pg.hcl")
	if err == nil || !strings.Contains(err.Error(), "secrets are not emitted") {
		t.Fatalf("ParseUsersHCL() error = %v, want secret rejection", err)
	}
}

func TestParseUsersHCLRejectsUnsupportedAttributes(t *testing.T) {
	_, err := ParseUsersHCL([]byte(`user "app_user" { superuser = true }`), "schema.pg.hcl")
	if err == nil || !strings.Contains(err.Error(), "user.app_user has unsupported attribute") {
		t.Fatalf("ParseUsersHCL() error = %v, want unsupported attribute", err)
	}
}

func TestDiffUsersCreate(t *testing.T) {
	statements := DiffUsers(nil, UserState{"app_user": {Name: "app_user", Inherit: true, ValidUntil: "infinity", Comment: "application user"}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE ROLE "app_user" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS VALID UNTIL 'infinity'`,
		`COMMENT ON ROLE "app_user" IS 'application user'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffUsers() missing %q:\n%s", want, got)
		}
	}
	if statements[0].Reverse != `DROP ROLE "app_user"` {
		t.Fatalf("create reverse = %q", statements[0].Reverse)
	}
	if statements[1].Reverse != `COMMENT ON ROLE "app_user" IS NULL` {
		t.Fatalf("comment reverse = %q", statements[1].Reverse)
	}
}

func TestDiffUsersDropHasReverse(t *testing.T) {
	statements := DiffUsers(UserState{"app_user": {Name: "app_user", Inherit: true, ValidUntil: "infinity", Comment: "application user"}}, nil)
	if len(statements) != 1 {
		t.Fatalf("DiffUsers() got %d statements, want 1", len(statements))
	}
	want := `CREATE ROLE "app_user" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS VALID UNTIL 'infinity';
COMMENT ON ROLE "app_user" IS 'application user'`
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffUsersAlter(t *testing.T) {
	statements := DiffUsers(UserState{"app_user": {Name: "app_user", Inherit: true}}, UserState{"app_user": {Name: "app_user", CreateDB: true, Inherit: true}})
	got := joinSQL(statements)
	want := `ALTER ROLE "app_user" LOGIN NOSUPERUSER CREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS`
	if !strings.Contains(got, want) {
		t.Fatalf("DiffUsers() missing %q:\n%s", want, got)
	}
	if statements[0].Reverse != `ALTER ROLE "app_user" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS` {
		t.Fatalf("alter reverse = %q", statements[0].Reverse)
	}
}

func TestDiffUsersCommentChangeHasReverse(t *testing.T) {
	statements := DiffUsers(
		UserState{"app_user": {Name: "app_user", Inherit: true, Comment: "old"}},
		UserState{"app_user": {Name: "app_user", Inherit: true, Comment: "new"}},
	)
	if len(statements) != 1 {
		t.Fatalf("DiffUsers() got %d statements, want 1", len(statements))
	}
	if statements[0].Reverse != `COMMENT ON ROLE "app_user" IS 'old'` {
		t.Fatalf("comment reverse = %q", statements[0].Reverse)
	}
}
