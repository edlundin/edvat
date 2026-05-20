package capabilities

import (
	"strings"
	"testing"
)

func TestAllIncludesInitialPlanStatuses(t *testing.T) {
	caps := All()
	if len(caps) == 0 {
		t.Fatal("All() returned no capabilities")
	}
	seen := map[string]Capability{}
	for _, cap := range caps {
		seen[cap.Family] = cap
	}
	if seen["table"].Status != StatusDelegated || seen["table"].Owner != OwnerAtlas {
		t.Fatalf("table capability = %#v", seen["table"])
	}
	if seen["data"].Status != StatusExperimental || seen["data"].Owner != OwnerEdvat {
		t.Fatalf("data capability = %#v", seen["data"])
	}
	for _, want := range []string{"INSERT", "UPSERT", "SYNC", "SQL seed files"} {
		if !strings.Contains(seen["data"].Notes, want) {
			t.Fatalf("data capability notes missing %q: %q", want, seen["data"].Notes)
		}
	}
	if seen["extension"].Status != StatusExperimental || seen["extension"].Owner != OwnerEdvat {
		t.Fatalf("extension capability = %#v", seen["extension"])
	}
	if seen["view"].Status != StatusExperimental || seen["view"].Owner != OwnerEdvat {
		t.Fatalf("view capability = %#v", seen["view"])
	}
	if seen["function"].Status != StatusExperimental || seen["function"].Owner != OwnerEdvat {
		t.Fatalf("function capability = %#v", seen["function"])
	}
	if seen["partition"].Status != StatusExperimental || seen["partition"].Owner != OwnerEdvat {
		t.Fatalf("partition capability = %#v", seen["partition"])
	}
	for _, want := range []string{"range", "list", "hash", "round-trip"} {
		if !strings.Contains(seen["partition"].Notes, want) {
			t.Fatalf("partition capability notes missing %q: %q", want, seen["partition"].Notes)
		}
	}
	if seen["exclusion"].Status != StatusExperimental || seen["exclusion"].Owner != OwnerEdvat {
		t.Fatalf("exclusion capability = %#v", seen["exclusion"])
	}
	for _, want := range []string{"include", "where", "expression", "round-trip"} {
		if !strings.Contains(seen["exclusion"].Notes, want) {
			t.Fatalf("exclusion capability notes missing %q: %q", want, seen["exclusion"].Notes)
		}
	}
	if seen["default_permission"].Status != StatusExperimental || seen["default_permission"].Owner != OwnerEdvat {
		t.Fatalf("default_permission capability = %#v", seen["default_permission"])
	}
	for _, want := range []string{"ALTER DEFAULT PRIVILEGES", "inspection", "round-trip", "validation"} {
		if !strings.Contains(seen["default_permission"].Notes, want) {
			t.Fatalf("default_permission capability notes missing %q: %q", want, seen["default_permission"].Notes)
		}
	}
	if seen["permission"].Status != StatusExperimental || seen["permission"].Owner != OwnerEdvat {
		t.Fatalf("permission capability = %#v", seen["permission"])
	}
	for _, want := range []string{"GRANT/REVOKE", "sequence", "schema", "routine", "type", "database", "foreign server ACL inspection"} {
		if !strings.Contains(seen["permission"].Notes, want) {
			t.Fatalf("permission capability notes missing %q: %q", want, seen["permission"].Notes)
		}
	}
	if seen["user"].Status != StatusExperimental || seen["user"].Owner != OwnerEdvat {
		t.Fatalf("user capability = %#v", seen["user"])
	}
}

func TestText(t *testing.T) {
	got := Text([]Capability{{Family: "table", Owner: OwnerAtlas, Status: StatusDelegated, Notes: "ok"}})
	for _, want := range []string{"FAMILY\tOWNER\tSTATUS\tNOTES", "table\tatlas_oss\tdelegated\tok"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Text() missing %q in %q", want, got)
		}
	}
}
