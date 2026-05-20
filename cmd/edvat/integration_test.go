package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edlundin/edvat/internal/pgext"

	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestLivePriorityMigrationRoundTrip(t *testing.T) {
	images := livePostgresImages()
	for _, image := range images {
		t.Run(image, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			url := startTestPostgres(t, ctx, image)
			runPriorityMigrationRoundTrip(t, ctx, url)
		})
	}
}

func TestLivePostgresImagesDefaultAndOverride(t *testing.T) {
	t.Setenv("EDVAT_TEST_DATABASE_URL", "")
	t.Setenv("EDVAT_TEST_POSTGRES_IMAGES", "")
	got := strings.Join(livePostgresImages(), ",")
	want := "postgres:15-alpine,postgres:16-alpine,postgres:17-alpine"
	if got != want {
		t.Fatalf("livePostgresImages() = %q, want %q", got, want)
	}
	t.Setenv("EDVAT_TEST_POSTGRES_IMAGES", "postgres:16-alpine, postgres:17-alpine")
	got = strings.Join(livePostgresImages(), ",")
	want = "postgres:16-alpine,postgres:17-alpine"
	if got != want {
		t.Fatalf("livePostgresImages() override = %q, want %q", got, want)
	}
}

func livePostgresImages() []string {
	if os.Getenv("EDVAT_TEST_DATABASE_URL") != "" {
		return []string{"external"}
	}
	if configured := strings.TrimSpace(os.Getenv("EDVAT_TEST_POSTGRES_IMAGES")); configured != "" {
		parts := strings.Split(configured, ",")
		images := make([]string, 0, len(parts))
		for _, part := range parts {
			if image := strings.TrimSpace(part); image != "" {
				images = append(images, image)
			}
		}
		if len(images) > 0 {
			return images
		}
	}
	return []string{"postgres:15-alpine", "postgres:16-alpine", "postgres:17-alpine"}
}

func runPriorityMigrationRoundTrip(t *testing.T, ctx context.Context, url string) {
	t.Helper()
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}

	schemaName := "edvat_live_priority"
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS "edvat_live_priority" CASCADE`); err != nil {
		t.Fatalf("drop test schema before test: %v", err)
	}
	defer db.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS "edvat_live_priority" CASCADE`)

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
schema "edvat_live_priority" {}

extension "pgcrypto" {
  schema = schema.public
}
extension "plpgsql" {}

table "users" {
  schema = schema.edvat_live_priority
  column "id" {
    null = false
    type = int
  }
  column "email" {
    null = false
    type = varchar(255)
  }
  column "tenant_id" {
    null = false
    type = int
  }
  primary_key { columns = [column.id] }
}

function "a_outer_email" {
  schema = schema.edvat_live_priority
  lang = SQL
  arg "id" { type = integer }
  return = varchar
  as = "SELECT edvat_live_priority.z_inner_email(id)"
}

function "touch_updated_at" {
  schema = schema.edvat_live_priority
  lang = PLPGSQL
  return = trigger
  as = "BEGIN NEW.email = COALESCE(TG_ARGV[0], NEW.email); RETURN NEW; END"
}

function "z_inner_email" {
  schema = schema.edvat_live_priority
  lang = SQL
  arg "id" { type = integer }
  return = varchar
  as = "SELECT email FROM edvat_live_priority.users WHERE users.id = z_inner_email.id"
}

procedure "touch_user" {
  schema = schema.edvat_live_priority
  lang = SQL
  arg "id" { type = integer }
  as = "UPDATE edvat_live_priority.users SET email = email WHERE users.id = touch_user.id"
}

policy "tenant_select" {
  on = schema.edvat_live_priority.users
  for = SELECT
  to = [public]
  using = "tenant_id = current_setting('app.tenant_id')::integer"
  comment = "tenant reads"
}

permission "public_read_users" {
  on = schema.edvat_live_priority.users
  to = public
  privileges = [SELECT]
}

view "a_outer_user_emails" {
  schema = schema.edvat_live_priority
  as = "SELECT email FROM edvat_live_priority.z_inner_user_emails"
}

