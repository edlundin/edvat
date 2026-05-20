package seed

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/edlundin/edvat/internal/baseatlas"

	"ariga.io/atlas/sql/schema"
)

func TestParseHCLDataBlocks(t *testing.T) {
	datasets, err := ParseHCL([]byte(`
schema "public" {}

table "countries" {
  schema = schema.public
}

data {
  table = table.countries
  rows = [
    { id = 1, code = "US", name = "United States", active = true },
    { id = 2, code = "IL", name = "Israel", active = true },
  ]
}
`), "seed.hcl")
	if err != nil {
		t.Fatalf("ParseHCL() error = %v", err)
	}
	want := []DataSet{{
		Table: "countries",
		Rows: []Row{
			{"id": int64(1), "code": "US", "name": "United States", "active": true},
			{"id": int64(2), "code": "IL", "name": "Israel", "active": true},
		},
	}}
	if !reflect.DeepEqual(datasets, want) {
		t.Fatalf("ParseHCL() = %#v, want %#v", datasets, want)
	}
}

func TestParseHCLExplicitKey(t *testing.T) {
	got, err := ParseHCL([]byte(`
data {
  table = schema.public.countries
  key = [code]
  rows = [{ code = "US", name = "United States" }]
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseHCL() error = %v", err)
	}
	want := []DataSet{{Table: "public.countries", KeyColumns: []string{"code"}, Rows: []Row{{"code": "US", "name": "United States"}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseHCL() = %#v, want %#v", got, want)
	}
}

func TestParseHCLSchemaTableReference(t *testing.T) {
	got, err := ParseHCL([]byte(`
data {
  table = schema.public.countries
  rows = [{ id = 1, code = "US" }]
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParseHCL() error = %v", err)
	}
	want := []DataSet{{Table: "public.countries", Rows: []Row{{"id": int64(1), "code": "US"}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseHCL() = %#v, want %#v", got, want)
	}
}

func TestPlanSeedStatements(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.pg.hcl")
	sqlPath := filepath.Join(dir, "seed.sql")
	writeSeedTestFile(t, schemaPath, `
schema "public" {}

table "countries" {
  schema = schema.public
}

data {
  table = table.countries
  key = [code]
  rows = [{ code = "US", name = "United States" }]
}
`)
	writeSeedTestFile(t, sqlPath, `INSERT INTO audit VALUES ('seed');`)

	statements, err := Plan(context.Background(), PlanConfig{
		SchemaPaths: []string{schemaPath},
		SQLPaths:    []string{sqlPath},
		Mode:        ModeUpsert,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	got := statementSQL(statements)
	want := []string{
		`INSERT INTO audit VALUES ('seed')`,
		`INSERT INTO "countries" ("code", "name") VALUES ('US', 'United States') ON CONFLICT ("code") DO UPDATE SET "name" = EXCLUDED."name"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Plan() SQL = %#v, want %#v", got, want)
	}
}

func TestPlanSyncUsesCurrentRowsAdapter(t *testing.T) {
	id := &schema.Column{Name: "id"}
	countries := &schema.Table{Name: "countries", Columns: []*schema.Column{id}, PrimaryKey: &schema.Index{Parts: []*schema.IndexPart{{C: id}}}}
	realm := schema.NewRealm(&schema.Schema{Name: "public", Tables: []*schema.Table{countries}})

	called := false
	statements, err := Plan(context.Background(), PlanConfig{
		Mode:    ModeSync,
		Desired: realm,
		CurrentRows: func(ctx context.Context, table string, columns []string) ([]Row, error) {
			called = true
			if table != "countries" || !reflect.DeepEqual(columns, []string{"id", "name"}) {
				t.Fatalf("CurrentRows(table, columns) = %q, %#v", table, columns)
			}
			return []Row{{"id": int64(1), "name": "USA"}}, nil
		},
		SchemaPaths: []string{writeSeedTestFile(t, filepath.Join(t.TempDir(), "schema.pg.hcl"), `
schema "public" {}

table "countries" {
  schema = schema.public
  column "id" { type = int }
  primary_key { columns = [column.id] }
}

data {
  table = table.countries
  rows = [{ id = 1, name = "United States" }]
}
`)},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if !called {
		t.Fatal("Plan() did not call CurrentRows")
	}
	got := statementSQL(statements)
	want := []string{`UPDATE "countries" SET "name" = 'United States' WHERE "id" = 1`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Plan() SQL = %#v, want %#v", got, want)
	}
}

func TestSplitSQL(t *testing.T) {
	got := SplitSQL([]byte("\nINSERT INTO countries VALUES ('US;A');\nDO $$ BEGIN RAISE NOTICE 'x;y'; END $$;\nUPDATE countries SET name = 'Israel''s State';\n"))
	want := []string{
		"INSERT INTO countries VALUES ('US;A')",
		"DO $$ BEGIN RAISE NOTICE 'x;y'; END $$",
		"UPDATE countries SET name = 'Israel''s State'",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitSQL() = %#v, want %#v", got, want)
	}
}

func TestInsertSQL(t *testing.T) {
	statements, err := InsertSQL(DataSet{
		Table: "countries",
		Rows: []Row{
			{"id": int64(1), "code": "US", "name": "United States"},
			{"id": int64(2), "code": "IL", "name": "Israel's State"},
		},
	}, []string{"id"})
	if err != nil {
		t.Fatalf("InsertSQL() error = %v", err)
	}
	want := []string{
		`INSERT INTO "countries" ("id", "code", "name") VALUES (1, 'US', 'United States') ON CONFLICT ("id") DO NOTHING`,
		`INSERT INTO "countries" ("id", "code", "name") VALUES (2, 'IL', 'Israel''s State') ON CONFLICT ("id") DO NOTHING`,
	}
	if !reflect.DeepEqual(statements, want) {
		t.Fatalf("InsertSQL() = %#v, want %#v", statements, want)
	}
}

func TestSyncSQLSingleKey(t *testing.T) {
	statements, err := SyncSQL(DataSet{
		Table: "countries",
		Rows: []Row{
			{"code": "US", "name": "United States"},
			{"code": "IL", "name": "Israel"},
		},
	}, []string{"code"})
	if err != nil {
		t.Fatalf("SyncSQL() error = %v", err)
	}
	want := []string{
		`INSERT INTO "countries" ("code", "name") VALUES ('US', 'United States') ON CONFLICT ("code") DO UPDATE SET "name" = EXCLUDED."name"`,
		`INSERT INTO "countries" ("code", "name") VALUES ('IL', 'Israel') ON CONFLICT ("code") DO UPDATE SET "name" = EXCLUDED."name"`,
		`DELETE FROM "countries" WHERE "code" NOT IN ('US', 'IL')`,
	}
	if !reflect.DeepEqual(statements, want) {
		t.Fatalf("SyncSQL() = %#v, want %#v", statements, want)
	}
}

func TestSyncSQLCompositeKey(t *testing.T) {
	statements, err := SyncSQL(DataSet{
		Table: "memberships",
		Rows: []Row{
			{"user_id": int64(1), "org_id": int64(2), "role": "owner"},
			{"user_id": int64(3), "org_id": int64(4), "role": "member"},
		},
	}, []string{"user_id", "org_id"})
	if err != nil {
		t.Fatalf("SyncSQL() error = %v", err)
	}
	want := []string{
		`INSERT INTO "memberships" ("user_id", "org_id", "role") VALUES (1, 2, 'owner') ON CONFLICT ("user_id", "org_id") DO UPDATE SET "role" = EXCLUDED."role"`,
		`INSERT INTO "memberships" ("user_id", "org_id", "role") VALUES (3, 4, 'member') ON CONFLICT ("user_id", "org_id") DO UPDATE SET "role" = EXCLUDED."role"`,
		`DELETE FROM "memberships" WHERE ("user_id", "org_id") NOT IN ((1, 2), (3, 4))`,
	}
	if !reflect.DeepEqual(statements, want) {
		t.Fatalf("SyncSQL() = %#v, want %#v", statements, want)
	}
}

func TestDiffSQL(t *testing.T) {
	statements, err := DiffSQL("countries", []Row{
		{"code": "US", "name": "USA"},
		{"code": "IL", "name": "Israel"},
		{"code": "NO", "name": "Norway"},
	}, []Row{
		{"code": "US", "name": "United States"},
		{"code": "IL", "name": "Israel"},
		{"code": "SE", "name": "Sweden"},
	}, []string{"code"})
	if err != nil {
		t.Fatalf("DiffSQL() error = %v", err)
	}
	want := []string{
		`UPDATE "countries" SET "name" = 'United States' WHERE "code" = 'US'`,
		`INSERT INTO "countries" ("code", "name") VALUES ('SE', 'Sweden') ON CONFLICT ("code") DO NOTHING`,
		`DELETE FROM "countries" WHERE "code" = 'NO'`,
	}
	if !reflect.DeepEqual(statements, want) {
		t.Fatalf("DiffSQL() = %#v, want %#v", statements, want)
	}
}

func TestDiffSQLNullSafe(t *testing.T) {
	statements, err := DiffSQL("countries", []Row{{"code": "US", "name": nil}}, []Row{{"code": "US", "name": "United States"}}, []string{"code"})
	if err != nil {
		t.Fatalf("DiffSQL() error = %v", err)
	}
	want := []string{`UPDATE "countries" SET "name" = 'United States' WHERE "code" = 'US'`}
	if !reflect.DeepEqual(statements, want) {
		t.Fatalf("DiffSQL() = %#v, want %#v", statements, want)
	}
}

func TestDiffSQLCompositeKey(t *testing.T) {
	statements, err := DiffSQL("memberships", []Row{
		{"user_id": int64(1), "org_id": int64(2), "role": "member"},
		{"user_id": int64(3), "org_id": int64(4), "role": "member"},
	}, []Row{
		{"user_id": int64(1), "org_id": int64(2), "role": "owner"},
	}, []string{"user_id", "org_id"})
	if err != nil {
		t.Fatalf("DiffSQL() error = %v", err)
	}
	want := []string{
		`UPDATE "memberships" SET "role" = 'owner' WHERE "user_id" = 1 AND "org_id" = 2`,
		`DELETE FROM "memberships" WHERE "user_id" = 3 AND "org_id" = 4`,
	}
	if !reflect.DeepEqual(statements, want) {
		t.Fatalf("DiffSQL() = %#v, want %#v", statements, want)
	}
}

func TestUpsertSQL(t *testing.T) {
	statements, err := UpsertSQL(DataSet{
		Table: "countries",
		Rows:  []Row{{"id": int64(1), "code": "US", "name": "United States"}},
	}, []string{"id"})
	if err != nil {
		t.Fatalf("UpsertSQL() error = %v", err)
	}
	want := []string{`INSERT INTO "countries" ("id", "code", "name") VALUES (1, 'US', 'United States') ON CONFLICT ("id") DO UPDATE SET "code" = EXCLUDED."code", "name" = EXCLUDED."name"`}
	if !reflect.DeepEqual(statements, want) {
		t.Fatalf("UpsertSQL() = %#v, want %#v", statements, want)
	}
}

func TestUpsertSQLWithOnlyKeyColumnsDoesNothingOnConflict(t *testing.T) {
	statements, err := UpsertSQL(DataSet{Table: "countries", Rows: []Row{{"id": int64(1)}}}, []string{"id"})
	if err != nil {
		t.Fatalf("UpsertSQL() error = %v", err)
	}
	want := []string{`INSERT INTO "countries" ("id") VALUES (1) ON CONFLICT ("id") DO NOTHING`}
	if !reflect.DeepEqual(statements, want) {
		t.Fatalf("UpsertSQL() = %#v, want %#v", statements, want)
	}
}

func TestInsertSQLForRealmUsesPrimaryKey(t *testing.T) {
	id := &schema.Column{Name: "id"}
	code := &schema.Column{Name: "code"}
	countries := &schema.Table{
		Name:       "countries",
		Columns:    []*schema.Column{id, code},
		PrimaryKey: &schema.Index{Parts: []*schema.IndexPart{{C: id}}},
	}
	realm := schema.NewRealm(&schema.Schema{Name: "public", Tables: []*schema.Table{countries}})
	statements, err := InsertSQLForRealm(DataSet{Table: "countries", Rows: []Row{{"id": int64(1), "code": "US"}}}, realm)
	if err != nil {
		t.Fatalf("InsertSQLForRealm() error = %v", err)
	}
	want := []string{`INSERT INTO "countries" ("id", "code") VALUES (1, 'US') ON CONFLICT ("id") DO NOTHING`}
	if !reflect.DeepEqual(statements, want) {
		t.Fatalf("InsertSQLForRealm() = %#v, want %#v", statements, want)
	}
}

func TestInsertSQLRequiresKey(t *testing.T) {
	_, err := InsertSQL(DataSet{Table: "countries", Rows: []Row{{"id": int64(1)}}}, nil)
	if err == nil || err.Error() != "data for table countries requires a primary key or explicit seed key" {
		t.Fatalf("InsertSQL() error = %v", err)
	}
}

func writeSeedTestFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func statementSQL(statements []baseatlas.Statement) []string {
	out := make([]string, 0, len(statements))
	for _, statement := range statements {
		out = append(out, statement.SQL)
	}
	return out
}
