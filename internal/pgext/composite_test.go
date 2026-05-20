package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseCompositesHCL(t *testing.T) {
	got, err := ParseCompositesHCL([]byte(`
schema "public" {}

composite "address" {
  schema = schema.public
  field "street" { type = text }
  field "zip" { type = integer }
  comment = "postal address"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseCompositesHCL() error = %v", err)
	}
	want := CompositeState{"public.address": {
		Name: "address", Schema: "public", Fields: []CompositeField{{Name: "street", Type: "text"}, {Name: "zip", Type: "integer"}}, Comment: "postal address",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseCompositesHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffCompositesCreate(t *testing.T) {
	statements := DiffComposites(nil, CompositeState{"public.address": {
		Name: "address", Schema: "public", Fields: []CompositeField{{Name: "street", Type: "text"}, {Name: "zip", Type: "integer"}}, Comment: "postal address",
	}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE TYPE "public"."address" AS ("street" text, "zip" integer)`,
		`COMMENT ON TYPE "public"."address" IS 'postal address'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffComposites() missing %q:\n%s", want, got)
		}
	}
	if statements[0].Reverse != `DROP TYPE "public"."address"` {
		t.Fatalf("create reverse = %q", statements[0].Reverse)
	}
	if statements[1].Reverse != `COMMENT ON TYPE "public"."address" IS NULL` {
		t.Fatalf("comment reverse = %q", statements[1].Reverse)
	}
}

func TestDiffCompositesDropHasReverse(t *testing.T) {
	statements := DiffComposites(CompositeState{"public.address": {
		Name: "address", Schema: "public", Fields: []CompositeField{{Name: "street", Type: "text"}, {Name: "zip", Type: "integer"}}, Comment: "postal address",
	}}, nil)
	if len(statements) != 1 {
		t.Fatalf("DiffComposites() got %d statements, want 1", len(statements))
	}
	want := `CREATE TYPE "public"."address" AS ("street" text, "zip" integer);
COMMENT ON TYPE "public"."address" IS 'postal address'`
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffCompositesCommentChangeHasReverse(t *testing.T) {
	statements := DiffComposites(
		CompositeState{"public.address": {Name: "address", Schema: "public", Fields: []CompositeField{{Name: "street", Type: "text"}}, Comment: "old"}},
		CompositeState{"public.address": {Name: "address", Schema: "public", Fields: []CompositeField{{Name: "street", Type: "text"}}, Comment: "new"}},
	)
	if len(statements) != 1 {
		t.Fatalf("DiffComposites() got %d statements, want 1", len(statements))
	}
	if statements[0].Reverse != `COMMENT ON TYPE "public"."address" IS 'old'` {
		t.Fatalf("comment reverse = %q", statements[0].Reverse)
	}
}

func TestDiffCompositesReplaceOnDefinitionChange(t *testing.T) {
	statements := DiffComposites(
		CompositeState{"public.address": {Name: "address", Schema: "public", Fields: []CompositeField{{Name: "street", Type: "text"}}}},
		CompositeState{"public.address": {Name: "address", Schema: "public", Fields: []CompositeField{{Name: "street", Type: "varchar(200)"}}}},
	)
	got := joinSQL(statements)
	for _, want := range []string{
		`DROP TYPE "public"."address"`,
		`CREATE TYPE "public"."address" AS ("street" varchar(200))`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffComposites() missing %q:\n%s", want, got)
		}
	}
}