view "z_inner_user_emails" {
  schema = schema.edvat_live_priority
  as = "SELECT email FROM edvat_live_priority.users"
}

trigger "users_touch" {
  on = schema.edvat_live_priority.users
  timing = BEFORE
  events = [UPDATE]
  update_of = [email]
  execute = schema.edvat_live_priority.touch_updated_at
  for_each = ROW
  args = ["touched@example.com"]
  when = "OLD.email IS DISTINCT FROM NEW.email"
}
`)

	if err := migrateDiff([]string{"priority_round_trip", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		t.Fatalf("initial migrateDiff() error = %v", err)
	}
	firstSQL := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	for _, want := range []string{`CREATE EXTENSION "pgcrypto"`, "CREATE TABLE", `CREATE OR REPLACE FUNCTION "edvat_live_priority"."a_outer_email"`, `CREATE OR REPLACE FUNCTION "edvat_live_priority"."z_inner_email"`, "CREATE OR REPLACE PROCEDURE", `CREATE POLICY "tenant_select"`, `GRANT SELECT ON TABLE "edvat_live_priority"."users" TO PUBLIC`, `CREATE VIEW "edvat_live_priority"."a_outer_user_emails"`, `CREATE VIEW "edvat_live_priority"."z_inner_user_emails"`, "CREATE TRIGGER", `FOR EACH ROW`, `WHEN (OLD.email IS DISTINCT FROM NEW.email)`, `'touched@example.com'`} {
		if !strings.Contains(firstSQL, want) {
			t.Fatalf("initial generated migration missing %q:\n%s", want, firstSQL)
		}
	}
	assertBefore(t, firstSQL, `CREATE OR REPLACE FUNCTION "edvat_live_priority"."z_inner_email"`, `CREATE OR REPLACE FUNCTION "edvat_live_priority"."a_outer_email"`)
	assertBefore(t, firstSQL, `CREATE TABLE "edvat_live_priority"."users"`, `CREATE POLICY "tenant_select"`)
	assertBefore(t, firstSQL, `CREATE VIEW "edvat_live_priority"."z_inner_user_emails"`, `CREATE VIEW "edvat_live_priority"."a_outer_user_emails"`)
	applySQLStatements(t, ctx, db, firstSQL)
	assertExtensionInstalled(t, ctx, db, "pgcrypto")

	if err := migrateDiff([]string{"priority_round_trip_empty", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		policies, policyInspectErr := pgext.InspectPolicies(ctx, db)
		triggers, triggerInspectErr := pgext.InspectTriggers(ctx, db)
		if policyInspectErr != nil || triggerInspectErr != nil {
			t.Fatalf("second migrateDiff() error = %v; inspect policies: %v; inspect triggers: %v", err, policyInspectErr, triggerInspectErr)
		}
		t.Fatalf("second migrateDiff() error = %v; inspected policies = %#v; inspected triggers = %#v", err, policies, triggers)
	}
	secondSQL := readLatestMigrationSQL(t, filepath.Join(dir, "migrations"))
	if strings.TrimSpace(secondSQL) != "" {
		views, inspectErr := pgext.InspectViews(ctx, db)
		if inspectErr != nil {
			t.Fatalf("second generated migration = %q, want empty after round trip for schema %s; inspect views: %v", secondSQL, schemaName, inspectErr)
		}
		t.Fatalf("second generated migration = %q, want empty after round trip for schema %s; inspected views = %#v", secondSQL, schemaName, views)
	}
}

func TestLiveListAndHashPartitionRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	url := startTestPostgres(t, ctx, "postgres:16-alpine")
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS "edvat_live_partition_variants" CASCADE`); err != nil {
		t.Fatalf("drop test schema before test: %v", err)
	}
	defer db.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS "edvat_live_partition_variants" CASCADE`)

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
schema "edvat_live_partition_variants" {}

table "events_by_region" {
  schema = schema.edvat_live_partition_variants
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
  schema = schema.edvat_live_partition_variants
  of = table.events_by_region
  list { in = ["'de'", "'fr'"] }
}

table "events_by_tenant" {
  schema = schema.edvat_live_partition_variants
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
  schema = schema.edvat_live_partition_variants
  of = table.events_by_tenant
  hash {
    modulus = 4
    remainder = 0
  }
}
`)
	if err := migrateDiff([]string{"partition_variants_round_trip", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		t.Fatalf("initial migrateDiff() error = %v", err)
	}
	firstSQL := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	for _, want := range []string{`FOR VALUES IN ('de', 'fr')`, `FOR VALUES WITH (MODULUS 4, REMAINDER 0)`} {
		if !strings.Contains(firstSQL, want) {
			t.Fatalf("initial generated migration missing %q:\n%s", want, firstSQL)
		}
	}
	applySQLStatements(t, ctx, db, firstSQL)

	if err := migrateDiff([]string{"partition_variants_round_trip_empty", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		partitions, inspectErr := pgext.InspectPartitions(ctx, db)
		if inspectErr != nil {
			t.Fatalf("second migrateDiff() error = %v; inspect partitions: %v", err, inspectErr)
		}
		t.Fatalf("second migrateDiff() error = %v; inspected partitions = %#v", err, partitions)
	}
	secondSQL := readLatestMigrationSQL(t, filepath.Join(dir, "migrations"))
	if strings.TrimSpace(secondSQL) != "" {
		partitions, inspectErr := pgext.InspectPartitions(ctx, db)
		if inspectErr != nil {
			t.Fatalf("second generated migration = %q, want empty after round trip; inspect partitions: %v", secondSQL, inspectErr)
		}
		t.Fatalf("second generated migration = %q, want empty after round trip; inspected partitions = %#v", secondSQL, partitions)
	}
}

func TestLivePartitionRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	url := startTestPostgres(t, ctx, "postgres:16-alpine")
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS "edvat_live_partition" CASCADE`); err != nil {
		t.Fatalf("drop test schema before test: %v", err)
	}
	defer db.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS "edvat_live_partition" CASCADE`)

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
schema "edvat_live_partition" {}

