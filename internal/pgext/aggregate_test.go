package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseAggregatesHCL(t *testing.T) {
	got, err := ParseAggregatesHCL([]byte(`
schema "public" {}

aggregate "sum2" {
  schema = schema.public
  args = [integer]
  state_func = schema.public.int_add
  state_type = integer
  init_cond = "0"
  parallel = SAFE
  comment = "sum integers"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseAggregatesHCL() error = %v", err)
	}
	want := AggregateState{"public.sum2(integer)": {
		Name: "sum2", Schema: "public", Args: []string{"integer"}, StateFunc: `"public"."int_add"`, StateType: "integer", InitCond: "0", Parallel: "SAFE", Comment: "sum integers",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseAggregatesHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffAggregatesCreate(t *testing.T) {
	statements := DiffAggregates(nil, AggregateState{"public.sum2(integer)": {
		Name: "sum2", Schema: "public", Args: []string{"integer"}, StateFunc: `"public"."int_add"`, StateType: "integer", InitCond: "0", Parallel: "SAFE", Comment: "sum integers",
	}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE AGGREGATE "public"."sum2"(integer) (SFUNC = "public"."int_add", STYPE = integer, INITCOND = '0', PARALLEL = SAFE)`,
		`COMMENT ON AGGREGATE "public"."sum2"(integer) IS 'sum integers'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffAggregates() missing %q:\n%s", want, got)
		}
	}
	if statements[0].Reverse != `DROP AGGREGATE "public"."sum2"(integer)` {
		t.Fatalf("create reverse = %q", statements[0].Reverse)
	}
	if statements[1].Reverse != `COMMENT ON AGGREGATE "public"."sum2"(integer) IS NULL` {
		t.Fatalf("comment reverse = %q", statements[1].Reverse)
	}
}

func TestDiffAggregatesDropHasReverse(t *testing.T) {
	statements := DiffAggregates(AggregateState{"public.sum2(integer)": {
		Name: "sum2", Schema: "public", Args: []string{"integer"}, StateFunc: `"public"."int_add"`, StateType: "integer", InitCond: "0", Parallel: "SAFE", Comment: "sum integers",
	}}, nil)
	if len(statements) != 1 {
		t.Fatalf("DiffAggregates() got %d statements, want 1", len(statements))
	}
	want := `CREATE AGGREGATE "public"."sum2"(integer) (SFUNC = "public"."int_add", STYPE = integer, INITCOND = '0', PARALLEL = SAFE);
COMMENT ON AGGREGATE "public"."sum2"(integer) IS 'sum integers'`
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffAggregatesCommentChangeHasReverse(t *testing.T) {
	statements := DiffAggregates(
		AggregateState{"public.sum2(integer)": {Name: "sum2", Schema: "public", Args: []string{"integer"}, StateFunc: `"public"."int_add"`, StateType: "integer", Comment: "old"}},
		AggregateState{"public.sum2(integer)": {Name: "sum2", Schema: "public", Args: []string{"integer"}, StateFunc: `"public"."int_add"`, StateType: "integer", Comment: "new"}},
	)
	if len(statements) != 1 {
		t.Fatalf("DiffAggregates() got %d statements, want 1", len(statements))
	}
	if statements[0].Reverse != `COMMENT ON AGGREGATE "public"."sum2"(integer) IS 'old'` {
		t.Fatalf("comment reverse = %q", statements[0].Reverse)
	}
}

func TestDiffAggregatesReplaceOnDefinitionChange(t *testing.T) {
	statements := DiffAggregates(
		AggregateState{"public.sum2(integer)": {Name: "sum2", Schema: "public", Args: []string{"integer"}, StateFunc: `"public"."old_add"`, StateType: "integer"}},
		AggregateState{"public.sum2(integer)": {Name: "sum2", Schema: "public", Args: []string{"integer"}, StateFunc: `"public"."int_add"`, StateType: "integer"}},
	)
	got := joinSQL(statements)
	for _, want := range []string{
		`DROP AGGREGATE "public"."sum2"(integer)`,
		`CREATE AGGREGATE "public"."sum2"(integer) (SFUNC = "public"."int_add", STYPE = integer)`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffAggregates() missing %q:\n%s", want, got)
		}
	}
}
