package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseUserMappingsHCL(t *testing.T) {
	got, err := ParseUserMappingsHCL([]byte(`
user_mapping {
  user = app_user
  server = analytics
  options = { user = "remote_user" }
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseUserMappingsHCL() error = %v", err)
	}
	want := UserMappingState{"app_user SERVER analytics": {User: "app_user", Server: "analytics", Options: map[string]string{"user": "remote_user"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseUserMappingsHCL() = %#v, want %#v", got, want)
	}
}

func TestParseUserMappingsHCLRejectsSecretOptions(t *testing.T) {
	_, err := ParseUserMappingsHCL([]byte(`
user_mapping {
  user = app_user
  server = analytics
  options = { password = "secret" }
}
`), "schema.pg.hcl")
	if err == nil || !strings.Contains(err.Error(), "looks secret") {
		t.Fatalf("ParseUserMappingsHCL() error = %v, want secret rejection", err)
	}
}

func TestDiffUserMappingsCreate(t *testing.T) {
	statements := DiffUserMappings(nil, UserMappingState{"app_user SERVER analytics": {User: "app_user", Server: "analytics", Options: map[string]string{"user": "remote_user"}}})
	got := joinSQL(statements)
	want := `CREATE USER MAPPING FOR "app_user" SERVER "analytics" OPTIONS ("user" 'remote_user')`
	if !strings.Contains(got, want) {
		t.Fatalf("DiffUserMappings() missing %q:\n%s", want, got)
	}
	if statements[0].Reverse != `DROP USER MAPPING FOR "app_user" SERVER "analytics"` {
		t.Fatalf("create reverse = %q", statements[0].Reverse)
	}
}

func TestDiffUserMappingsDropHasReverse(t *testing.T) {
	statements := DiffUserMappings(UserMappingState{"app_user SERVER analytics": {User: "app_user", Server: "analytics", Options: map[string]string{"user": "remote_user"}}}, nil)
	if len(statements) != 1 {
		t.Fatalf("DiffUserMappings() got %d statements, want 1", len(statements))
	}
	want := `CREATE USER MAPPING FOR "app_user" SERVER "analytics" OPTIONS ("user" 'remote_user')`
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffUserMappingsReplace(t *testing.T) {
	statements := DiffUserMappings(
		UserMappingState{"app_user SERVER analytics": {User: "app_user", Server: "analytics", Options: map[string]string{"user": "old"}}},
		UserMappingState{"app_user SERVER analytics": {User: "app_user", Server: "analytics", Options: map[string]string{"user": "new"}}},
	)
	got := joinSQL(statements)
	for _, want := range []string{`DROP USER MAPPING FOR "app_user" SERVER "analytics"`, `CREATE USER MAPPING FOR "app_user" SERVER "analytics" OPTIONS ("user" 'new')`} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffUserMappings() missing %q:\n%s", want, got)
		}
	}
	if statements[0].Reverse != `CREATE USER MAPPING FOR "app_user" SERVER "analytics" OPTIONS ("user" 'old')` {
		t.Fatalf("drop reverse = %q", statements[0].Reverse)
	}
	if statements[1].Reverse != `DROP USER MAPPING FOR "app_user" SERVER "analytics"` {
		t.Fatalf("create reverse = %q", statements[1].Reverse)
	}
}
