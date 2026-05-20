package migrationplan

import (
	"reflect"
	"testing"

	"github.com/edlundin/edvat/internal/baseatlas"
	"github.com/edlundin/edvat/internal/pgext"
)

func TestOrderMigrationStatementsDependencyPriority(t *testing.T) {
	got := orderMigrationStatements([]baseatlas.Statement{
		{SQL: `CREATE TRIGGER "users_touch" BEFORE UPDATE ON "public"."users" EXECUTE FUNCTION "public"."touch_updated_at"()`},
		{SQL: `CREATE TABLE "public"."users" ("id" integer)`},
		{SQL: `CREATE OR REPLACE FUNCTION "public"."touch_updated_at"() RETURNS trigger LANGUAGE PLPGSQL AS $$ BEGIN RETURN NEW; END $$`},
		{SQL: `CREATE EXTENSION "plpgsql"`},
		{SQL: `INSERT INTO "public"."users" ("id") VALUES (1)`},
	})
	want := []baseatlas.Statement{
		{SQL: `CREATE EXTENSION "plpgsql"`},
		{SQL: `CREATE TABLE "public"."users" ("id" integer)`},
		{SQL: `CREATE OR REPLACE FUNCTION "public"."touch_updated_at"() RETURNS trigger LANGUAGE PLPGSQL AS $$ BEGIN RETURN NEW; END $$`},
		{SQL: `CREATE TRIGGER "users_touch" BEFORE UPDATE ON "public"."users" EXECUTE FUNCTION "public"."touch_updated_at"()`},
		{SQL: `INSERT INTO "public"."users" ("id") VALUES (1)`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderMigrationStatements() = %#v, want %#v", got, want)
	}
}

func TestOrderMigrationStatementsFunctionBeforeTableDefault(t *testing.T) {
	got := orderMigrationStatements([]baseatlas.Statement{
		{SQL: `CREATE TABLE "public"."users" ("id" uuid NOT NULL DEFAULT uuid_generate_v7(), PRIMARY KEY ("id"))`},
		{SQL: `CREATE OR REPLACE FUNCTION "public"."uuid_generate_v7"() RETURNS uuid LANGUAGE PLpgSQL AS $$ BEGIN RETURN gen_random_uuid(); END $$`},
	})
	want := []baseatlas.Statement{
		{SQL: `CREATE OR REPLACE FUNCTION "public"."uuid_generate_v7"() RETURNS uuid LANGUAGE PLpgSQL AS $$ BEGIN RETURN gen_random_uuid(); END $$`},
		{SQL: `CREATE TABLE "public"."users" ("id" uuid NOT NULL DEFAULT uuid_generate_v7(), PRIMARY KEY ("id"))`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderMigrationStatements() = %#v, want %#v", got, want)
	}
}

func TestOrderMigrationStatementsFunctionDependencies(t *testing.T) {
	got := orderMigrationStatements([]baseatlas.Statement{
		{SQL: `CREATE OR REPLACE FUNCTION "public"."outer_fn"() RETURNS integer LANGUAGE SQL AS $$ SELECT public.inner_fn() $$`},
		{SQL: `CREATE OR REPLACE FUNCTION "public"."inner_fn"() RETURNS integer LANGUAGE SQL AS $$ SELECT 1 $$`},
	})
	want := []baseatlas.Statement{
		{SQL: `CREATE OR REPLACE FUNCTION "public"."inner_fn"() RETURNS integer LANGUAGE SQL AS $$ SELECT 1 $$`},
		{SQL: `CREATE OR REPLACE FUNCTION "public"."outer_fn"() RETURNS integer LANGUAGE SQL AS $$ SELECT public.inner_fn() $$`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderMigrationStatements() = %#v, want %#v", got, want)
	}
}

func TestOrderMigrationStatementsDoesNotReorderOnObjectNamePrefix(t *testing.T) {
	got := orderMigrationStatements([]baseatlas.Statement{
		{SQL: `CREATE OR REPLACE FUNCTION "public"."outer"() RETURNS integer LANGUAGE SQL AS $$ SELECT public.inner_extra() $$`},
		{SQL: `CREATE OR REPLACE FUNCTION "public"."inner"() RETURNS integer LANGUAGE SQL AS $$ SELECT 1 $$`},
	})
	want := []baseatlas.Statement{
		{SQL: `CREATE OR REPLACE FUNCTION "public"."outer"() RETURNS integer LANGUAGE SQL AS $$ SELECT public.inner_extra() $$`},
		{SQL: `CREATE OR REPLACE FUNCTION "public"."inner"() RETURNS integer LANGUAGE SQL AS $$ SELECT 1 $$`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderMigrationStatements() = %#v, want %#v", got, want)
	}
}

func TestOrderMigrationStatementsViewDependencies(t *testing.T) {
	got := orderMigrationStatements([]baseatlas.Statement{
		{SQL: `CREATE VIEW "public"."active_user_names" AS SELECT name FROM public.active_users`},
		{SQL: `CREATE VIEW "public"."active_users" AS SELECT id, name FROM public.users WHERE active`},
	})
	want := []baseatlas.Statement{
		{SQL: `CREATE VIEW "public"."active_users" AS SELECT id, name FROM public.users WHERE active`},
		{SQL: `CREATE VIEW "public"."active_user_names" AS SELECT name FROM public.active_users`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderMigrationStatements() = %#v, want %#v", got, want)
	}
}

func TestOrderMigrationStatementsTriggerAfterReferencedFunction(t *testing.T) {
	got := orderMigrationStatements([]baseatlas.Statement{
		{SQL: `CREATE TRIGGER "users_touch" BEFORE UPDATE ON "public"."users" EXECUTE FUNCTION "public"."touch_updated_at"()`},
		{SQL: `CREATE OR REPLACE FUNCTION "public"."touch_updated_at"() RETURNS trigger LANGUAGE PLPGSQL AS $$ BEGIN RETURN NEW; END $$`},
	})
	want := []baseatlas.Statement{
		{SQL: `CREATE OR REPLACE FUNCTION "public"."touch_updated_at"() RETURNS trigger LANGUAGE PLPGSQL AS $$ BEGIN RETURN NEW; END $$`},
		{SQL: `CREATE TRIGGER "users_touch" BEFORE UPDATE ON "public"."users" EXECUTE FUNCTION "public"."touch_updated_at"()`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderMigrationStatements() = %#v, want %#v", got, want)
	}
}

func TestOrderMigrationStatementsForeignServerBeforeGrant(t *testing.T) {
	got := orderMigrationStatements([]baseatlas.Statement{
		{SQL: `GRANT USAGE ON FOREIGN SERVER "analytics" TO "reader"`},
		{SQL: `CREATE SERVER "analytics" FOREIGN DATA WRAPPER "postgres_fdw"`},
		{SQL: `CREATE EXTENSION "postgres_fdw"`},
	})
	want := []baseatlas.Statement{
		{SQL: `CREATE EXTENSION "postgres_fdw"`},
		{SQL: `CREATE SERVER "analytics" FOREIGN DATA WRAPPER "postgres_fdw"`},
		{SQL: `GRANT USAGE ON FOREIGN SERVER "analytics" TO "reader"`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderMigrationStatements() = %#v, want %#v", got, want)
	}
}

func TestOrderMigrationStatementsPolicyAfterTable(t *testing.T) {
	got := orderMigrationStatements([]baseatlas.Statement{
		{SQL: `CREATE POLICY "own_rows" ON "public"."users" FOR SELECT USING (tenant_id = current_setting('app.tenant_id')::uuid)`},
		{SQL: `CREATE TABLE "public"."users" ("id" integer)`},
	})
	want := []baseatlas.Statement{
		{SQL: `CREATE TABLE "public"."users" ("id" integer)`},
		{SQL: `CREATE POLICY "own_rows" ON "public"."users" FOR SELECT USING (tenant_id = current_setting('app.tenant_id')::uuid)`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderMigrationStatements() = %#v, want %#v", got, want)
	}
}

func TestOrderMigrationStatementsCreateTableReferences(t *testing.T) {
	got := orderMigrationStatements([]baseatlas.Statement{
		{SQL: `CREATE TABLE "public"."users" ("id" integer, "org_id" integer, CONSTRAINT "users_org_id_fkey" FOREIGN KEY ("org_id") REFERENCES "public"."orgs" ("id"))`},
		{SQL: `CREATE TABLE "public"."orgs" ("id" integer)`},
		{SQL: `CREATE TABLE "public"."memberships" ("user_id" integer, CONSTRAINT "memberships_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id"))`},
	})
	want := []baseatlas.Statement{
		{SQL: `CREATE TABLE "public"."orgs" ("id" integer)`},
		{SQL: `CREATE TABLE "public"."users" ("id" integer, "org_id" integer, CONSTRAINT "users_org_id_fkey" FOREIGN KEY ("org_id") REFERENCES "public"."orgs" ("id"))`},
		{SQL: `CREATE TABLE "public"."memberships" ("user_id" integer, CONSTRAINT "memberships_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id"))`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderMigrationStatements() = %#v, want %#v", got, want)
	}
}

func TestOrderMigrationStatementsTableConstraintsAfterTablesBeforeIndexes(t *testing.T) {
	got := orderMigrationStatements([]baseatlas.Statement{
		{SQL: `CREATE INDEX "idx_users_org_id" ON "public"."users" ("org_id")`},
		{SQL: `ALTER TABLE "public"."users" ADD CONSTRAINT "users_org_id_fkey" FOREIGN KEY ("org_id") REFERENCES "public"."orgs" ("id")`},
		{SQL: `CREATE TABLE "public"."users" ("id" integer, "org_id" integer)`},
		{SQL: `CREATE TABLE "public"."orgs" ("id" integer)`},
	})
	want := []baseatlas.Statement{
		{SQL: `CREATE TABLE "public"."users" ("id" integer, "org_id" integer)`},
		{SQL: `CREATE TABLE "public"."orgs" ("id" integer)`},
		{SQL: `ALTER TABLE "public"."users" ADD CONSTRAINT "users_org_id_fkey" FOREIGN KEY ("org_id") REFERENCES "public"."orgs" ("id")`},
		{SQL: `CREATE INDEX "idx_users_org_id" ON "public"."users" ("org_id")`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderMigrationStatements() = %#v, want %#v", got, want)
	}
}

func TestOrderMigrationStatementsDropsDependentsFirst(t *testing.T) {
	got := orderMigrationStatements([]baseatlas.Statement{
		{SQL: `DROP FUNCTION "public"."touch_updated_at"()`},
		{SQL: `DROP EXTENSION "plpgsql"`},
		{SQL: `DROP TABLE "public"."users"`},
		{SQL: `DROP TRIGGER "users_touch" ON "public"."users"`},
	})
	want := []baseatlas.Statement{
		{SQL: `DROP TRIGGER "users_touch" ON "public"."users"`},
		{SQL: `DROP FUNCTION "public"."touch_updated_at"()`},
		{SQL: `DROP TABLE "public"."users"`},
		{SQL: `DROP EXTENSION "plpgsql"`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderMigrationStatements() = %#v, want %#v", got, want)
	}
}

func TestDestructiveStatementComments(t *testing.T) {
	got := destructiveStatementComments([]baseatlas.Statement{
		{Comment: "create function public.ok", SQL: `CREATE OR REPLACE FUNCTION "public"."ok"() RETURNS integer LANGUAGE SQL AS $$ SELECT 1 $$`},
		{Comment: "drop trigger public.users.audit (destructive)", SQL: `DROP TRIGGER "audit" ON "public"."users"`},
	})
	want := []string{"drop trigger public.users.audit (destructive)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("destructiveStatementComments() = %#v, want %#v", got, want)
	}
}

func TestSuppressManagedExclusionConstraintDrops(t *testing.T) {
	statements := []baseatlas.Statement{
		{SQL: `ALTER TABLE "public"."reservations" DROP CONSTRAINT "reservations_no_overlap"`},
		{SQL: `ALTER TABLE "public"."reservations" DROP CONSTRAINT "other_constraint"`},
	}
	got := suppressManagedExclusionConstraintDrops(statements, pgext.ExclusionState{
		"public.reservations.reservations_no_overlap": {Name: "reservations_no_overlap", Schema: "public", Table: "reservations"},
	})
	want := []baseatlas.Statement{{SQL: `ALTER TABLE "public"."reservations" DROP CONSTRAINT "other_constraint"`}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("suppressManagedExclusionConstraintDrops() = %#v, want %#v", got, want)
	}
}

func TestDestructiveStatementCommentsCatchesUncommentedDropAndRevoke(t *testing.T) {
	got := destructiveStatementComments([]baseatlas.Statement{
		{SQL: `CREATE OR REPLACE VIEW "public"."active_users" AS SELECT 1`},
		{SQL: `DROP VIEW "public"."old_users"`},
		{SQL: `REVOKE SELECT ON TABLE "public"."users" FROM "reader"`},
	})
	want := []string{`DROP VIEW "public"."old_users"`, `REVOKE SELECT ON TABLE "public"."users" FROM "reader"`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("destructiveStatementComments() = %#v, want %#v", got, want)
	}
}

func TestMergeExtensionStatementsAfterSchemaStatements(t *testing.T) {
	got := mergeExtensionStatements(
		[]baseatlas.Statement{
			{SQL: `CREATE SCHEMA IF NOT EXISTS "public"`},
			{SQL: `CREATE TABLE "public"."users" ("id" integer)`},
		},
		[]baseatlas.Statement{{SQL: `CREATE EXTENSION "pgcrypto" WITH SCHEMA "public"`}},
	)
	want := []baseatlas.Statement{
		{SQL: `CREATE SCHEMA IF NOT EXISTS "public"`},
		{SQL: `CREATE EXTENSION "pgcrypto" WITH SCHEMA "public"`},
		{SQL: `CREATE TABLE "public"."users" ("id" integer)`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeExtensionStatements() = %#v, want %#v", got, want)
	}
}