table "events" {
  schema = schema.edvat_live_partition
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
  schema = schema.edvat_live_partition
  of = table.events
  range {
    from = ["'2025-01-01'"]
    to = ["'2026-01-01'"]
  }
  comment = "2025 events"
}
`)
	if err := migrateDiff([]string{"partition_round_trip", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		t.Fatalf("initial migrateDiff() error = %v", err)
	}
	firstSQL := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	for _, want := range []string{`CREATE TABLE "edvat_live_partition"."events"`, `PARTITION BY RANGE`, `CREATE TABLE "edvat_live_partition"."events_2025" PARTITION OF "edvat_live_partition"."events" FOR VALUES FROM ('2025-01-01') TO ('2026-01-01')`, `COMMENT ON TABLE "edvat_live_partition"."events_2025" IS '2025 events'`} {
		if !strings.Contains(firstSQL, want) {
			t.Fatalf("initial generated migration missing %q:\n%s", want, firstSQL)
		}
	}
	applySQLStatements(t, ctx, db, firstSQL)

	if err := migrateDiff([]string{"partition_round_trip_empty", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		partitions, inspectErr := pgext.InspectPartitions(ctx, db)
		if inspectErr != nil {
			t.Fatalf("second migrateDiff() error = %v; inspect partitions: %v", err, inspectErr)
		}
		t.Fatalf("second migrateDiff() error = %v; inspected partitions = %#v", err, partitions)
	}
	secondSQL := readLatestMigrationSQL(t, filepath.Join(dir, "migrations"))
	if strings.TrimSpace(secondSQL) != "" {
		partitions, inspectErr := pgext.InspectPartitions(ctx, db)
		if inspectErr != nil {
			t.Fatalf("second generated migration = %q, want empty after round trip; inspect partitions: %v", secondSQL, inspectErr)
		}
		t.Fatalf("second generated migration = %q, want empty after round trip; inspected partitions = %#v", secondSQL, partitions)
	}
}

func TestLiveExclusionConstraintRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	url := startTestPostgres(t, ctx, "postgres:16-alpine")
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS "edvat_live_exclusion" CASCADE`); err != nil {
		t.Fatalf("drop test schema before test: %v", err)
	}
	defer db.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS "edvat_live_exclusion" CASCADE`)

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
schema "edvat_live_exclusion" {}

table "reservations" {
  schema = schema.edvat_live_exclusion
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
    where = "period IS NOT NULL"
  }
}
`)
	if err := migrateDiff([]string{"exclusion_round_trip", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		t.Fatalf("initial migrateDiff() error = %v", err)
	}
	firstSQL := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	for _, want := range []string{`CREATE TABLE "edvat_live_exclusion"."reservations"`, `ALTER TABLE "edvat_live_exclusion"."reservations" ADD CONSTRAINT "reservations_no_overlap" EXCLUDE USING gist ("period" WITH &&) INCLUDE ("note") WHERE (period IS NOT NULL)`} {
		if !strings.Contains(firstSQL, want) {
			t.Fatalf("initial generated migration missing %q:\n%s", want, firstSQL)
		}
	}
	applySQLStatements(t, ctx, db, firstSQL)

	if err := migrateDiff([]string{"exclusion_round_trip_empty", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		exclusions, inspectErr := pgext.InspectExclusions(ctx, db)
		if inspectErr != nil {
			t.Fatalf("second migrateDiff() error = %v; inspect exclusions: %v", err, inspectErr)
		}
		t.Fatalf("second migrateDiff() error = %v; inspected exclusions = %#v", err, exclusions)
	}
	secondSQL := readLatestMigrationSQL(t, filepath.Join(dir, "migrations"))
	if strings.TrimSpace(secondSQL) != "" {
		exclusions, inspectErr := pgext.InspectExclusions(ctx, db)
		if inspectErr != nil {
			t.Fatalf("second generated migration = %q, want empty after round trip; inspect exclusions: %v", secondSQL, inspectErr)
		}
		t.Fatalf("second generated migration = %q, want empty after round trip; inspected exclusions = %#v", secondSQL, exclusions)
	}
}

func TestLiveSeedSyncCurrentRowDiff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	url := startTestPostgres(t, ctx, "postgres:16-alpine")
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS "edvat_live_seed" CASCADE`); err != nil {
		t.Fatalf("drop test schema before test: %v", err)
	}
	defer db.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS "edvat_live_seed" CASCADE`)

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
schema "edvat_live_seed" {}

