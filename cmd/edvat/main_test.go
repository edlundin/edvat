package main

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ariga.io/atlas/sql/migrate"
)

func TestAtlasCLIParityFixtureTableOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	atlasPath, devURL, edvatDir, atlasDir := setupAtlasParityFixture(t, ctx)
	writeAtlasParityProject(t, edvatDir, "migrations", true)
	writeAtlasParityProject(t, atlasDir, "file://migrations", false)

	edvatSQL, atlasSQL := runAtlasParityDiff(t, atlasPath, devURL, "create_users", edvatDir, atlasDir)
	assertParitySQLContains(t, edvatSQL, atlasSQL, []string{`CREATE TABLE "public"."orgs"`, `CREATE TABLE "public"."users"`, `PRIMARY KEY ("id")`, `CREATE INDEX "idx_users_email"`, `FOREIGN KEY ("org_id") REFERENCES "public"."orgs" ("id")`})
}

func TestAtlasCLIParityFixtureEnumTable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	atlasPath, devURL, edvatDir, atlasDir := setupAtlasParityFixture(t, ctx)
	writeAtlasEnumProject(t, edvatDir, "migrations", true)
	writeAtlasEnumProject(t, atlasDir, "file://migrations", false)

	edvatSQL, atlasSQL := runAtlasParityDiff(t, atlasPath, devURL, "create_status", edvatDir, atlasDir)
	assertParitySQLContains(t, edvatSQL, atlasSQL, []string{`CREATE TYPE "public"."status" AS ENUM`, `CREATE TABLE "public"."tasks"`, `"status" "public"."status"`})
	assertBefore(t, edvatSQL, `CREATE TYPE "public"."status"`, `CREATE TABLE "public"."tasks"`)
}

func TestAtlasCLIParityFixtureCheckConstraints(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	atlasPath, devURL, edvatDir, atlasDir := setupAtlasParityFixture(t, ctx)
	writeAtlasCheckProject(t, edvatDir, "migrations", true)
	writeAtlasCheckProject(t, atlasDir, "file://migrations", false)

	edvatSQL, atlasSQL := runAtlasParityDiff(t, atlasPath, devURL, "create_check_constraints", edvatDir, atlasDir)
	assertParitySQLContains(t, edvatSQL, atlasSQL, []string{`CREATE TABLE "public"."products"`, `CONSTRAINT "price_positive" CHECK (price > 0)`, `CONSTRAINT "sku_not_empty" CHECK`})
}

func writeAtlasCheckProject(t *testing.T, dir, migrationDir string, includeEdvatExtension bool) {
	t.Helper()
	writeTestFile(t, filepath.Join(dir, "atlas.hcl"), `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "`+migrationDir+`" }
}
`)
	extensionBlock := ""
	if includeEdvatExtension {
		extensionBlock = `
extension "plpgsql" {}
`
	}
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}
`+extensionBlock+`
table "products" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "price" {
    null = false
    type = int
  }
  column "sku" {
    null = false
    type = varchar(64)
  }
  primary_key { columns = [column.id] }
  check "price_positive" { expr = "price > 0" }
  check "sku_not_empty" { expr = "sku <> ''" }
}
`)
}

func TestAtlasCLIParityFixtureExclusionConstraints(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	atlasPath, devURL, edvatDir, atlasDir := setupAtlasParityFixture(t, ctx)
	writeAtlasExclusionProject(t, edvatDir, "migrations", true)
	writeAtlasExclusionProject(t, atlasDir, "file://migrations", false)

	edvatSQL, atlasSQL := runAtlasParityDiff(t, atlasPath, devURL, "create_exclusion_constraints", edvatDir, atlasDir)
	assertParitySQLContains(t, edvatSQL, atlasSQL, []string{`CREATE TABLE "public"."reservations"`, `CONSTRAINT "reservations_no_overlap" EXCLUDE USING`, `"period" WITH &&`, `INCLUDE ("note")`})
}

func TestAtlasCLIParityFixtureRichExclusionConstraints(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	atlasPath, devURL, edvatDir, atlasDir := setupAtlasParityFixture(t, ctx)
	writeAtlasRichExclusionProject(t, edvatDir, "migrations", true)
	writeAtlasRichExclusionProject(t, atlasDir, "file://migrations", false)

	edvatSQL, atlasSQL := runAtlasParityDiff(t, atlasPath, devURL, "create_rich_exclusion_constraints", edvatDir, atlasDir)
	assertParitySQLContains(t, edvatSQL, atlasSQL, []string{`CREATE TABLE "public"."reservations"`, `CONSTRAINT "reservations_room_open_period_excl" EXCLUDE USING`, `daterange(start_date, end_date, '[]'`, `cancelled_at IS NULL`})
}

func writeAtlasExclusionProject(t *testing.T, dir, migrationDir string, includeEdvatExtension bool) {
	t.Helper()
	writeTestFile(t, filepath.Join(dir, "atlas.hcl"), `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "`+migrationDir+`" }
}
`)
	extensionBlock := ""
	if includeEdvatExtension {
		extensionBlock = `
