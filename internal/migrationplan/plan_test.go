package migrationplan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/edlundin/edvat/internal/project"
)

func TestBuildCreateFromEmpty(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.pg.hcl")
	writePlanTestFile(t, schemaPath, `
schema "public" {}

table "users" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  primary_key {
    columns = [column.id]
  }
}
`)

	plan, err := Build(context.Background(), project.EnvConfig{
		SchemaPaths:  []string{schemaPath},
		MigrationDir: filepath.Join(dir, "migrations"),
	}, Options{Name: "create_users"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	got := planSQL(plan)
	if !strings.Contains(got, `CREATE TABLE "public"."users"`) {
		t.Fatalf("Build() SQL missing users table:\n%s", got)
	}
	if len(plan.Findings) != 0 {
		t.Fatalf("Build() findings = %#v, want none", plan.Findings)
	}
}

func TestBuildRejectsRoleBlocksWithoutManageRoles(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.pg.hcl")
	writePlanTestFile(t, schemaPath, `
schema "public" {}

role "app_reader" {}
`)

	_, err := Build(context.Background(), project.EnvConfig{
		SchemaPaths:  []string{schemaPath},
		MigrationDir: filepath.Join(dir, "migrations"),
	}, Options{Name: "roles"})
	if err == nil || !strings.Contains(err.Error(), "role blocks require --manage-roles") {
		t.Fatalf("Build() error = %v, want manage roles error", err)
	}
}

func TestDestructiveErrorCarriesFindings(t *testing.T) {
	err := error(DestructiveError{Findings: []Finding{{Kind: "destructive", Message: `DROP TABLE "users"`}}})
	var destructiveErr DestructiveError
	if !errors.As(err, &destructiveErr) {
		t.Fatalf("DestructiveError does not satisfy error")
	}
	if !strings.Contains(err.Error(), `DROP TABLE "users"`) {
		t.Fatalf("DestructiveError.Error() = %q", err.Error())
	}
}

func writePlanTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func planSQL(plan Plan) string {
	parts := make([]string, 0, len(plan.Statements))
	for _, statement := range plan.Statements {
		parts = append(parts, statement.SQL)
	}
	return strings.Join(parts, ";\n")
}