table "countries" {
  schema = schema.edvat_live_seed
  column "code" {
    null = false
    type = varchar(2)
  }
  column "name" {
    null = false
    type = varchar(255)
  }
  primary_key { columns = [column.code] }
}

data {
  table = schema.edvat_live_seed.countries
  rows = [
    { code = "US", name = "United States" },
    { code = "IL", name = "Israel" },
  ]
}
`)
	if err := migrateDiff([]string{"seed_sync_init", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		t.Fatalf("initial migrateDiff() error = %v", err)
	}
	applySQLStatements(t, ctx, db, readOnlyMigrationSQL(t, filepath.Join(dir, "migrations")))
	if _, err := db.ExecContext(ctx, `UPDATE "edvat_live_seed"."countries" SET "name" = 'USA' WHERE "code" = 'US'`); err != nil {
		t.Fatalf("mutate seed row: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO "edvat_live_seed"."countries" ("code", "name") VALUES ('NO', 'Norway')`); err != nil {
		t.Fatalf("insert extra seed row: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM "edvat_live_seed"."countries" WHERE "code" = 'IL'`); err != nil {
		t.Fatalf("delete desired seed row: %v", err)
	}
	if err := migrateDiff([]string{"seed_sync_diff", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		t.Fatalf("diff migrateDiff() error = %v", err)
	}
	got := readMigrationSQLByName(t, filepath.Join(dir, "migrations"), "seed_sync_diff")
	for _, want := range []string{
		`UPDATE "edvat_live_seed"."countries" SET "name" = 'United States' WHERE "code" = 'US'`,
		`INSERT INTO "edvat_live_seed"."countries" ("code", "name") VALUES ('IL', 'Israel') ON CONFLICT ("code") DO NOTHING`,
		`DELETE FROM "edvat_live_seed"."countries" WHERE "code" = 'NO'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated seed diff missing %q:\n%s", want, got)
		}
	}
}

func TestLiveDefaultPermissionRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	url := startTestPostgres(t, ctx, "postgres:16-alpine")
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS "edvat_live_default_permission" CASCADE`); err != nil {
		t.Fatalf("drop test schema before test: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DROP ROLE IF EXISTS "edvat_default_permission_reader"`); err != nil {
		t.Fatalf("drop test role before test: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE ROLE "edvat_default_permission_reader"`); err != nil {
		t.Fatalf("create test role: %v", err)
	}
	defer db.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS "edvat_live_default_permission" CASCADE`)
	defer db.ExecContext(context.Background(), `DROP ROLE IF EXISTS "edvat_default_permission_reader"`)

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
schema "edvat_live_default_permission" {}

default_permission "public_read_tables" {
  schema = schema.edvat_live_default_permission
  for_role = "edvat"
  on = "TABLES"
  to = "public"
  privileges = ["SELECT"]
}

default_permission "public_use_sequences" {
  schema = schema.edvat_live_default_permission
  for_role = "edvat"
  on = "SEQUENCES"
  to = "edvat_default_permission_reader"
  privileges = ["SELECT", "USAGE"]
  grantable = true
}

default_permission "reader_execute_functions" {
  schema = schema.edvat_live_default_permission
  for_role = "edvat"
  on = "FUNCTIONS"
  to = "edvat_default_permission_reader"
  privileges = ["EXECUTE"]
}

default_permission "reader_use_types" {
  schema = schema.edvat_live_default_permission
  for_role = "edvat"
  on = "TYPES"
  to = "edvat_default_permission_reader"
  privileges = ["USAGE"]
}
`)
	if err := migrateDiff([]string{"default_permission_round_trip", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		t.Fatalf("initial migrateDiff() error = %v", err)
	}
	firstSQL := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	for _, want := range []string{
		`ALTER DEFAULT PRIVILEGES FOR ROLE "edvat" IN SCHEMA "edvat_live_default_permission" GRANT SELECT ON TABLES TO PUBLIC`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE "edvat" IN SCHEMA "edvat_live_default_permission" GRANT SELECT, USAGE ON SEQUENCES TO "edvat_default_permission_reader" WITH GRANT OPTION`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE "edvat" IN SCHEMA "edvat_live_default_permission" GRANT EXECUTE ON FUNCTIONS TO "edvat_default_permission_reader"`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE "edvat" IN SCHEMA "edvat_live_default_permission" GRANT USAGE ON TYPES TO "edvat_default_permission_reader"`,
	} {
		if !strings.Contains(firstSQL, want) {
			t.Fatalf("initial generated migration missing %q:\n%s", want, firstSQL)
		}
	}
	applySQLStatements(t, ctx, db, firstSQL)

	if err := migrateDiff([]string{"default_permission_round_trip_empty", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		defaultPermissions, inspectErr := pgext.InspectDefaultPermissions(ctx, db)
		if inspectErr != nil {
			t.Fatalf("second migrateDiff() error = %v; inspect default permissions: %v", err, inspectErr)
		}
		t.Fatalf("second migrateDiff() error = %v; inspected default permissions = %#v", err, defaultPermissions)
	}
	secondSQL := readLatestMigrationSQL(t, filepath.Join(dir, "migrations"))
	if strings.TrimSpace(secondSQL) != "" {
		defaultPermissions, inspectErr := pgext.InspectDefaultPermissions(ctx, db)
		if inspectErr != nil {
			t.Fatalf("second generated migration = %q, want empty after round trip; inspect default permissions: %v", secondSQL, inspectErr)
		}
		t.Fatalf("second generated migration = %q, want empty after round trip; inspected default permissions = %#v", secondSQL, defaultPermissions)
	}
}

func TestLivePermissionExtendedACLInspectionRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	url := startTestPostgres(t, ctx, "postgres:16-alpine")
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	var databaseName string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatalf("current database: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS "edvat_live_permission_acl" CASCADE`); err != nil {
		t.Fatalf("drop test schema before test: %v", err)
	}
	defer db.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS "edvat_live_permission_acl" CASCADE`)
	if _, err := db.ExecContext(ctx, `DROP ROLE IF EXISTS edvat_acl_reader`); err != nil {
		t.Skipf("database user cannot drop test role: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE ROLE edvat_acl_reader`); err != nil {
		t.Skipf("database user cannot create test role: %v", err)
	}
	defer db.ExecContext(context.Background(), `DROP ROLE IF EXISTS edvat_acl_reader`)

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
schema "edvat_live_permission_acl" {}

sequence "order_seq" {
  schema = schema.edvat_live_permission_acl
  cycle = true
}

domain "email_domain" {
  schema = schema.edvat_live_permission_acl
  type = text
}

function "is_positive" {
  schema = schema.edvat_live_permission_acl
  lang = SQL
  arg "v" { type = integer }
  return = boolean
  as = "SELECT v > 0"
}

permission "schema_usage" {
  on = schema.edvat_live_permission_acl
  to = edvat_acl_reader
  privileges = [USAGE]
}

permission "sequence_usage" {
  on = sequence.edvat_live_permission_acl.order_seq
  to = edvat_acl_reader
  privileges = [USAGE, SELECT]
}

permission "domain_usage" {
  on = domain.edvat_live_permission_acl.email_domain
  to = edvat_acl_reader
  privileges = [USAGE]
}

permission "function_execute" {
  on = "FUNCTION \"edvat_live_permission_acl\".\"is_positive\"(integer)"
  to = edvat_acl_reader
  privileges = [EXECUTE]
}

permission "database_connect" {
  on = "DATABASE \"`+databaseName+`\""
  to = edvat_acl_reader
  privileges = [CONNECT]
}
`)
	if err := migrateDiff([]string{"permission_extended_acl_round_trip", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		t.Fatalf("initial migrateDiff() error = %v", err)
	}
	firstSQL := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	for _, want := range []string{
		`GRANT USAGE ON SCHEMA "edvat_live_permission_acl" TO "edvat_acl_reader"`,
		`GRANT USAGE, SELECT ON SEQUENCE "edvat_live_permission_acl"."order_seq" TO "edvat_acl_reader"`,
		`GRANT USAGE ON TYPE "edvat_live_permission_acl"."email_domain" TO "edvat_acl_reader"`,
		`GRANT EXECUTE ON FUNCTION "edvat_live_permission_acl"."is_positive"(integer) TO "edvat_acl_reader"`,
		`GRANT CONNECT ON DATABASE "` + databaseName + `" TO "edvat_acl_reader"`,
	} {
		if !strings.Contains(firstSQL, want) {
			t.Fatalf("initial generated migration missing %q:\n%s", want, firstSQL)
		}
	}
	applySQLStatements(t, ctx, db, firstSQL)
	if err := migrateDiff([]string{"permission_extended_acl_round_trip_empty", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		permissions, inspectErr := pgext.InspectPermissions(ctx, db)
		if inspectErr != nil {
			t.Fatalf("second migrateDiff() error = %v; inspect permissions: %v", err, inspectErr)
		}
		t.Fatalf("second migrateDiff() error = %v; inspected permissions = %#v", err, permissions)
	}
	secondSQL := readLatestMigrationSQL(t, filepath.Join(dir, "migrations"))
	if strings.Contains(secondSQL, "GRANT ") || strings.Contains(secondSQL, "REVOKE ") {
		t.Fatalf("second generated migration contains permission churn after extended ACL round trip: %q", secondSQL)
	}
}

func TestLiveForeignServerPermissionRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	url := startTestPostgres(t, ctx, "postgres:16-alpine")
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DROP SERVER IF EXISTS "edvat_live_analytics" CASCADE`); err != nil {
		t.Fatalf("drop test server before test: %v", err)
	}
	defer db.ExecContext(context.Background(), `DROP SERVER IF EXISTS "edvat_live_analytics" CASCADE`)
	if _, err := db.ExecContext(ctx, `DROP ROLE IF EXISTS edvat_server_reader`); err != nil {
		t.Skipf("database user cannot drop test role: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE ROLE edvat_server_reader`); err != nil {
		t.Skipf("database user cannot create test role: %v", err)
	}
	defer db.ExecContext(context.Background(), `DROP ROLE IF EXISTS edvat_server_reader`)

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

extension "postgres_fdw" {}

server "edvat_live_analytics" {
  fdw = postgres_fdw
  options = { host = "localhost", dbname = "analytics" }
}

permission "server_usage" {
  on = server.edvat_live_analytics
  to = edvat_server_reader
  privileges = [USAGE]
}
`)
	if err := migrateDiff([]string{"foreign_server_permission_round_trip", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		t.Fatalf("initial migrateDiff() error = %v", err)
	}
	firstSQL := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	for _, want := range []string{`CREATE EXTENSION "postgres_fdw"`, `CREATE SERVER "edvat_live_analytics"`, `GRANT USAGE ON FOREIGN SERVER "edvat_live_analytics" TO "edvat_server_reader"`} {
		if !strings.Contains(firstSQL, want) {
			t.Fatalf("initial generated migration missing %q:\n%s", want, firstSQL)
		}
	}
	applySQLStatements(t, ctx, db, firstSQL)
	if err := migrateDiff([]string{"foreign_server_permission_round_trip_empty", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		permissions, inspectErr := pgext.InspectPermissions(ctx, db)
		if inspectErr != nil {
			t.Fatalf("second migrateDiff() error = %v; inspect permissions: %v", err, inspectErr)
		}
		t.Fatalf("second migrateDiff() error = %v; inspected permissions = %#v", err, permissions)
	}
	secondSQL := readLatestMigrationSQL(t, filepath.Join(dir, "migrations"))
	if strings.Contains(secondSQL, "GRANT ") || strings.Contains(secondSQL, "REVOKE ") {
		t.Fatalf("second generated migration contains foreign-server permission churn: %q", secondSQL)
	}
}

func TestLivePermissionGrantOptionRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	url := startTestPostgres(t, ctx, "postgres:16-alpine")
	db, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS "edvat_live_grant_option" CASCADE`); err != nil {
		t.Fatalf("drop test schema before test: %v", err)
	}
	defer db.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS "edvat_live_grant_option" CASCADE`)
	if _, err := db.ExecContext(ctx, `DROP ROLE IF EXISTS edvat_grant_reader`); err != nil {
		t.Skipf("database user cannot drop test role: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE ROLE edvat_grant_reader`); err != nil {
		t.Skipf("database user cannot create test role: %v", err)
	}
	defer db.ExecContext(context.Background(), `DROP ROLE IF EXISTS edvat_grant_reader`)

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
schema "edvat_live_grant_option" {}

table "reports" {
  schema = schema.edvat_live_grant_option
  column "id" {
    null = false
    type = int
  }
  primary_key { columns = [column.id] }
}

permission "grantable_read_reports" {
  on = schema.edvat_live_grant_option.reports
  to = edvat_grant_reader
  privileges = [SELECT]
  grantable = true
}
`)
	if err := migrateDiff([]string{"grant_option_round_trip", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		t.Fatalf("initial migrateDiff() error = %v", err)
	}
	firstSQL := readOnlyMigrationSQL(t, filepath.Join(dir, "migrations"))
	for _, want := range []string{`CREATE TABLE "edvat_live_grant_option"."reports"`, `GRANT SELECT ON TABLE "edvat_live_grant_option"."reports" TO "edvat_grant_reader" WITH GRANT OPTION`} {
		if !strings.Contains(firstSQL, want) {
			t.Fatalf("initial generated migration missing %q:\n%s", want, firstSQL)
		}
	}
	applySQLStatements(t, ctx, db, firstSQL)
	if err := migrateDiff([]string{"grant_option_round_trip_empty", "--config", configPath, "--env", "local", "--dev-url", url}); err != nil {
		permissions, inspectErr := pgext.InspectPermissions(ctx, db)
		if inspectErr != nil {
			t.Fatalf("second migrateDiff() error = %v; inspect permissions: %v", err, inspectErr)
		}
		t.Fatalf("second migrateDiff() error = %v; inspected permissions = %#v", err, permissions)
	}
	secondSQL := readLatestMigrationSQL(t, filepath.Join(dir, "migrations"))
	if strings.TrimSpace(secondSQL) != "" {
		t.Fatalf("second generated migration = %q, want empty after grant option round trip", secondSQL)
	}
}

func assertExtensionInstalled(t *testing.T, ctx context.Context, db *sql.DB, name string) {
	t.Helper()
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)`, name).Scan(&exists); err != nil {
		t.Fatalf("query extension %s: %v", name, err)
	}
	if !exists {
		t.Fatalf("extension %s was not installed after applying generated migration", name)
	}
}