extension "plpgsql" {}
`
	}
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}
`+extensionBlock+`
table "reservations" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "period" {
    null = false
    type = tsrange
  }
  column "note" {
    null = true
    type = text
  }
  primary_key { columns = [column.id] }
  exclude "reservations_no_overlap" {
    type = "GIST"
    on {
      column = column.period
      op = "&&"
    }
    include = [column.note]
  }
}
`)
}

func writeAtlasRichExclusionProject(t *testing.T, dir, migrationDir string, includeEdvatExtension bool) {
	t.Helper()
	writeTestFile(t, filepath.Join(dir, "atlas.hcl"), `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "`+migrationDir+`" }
}
`)
	extensionBlock := ""
	if includeEdvatExtension {
		extensionBlock = `
extension "plpgsql" {}
`
	}
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}
`+extensionBlock+`
table "reservations" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "room_id" {
    null = false
    type = int
  }
  column "start_date" {
    null = false
    type = date
  }
  column "end_date" {
    null = false
    type = date
  }
  column "cancelled_at" {
    null = true
    type = timestamp
  }
  primary_key { columns = [column.id] }
  exclude "reservations_room_open_period_excl" {
    type = "GIST"
    on {
      expr = "daterange(start_date, end_date, '[]')"
      op = "&&"
    }
    where = "(cancelled_at IS NULL)"
  }
}
`)
}

func TestAtlasCLIParityFixtureListAndHashPartitions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	atlasPath, devURL, edvatDir, atlasDir := setupAtlasParityFixture(t, ctx)
	writeAtlasListHashPartitionProject(t, edvatDir, "migrations", true)
	writeAtlasListHashPartitionProject(t, atlasDir, "file://migrations", false)

	edvatSQL, atlasSQL := runAtlasParityDiffAllowingAtlasLoginFeatureSkip(t, atlasPath, devURL, "create_list_hash_partitions", edvatDir, atlasDir)
	assertParitySQLContains(t, edvatSQL, atlasSQL, []string{`CREATE TABLE "public"."events_by_region"`, `PARTITION BY LIST`, `CREATE TABLE "public"."events_eu" PARTITION OF "public"."events_by_region" FOR VALUES IN ('de', 'fr')`, `CREATE TABLE "public"."events_by_tenant"`, `PARTITION BY HASH`, `CREATE TABLE "public"."events_tenant_0" PARTITION OF "public"."events_by_tenant" FOR VALUES WITH (MODULUS 4, REMAINDER 0)`})
	assertBefore(t, edvatSQL, `CREATE TABLE "public"."events_by_region"`, `CREATE TABLE "public"."events_eu"`)
	assertBefore(t, edvatSQL, `CREATE TABLE "public"."events_by_tenant"`, `CREATE TABLE "public"."events_tenant_0"`)
}

func writeAtlasListHashPartitionProject(t *testing.T, dir, migrationDir string, includeEdvatExtension bool) {
	t.Helper()
	writeTestFile(t, filepath.Join(dir, "atlas.hcl"), `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "`+migrationDir+`" }
}
`)
	extensionBlock := ""
	if includeEdvatExtension {
		extensionBlock = `
extension "plpgsql" {}
`
	}
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}
`+extensionBlock+`
table "events_by_region" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "region" {
    null = false
    type = text
  }
  primary_key { columns = [column.id, column.region] }
  partition {
    type = LIST
    columns = [column.region]
  }
}

partition "events_eu" {
  schema = schema.public
  of = table.events_by_region
  list { in = ["'de'", "'fr'"] }
}

table "events_by_tenant" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "tenant_id" {
    null = false
    type = int
  }
  primary_key { columns = [column.id, column.tenant_id] }
  partition {
    type = HASH
    columns = [column.tenant_id]
  }
}

partition "events_tenant_0" {
  schema = schema.public
  of = table.events_by_tenant
  hash {
    modulus = 4
    remainder = 0
  }
}
`)
}

func TestAtlasCLIParityFixtureRangePartitions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	atlasPath, devURL, edvatDir, atlasDir := setupAtlasParityFixture(t, ctx)
	writeAtlasPartitionProject(t, edvatDir, "migrations", true)
	writeAtlasPartitionProject(t, atlasDir, "file://migrations", false)

	edvatSQL, atlasSQL := runAtlasParityDiffAllowingAtlasLoginFeatureSkip(t, atlasPath, devURL, "create_range_partitions", edvatDir, atlasDir)
	assertParitySQLContains(t, edvatSQL, atlasSQL, []string{`CREATE TABLE "public"."events"`, `PARTITION BY RANGE`, `CREATE TABLE "public"."events_2025" PARTITION OF "public"."events" FOR VALUES FROM ('2025-01-01') TO ('2026-01-01')`})
	assertBefore(t, edvatSQL, `CREATE TABLE "public"."events"`, `CREATE TABLE "public"."events_2025"`)
}

