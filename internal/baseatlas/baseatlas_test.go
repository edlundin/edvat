package baseatlas

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ariga.io/atlas/sql/postgres"
	"ariga.io/atlas/sql/schema"
)

func TestEngineLoadDiffPlanSimplePostgresHCL(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.pg.hcl")
	writeFile(t, schemaPath, `
schema "public" {}

table "users" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "email" {
    null = false
    type = varchar(255)
  }
  primary_key {
    columns = [column.id]
  }
  index "idx_users_email" {
    columns = [column.email]
  }
}
`)

	engine := New()
	desired, err := engine.LoadDesired(context.Background(), ProjectConfig{SchemaPaths: []string{schemaPath}})
	if err != nil {
		t.Fatalf("LoadDesired() error = %v", err)
	}
	if len(desired.Schemas) != 1 || desired.Schemas[0].Name != "public" {
		t.Fatalf("LoadDesired() schemas = %#v", desired.Schemas)
	}

	changes, err := engine.Diff(context.Background(), nil, desired)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("Diff() returned no changes")
	}

	statements, err := engine.PlanSQL(context.Background(), "create_users", changes)
	if err != nil {
		t.Fatalf("PlanSQL() error = %v", err)
	}
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE SCHEMA IF NOT EXISTS "public"`,
		`CREATE TABLE "public"."users"`,
		`PRIMARY KEY ("id")`,
		`CREATE INDEX "idx_users_email"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("planned SQL missing %q:\n%s", want, got)
		}
	}
}

func TestEngineLoadDesiredIgnoresEdvatOwnedBlocks(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.pg.hcl")
	writeFile(t, schemaPath, `
schema "public" {}

extension "pgcrypto" {
  schema = schema.public
  version = "1.3"
}

view "active_countries" {
  schema = schema.public
  as = "SELECT id FROM countries"
}

table "countries" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  primary_key {
    columns = [column.id]
  }
}

data {
  table = table.countries
  rows = [
    { id = 1, code = "US" },
  ]
}

default_permission "country_reader" {
  schema = schema.public
  grantee = "reader"
  privileges = ["SELECT"]
}
`)
	if _, err := New().LoadDesired(context.Background(), ProjectConfig{SchemaPaths: []string{schemaPath}}); err != nil {
		t.Fatalf("LoadDesired() error = %v", err)
	}
}

func TestDiffTreatsPublicSchemaAsImplicit(t *testing.T) {
	current := publicImplicitFixture(`uuid_generate_v7()`, `kind = 'owner'::organization_deployment_kinds`)
	desired := publicImplicitFixture(`public.uuid_generate_v7()`, `kind = 'owner'::"public"."organization_deployment_kinds"`)
	current.Schemas[0].Tables[0].Columns[1].Type.Type = &postgres.UserDefinedType{T: "organization_deployment_kinds", C: "e"}
	current.Schemas[0].Tables[0].Columns[1].Type.Raw = `USER-DEFINED`
	desired.Schemas[0].Tables[0].Columns[1].Type.Raw = `"public"."organization_deployment_kinds"`
	desired.Schemas[0].Tables[0].Columns[1].Default = &schema.RawExpr{X: `'owner'::"public"."organization_deployment_kinds"`}

	changes, err := New().Diff(context.Background(), current, desired)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("Diff() changes = %#v, want none", changes)
	}
}

func publicImplicitFixture(defaultExpr, predicate string) *schema.Realm {
	public := &schema.Schema{Name: "public"}
	kind := &schema.EnumType{T: "organization_deployment_kinds", Schema: public, Values: []string{"owner", "shared_readonly"}}
	id := &schema.Column{Name: "id", Type: &schema.ColumnType{Type: &schema.UUIDType{T: "uuid"}}, Default: &schema.RawExpr{X: defaultExpr}}
	kindColumn := &schema.Column{Name: "kind", Type: &schema.ColumnType{Type: kind}, Default: &schema.RawExpr{X: `'owner'::organization_deployment_kinds`}}
	table := schema.NewTable("organization_deployments").AddColumns(id, kindColumn)
	table.Indexes = []*schema.Index{{Name: "organization_deployments_owner_deployment_unique", Unique: true, Table: table, Parts: []*schema.IndexPart{schema.NewColumnPart(id)}, Attrs: []schema.Attr{&postgres.IndexPredicate{P: predicate}}}}
	public.AddObjects(kind).AddTables(table)
	return schema.NewRealm(public)
}

func TestNormalizePlannedSQLWrapsTimezoneDefault(t *testing.T) {
	got := normalizePlannedSQL(`"created_at" timestamp(3) NOT NULL DEFAULT now() AT TIME ZONE 'utc'::text`)
	want := `"created_at" timestamp(3) NOT NULL DEFAULT (now() AT TIME ZONE 'utc'::text)`
	if got != want {
		t.Fatalf("normalizePlannedSQL() = %q, want %q", got, want)
	}
}

func TestEngineLoadDesiredRequiresSource(t *testing.T) {
	_, err := New().LoadDesired(context.Background(), ProjectConfig{})
	if !errors.Is(err, ErrNoSchemaSources) {
		t.Fatalf("LoadDesired() error = %v, want %v", err, ErrNoSchemaSources)
	}
}

func TestEngineInspectCurrentCanBeInjected(t *testing.T) {
	called := false
	engine := New()
	engine.Inspector = func(ctx context.Context, url string) (*schema.Realm, error) {
		called = url == "postgres://example/db"
		return nil, ErrInspectUnavailable
	}
	_, err := engine.InspectCurrent(context.Background(), "postgres://example/db")
	if !errors.Is(err, ErrInspectUnavailable) {
		t.Fatalf("InspectCurrent() error = %v", err)
	}
	if !called {
		t.Fatal("InspectCurrent() did not call injected inspector")
	}
}

func TestEngineInspectCurrentRequiresURLWithoutInjectedInspector(t *testing.T) {
	_, err := New().InspectCurrent(context.Background(), "")
	if !errors.Is(err, ErrInspectUnavailable) {
		t.Fatalf("InspectCurrent() error = %v, want %v", err, ErrInspectUnavailable)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func joinSQL(statements []Statement) string {
	parts := make([]string, 0, len(statements))
	for _, statement := range statements {
		parts = append(parts, statement.SQL)
	}
	return strings.Join(parts, ";\n")
}
