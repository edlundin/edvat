package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseForeignTablesHCL(t *testing.T) {
	got, err := ParseForeignTablesHCL([]byte(`
schema "public" {}

foreign_table "remote_users" {
  schema = schema.public
  server = analytics
  column "id" { type = integer }
  column "email" { type = text }
  options = { schema_name = "public", table_name = "users" }
  comment = "remote users"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseForeignTablesHCL() error = %v", err)
	}
	want := ForeignTableState{"public.remote_users": {Name: "remote_users", Schema: "public", Server: "analytics", Columns: []ForeignColumn{{Name: "id", Type: "integer"}, {Name: "email", Type: "text"}}, Options: map[string]string{"schema_name": "public", "table_name": "users"}, Comment: "remote users"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseForeignTablesHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffForeignTablesCreate(t *testing.T) {
	statements := DiffForeignTables(nil, ForeignTableState{"public.remote_users": {Name: "remote_users", Schema: "public", Server: "analytics", Columns: []ForeignColumn{{Name: "id", Type: "integer"}}, Options: map[string]string{"table_name": "users"}, Comment: "remote users"}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE FOREIGN TABLE "public"."remote_users" ("id" integer) SERVER "analytics" OPTIONS ("table_name" 'users')`,
		`COMMENT ON FOREIGN TABLE "public"."remote_users" IS 'remote users'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffForeignTables() missing %q:\n%s", want, got)
		}
	}
	if statements[0].Reverse != `DROP FOREIGN TABLE "public"."remote_users"` {
		t.Fatalf("create reverse = %q", statements[0].Reverse)
	}
	if statements[1].Reverse != `COMMENT ON FOREIGN TABLE "public"."remote_users" IS NULL` {
		t.Fatalf("comment reverse = %q", statements[1].Reverse)
	}
}

func TestDiffForeignTablesDropHasReverse(t *testing.T) {
	statements := DiffForeignTables(ForeignTableState{"public.remote_users": {Name: "remote_users", Schema: "public", Server: "analytics", Columns: []ForeignColumn{{Name: "id", Type: "integer"}}, Options: map[string]string{"table_name": "users"}, Comment: "remote users"}}, nil)
	if len(statements) != 1 {
		t.Fatalf("DiffForeignTables() got %d statements, want 1", len(statements))
	}
	want := `CREATE FOREIGN TABLE "public"."remote_users" ("id" integer) SERVER "analytics" OPTIONS ("table_name" 'users');
COMMENT ON FOREIGN TABLE "public"."remote_users" IS 'remote users'`
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffForeignTablesCommentChangeHasReverse(t *testing.T) {
	statements := DiffForeignTables(
		ForeignTableState{"public.remote_users": {Name: "remote_users", Schema: "public", Server: "analytics", Columns: []ForeignColumn{{Name: "id", Type: "integer"}}, Comment: "old"}},
		ForeignTableState{"public.remote_users": {Name: "remote_users", Schema: "public", Server: "analytics", Columns: []ForeignColumn{{Name: "id", Type: "integer"}}, Comment: "new"}},
	)
	if len(statements) != 1 {
		t.Fatalf("DiffForeignTables() got %d statements, want 1", len(statements))
	}
	if statements[0].Reverse != `COMMENT ON FOREIGN TABLE "public"."remote_users" IS 'old'` {
		t.Fatalf("comment reverse = %q", statements[0].Reverse)
	}
}

func TestDiffForeignTablesReplace(t *testing.T) {
	statements := DiffForeignTables(
		ForeignTableState{"public.remote_users": {Name: "remote_users", Schema: "public", Server: "old", Columns: []ForeignColumn{{Name: "id", Type: "integer"}}}},
		ForeignTableState{"public.remote_users": {Name: "remote_users", Schema: "public", Server: "analytics", Columns: []ForeignColumn{{Name: "id", Type: "integer"}}}},
	)
	got := joinSQL(statements)
	for _, want := range []string{`DROP FOREIGN TABLE "public"."remote_users"`, `CREATE FOREIGN TABLE "public"."remote_users" ("id" integer) SERVER "analytics"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffForeignTables() missing %q:\n%s", want, got)
		}
	}
}
