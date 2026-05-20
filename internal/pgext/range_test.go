package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseRangesHCL(t *testing.T) {
	got, err := ParseRangesHCL([]byte(`
schema "public" {}

range "floatrange" {
  schema = schema.public
  subtype = float8
  subtype_diff = schema.public.float8mi
  multirange_name = "floatmultirange"
  comment = "float range"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseRangesHCL() error = %v", err)
	}
	want := RangeState{"public.floatrange": {
		Name: "floatrange", Schema: "public", Subtype: "float8", SubtypeDiff: `"public"."float8mi"`, MultirangeName: "floatmultirange", Comment: "float range",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseRangesHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffRangesCreate(t *testing.T) {
	statements := DiffRanges(nil, RangeState{"public.floatrange": {
		Name: "floatrange", Schema: "public", Subtype: "float8", SubtypeDiff: `"public"."float8mi"`, MultirangeName: "floatmultirange", Comment: "float range",
	}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE TYPE "public"."floatrange" AS RANGE (SUBTYPE = float8, SUBTYPE_DIFF = "public"."float8mi", MULTIRANGE_TYPE_NAME = "floatmultirange")`,
		`COMMENT ON TYPE "public"."floatrange" IS 'float range'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffRanges() missing %q:\n%s", want, got)
		}
	}
	if statements[0].Reverse != `DROP TYPE "public"."floatrange"` {
		t.Fatalf("create reverse = %q", statements[0].Reverse)
	}
	if statements[1].Reverse != `COMMENT ON TYPE "public"."floatrange" IS NULL` {
		t.Fatalf("comment reverse = %q", statements[1].Reverse)
	}
}

func TestDiffRangesDropHasReverse(t *testing.T) {
	statements := DiffRanges(RangeState{"public.floatrange": {
		Name: "floatrange", Schema: "public", Subtype: "float8", SubtypeDiff: `"public"."float8mi"`, MultirangeName: "floatmultirange", Comment: "float range",
	}}, nil)
	if len(statements) != 1 {
		t.Fatalf("DiffRanges() got %d statements, want 1", len(statements))
	}
	want := `CREATE TYPE "public"."floatrange" AS RANGE (SUBTYPE = float8, SUBTYPE_DIFF = "public"."float8mi", MULTIRANGE_TYPE_NAME = "floatmultirange");
COMMENT ON TYPE "public"."floatrange" IS 'float range'`
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffRangesCommentChangeHasReverse(t *testing.T) {
	statements := DiffRanges(
		RangeState{"public.floatrange": {Name: "floatrange", Schema: "public", Subtype: "float8", Comment: "old"}},
		RangeState{"public.floatrange": {Name: "floatrange", Schema: "public", Subtype: "float8", Comment: "new"}},
	)
	if len(statements) != 1 {
		t.Fatalf("DiffRanges() got %d statements, want 1", len(statements))
	}
	if statements[0].Reverse != `COMMENT ON TYPE "public"."floatrange" IS 'old'` {
		t.Fatalf("comment reverse = %q", statements[0].Reverse)
	}
}

func TestDiffRangesReplaceOnDefinitionChange(t *testing.T) {
	statements := DiffRanges(
		RangeState{"public.floatrange": {Name: "floatrange", Schema: "public", Subtype: "float8"}},
		RangeState{"public.floatrange": {Name: "floatrange", Schema: "public", Subtype: "numeric"}},
	)
	got := joinSQL(statements)
	for _, want := range []string{
		`DROP TYPE "public"."floatrange"`,
		`CREATE TYPE "public"."floatrange" AS RANGE (SUBTYPE = numeric)`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffRanges() missing %q:\n%s", want, got)
		}
	}
}