func writeAtlasPartitionProject(t *testing.T, dir, migrationDir string, includeEdvatExtension bool) {
	t.Helper()
	writeTestFile(t, filepath.Join(dir, "atlas.hcl"), `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "`+migrationDir+`" }
}
`)
	extensionBlock := ""
	if includeEdvatExtension {
		extensionBlock = `
extension "plpgsql" {}
`
	}
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}
`+extensionBlock+`
table "events" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "occurred_at" {
    null = false
    type = date
  }
  primary_key { columns = [column.id, column.occurred_at] }
  partition {
    type = RANGE
    columns = [column.occurred_at]
  }
}

partition "events_2025" {
  schema = schema.public
  of = table.events
  range {
    from = ["'2025-01-01'"]
    to = ["'2026-01-01'"]
  }
}
`)
}

func TestAtlasCLIExistingStateParityEnumAddValue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	atlasPath, currentURL, edvatDir, atlasDir := setupAtlasParityFixture(t, ctx)
	atlasDevURL := startTestPostgres(t, ctx, "postgres:16-alpine")
	prepareExistingStateEnumDatabase(t, ctx, currentURL)
	writeAtlasExistingStateEnumProject(t, edvatDir, "migrations", true)
	writeAtlasExistingStateEnumProject(t, atlasDir, "file://migrations", false)

	edvatSQL, atlasSQL := runAtlasExistingStateParityDiff(t, atlasPath, currentURL, atlasDevURL, "add_status_done", edvatDir, atlasDir)
	assertParitySQLContains(t, edvatSQL, atlasSQL, []string{`ALTER TYPE "public"."status" ADD VALUE 'done'`})
}

func TestAtlasCLIExistingStateParityAddColumnAndIndex(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	atlasPath, currentURL, edvatDir, atlasDir := setupAtlasParityFixture(t, ctx)
	atlasDevURL := startTestPostgres(t, ctx, "postgres:16-alpine")
	prepareExistingStateParityDatabase(t, ctx, currentURL)
	writeAtlasExistingStateProject(t, edvatDir, "migrations", true)
	writeAtlasExistingStateProject(t, atlasDir, "file://migrations", false)

	edvatSQL, atlasSQL := runAtlasExistingStateParityDiff(t, atlasPath, currentURL, atlasDevURL, "add_user_name", edvatDir, atlasDir)
	assertParitySQLContains(t, edvatSQL, atlasSQL, []string{`ALTER TABLE "public"."users" ADD COLUMN "name"`, `CREATE INDEX "idx_users_name"`})
}

func TestAtlasCLIParityFixtureUniqueIndexes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	atlasPath, devURL, edvatDir, atlasDir := setupAtlasParityFixture(t, ctx)
	writeAtlasUniqueProject(t, edvatDir, "migrations", true)
	writeAtlasUniqueProject(t, atlasDir, "file://migrations", false)

	edvatSQL, atlasSQL := runAtlasParityDiff(t, atlasPath, devURL, "create_unique_indexes", edvatDir, atlasDir)
	assertParitySQLContains(t, edvatSQL, atlasSQL, []string{`CREATE TABLE "public"."accounts"`, `CREATE UNIQUE INDEX "accounts_email_key"`, `CREATE UNIQUE INDEX "accounts_tenant_slug_key"`})
}

func prepareExistingStateEnumDatabase(t *testing.T, ctx context.Context, devURL string) {
	t.Helper()
	db, err := sql.Open("postgres", devURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS "public"."tasks"`,
		`DROP TYPE IF EXISTS "public"."status"`,
		`CREATE TYPE "public"."status" AS ENUM ('todo')`,
		`CREATE TABLE "public"."tasks" ("id" integer NOT NULL, "status" "public"."status" NOT NULL, PRIMARY KEY ("id"))`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("exec %s: %v", stmt, err)
		}
	}
}

func writeAtlasExistingStateEnumProject(t *testing.T, dir, migrationDir string, includeEdvatExtension bool) {
	t.Helper()
	writeTestFile(t, filepath.Join(dir, "atlas.hcl"), `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "`+migrationDir+`" }
}
`)
	extensionBlock := ""
	if includeEdvatExtension {
		extensionBlock = `
extension "plpgsql" {}
`
	}
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}
`+extensionBlock+`
enum "status" {
  schema = schema.public
  values = ["todo", "done"]
}

