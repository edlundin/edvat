package pgext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParsePartitionsHCL(t *testing.T) {
	got, err := ParsePartitionsHCL([]byte(`
schema "public" {}

partition "events_2025" {
  schema = schema.public
  of = schema.public.events
  for = "FROM ('2025-01-01') TO ('2026-01-01')"
  comment = "2025 events"
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParsePartitionsHCL() error = %v", err)
	}
	want := PartitionState{"public.events_2025": {Name: "events_2025", Schema: "public", Of: `"public"."events"`, For: "FROM ('2025-01-01') TO ('2026-01-01')", Comment: "2025 events"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParsePartitionsHCL() = %#v, want %#v", got, want)
	}
}

func TestParsePartitionsHCLAtlasRangeListAndHashBlocks(t *testing.T) {
	got, err := ParsePartitionsHCL([]byte(`
schema "public" {}

table "events" {
  schema = schema.public
}

partition "events_2025" {
  schema = schema.public
  of = table.events
  range {
    from = ["'2025-01-01'"]
    to = ["'2026-01-01'"]
  }
}

partition "events_default_region" {
  schema = schema.public
  of = table.events
  list { in = ["'eu'", "'us'"] }
}

partition "events_hash_0" {
  schema = schema.public
  of = table.events
  hash {
    modulus = 4
    remainder = 0
  }
}

partition "events_default" {
  schema = schema.public
  of = table.events
  default {}
}
`), "schema.pg.hcl")
	if err != nil {
		t.Fatalf("ParsePartitionsHCL() error = %v", err)
	}
	want := PartitionState{
		"public.events_2025":           {Name: "events_2025", Schema: "public", Of: `"public"."events"`, For: "FROM ('2025-01-01') TO ('2026-01-01')"},
		"public.events_default_region": {Name: "events_default_region", Schema: "public", Of: `"public"."events"`, For: "IN ('eu', 'us')"},
		"public.events_hash_0":         {Name: "events_hash_0", Schema: "public", Of: `"public"."events"`, For: "WITH (MODULUS 4, REMAINDER 0)"},
		"public.events_default":        {Name: "events_default", Schema: "public", Of: `"public"."events"`, For: "DEFAULT"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParsePartitionsHCL() = %#v, want %#v", got, want)
	}
}

func TestDiffPartitionsCreate(t *testing.T) {
	statements := DiffPartitions(nil, PartitionState{"public.events_2025": {Name: "events_2025", Schema: "public", Of: `"public"."events"`, For: "FROM ('2025-01-01') TO ('2026-01-01')", Comment: "2025 events"}})
	got := joinSQL(statements)
	for _, want := range []string{
		`CREATE TABLE "public"."events_2025" PARTITION OF "public"."events" FOR VALUES FROM ('2025-01-01') TO ('2026-01-01')`,
		`COMMENT ON TABLE "public"."events_2025" IS '2025 events'`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffPartitions() missing %q:\n%s", want, got)
		}
	}
}

func TestDiffPartitionsDropHasReverse(t *testing.T) {
	statements := DiffPartitions(PartitionState{"public.events_2025": {Name: "events_2025", Schema: "public", Of: `"public"."events"`, For: "FROM (1) TO (2)", Comment: "old"}}, nil)
	if len(statements) != 1 {
		t.Fatalf("DiffPartitions() got %d statements, want 1", len(statements))
	}
	want := "CREATE TABLE \"public\".\"events_2025\" PARTITION OF \"public\".\"events\" FOR VALUES FROM (1) TO (2);\nCOMMENT ON TABLE \"public\".\"events_2025\" IS 'old'"
	if statements[0].Reverse != want {
		t.Fatalf("drop reverse = %q, want %q", statements[0].Reverse, want)
	}
}

func TestDiffPartitionsReplace(t *testing.T) {
	statements := DiffPartitions(
		PartitionState{"public.events_2025": {Name: "events_2025", Schema: "public", Of: `"public"."events"`, For: "FROM (1) TO (2)"}},
		PartitionState{"public.events_2025": {Name: "events_2025", Schema: "public", Of: `"public"."events"`, For: "FROM (2) TO (3)"}},
	)
	got := joinSQL(statements)
	for _, want := range []string{`DROP TABLE "public"."events_2025"`, `CREATE TABLE "public"."events_2025" PARTITION OF "public"."events" FOR VALUES FROM (2) TO (3)`} {
		if !strings.Contains(got, want) {
			t.Fatalf("DiffPartitions() missing %q:\n%s", want, got)
		}
	}
}
