package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseViewsHCL(t *testing.T) {
	got, err := ParseViewsHCL([]byte(`
schema "public" {}

view "active_users" {
  schema = schema.public
  as = <<-SQL
    SELECT id, name FROM users WHERE active
  SQL
  comment = "active users only"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseViewsHCL() error = %v", err)
	}
	want := ViewState{"public.active_users": {Name: "active_users", Schema: "public", SQL: "SELECT id, name FROM users WHERE active", Comment: "active users only"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseViewsHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffViewsCreate(t *testing.T) {
	statements := DiffViews(nil, ViewState{"public.active_users": {Name: "active_users", Schema: "public", SQL: "SELECT id FROM users", Comment: "active"}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE VIEW "public"."active_users" AS`,
		`SELECT id FROM users`,
		`COMMENT ON VIEW "public"."active_users" IS 'active'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffViews() missing %q:\n%s", want, got)
		}
	}
}

func TestDiffViewsIgnoresInspectedSelectListQualifier(t *testing.T) {
	statements := DiffViews(
		ViewState{"public.active_users": {Name: "active_users", Schema: "public", SQL: "SELECT users.email FROM public.users;"}},
		ViewState{"public.active_users": {Name: "active_users", Schema: "public", SQL: "SELECT email FROM public.users"}},
	)
	if len(statements) != 0 {
		t.Fatalf("DiffViews() = %#v, want no statements", statements)
	}
}

func TestDiffViewsIgnoresInspectedTrailingSemicolon(t *testing.T) {
	statements := DiffViews(
		ViewState{"public.active_users": {Name: "active_users", Schema: "public", SQL: "SELECT users.email FROM public.users;"}},
		ViewState{"public.active_users": {Name: "active_users", Schema: "public", SQL: "SELECT users.email FROM public.users"}},
	)
	if len(statements) != 0 {
		t.Fatalf("DiffViews() = %#v, want no statements", statements)
	}
}

func TestDiffViewsCreateCommentIncludesReverse(t *testing.T) {
	statements := DiffViews(nil, ViewState{"public.active_users": {Name: "active_users", Schema: "public", SQL: "SELECT id FROM users", Comment: "active"}})
	if len(statements) < 2 || statements[1].Reverse != `COMMENT ON VIEW "public"."active_users" IS NULL` {
		t.Fatalf("Reverse = %q, want clear view comment", statements[1].Reverse)
	}
}

func TestDiffViewsCreateIncludesReverse(t *testing.T) {
	statements := DiffViews(nil, ViewState{"public.active_users": {Name: "active_users", Schema: "public", SQL: "SELECT id FROM users"}})
	if len(statements) == 0 || statements[0].Reverse != `DROP VIEW "public"."active_users"` {
		t.Fatalf("Reverse = %q, want drop view", statements[0].Reverse)
	}
}

func TestDiffViewsReplaceAndComment(t *testing.T) {
	statements := DiffViews(
		ViewState{"public.active_users": {Name: "active_users", Schema: "public", SQL: "SELECT id FROM users", Comment: "old"}},
		ViewState{"public.active_users": {Name: "active_users", Schema: "public", SQL: "SELECT id, name FROM users", Comment: "new"}},
	)
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE OR REPLACE VIEW "public"."active_users" AS`,
		`SELECT id, name FROM users`,
		`COMMENT ON VIEW "public"."active_users" IS 'new'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffViews() missing %q:\n%s", want, got)
		}
	}
}

func TestDiffViewsDrop(t *testing.T) {
	statements := DiffViews(ViewState{"public.active_users": {Name: "active_users", Schema: "public"}}, nil)
	got := joinSQL(statements)
	if !strings.Contains(got, `DROP VIEW "public"."active_users"`) {
		t.Fatalf("DiffViews() = %s", got)
	}
}
