package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseServersHCL(t *testing.T) {
	got, err := ParseServersHCL([]byte(`
server "analytics" {
  fdw = postgres_fdw
  type = "postgres"
  version = "15"
  options = { host = "localhost", dbname = "analytics" }
  comment = "analytics db"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseServersHCL() error = %v", err)
	}
	want := ServerState{"analytics": {Name: "analytics", FDW: "postgres_fdw", Type: "postgres", Version: "15", Options: map[string]string{"host": "localhost", "dbname": "analytics"}, Comment: "analytics db"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseServersHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffServersCreate(t *testing.T) {
	statements := DiffServers(nil, ServerState{"analytics": {Name: "analytics", FDW: "postgres_fdw", Type: "postgres", Version: "15", Options: map[string]string{"host": "localhost"}, Comment: "analytics db"}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE SERVER "analytics" TYPE 'postgres' VERSION '15' FOREIGN DATA WRAPPER "postgres_fdw" OPTIONS ("host" 'localhost')`,
		`COMMENT ON SERVER "analytics" IS 'analytics db'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffServers() missing %q:\n%s", want, got)
		}
	}
	if statements[0].Reverse != `DROP SERVER "analytics"` {
		t.Fatalf("create reverse = %q", statements[0].Reverse)
	}
	if statements[1].Reverse != `COMMENT ON SERVER "analytics" IS NULL` {
		t.Fatalf("comment reverse = %q", statements[1].Reverse)
	}
}

func TestDiffServersDropHasReverse(t *testing.T) {
	statements := DiffServers(ServerState{"analytics": {Name: "analytics", FDW: "postgres_fdw", Type: "postgres", Version: "15", Options: map[string]string{"host": "localhost"}, Comment: "analytics db"}}, nil)
	if len(statements) != 1 {
		t.Fatalf("DiffServers() got %d statements, want 1", len(statements))
	}
	want := `CREATE SERVER "analytics" TYPE 'postgres' VERSION '15' FOREIGN DATA WRAPPER "postgres_fdw" OPTIONS ("host" 'localhost');
COMMENT ON SERVER "analytics" IS 'analytics db'`
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffServersCommentChangeHasReverse(t *testing.T) {
	statements := DiffServers(
		ServerState{"analytics": {Name: "analytics", FDW: "postgres_fdw", Comment: "old"}},
		ServerState{"analytics": {Name: "analytics", FDW: "postgres_fdw", Comment: "new"}},
	)
	if len(statements) != 1 {
		t.Fatalf("DiffServers() got %d statements, want 1", len(statements))
	}
	if statements[0].Reverse != `COMMENT ON SERVER "analytics" IS 'old'` {
		t.Fatalf("comment reverse = %q", statements[0].Reverse)
	}
}

func TestDiffServersReplace(t *testing.T) {
	statements := DiffServers(ServerState{"analytics": {Name: "analytics", FDW: "old_fdw"}}, ServerState{"analytics": {Name: "analytics", FDW: "postgres_fdw"}})
	got := joinSQL(statements)
	for _, want := range []string{`DROP SERVER "analytics"`, `CREATE SERVER "analytics" FOREIGN DATA WRAPPER "postgres_fdw"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffServers() missing %q:\n%s", want, got)
		}
	}
}
