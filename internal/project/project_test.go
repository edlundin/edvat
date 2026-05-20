package project

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/edlundin/edvat/internal/seed"
)

func TestLoadEnvAtlasSchemaSrcAndSeedMode(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeFile(t, configPath, `
env "local" {
  schema {
    src = "file://schema.pg.hcl"
  }
  migration {
    dir = "file://migrations"
  }
  data {
    mode = UPSERT
  }
}
`)
	got, err := LoadEnv(configPath, "local")
	if err != nil {
		t.Fatalf("LoadEnv() error = %v", err)
	}
	want := EnvConfig{
		Name:         "local",
		SchemaPaths:  []string{filepath.Join(dir, "schema.pg.hcl")},
		MigrationDir: filepath.Join(dir, "migrations"),
		SeedMode:     seed.ModeUpsert,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadEnv() = %#v, want %#v", got, want)
	}
}

func TestLoadEnvDataSrc(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
  data {
    mode = SYNC
    src = ["seed/countries.sql", "seed/roles.sql"]
  }
}
`)
	got, err := LoadEnv(configPath, "local")
	if err != nil {
		t.Fatalf("LoadEnv() error = %v", err)
	}
	want := []string{filepath.Join(dir, "seed", "countries.sql"), filepath.Join(dir, "seed", "roles.sql")}
	if !reflect.DeepEqual(got.SeedPaths, want) {
		t.Fatalf("SeedPaths = %#v, want %#v", got.SeedPaths, want)
	}
}

func TestLoadEnvCompatibilityDirectSrc(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeFile(t, configPath, `
env "dev" {
  src = "schema.pg.hcl"
  migration { dir = "migrations" }
}
`)
	got, err := LoadEnv(configPath, "dev")
	if err != nil {
		t.Fatalf("LoadEnv() error = %v", err)
	}
	if got.SchemaPaths[0] != filepath.Join(dir, "schema.pg.hcl") {
		t.Fatalf("SchemaPaths = %#v", got.SchemaPaths)
	}
	if got.SeedMode != seed.ModeInsert {
		t.Fatalf("SeedMode = %s, want INSERT", got.SeedMode)
	}
}

func TestLoadEnvCompatibilityDirectSrcTuple(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeFile(t, configPath, `
env "local" {
  src = ["one.pg.hcl", "two.pg.hcl"]
  migration { dir = "migrations" }
}
`)
	got, err := LoadEnv(configPath, "local")
	if err != nil {
		t.Fatalf("LoadEnv() error = %v", err)
	}
	want := []string{filepath.Join(dir, "one.pg.hcl"), filepath.Join(dir, "two.pg.hcl")}
	if !reflect.DeepEqual(got.SchemaPaths, want) {
		t.Fatalf("SchemaPaths = %#v, want %#v", got.SchemaPaths, want)
	}
}

func TestLoadEnvCompatibilityDirectSrcDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "schemas", "b.pg.hcl"), ``)
	writeFile(t, filepath.Join(dir, "schemas", "a.pg.hcl"), ``)
	writeFile(t, filepath.Join(dir, "schemas", "ignore.sql"), ``)
	configPath := filepath.Join(dir, "atlas.hcl")
	writeFile(t, configPath, `
env "local" {
  src = "file://schemas"
  migration { dir = "migrations" }
}
`)
	got, err := LoadEnv(configPath, "local")
	if err != nil {
		t.Fatalf("LoadEnv() error = %v", err)
	}
	want := []string{filepath.Join(dir, "schemas", "a.pg.hcl"), filepath.Join(dir, "schemas", "b.pg.hcl")}
	if !reflect.DeepEqual(got.SchemaPaths, want) {
		t.Fatalf("SchemaPaths = %#v, want %#v", got.SchemaPaths, want)
	}
}

func TestLoadEnvCompatibilityHCLSchemaDataSource(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeFile(t, configPath, `
data "hcl_schema" "app" {
  paths = ["one.pg.hcl", "two.pg.hcl"]
}

env "local" {
  src = data.hcl_schema.app.url
  migration { dir = "file://migrations" }
}
`)
	got, err := LoadEnv(configPath, "local")
	if err != nil {
		t.Fatalf("LoadEnv() error = %v", err)
	}
	want := []string{filepath.Join(dir, "one.pg.hcl"), filepath.Join(dir, "two.pg.hcl")}
	if !reflect.DeepEqual(got.SchemaPaths, want) {
		t.Fatalf("SchemaPaths = %#v, want %#v", got.SchemaPaths, want)
	}
}

func TestLoadEnvMissingEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeFile(t, configPath, `
env "local" {
  migration { dir = "migrations" }
}
`)
	_, err := LoadEnv(configPath, "prod")
	if !errors.Is(err, ErrMissingEnv) {
		t.Fatalf("LoadEnv() error = %v, want %v", err, ErrMissingEnv)
	}
}

func TestLoadEnvMissingMigrationDir(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
}
`)
	_, err := LoadEnv(configPath, "local")
	if !errors.Is(err, ErrMissingMigrationDir) {
		t.Fatalf("LoadEnv() error = %v, want %v", err, ErrMissingMigrationDir)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
