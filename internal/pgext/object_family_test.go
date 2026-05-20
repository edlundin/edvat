package pgext

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseStateFilesMergesParsedState(t *testing.T) {
	dir := t.TempDir()
	first := writeObjectFamilyTestFile(t, filepath.Join(dir, "first.hcl"), "first")
	second := writeObjectFamilyTestFile(t, filepath.Join(dir, "second.hcl"), "second")

	got, err := parseStateFiles([]string{first, second}, "test family", func(src []byte, filename string) (map[string]string, error) {
		return map[string]string{string(src): filename}, nil
	})
	if err != nil {
		t.Fatalf("parseStateFiles() error = %v", err)
	}
	want := map[string]string{"first": first, "second": second}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseStateFiles() = %#v, want %#v", got, want)
	}
}

func TestStateIDsDesiredFirstAndCurrentOnly(t *testing.T) {
	got := stateIDs(map[string]int{"b": 1, "c": 1}, map[string]int{"a": 1, "b": 2})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stateIDs() = %#v, want %#v", got, want)
	}
}

func writeObjectFamilyTestFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}