func startTestPostgres(t *testing.T, ctx context.Context, image string) string {
	t.Helper()
	if url := os.Getenv("EDVAT_TEST_DATABASE_URL"); url != "" {
		return url
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)
	container, err := postgres.Run(ctx,
		image,
		postgres.WithDatabase("edvat_test"),
		postgres.WithUsername("edvat"),
		postgres.WithPassword("edvat"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres testcontainer: %v", err)
	}
	testcontainers.CleanupContainer(t, container)
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres testcontainer connection string: %v", err)
	}
	return url
}

func readOnlyMigrationSQL(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var sqlFiles []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			sqlFiles = append(sqlFiles, filepath.Join(dir, entry.Name()))
		}
	}
	if len(sqlFiles) == 0 {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".sql") && !strings.HasSuffix(entry.Name(), ".down.sql") {
				sqlFiles = append(sqlFiles, filepath.Join(dir, entry.Name()))
			}
		}
	}
	if len(sqlFiles) != 1 {
		t.Fatalf("got %d migration sql files, want 1", len(sqlFiles))
	}
	body, err := os.ReadFile(sqlFiles[0])
	if err != nil {
		t.Fatalf("read migration sql: %v", err)
	}
	return string(body)
}

func readMigrationSQLByName(t *testing.T, dir, name string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") && strings.Contains(entry.Name(), name) {
			body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				t.Fatalf("read migration sql: %v", err)
			}
			return string(body)
		}
	}
	t.Fatalf("no generated migration sql for %s", name)
	return ""
}