table "tasks" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "status" {
    null = false
    type = enum.status
  }
  primary_key { columns = [column.id] }
}
`)
}

func prepareExistingStateParityDatabase(t *testing.T, ctx context.Context, devURL string) {
	t.Helper()
	db, err := sql.Open("postgres", devURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS "public"."users"`,
		`CREATE TABLE "public"."users" ("id" integer NOT NULL, PRIMARY KEY ("id"))`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("exec %s: %v", stmt, err)
		}
	}
}

func writeAtlasExistingStateProject(t *testing.T, dir, migrationDir string, includeEdvatExtension bool) {
	t.Helper()
	writeTestFile(t, filepath.Join(dir, "atlas.hcl"), `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "`+migrationDir+`" }
}
`)
	extensionBlock := ""
	if includeEdvatExtension {
		extensionBlock = `
extension "plpgsql" {}
`
	}
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}
`+extensionBlock+`
table "users" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "name" {
    null = true
    type = varchar(255)
  }
  primary_key { columns = [column.id] }
  index "idx_users_name" {
    columns = [column.name]
  }
}
`)
}

func writeAtlasUniqueProject(t *testing.T, dir, migrationDir string, includeEdvatExtension bool) {
	t.Helper()
	writeTestFile(t, filepath.Join(dir, "atlas.hcl"), `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "`+migrationDir+`" }
}
`)
	extensionBlock := ""
	if includeEdvatExtension {
		extensionBlock = `
extension "plpgsql" {}
`
	}
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}
`+extensionBlock+`
table "accounts" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "tenant_id" {
    null = false
    type = int
  }
  column "email" {
    null = false
    type = varchar(255)
  }
  column "slug" {
    null = false
    type = varchar(255)
  }
  primary_key { columns = [column.id] }
  index "accounts_email_key" {
    unique = true
    columns = [column.email]
  }
  index "accounts_tenant_slug_key" {
    unique = true
    columns = [column.tenant_id, column.slug]
  }
}
`)
}

func setupAtlasParityFixture(t *testing.T, ctx context.Context) (string, string, string, string) {
	t.Helper()
	devURL := startTestPostgres(t, ctx, "postgres:16-alpine")
	atlasPath, err := exec.LookPath("atlas")
	if err != nil {
		t.Skip("atlas CLI not found")
	}
	return atlasPath, devURL, t.TempDir(), t.TempDir()
}

func assertParitySQLContains(t *testing.T, edvatSQL, atlasSQL string, wants []string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(edvatSQL, want) {
			t.Fatalf("edvat migration missing %q:\n%s", want, edvatSQL)
		}
		if !strings.Contains(atlasSQL, want) {
			t.Fatalf("atlas migration missing %q:\n%s", want, atlasSQL)
		}
	}
}

func runAtlasParityDiff(t *testing.T, atlasPath, devURL, name, edvatDir, atlasDir string) (string, string) {
	t.Helper()
	return runAtlasParityDiffWithOptions(t, atlasPath, devURL, name, edvatDir, atlasDir, false)
}

func runAtlasParityDiffAllowingAtlasLoginFeatureSkip(t *testing.T, atlasPath, devURL, name, edvatDir, atlasDir string) (string, string) {
	t.Helper()
	return runAtlasParityDiffWithOptions(t, atlasPath, devURL, name, edvatDir, atlasDir, true)
}

func runAtlasParityDiffWithOptions(t *testing.T, atlasPath, devURL, name, edvatDir, atlasDir string, skipLoginFeature bool) (string, string) {
	t.Helper()
	if err := migrateDiff([]string{name, "--config", filepath.Join(edvatDir, "atlas.hcl"), "--env", "local", "--dev-url", devURL}); err != nil {
		t.Fatalf("edvat migrateDiff() error = %v", err)
	}
	cmd := exec.Command(atlasPath, "migrate", "diff", name, "--config", "file://"+filepath.Join(atlasDir, "atlas.hcl"), "--env", "local", "--dev-url", devURL)
	cmd.Dir = atlasDir
	if out, err := cmd.CombinedOutput(); err != nil {
		if skipLoginFeature && strings.Contains(string(out), "available to logged-in users only") {
			t.Skipf("atlas CLI requires login for this parity fixture: %s", out)
		}
		t.Fatalf("atlas migrate diff error = %v\n%s", err, out)
	}
	return readOnlyMigrationSQL(t, filepath.Join(edvatDir, "migrations")), readOnlyMigrationSQL(t, filepath.Join(atlasDir, "migrations"))
}

