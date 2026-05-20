package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseExclusionsHCL(t *testing.T) {
	got, err := ParseExclusionsHCL([]byte(`
schema "public" {}

table "reservations" {
  schema = schema.public
  column "period" { type = tsrange }
  exclude "reservations_no_overlap" {
    type = "GIST"
    on {
      column = column.period
      op = "&&"
    }
  }
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseExclusionsHCL() error = %v", err)
	}
	want := ExclusionState{"public.reservations.reservations_no_overlap": {Name: "reservations_no_overlap", Schema: "public", Table: "reservations", Type: "gist", Columns: []ExclusionColumn{{Column: "period", Op: "&&"}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseExclusionsHCL() = %#v, want %#v", got, want)
	}
}

func TestParseExclusionsHCLExpressionOn(t *testing.T) {
	got, err := ParseExclusionsHCL([]byte(`
schema "public" {}

table "reservations" {
  schema = schema.public
  column "period" { type = tsrange }
  exclude "reservations_no_overlap" {
    type = "GIST"
    on {
      expr = "lower(period)"
      op = "&&"
    }
  }
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseExclusionsHCL() error = %v", err)
	}
	want := ExclusionState{"public.reservations.reservations_no_overlap": {Name: "reservations_no_overlap", Schema: "public", Table: "reservations", Type: "gist", Columns: []ExclusionColumn{{Expression: "lower(period)", Op: "&&"}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseExclusionsHCL() = %#v, want %#v", got, want)
	}
}

func TestParseExclusionsHCLIncludeAndWhere(t *testing.T) {
	got, err := ParseExclusionsHCL([]byte(`
schema "public" {}

table "reservations" {
  schema = schema.public
  column "room_id" { type = int }
  column "period" { type = tsrange }
  column "note" { type = text }
  exclude "reservations_no_overlap" {
    type = "GIST"
    on {
      column = column.room_id
      op = "="
    }
    on {
      column = column.period
      op = "&&"
    }
    include = [column.note]
    where = "period IS NOT NULL"
  }
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseExclusionsHCL() error = %v", err)
	}
	want := ExclusionState{"public.reservations.reservations_no_overlap": {Name: "reservations_no_overlap", Schema: "public", Table: "reservations", Type: "gist", Columns: []ExclusionColumn{{Column: "room_id", Op: "="}, {Column: "period", Op: "&&"}}, Include: []string{"note"}, Where: "period IS NOT NULL"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseExclusionsHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffExclusionsCreateExpressionOn(t *testing.T) {
	statements := DiffExclusions(nil, ExclusionState{"public.reservations.reservations_no_overlap": {Name: "reservations_no_overlap", Schema: "public", Table: "reservations", Type: "gist", Columns: []ExclusionColumn{{Expression: "lower(period)", Op: "&&"}}}})
	got := joinSQL(statements)
	want := `ALTER TABLE "public"."reservations" ADD CONSTRAINT "reservations_no_overlap" EXCLUDE USING gist ((lower(period)) WITH &&)`
	if !strings.Contains(got, want) {
		t.Fatalf("DiffExclusions() missing %q:\n%s", want, got)
	}
}

func TestDiffExclusionsCreate(t *testing.T) {
	statements := DiffExclusions(nil, ExclusionState{"public.reservations.reservations_no_overlap": {Name: "reservations_no_overlap", Schema: "public", Table: "reservations", Type: "gist", Columns: []ExclusionColumn{{Column: "room_id", Op: "="}, {Column: "period", Op: "&&"}}, Include: []string{"note"}, Where: "period IS NOT NULL"}})
	got := joinSQL(statements)
	want := `ALTER TABLE "public"."reservations" ADD CONSTRAINT "reservations_no_overlap" EXCLUDE USING gist ("room_id" WITH =, "period" WITH &&) INCLUDE ("note") WHERE (period IS NOT NULL)`
	if !strings.Contains(got, want) {
		t.Fatalf("DiffExclusions() missing %q:\n%s", want, got)
	}
}

func TestDiffExclusionsDropHasReverse(t *testing.T) {
	statements := DiffExclusions(ExclusionState{"public.reservations.reservations_no_overlap": {Name: "reservations_no_overlap", Schema: "public", Table: "reservations", Type: "gist", Columns: []ExclusionColumn{{Column: "period", Op: "&&"}}}}, nil)
	if len(statements) != 1 {
		t.Fatalf("DiffExclusions() got %d statements, want 1", len(statements))
	}
	want := `ALTER TABLE "public"."reservations" ADD CONSTRAINT "reservations_no_overlap" EXCLUDE USING gist ("period" WITH &&)`
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestExclusionColumnsFromDefKeepsCommasInsideExpressions(t *testing.T) {
	def := `EXCLUDE USING gist ((daterange(start_at, end_at, '[]'::text)) WITH &&, room_id WITH =) INCLUDE (note, created_by) WHERE ((cancelled_at IS NULL))`
	got := exclusionColumnsFromDef(def)
	want := []ExclusionColumn{{Expression: `daterange(start_at, end_at, '[]'::text)`, Op: "&&"}, {Column: "room_id", Op: "="}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exclusionColumnsFromDef() = %#v, want %#v", got, want)
	}
	include := exclusionIncludeFromDef(def)
	if !reflect.DeepEqual(include, []string{"note", "created_by"}) {
		t.Fatalf("exclusionIncludeFromDef() = %#v", include)
	}
}
