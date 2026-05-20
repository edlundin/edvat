package pgext

import (
	"reflect"
	"strings"
	"testing"

	"github.com/edlundin/edvat/internal/baseatlas"
)

func TestParseHCL(t *testing.T) {
	got, err := ParseHCL([]byte(`
schema "public" {}

extension "pgcrypto" {
  schema = schema.public
  version = "1.3"
  comment = "cryptographic functions"
}

extension "citext" {
  version = "1.6"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseHCL() error = %v", err)
	}
	want := State{
		"pgcrypto": {Name: "pgcrypto", Schema: "public", Version: "1.3", Comment: "cryptographic functions"},
		"citext":   {Name: "citext", Version: "1.6"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffCreateExtension(t *testing.T) {
	statements := Diff(nil, State{"pgcrypto": {Name: "pgcrypto", Schema: "public", Version: "1.3", Comment: "crypto's funcs"}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE EXTENSION "pgcrypto" WITH SCHEMA "public" VERSION '1.3'`,
		`COMMENT ON EXTENSION "pgcrypto" IS 'crypto''s funcs'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("Diff() missing %q:\n%s", want, got)
		}
	}
}

func TestDiffCreateExtensionIncludesReverse(t *testing.T) {
	statements := Diff(nil, State{"pgcrypto": {Name: "pgcrypto", Schema: "public"}})
	if len(statements) == 0 || statements[0].Reverse != `DROP EXTENSION "pgcrypto"` {
		t.Fatalf("Reverse = %q, want drop extension", statements[0].Reverse)
	}
}

func TestDiffCreateExtensionCommentIncludesReverse(t *testing.T) {
	statements := Diff(nil, State{"pgcrypto": {Name: "pgcrypto", Comment: "crypto"}})
	if len(statements) < 2 || statements[1].Reverse != `COMMENT ON EXTENSION "pgcrypto" IS NULL` {
		t.Fatalf("Reverse = %q, want clear extension comment", statements[1].Reverse)
	}
}

func TestDiffUpdateCommentAndVersion(t *testing.T) {
	statements := Diff(
		State{"pgcrypto": {Name: "pgcrypto", Schema: "public", Version: "1.2", Comment: "old"}},
		State{"pgcrypto": {Name: "pgcrypto", Schema: "public", Version: "1.3", Comment: "new"}},
	)
	got := joinSQL(statements)
	for _, want := range []string{
		`ALTER EXTENSION "pgcrypto" UPDATE TO '1.3'`,
		`COMMENT ON EXTENSION "pgcrypto" IS 'new'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("Diff() missing %q:\n%s", want, got)
		}
	}
}

func TestDiffDoesNotDropImplicitPlpgsqlExtension(t *testing.T) {
	statements := Diff(State{"plpgsql": {Name: "plpgsql"}}, nil)
	if len(statements) != 0 {
		t.Fatalf("Diff() = %#v, want no plpgsql drop", statements)
	}
}

func TestDiffDropExtension(t *testing.T) {
	statements := Diff(State{"pgcrypto": {Name: "pgcrypto"}}, nil)
	got := joinSQL(statements)
	if !strings.Contains(got, `DROP EXTENSION "pgcrypto"`) {
		t.Fatalf("Diff() = %s", got)
	}
}

func joinSQL(statements []baseatlas.Statement) string {
	parts := make([]string, 0, len(statements))
	for _, statement := range statements {
		parts = append(parts, statement.SQL)
	}
	return strings.Join(parts, ";\n")
}