func runAtlasExistingStateParityDiff(t *testing.T, atlasPath, currentURL, atlasDevURL, name, edvatDir, atlasDir string) (string, string) {
	t.Helper()
	if err := migrateDiff([]string{name, "--config", filepath.Join(edvatDir, "atlas.hcl"), "--env", "local", "--dev-url", currentURL}); err != nil {
		t.Fatalf("edvat migrateDiff() error = %v", err)
	}
	cmd := exec.Command(atlasPath, "schema", "diff", "--from", currentURL, "--to", "file://"+filepath.Join(atlasDir, "schema.pg.hcl"), "--dev-url", atlasDevURL, "--format", `{{ sql . "  " }}`)
	cmd.Dir = atlasDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("atlas schema diff error = %v\n%s", err, out)
	}
	return readOnlyMigrationSQL(t, filepath.Join(edvatDir, "migrations")), string(out)
}

func writeAtlasEnumProject(t *testing.T, dir, migrationDir string, includeEdvatExtension bool) {
	t.Helper()
	writeTestFile(t, filepath.Join(dir, "atlas.hcl"), `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "`+migrationDir+`" }
}
`)
	extensionBlock := ""
	if includeEdvatExtension {
		extensionBlock = `
extension "plpgsql" {}
`
	}
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}
`+extensionBlock+`
enum "status" {
  schema = schema.public
  values = ["todo", "done"]
}

table "tasks" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "status" {
    null = false
    type = enum.status
  }
  primary_key { columns = [column.id] }
}
`)
}

func writeAtlasParityProject(t *testing.T, dir, migrationDir string, includeEdvatExtension bool) {
	t.Helper()
	writeTestFile(t, filepath.Join(dir, "atlas.hcl"), `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "`+migrationDir+`" }
}
`)
	extensionBlock := ""
	if includeEdvatExtension {
		extensionBlock = `
extension "plpgsql" {}
`
	}
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}
`+extensionBlock+`
table "orgs" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  primary_key { columns = [column.id] }
}

table "users" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "org_id" {
    null = false
    type = int
  }
  column "email" {
    null = false
    type = varchar(255)
  }
  primary_key { columns = [column.id] }
  foreign_key "users_org_id_fkey" {
    columns = [column.org_id]
    ref_columns = [table.orgs.column.id]
  }
  index "idx_users_email" { columns = [column.email] }
}
`)
}

func TestMigrateDiffGeneratesExpressionExclusionConstraint(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeTestFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
}
`)
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}

table "reservations" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "period" {
    null = false
    type = tsrange
  }
  primary_key { columns = [column.id] }
  exclude "reservations_no_overlap" {
    type = "GIST"
    on {
      expr = "lower(period)"
      op = "&&"
    }
  }
}
`)
	if err := migrateDiff([]string{"add_expression_exclusion", "--config", configPath, "--env", "local"}); err != nil {
		t.Fatalf("migrateDiff() error = %v", err)
	}
	got := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	want := `ALTER TABLE "public"."reservations" ADD CONSTRAINT "reservations_no_overlap" EXCLUDE USING gist ((lower(period)) WITH &&)`
	if !strings.Contains(got, want) {
		t.Fatalf("generated migration missing %q:\n%s", want, got)
	}
}

func TestMigrateDiffGeneratesExclusionConstraintWherePredicate(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeTestFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
}
`)
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}

table "reservations" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "period" {
    null = false
    type = tsrange
  }
  primary_key { columns = [column.id] }
  exclude "reservations_no_overlap" {
    type = "GIST"
    on {
      column = column.period
      op = "&&"
    }
    where = "period IS NOT NULL"
  }
}
`)
	if err := migrateDiff([]string{"add_exclusion_where", "--config", configPath, "--env", "local"}); err != nil {
		t.Fatalf("migrateDiff() error = %v", err)
	}
	got := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	want := `ALTER TABLE "public"."reservations" ADD CONSTRAINT "reservations_no_overlap" EXCLUDE USING gist ("period" WITH &&) WHERE (period IS NOT NULL)`
	if !strings.Contains(got, want) {
		t.Fatalf("generated migration missing %q:\n%s", want, got)
	}
}

func TestMigrateDiffGeneratesListAndHashPartitions(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeTestFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
}
`)
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}

table "events_by_region" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "region" {
    null = false
    type = text
  }
  primary_key { columns = [column.id, column.region] }
  partition {
    type = LIST
    columns = [column.region]
  }
}

partition "events_eu" {
  schema = schema.public
  of = table.events_by_region
  list { in = ["'de'", "'fr'"] }
}

table "events_by_tenant" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "tenant_id" {
    null = false
    type = int
  }
  primary_key { columns = [column.id, column.tenant_id] }
  partition {
    type = HASH
    columns = [column.tenant_id]
  }
}

partition "events_tenant_0" {
  schema = schema.public
  of = table.events_by_tenant
  hash {
    modulus = 4
    remainder = 0
  }
}
`)
	if err := migrateDiff([]string{"add_list_hash_partitions", "--config", configPath, "--env", "local"}); err != nil {
		t.Fatalf("migrateDiff() error = %v", err)
	}
	got := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	for _, want := range []string{
		`CREATE TABLE "public"."events_by_region"`,
		`PARTITION BY LIST`,
		`CREATE TABLE "public"."events_eu" PARTITION OF "public"."events_by_region" FOR VALUES IN ('de', 'fr')`,
		`CREATE TABLE "public"."events_by_tenant"`,
		`PARTITION BY HASH`,
		`CREATE TABLE "public"."events_tenant_0" PARTITION OF "public"."events_by_tenant" FOR VALUES WITH (MODULUS 4, REMAINDER 0)`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated migration missing %q:\n%s", want, got)
		}
	}
	assertBefore(t, got, `CREATE TABLE "public"."events_by_region"`, `CREATE TABLE "public"."events_eu"`)
	assertBefore(t, got, `CREATE TABLE "public"."events_by_tenant"`, `CREATE TABLE "public"."events_tenant_0"`)
}

