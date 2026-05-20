package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseFunctionsHCL(t *testing.T) {
	got, err := ParseFunctionsHCL([]byte(`
schema "public" {}

function "positive" {
  schema = schema.public
  lang = SQL
  arg "v" { type = integer }
  return = boolean
  as = "SELECT v > 0"
  comment = "positive number check"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseFunctionsHCL() error = %v", err)
	}
	want := FunctionState{"public.positive(integer)": {
		Name: "positive", Schema: "public", Language: "SQL", Args: []FunctionArg{{Name: "v", Type: "integer"}}, ReturnType: "boolean", Body: "SELECT v > 0", Comment: "positive number check",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseFunctionsHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffFunctionsCreate(t *testing.T) {
	statements := DiffFunctions(nil, FunctionState{"public.positive(integer)": {
		Name: "positive", Schema: "public", Language: "SQL", Args: []FunctionArg{{Name: "v", Type: "integer"}}, ReturnType: "boolean", Body: "SELECT v > 0", Comment: "positive number check",
	}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE OR REPLACE FUNCTION "public"."positive"("v" integer) RETURNS boolean LANGUAGE SQL AS $$`,
		`SELECT v > 0`,
		`COMMENT ON FUNCTION "public"."positive"(integer) IS 'positive number check'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffFunctions() missing %q:\n%s", want, got)
		}
	}
}

func TestDiffFunctionsCreateCommentIncludesReverse(t *testing.T) {
	statements := DiffFunctions(nil, FunctionState{"public.positive(integer)": {Name: "positive", Schema: "public", Language: "SQL", Args: []FunctionArg{{Name: "n", Type: "integer"}}, ReturnType: "integer", Body: "SELECT n", Comment: "positive"}})
	if len(statements) < 2 || statements[1].Reverse != `COMMENT ON FUNCTION "public"."positive"(integer) IS NULL` {
		t.Fatalf("Reverse = %q, want clear function comment", statements[1].Reverse)
	}
}

func TestDiffFunctionsCreateIncludesReverse(t *testing.T) {
	statements := DiffFunctions(nil, FunctionState{"public.positive(integer)": {Name: "positive", Schema: "public", Language: "SQL", Args: []FunctionArg{{Name: "n", Type: "integer"}}, ReturnType: "integer", Body: "SELECT n"}})
	if len(statements) == 0 || statements[0].Reverse != `DROP FUNCTION "public"."positive"(integer)` {
		t.Fatalf("Reverse = %q, want drop function", statements[0].Reverse)
	}
}

func TestDiffFunctionsTreatsPostgresReturnTypeAliasesAsEquivalent(t *testing.T) {
	statements := DiffFunctions(
		FunctionState{"public.email(integer)": {Name: "email", Schema: "public", Language: "sql", Args: []FunctionArg{{Name: "id", Type: "integer"}}, ReturnType: "character varying", Body: "SELECT email FROM users WHERE users.id = email.id"}},
		FunctionState{"public.email(integer)": {Name: "email", Schema: "public", Language: "SQL", Args: []FunctionArg{{Name: "id", Type: "integer"}}, ReturnType: "varchar", Body: "SELECT email FROM users WHERE users.id = email.id"}},
	)
	if len(statements) != 0 {
		t.Fatalf("DiffFunctions() = %#v, want no statements", statements)
	}
}

func TestDiffFunctionsReplaceAndComment(t *testing.T) {
	current := FunctionState{"public.positive(integer)": {Name: "positive", Schema: "public", Language: "SQL", Args: []FunctionArg{{Name: "v", Type: "integer"}}, ReturnType: "boolean", Body: "SELECT v >= 0", Comment: "old"}}
	desired := FunctionState{"public.positive(integer)": {Name: "positive", Schema: "public", Language: "SQL", Args: []FunctionArg{{Name: "v", Type: "integer"}}, ReturnType: "boolean", Body: "SELECT v > 0", Comment: "new"}}
	statements := DiffFunctions(current, desired)
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE OR REPLACE FUNCTION "public"."positive"("v" integer) RETURNS boolean LANGUAGE SQL`,
		`COMMENT ON FUNCTION "public"."positive"(integer) IS 'new'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffFunctions() missing %q:\n%s", want, got)
		}
	}
}

func TestDiffFunctionsIgnoresInspectedCreateWrapper(t *testing.T) {
	current := FunctionState{"public.positive(integer)": {Name: "positive", Schema: "public", Language: "sql", Args: []FunctionArg{{Name: "v", Type: "integer"}}, ReturnType: "boolean", Body: `CREATE OR REPLACE FUNCTION public.positive(v integer)
 RETURNS boolean
 LANGUAGE sql
AS $$
SELECT v > 0
$$`, Comment: "same"}}
	desired := FunctionState{"public.positive(integer)": {Name: "positive", Schema: "public", Language: "SQL", Args: []FunctionArg{{Name: "v", Type: "integer"}}, ReturnType: "boolean", Body: "SELECT v > 0", Comment: "same"}}
	if statements := DiffFunctions(current, desired); len(statements) != 0 {
		t.Fatalf("DiffFunctions() = %#v, want empty", statements)
	}
}

func TestDiffFunctionsDrop(t *testing.T) {
	statements := DiffFunctions(FunctionState{"public.positive(integer)": {Name: "positive", Schema: "public", Args: []FunctionArg{{Name: "v", Type: "integer"}}}}, nil)
	got := joinSQL(statements)
	if !strings.Contains(got, `DROP FUNCTION "public"."positive"(integer)`) {
		t.Fatalf("DiffFunctions() = %s", got)
	}
}
