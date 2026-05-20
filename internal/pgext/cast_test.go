package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseCastsHCL(t *testing.T) {
	got, err := ParseCastsHCL([]byte(`
schema "public" {}

cast {
  source = text
  target = integer
  with = schema.public.text_to_int
  assignment = true
  comment = "text to int"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseCastsHCL() error = %v", err)
	}
	want := CastState{"text AS integer": {Source: "text", Target: "integer", Function: `"public"."text_to_int"`, Assignment: true, Comment: "text to int"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseCastsHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffCastsCreate(t *testing.T) {
	statements := DiffCasts(nil, CastState{"text AS integer": {Source: "text", Target: "integer", Function: `"public"."text_to_int"`, Assignment: true, Comment: "text to int"}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE CAST (text AS integer) WITH FUNCTION "public"."text_to_int" AS ASSIGNMENT`,
		`COMMENT ON CAST (text AS integer) IS 'text to int'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffCasts() missing %q:\n%s", want, got)
		}
	}
	if statements[0].Reverse != `DROP CAST (text AS integer)` {
		t.Fatalf("create reverse = %q", statements[0].Reverse)
	}
	if statements[1].Reverse != `COMMENT ON CAST (text AS integer) IS NULL` {
		t.Fatalf("comment reverse = %q", statements[1].Reverse)
	}
}

func TestDiffCastsDropHasReverse(t *testing.T) {
	statements := DiffCasts(CastState{"text AS integer": {Source: "text", Target: "integer", Function: `"public"."text_to_int"`, Assignment: true, Comment: "text to int"}}, nil)
	if len(statements) != 1 {
		t.Fatalf("DiffCasts() got %d statements, want 1", len(statements))
	}
	want := `CREATE CAST (text AS integer) WITH FUNCTION "public"."text_to_int" AS ASSIGNMENT;
COMMENT ON CAST (text AS integer) IS 'text to int'`
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffCastsCommentChangeHasReverse(t *testing.T) {
	statements := DiffCasts(
		CastState{"text AS integer": {Source: "text", Target: "integer", Method: "INOUT", Comment: "old"}},
		CastState{"text AS integer": {Source: "text", Target: "integer", Method: "INOUT", Comment: "new"}},
	)
	if len(statements) != 1 {
		t.Fatalf("DiffCasts() got %d statements, want 1", len(statements))
	}
	if statements[0].Reverse != `COMMENT ON CAST (text AS integer) IS 'old'` {
		t.Fatalf("comment reverse = %q", statements[0].Reverse)
	}
}

func TestDiffCastsReplaceOnDefinitionChange(t *testing.T) {
	statements := DiffCasts(
		CastState{"text AS integer": {Source: "text", Target: "integer", Method: "INOUT"}},
		CastState{"text AS integer": {Source: "text", Target: "integer", Method: "BINARY"}},
	)
	got := joinSQL(statements)
	for _, want := range []string{
		`DROP CAST (text AS integer)`,
		`CREATE CAST (text AS integer) WITH BINARY`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffCasts() missing %q:\n%s", want, got)
		}
	}
}