func TestMigrateDiffGeneratesRangePartitionsAfterParentTable(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeTestFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
}
`)
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}

table "events" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "occurred_at" {
    null = false
    type = date
  }
  primary_key { columns = [column.id, column.occurred_at] }
  partition {
    type = RANGE
    columns = [column.occurred_at]
  }
}

partition "events_2025" {
  schema = schema.public
  of = table.events
  range {
    from = ["'2025-01-01'"]
    to = ["'2026-01-01'"]
  }
}
`)
	if err := migrateDiff([]string{"add_range_partition", "--config", configPath, "--env", "local"}); err != nil {
		t.Fatalf("migrateDiff() error = %v", err)
	}
	got := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	for _, want := range []string{
		`CREATE TABLE "public"."events"`,
		`PARTITION BY RANGE`,
		`CREATE TABLE "public"."events_2025" PARTITION OF "public"."events" FOR VALUES FROM ('2025-01-01') TO ('2026-01-01')`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated migration missing %q:\n%s", want, got)
		}
	}
	assertBefore(t, got, `CREATE TABLE "public"."events"`, `CREATE TABLE "public"."events_2025"`)
}

func TestMigrateDiffGeneratesPriorityObjectsInDependencyOrder(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeTestFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
}
`)
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}

extension "pgcrypto" {
  schema = schema.public
}

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
  primary_key { columns = [column.id] }
}

function "touch_updated_at" {
  schema = schema.public
  lang = PLPGSQL
  return = trigger
  as = "BEGIN RETURN NEW; END"
}

procedure "touch_user" {
  schema = schema.public
  lang = SQL
  arg "id" { type = integer }
  as = "UPDATE users SET email = email WHERE users.id = touch_user.id"
}

trigger "users_touch" {
  on = schema.public.users
  timing = BEFORE
  events = [UPDATE]
  update_of = [email]
  execute = schema.public.touch_updated_at
}
`)
	if err := migrateDiff([]string{"add_priority_objects", "--config", configPath, "--env", "local"}); err != nil {
		t.Fatalf("migrateDiff() error = %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "migrations"))
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var sqlPath string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			sqlPath = filepath.Join(dir, "migrations", entry.Name())
			break
		}
	}
	if sqlPath == "" {
		t.Fatal("no generated migration sql")
	}
	body, err := os.ReadFile(sqlPath)
	if err != nil {
		t.Fatalf("read generated migration: %v", err)
	}
	got := string(body)
	wants := []string{
		`CREATE EXTENSION "pgcrypto" WITH SCHEMA "public"`,
		`CREATE TABLE "public"."users"`,
		`CREATE OR REPLACE FUNCTION "public"."touch_updated_at"() RETURNS trigger LANGUAGE PLPGSQL`,
		`CREATE OR REPLACE PROCEDURE "public"."touch_user"("id" integer) LANGUAGE SQL`,
		`CREATE TRIGGER "users_touch" BEFORE UPDATE OF "email" ON "public"."users" EXECUTE FUNCTION "public"."touch_updated_at"()`,
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("generated migration missing %q:\n%s", want, got)
		}
	}
	assertBefore(t, got, `CREATE EXTENSION`, `CREATE TABLE`)
	assertBefore(t, got, `CREATE OR REPLACE FUNCTION`, `CREATE TRIGGER`)
	assertBefore(t, got, `CREATE OR REPLACE PROCEDURE`, `CREATE TRIGGER`)
}

