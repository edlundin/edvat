package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseSequencesHCL(t *testing.T) {
	got, err := ParseSequencesHCL([]byte(`
schema "public" {}

sequence "order_seq" {
  schema = schema.public
  type = bigint
  start = 100
  increment = 5
  cache = 10
  cycle = true
  comment = "order ids"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseSequencesHCL() error = %v", err)
	}
	want := SequenceState{"public.order_seq": {Name: "order_seq", Schema: "public", Type: "bigint", Start: "100", Increment: "5", Cache: "10", Cycle: true, CycleSet: true, Comment: "order ids"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseSequencesHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffSequencesCreate(t *testing.T) {
	statements := DiffSequences(nil, SequenceState{"public.order_seq": {Name: "order_seq", Schema: "public", Type: "bigint", Start: "100", Increment: "5", Cache: "10", Cycle: true, Comment: "order ids"}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE SEQUENCE "public"."order_seq" AS bigint INCREMENT BY 5 START WITH 100 CACHE 10 CYCLE`,
		`COMMENT ON SEQUENCE "public"."order_seq" IS 'order ids'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffSequences() missing %q:\n%s", want, got)
		}
	}
	if statements[0].Reverse != `DROP SEQUENCE "public"."order_seq"` {
		t.Fatalf("create reverse = %q", statements[0].Reverse)
	}
	if statements[1].Reverse != `COMMENT ON SEQUENCE "public"."order_seq" IS NULL` {
		t.Fatalf("comment reverse = %q", statements[1].Reverse)
	}
}

func TestDiffSequencesDropHasReverse(t *testing.T) {
	statements := DiffSequences(SequenceState{"public.order_seq": {Name: "order_seq", Schema: "public", Start: "100", CycleSet: true, Comment: "order ids"}}, nil)
	if len(statements) != 1 {
		t.Fatalf("DiffSequences() got %d statements, want 1", len(statements))
	}
	want := `CREATE SEQUENCE "public"."order_seq" START WITH 100 NO CYCLE;
COMMENT ON SEQUENCE "public"."order_seq" IS 'order ids'`
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffSequencesIgnoresUnspecifiedDesiredDefaults(t *testing.T) {
	statements := DiffSequences(
		SequenceState{"public.order_seq": {Name: "order_seq", Schema: "public", Type: "bigint", Start: "1", Increment: "1", Min: "1", Max: "9223372036854775807", Cache: "1", CycleSet: true}},
		SequenceState{"public.order_seq": {Name: "order_seq", Schema: "public"}},
	)
	if len(statements) != 0 {
		t.Fatalf("DiffSequences() = %#v, want no churn for unspecified desired defaults", statements)
	}
}

func TestDiffSequencesAlter(t *testing.T) {
	statements := DiffSequences(SequenceState{"public.order_seq": {Name: "order_seq", Schema: "public", Start: "1", CycleSet: true}}, SequenceState{"public.order_seq": {Name: "order_seq", Schema: "public", Start: "10", CycleSet: true}})
	got := joinSQL(statements)
	want := `ALTER SEQUENCE "public"."order_seq" START WITH 10 NO CYCLE`
	if !strings.Contains(got, want) {
		t.Fatalf("DiffSequences() missing %q:\n%s", want, got)
	}
	if statements[0].Reverse != `ALTER SEQUENCE "public"."order_seq" START WITH 1 NO CYCLE` {
		t.Fatalf("alter reverse = %q", statements[0].Reverse)
	}
}
