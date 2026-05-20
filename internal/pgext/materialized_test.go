package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseMaterializedViewsHCL(t *testing.T) {
	got, err := ParseMaterializedViewsHCL([]byte(`
schema "public" {}

materialized "user_stats" {
  schema = schema.public
  as = "SELECT count(*) AS total FROM users"
  comment = "user statistics"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseMaterializedViewsHCL() error = %v", err)
	}
	want := MaterializedViewState{"public.user_stats": {Name: "user_stats", Schema: "public", SQL: "SELECT count(*) AS total FROM users", Comment: "user statistics"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseMaterializedViewsHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffMaterializedViewsCreate(t *testing.T) {
	statements := DiffMaterializedViews(nil, MaterializedViewState{"public.user_stats": {Name: "user_stats", Schema: "public", SQL: "SELECT count(*) AS total FROM users", Comment: "user statistics"}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE MATERIALIZED VIEW "public"."user_stats" AS`,
		`SELECT count(*) AS total FROM users`,
		`COMMENT ON MATERIALIZED VIEW "public"."user_stats" IS 'user statistics'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffMaterializedViews() missing %q:\n%s", want, got)
		}
	}
	if statements[0].Reverse != `DROP MATERIALIZED VIEW "public"."user_stats"` {
		t.Fatalf("create reverse = %q", statements[0].Reverse)
	}
	if statements[1].Reverse != `COMMENT ON MATERIALIZED VIEW "public"."user_stats" IS NULL` {
		t.Fatalf("comment reverse = %q", statements[1].Reverse)
	}
}

func TestDiffMaterializedViewsDropHasReverse(t *testing.T) {
	statements := DiffMaterializedViews(MaterializedViewState{"public.user_stats": {Name: "user_stats", Schema: "public", SQL: "SELECT count(*) AS total FROM users", Comment: "user statistics"}}, nil)
	if len(statements) != 1 {
		t.Fatalf("DiffMaterializedViews() got %d statements, want 1", len(statements))
	}
	want := `CREATE MATERIALIZED VIEW "public"."user_stats" AS
SELECT count(*) AS total FROM users;
COMMENT ON MATERIALIZED VIEW "public"."user_stats" IS 'user statistics'`
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffMaterializedViewsCommentChangeHasReverse(t *testing.T) {
	statements := DiffMaterializedViews(
		MaterializedViewState{"public.user_stats": {Name: "user_stats", Schema: "public", SQL: "SELECT 1", Comment: "old"}},
		MaterializedViewState{"public.user_stats": {Name: "user_stats", Schema: "public", SQL: "SELECT 1", Comment: "new"}},
	)
	if len(statements) != 1 {
		t.Fatalf("DiffMaterializedViews() got %d statements, want 1", len(statements))
	}
	if statements[0].Reverse != `COMMENT ON MATERIALIZED VIEW "public"."user_stats" IS 'old'` {
		t.Fatalf("comment reverse = %q", statements[0].Reverse)
	}
}

func TestDiffMaterializedViewsReplaceOnDefinitionChange(t *testing.T) {
	statements := DiffMaterializedViews(
		MaterializedViewState{"public.user_stats": {Name: "user_stats", Schema: "public", SQL: "SELECT 1"}},
		MaterializedViewState{"public.user_stats": {Name: "user_stats", Schema: "public", SQL: "SELECT 2"}},
	)
	got := joinSQL(statements)
	for _, want := range []string{
		`DROP MATERIALIZED VIEW "public"."user_stats"`,
		`CREATE MATERIALIZED VIEW "public"."user_stats" AS`,
		`SELECT 2`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffMaterializedViews() missing %q:\n%s", want, got)
		}
	}
}
