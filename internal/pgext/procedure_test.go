package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseProceduresHCL(t *testing.T) {
	got, err := ParseProceduresHCL([]byte(`
schema "public" {}

procedure "touch" {
  schema = schema.public
  lang = SQL
  arg "id" { type = integer }
  as = "UPDATE users SET updated_at = now() WHERE users.id = touch.id"
  comment = "touch user"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseProceduresHCL() error = %v", err)
	}
	want := ProcedureState{"public.touch(integer)": {
		Name: "touch", Schema: "public", Language: "SQL", Args: []FunctionArg{{Name: "id", Type: "integer"}}, Body: "UPDATE users SET updated_at = now() WHERE users.id = touch.id", Comment: "touch user",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseProceduresHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffProceduresCreate(t *testing.T) {
	statements := DiffProcedures(nil, ProcedureState{"public.touch(integer)": {
		Name: "touch", Schema: "public", Language: "SQL", Args: []FunctionArg{{Name: "id", Type: "integer"}}, Body: "SELECT 1", Comment: "touch user",
	}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE OR REPLACE PROCEDURE "public"."touch"("id" integer) LANGUAGE SQL AS $$`,
		`SELECT 1`,
		`COMMENT ON PROCEDURE "public"."touch"(integer) IS 'touch user'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffProcedures() missing %q:\n%s", want, got)
		}
	}
	if statements[0].Reverse != `DROP PROCEDURE "public"."touch"(integer)` {
		t.Fatalf("create reverse = %q", statements[0].Reverse)
	}
	if statements[1].Reverse != `COMMENT ON PROCEDURE "public"."touch"(integer) IS NULL` {
		t.Fatalf("comment reverse = %q", statements[1].Reverse)
	}
}

func TestDiffProceduresIgnoresInspectedCreateWrapper(t *testing.T) {
	current := ProcedureState{"public.touch(integer)": {Name: "touch", Schema: "public", Language: "sql", Args: []FunctionArg{{Name: "id", Type: "integer"}}, Body: `CREATE OR REPLACE PROCEDURE public.touch(id integer)
 LANGUAGE sql
AS $$
SELECT 1
$$`, Comment: "same"}}
	desired := ProcedureState{"public.touch(integer)": {Name: "touch", Schema: "public", Language: "SQL", Args: []FunctionArg{{Name: "id", Type: "integer"}}, Body: "SELECT 1", Comment: "same"}}
	if statements := DiffProcedures(current, desired); len(statements) != 0 {
		t.Fatalf("DiffProcedures() = %#v, want empty", statements)
	}
}

func TestDiffProceduresDrop(t *testing.T) {
	statements := DiffProcedures(ProcedureState{"public.touch(integer)": {Name: "touch", Schema: "public", Language: "SQL", Args: []FunctionArg{{Type: "integer"}}, Body: "SELECT 1", Comment: "touch user"}}, nil)
	got := joinSQL(statements)
	if !strings.Contains(got, `DROP PROCEDURE "public"."touch"(integer)`) {
		t.Fatalf("DiffProcedures() = %s", got)
	}
	want := `CREATE OR REPLACE PROCEDURE "public"."touch"(integer) LANGUAGE SQL AS $$
SELECT 1
$$;
COMMENT ON PROCEDURE "public"."touch"(integer) IS 'touch user'`
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffProceduresReplaceHasReverse(t *testing.T) {
	statements := DiffProcedures(
		ProcedureState{"public.touch(integer)": {Name: "touch", Schema: "public", Language: "SQL", Args: []FunctionArg{{Type: "integer"}}, Body: "SELECT 1"}},
		ProcedureState{"public.touch(integer)": {Name: "touch", Schema: "public", Language: "SQL", Args: []FunctionArg{{Type: "integer"}}, Body: "SELECT 2"}},
	)
	if len(statements) != 1 {
		t.Fatalf("DiffProcedures() got %d statements, want 1", len(statements))
	}
	want := `CREATE OR REPLACE PROCEDURE "public"."touch"(integer) LANGUAGE SQL AS $$
SELECT 1
$$`
	if statements[0].Reverse != want {
		t.Fatalf("replace reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffProceduresCommentChangeHasReverse(t *testing.T) {
	statements := DiffProcedures(
		ProcedureState{"public.touch(integer)": {Name: "touch", Schema: "public", Language: "SQL", Args: []FunctionArg{{Type: "integer"}}, Body: "SELECT 1", Comment: "old"}},
		ProcedureState{"public.touch(integer)": {Name: "touch", Schema: "public", Language: "SQL", Args: []FunctionArg{{Type: "integer"}}, Body: "SELECT 1", Comment: "new"}},
	)
	if len(statements) != 1 {
		t.Fatalf("DiffProcedures() got %d statements, want 1", len(statements))
	}
	if statements[0].Reverse != `COMMENT ON PROCEDURE "public"."touch"(integer) IS 'old'` {
		t.Fatalf("comment reverse = %q", statements[0].Reverse)
	}
}
