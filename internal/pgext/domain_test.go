package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseDomainsHCL(t *testing.T) {
	got, err := ParseDomainsHCL([]byte(`
schema "public" {}

domain "email" {
  schema = schema.public
  type = text
  null = false
  default = "''"
  check "email_has_at" { expr = "VALUE LIKE '%@%'" }
  comment = "email address"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseDomainsHCL() error = %v", err)
	}
	want := DomainState{"public.email": {
		Name: "email", Schema: "public", Type: "text", NotNull: true, Default: `"''"`, Checks: []DomainCheck{{Name: "email_has_at", Expr: "VALUE LIKE '%@%'"}}, Comment: "email address",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseDomainsHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffDomainsCreate(t *testing.T) {
	statements := DiffDomains(nil, DomainState{"public.email": {
		Name: "email", Schema: "public", Type: "text", NotNull: true, Checks: []DomainCheck{{Name: "email_has_at", Expr: "VALUE LIKE '%@%'"}}, Comment: "email address",
	}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE DOMAIN "public"."email" AS text NOT NULL CONSTRAINT "email_has_at" CHECK (VALUE LIKE '%@%')`,
		`COMMENT ON DOMAIN "public"."email" IS 'email address'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffDomains() missing %q:\n%s", want, got)
		}
	}
	if statements[0].Reverse != `DROP DOMAIN "public"."email"` {
		t.Fatalf("create reverse = %q", statements[0].Reverse)
	}
	if statements[1].Reverse != `COMMENT ON DOMAIN "public"."email" IS NULL` {
		t.Fatalf("comment reverse = %q", statements[1].Reverse)
	}
}

func TestDiffDomainsDropHasReverse(t *testing.T) {
	statements := DiffDomains(DomainState{"public.email": {
		Name: "email", Schema: "public", Type: "text", NotNull: true, Checks: []DomainCheck{{Name: "email_has_at", Expr: "VALUE LIKE '%@%'"}}, Comment: "email address",
	}}, nil)
	if len(statements) != 1 {
		t.Fatalf("DiffDomains() got %d statements, want 1", len(statements))
	}
	want := `CREATE DOMAIN "public"."email" AS text NOT NULL CONSTRAINT "email_has_at" CHECK (VALUE LIKE '%@%');
COMMENT ON DOMAIN "public"."email" IS 'email address'`
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffDomainsCommentChangeHasReverse(t *testing.T) {
	statements := DiffDomains(
		DomainState{"public.email": {Name: "email", Schema: "public", Type: "text", Comment: "old"}},
		DomainState{"public.email": {Name: "email", Schema: "public", Type: "text", Comment: "new"}},
	)
	if len(statements) != 1 {
		t.Fatalf("DiffDomains() got %d statements, want 1", len(statements))
	}
	if statements[0].Reverse != `COMMENT ON DOMAIN "public"."email" IS 'old'` {
		t.Fatalf("comment reverse = %q", statements[0].Reverse)
	}
}

func TestDiffDomainsReplaceOnDefinitionChange(t *testing.T) {
	statements := DiffDomains(
		DomainState{"public.email": {Name: "email", Schema: "public", Type: "text"}},
		DomainState{"public.email": {Name: "email", Schema: "public", Type: "varchar(320)"}},
	)
	got := joinSQL(statements)
	for _, want := range []string{
		`DROP DOMAIN "public"."email"`,
		`CREATE DOMAIN "public"."email" AS varchar(320)`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffDomains() missing %q:\n%s", want, got)
		}
	}
}