func TestMigrateDiffDoesNotWriteMigrationWhenPlanIsEmpty(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeTestFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
}
`)
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `schema "public" {}`)

	if err := migrateDiff([]string{"empty", "--config", configPath, "--env", "local"}); err != nil {
		t.Fatalf("migrateDiff() error = %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "migrations"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read migrations: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			t.Fatalf("wrote migration for empty plan: %s", entry.Name())
		}
	}
}

func TestMigrateDiffRequiresDevURLWhenMigrationsExist(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeTestFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
}
`)
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `schema "public" {}`)
	if err := os.Mkdir(filepath.Join(dir, "migrations"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "migrations", "20260101000000_init.up.sql"), `CREATE EXTENSION "pgcrypto" WITH SCHEMA "public";`)

	err := migrateDiff([]string{"voleo", "--config", configPath, "--env", "local"})
	if err == nil || !strings.Contains(err.Error(), "--dev-url is required when migration dir is not empty") {
		t.Fatalf("migrateDiff() error = %v, want --dev-url requirement", err)
	}
}

func TestMigrateDiffRejectsInvalidDefaultPermissionPrivilege(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeTestFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
}
`)
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}

default_permission "bad" {
  schema = schema.public
  on = "TABLES"
  to = "reader"
  privileges = ["EXECUTE"]
}
`)
	err := migrateDiff([]string{"bad_default_permission", "--config", configPath, "--env", "local"})
	if err == nil || !strings.Contains(err.Error(), "unsupported privilege") {
		t.Fatalf("migrateDiff() error = %v, want unsupported privilege", err)
	}
}

func TestMigrateDiffGeneratesDefaultPermissionFunctionsAndTypes(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeTestFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
}
`)
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}

default_permission "reader_execute_functions" {
  schema = schema.public
  for_role = "app_owner"
  on = "FUNCTIONS"
  to = "reader"
  privileges = ["EXECUTE"]
}

default_permission "reader_use_types" {
  schema = schema.public
  for_role = "app_owner"
  on = "TYPES"
  to = "reader"
  privileges = ["USAGE"]
}
`)
	if err := migrateDiff([]string{"add_default_permission_function_types", "--config", configPath, "--env", "local"}); err != nil {
		t.Fatalf("migrateDiff() error = %v", err)
	}
	got := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	for _, want := range []string{
		`ALTER DEFAULT PRIVILEGES FOR ROLE "app_owner" IN SCHEMA "public" GRANT EXECUTE ON FUNCTIONS TO "reader"`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE "app_owner" IN SCHEMA "public" GRANT USAGE ON TYPES TO "reader"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated migration missing %q:\n%s", want, got)
		}
	}
}

func TestMigrateDiffGeneratesDefaultPermissionGrantOption(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeTestFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
}
`)
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}

default_permission "reader_use_sequences" {
  schema = schema.public
  for_role = "app_owner"
  on = "SEQUENCES"
  to = "reader"
  privileges = ["USAGE", "SELECT"]
  grantable = true
}
`)
	if err := migrateDiff([]string{"add_default_permission_grant_option", "--config", configPath, "--env", "local"}); err != nil {
		t.Fatalf("migrateDiff() error = %v", err)
	}
	got := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	want := `ALTER DEFAULT PRIVILEGES FOR ROLE "app_owner" IN SCHEMA "public" GRANT SELECT, USAGE ON SEQUENCES TO "reader" WITH GRANT OPTION`
	if !strings.Contains(got, want) {
		t.Fatalf("generated migration missing %q:\n%s", want, got)
	}
}

func TestMigrateDiffGeneratesDefaultPermissions(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeTestFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
}
`)
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}

default_permission "public_read_tables" {
  schema = schema.public
  on = "TABLES"
  to = "public"
  privileges = ["SELECT"]
}
`)
	if err := migrateDiff([]string{"add_default_permissions", "--config", configPath, "--env", "local"}); err != nil {
		t.Fatalf("migrateDiff() error = %v", err)
	}
	got := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	want := `ALTER DEFAULT PRIVILEGES IN SCHEMA "public" GRANT SELECT ON TABLES TO PUBLIC`
	if !strings.Contains(got, want) {
		t.Fatalf("generated migration missing %q:\n%s", want, got)
	}
}

func TestMigrateDiffGeneratesSyncSeedDataWithExplicitKey(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeTestFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
  data { mode = SYNC }
}
`)
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}

table "countries" {
  schema = schema.public
  column "code" {
    null = false
    type = varchar(2)
  }
  column "name" {
    null = false
    type = varchar(255)
  }
  index "countries_code_key" {
    unique = true
    columns = [column.code]
  }
}

data {
  table = schema.public.countries
  key = [code]
  rows = [
    { code = "US", name = "United States" },
    { code = "IL", name = "Israel" },
  ]
}
`)
	if err := migrateDiff([]string{"seed_sync_key", "--config", configPath, "--env", "local"}); err != nil {
		t.Fatalf("migrateDiff() error = %v", err)
	}
	got := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	for _, want := range []string{
		`INSERT INTO "public"."countries" ("code", "name") VALUES ('US', 'United States') ON CONFLICT ("code") DO UPDATE SET "name" = EXCLUDED."name"`,
		`INSERT INTO "public"."countries" ("code", "name") VALUES ('IL', 'Israel') ON CONFLICT ("code") DO UPDATE SET "name" = EXCLUDED."name"`,
		`DELETE FROM "public"."countries" WHERE "code" NOT IN ('US', 'IL')`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated migration missing %q:\n%s", want, got)
		}
	}
}

