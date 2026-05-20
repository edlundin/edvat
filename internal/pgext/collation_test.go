package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseCollationsHCL(t *testing.T) {
	got, err := ParseCollationsHCL([]byte(`
schema "public" {}

collation "case_insensitive" {
  schema = schema.public
  provider = icu
  locale = "und-u-ks-level2"
  deterministic = false
  comment = "case insensitive collation"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseCollationsHCL() error = %v", err)
	}
	falseValue := false
	want := CollationState{"public.case_insensitive": {
		Name: "case_insensitive", Schema: "public", Provider: "icu", Locale: "und-u-ks-level2", Deterministic: &falseValue, Comment: "case insensitive collation",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseCollationsHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffCollationsCreate(t *testing.T) {
	falseValue := false
	statements := DiffCollations(nil, CollationState{"public.case_insensitive": {
		Name: "case_insensitive", Schema: "public", Provider: "icu", Locale: "und-u-ks-level2", Deterministic: &falseValue, Comment: "case insensitive collation",
	}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE COLLATION "public"."case_insensitive" (provider = 'icu', locale = 'und-u-ks-level2', deterministic = false)`,
		`COMMENT ON COLLATION "public"."case_insensitive" IS 'case insensitive collation'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffCollations() missing %q:\n%s", want, got)
		}
	}
	if statements[0].Reverse != `DROP COLLATION "public"."case_insensitive"` {
		t.Fatalf("create reverse = %q", statements[0].Reverse)
	}
	if statements[1].Reverse != `COMMENT ON COLLATION "public"."case_insensitive" IS NULL` {
		t.Fatalf("comment reverse = %q", statements[1].Reverse)
	}
}

func TestDiffCollationsDropHasReverse(t *testing.T) {
	falseValue := false
	statements := DiffCollations(CollationState{"public.case_insensitive": {
		Name: "case_insensitive", Schema: "public", Provider: "icu", Locale: "und-u-ks-level2", Deterministic: &falseValue, Comment: "case insensitive collation",
	}}, nil)
	if len(statements) != 1 {
		t.Fatalf("DiffCollations() got %d statements, want 1", len(statements))
	}
	want := `CREATE COLLATION "public"."case_insensitive" (provider = 'icu', locale = 'und-u-ks-level2', deterministic = false);
COMMENT ON COLLATION "public"."case_insensitive" IS 'case insensitive collation'`
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffCollationsCommentChangeHasReverse(t *testing.T) {
	statements := DiffCollations(
		CollationState{"public.ci": {Name: "ci", Schema: "public", Locale: "en_US", Comment: "old"}},
		CollationState{"public.ci": {Name: "ci", Schema: "public", Locale: "en_US", Comment: "new"}},
	)
	if len(statements) != 1 {
		t.Fatalf("DiffCollations() got %d statements, want 1", len(statements))
	}
	if statements[0].Reverse != `COMMENT ON COLLATION "public"."ci" IS 'old'` {
		t.Fatalf("comment reverse = %q", statements[0].Reverse)
	}
}

func TestDiffCollationsReplaceOnDefinitionChange(t *testing.T) {
	statements := DiffCollations(
		CollationState{"public.ci": {Name: "ci", Schema: "public", Locale: "en_US"}},
		CollationState{"public.ci": {Name: "ci", Schema: "public", Locale: "sv_SE"}},
	)
	got := joinSQL(statements)
	for _, want := range []string{
		`DROP COLLATION "public"."ci"`,
		`CREATE COLLATION "public"."ci" (locale = 'sv_SE')`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffCollations() missing %q:\n%s", want, got)
		}
	}
}