func readLatestMigrationSQL(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var latest string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") && entry.Name() > latest {
			latest = entry.Name()
		}
	}
	if latest == "" {
		t.Fatal("no generated migration sql")
	}
	body, err := os.ReadFile(filepath.Join(dir, latest))
	if err != nil {
		t.Fatalf("read latest migration sql: %v", err)
	}
	return string(body)
}

func applySQLStatements(t *testing.T, ctx context.Context, db *sql.DB, sqlText string) {
	t.Helper()
	for _, stmt := range splitSQLStatements(sqlText) {
		stmt = stripSQLCommentLines(stmt)
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("apply statement %q: %v", stmt, err)
		}
	}
}

func stripSQLCommentLines(stmt string) string {
	var out []string
	for _, line := range strings.Split(stmt, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func splitSQLStatements(sqlText string) []string {
	var statements []string
	var b strings.Builder
	inSingle := false
	inDollar := false
	for i := 0; i < len(sqlText); i++ {
		if !inSingle && i+1 < len(sqlText) && sqlText[i:i+2] == "$$" {
			inDollar = !inDollar
			b.WriteString("$$")
			i++
			continue
		}
		ch := sqlText[i]
		if ch == '\'' && !inDollar {
			inSingle = !inSingle
		}
		if ch == ';' && !inSingle && !inDollar {
			statements = append(statements, b.String())
			b.Reset()
			continue
		}
		b.WriteByte(ch)
	}
	if strings.TrimSpace(b.String()) != "" {
		statements = append(statements, b.String())
	}
	return statements
}
