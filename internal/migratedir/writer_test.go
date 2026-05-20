package migratedir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edlundin/edvat/internal/baseatlas"

	"ariga.io/atlas/sql/migrate"
)

func TestWriterWrite(t *testing.T) {
	dir := t.TempDir()
	path, err := Writer{
		Dir:   dir,
		Clock: func() time.Time { return time.Date(2026, 5, 17, 12, 34, 56, 0, time.UTC) },
	}.Write("Create Users", []baseatlas.Statement{
		{Comment: `create "users" table`, SQL: `CREATE TABLE "users" ("id" integer)`},
		{SQL: `CREATE INDEX "idx_users_id" ON "users" ("id");`},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if filepath.Base(path) != "20260517123456_create_users.up.sql" {
		t.Fatalf("path = %s", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	got := string(content)
	for _, want := range []string{
		`-- create "users" table`,
		`CREATE TABLE "users" ("id" integer);`,
		`CREATE INDEX "idx_users_id" ON "users" ("id");`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("migration missing %q:\n%s", want, got)
		}
	}
	down, err := os.ReadFile(strings.TrimSuffix(path, ".up.sql") + ".down.sql")
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	if len(down) != 0 {
		t.Fatalf("empty down migration = %q", down)
	}
	if _, err := os.Stat(filepath.Join(dir, migrate.HashFileName)); err != nil {
		t.Fatalf("atlas.sum was not written: %v", err)
	}
	local, err := migrate.NewLocalDir(dir)
	if err != nil {
		t.Fatalf("NewLocalDir() error = %v", err)
	}
	if err := migrate.Validate(local); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestWriterWriteDownMigrationFromReverseStatements(t *testing.T) {
	dir := t.TempDir()
	path, err := Writer{
		Dir:   dir,
		Clock: func() time.Time { return time.Date(2026, 5, 17, 12, 34, 56, 0, time.UTC) },
	}.Write("Create Users", []baseatlas.Statement{
		{SQL: `CREATE TABLE "users" ("id" integer)`, Reverse: `DROP TABLE "users"`},
		{SQL: `CREATE INDEX "idx_users_id" ON "users" ("id")`, Reverse: `DROP INDEX "idx_users_id"`},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	downPath := strings.TrimSuffix(path, ".up.sql") + ".down.sql"
	content, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	got := string(content)
	first := strings.Index(got, `DROP INDEX "idx_users_id";`)
	second := strings.Index(got, `DROP TABLE "users";`)
	if first == -1 || second == -1 || first > second {
		t.Fatalf("down migration order/content wrong:\n%s", got)
	}
}

func TestWriterRejectsDuplicate(t *testing.T) {
	dir := t.TempDir()
	writer := Writer{Dir: dir, Clock: func() time.Time { return time.Date(2026, 5, 17, 12, 34, 56, 0, time.UTC) }}
	if _, err := writer.Write("create users", []baseatlas.Statement{{SQL: "SELECT 1"}}); err != nil {
		t.Fatalf("first Write() error = %v", err)
	}
	_, err := writer.Write("create users", []baseatlas.Statement{{SQL: "SELECT 2"}})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second Write() error = %v", err)
	}
}

func TestHashRewritesAtlasSum(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "20260517123456_create_users.up.sql"), []byte("SELECT 1;\n"), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}
	if err := Hash(dir); err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	local, err := migrate.NewLocalDir(dir)
	if err != nil {
		t.Fatalf("NewLocalDir() error = %v", err)
	}
	if err := migrate.Validate(local); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