func TestMigrateDiffGeneratesUpsertSeedDataWithExplicitKey(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeTestFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
  data { mode = UPSERT }
}
`)
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}

table "countries" {
  schema = schema.public
  column "code" {
    null = false
    type = varchar(2)
  }
  column "name" {
    null = false
    type = varchar(255)
  }
  index "countries_code_key" {
    unique = true
    columns = [column.code]
  }
}

data {
  table = schema.public.countries
  key = [code]
  rows = [
    { code = "US", name = "United States" },
  ]
}
`)
	if err := migrateDiff([]string{"seed_upsert_key", "--config", configPath, "--env", "local"}); err != nil {
		t.Fatalf("migrateDiff() error = %v", err)
	}
	got := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	want := `INSERT INTO "public"."countries" ("code", "name") VALUES ('US', 'United States') ON CONFLICT ("code") DO UPDATE SET "name" = EXCLUDED."name"`
	if !strings.Contains(got, want) {
		t.Fatalf("generated migration missing %q:\n%s", want, got)
	}
}

func TestMigrateDiffGeneratesUpsertSeedData(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeTestFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
  data { mode = UPSERT }
}
`)
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}

table "countries" {
  schema = schema.public
  column "id" {
    null = false
    type = int
  }
  column "code" {
    null = false
    type = varchar(2)
  }
  column "name" {
    null = false
    type = varchar(255)
  }
  primary_key { columns = [column.id] }
}

data {
  table = schema.public.countries
  rows = [
    { id = 1, code = "US", name = "United States" },
  ]
}
`)
	if err := migrateDiff([]string{"seed_upsert", "--config", configPath, "--env", "local"}); err != nil {
		t.Fatalf("migrateDiff() error = %v", err)
	}
	got := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	want := `INSERT INTO "public"."countries" ("id", "code", "name") VALUES (1, 'US', 'United States') ON CONFLICT ("id") DO UPDATE SET "code" = EXCLUDED."code", "name" = EXCLUDED."name"`
	if !strings.Contains(got, want) {
		t.Fatalf("generated migration missing %q:\n%s", want, got)
	}
}

func TestMigrateDiffIncludesSQLSeedFiles(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeTestFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
  data { src = ["seed/countries.sql"] }
}
`)
	writeTestFile(t, filepath.Join(dir, "schema.pg.hcl"), `
schema "public" {}

table "countries" {
  schema = schema.public
  column "code" { type = varchar(2) }
  primary_key { columns = [column.code] }
}
`)
	if err := os.MkdirAll(filepath.Join(dir, "seed"), 0o755); err != nil {
		t.Fatalf("mkdir seed: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "seed", "countries.sql"), `
INSERT INTO "public"."countries" ("code") VALUES ('US;A') ON CONFLICT DO NOTHING;
INSERT INTO "public"."countries" ("code") VALUES ('IL') ON CONFLICT DO NOTHING;
`)
	if err := migrateDiff([]string{"sql_seed", "--config", configPath, "--env", "local"}); err != nil {
		t.Fatalf("migrateDiff() error = %v", err)
	}
	got := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	for _, want := range []string{
		`INSERT INTO "public"."countries" ("code") VALUES ('US;A') ON CONFLICT DO NOTHING`,
		`INSERT INTO "public"."countries" ("code") VALUES ('IL') ON CONFLICT DO NOTHING`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated migration missing %q:\n%s", want, got)
		}
	}
}

func TestMigrateHash(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "atlas.hcl")
	writeTestFile(t, configPath, `
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
}
`)
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		t.Fatalf("mkdir migrations: %v", err)
	}
	writeTestFile(t, filepath.Join(migrationsDir, "20260517123456_create_users.sql"), "SELECT 1;\n")

	if err := migrateHash([]string{"--config", configPath, "--env", "local"}); err != nil {
		t.Fatalf("migrateHash() error = %v", err)
	}
	local, err := migrate.NewLocalDir(migrationsDir)
	if err != nil {
		t.Fatalf("NewLocalDir() error = %v", err)
	}
	if err := migrate.Validate(local); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func assertBefore(t *testing.T, got, first, second string) {
	t.Helper()
	firstIndex := strings.Index(got, first)
	secondIndex := strings.Index(got, second)
	if firstIndex == -1 || secondIndex == -1 || firstIndex > secondIndex {
		t.Fatalf("expected %q before %q:\n%s", first, second, got)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}
