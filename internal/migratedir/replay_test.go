package migratedir

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakeExec struct {
	statements []string
	failOn     string
}

func (f *fakeExec) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	f.statements = append(f.statements, query)
	if f.failOn != "" && strings.Contains(query, f.failOn) {
		return nil, errFakeExec
	}
	return nil, nil
}

var errFakeExec = os.ErrPermission

func TestReplayAppliesSQLFilesInNameOrder(t *testing.T) {
	dir := t.TempDir()
	writeMigration(t, dir, "002_second.sql", "SELECT 2;")
	writeMigration(t, dir, "001_first.sql", "SELECT 1;")
	writeMigration(t, dir, "001_first.down.sql", "SELECT down;")
	writeMigration(t, dir, "atlas.sum", "ignored")
	writeMigration(t, dir, "003_empty.sql", "  \n")

	exec := &fakeExec{}
	applied, err := Replay(context.Background(), exec, dir)
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	wantStatements := []string{"SELECT 1;", "SELECT 2;"}
	if !reflect.DeepEqual(exec.statements, wantStatements) {
		t.Fatalf("statements = %#v, want %#v", exec.statements, wantStatements)
	}
	if len(applied) != 2 || filepath.Base(applied[0].Path) != "001_first.sql" || filepath.Base(applied[1].Path) != "002_second.sql" {
		t.Fatalf("applied = %#v", applied)
	}
}

func TestReplayMissingDirIsNoop(t *testing.T) {
	applied, err := Replay(context.Background(), &fakeExec{}, filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("applied = %#v, want empty", applied)
	}
}

func TestReplayStopsOnError(t *testing.T) {
	dir := t.TempDir()
	writeMigration(t, dir, "001_first.sql", "SELECT 1;")
	writeMigration(t, dir, "002_bad.sql", "SELECT bad;")
	writeMigration(t, dir, "003_later.sql", "SELECT 3;")
	exec := &fakeExec{failOn: "bad"}
	applied, err := Replay(context.Background(), exec, dir)
	if err == nil || !strings.Contains(err.Error(), "002_bad.sql") {
		t.Fatalf("Replay() error = %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("applied = %#v, want first migration only", applied)
	}
}

func writeMigration(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}
}
