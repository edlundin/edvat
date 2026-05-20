package capabilities

import "fmt"

type Owner string

type Status string

const (
	OwnerAtlas Owner = "atlas_oss"
	OwnerEdvat Owner = "edvat"

	StatusDelegated    Status = "delegated"
	StatusExperimental Status = "experimental"
	StatusUnsupported  Status = "unsupported"
)

type Capability struct {
	Family string
	Owner  Owner
	Status Status
	Notes  string
}

func All() []Capability {
	return []Capability{
		{Family: "schema", Owner: OwnerAtlas, Status: StatusDelegated, Notes: "parsed, diffed, and planned by Atlas OSS"},
		{Family: "table", Owner: OwnerAtlas, Status: StatusDelegated, Notes: "columns, primary keys, foreign keys, checks, and indexes delegated to Atlas OSS"},
		{Family: "enum", Owner: OwnerAtlas, Status: StatusDelegated, Notes: "delegated to Atlas OSS; create and existing-state add-value parity covered"},
		{Family: "sequence", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL sequence blocks parse; create, alter, drop, and comments implemented"},
		{Family: "partition", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL standalone partition blocks parse; range, list, hash, and default create/drop/replacement/comments implemented with live round-trip coverage and reverses"},
		{Family: "exclusion", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL table exclude blocks parse; include columns, where predicates, expression terms, create/drop/replacement with reverses, inspection, and live round-trip coverage implemented"},
		{Family: "data", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL data blocks and SQL seed files parse; INSERT, UPSERT, and SYNC generation implemented"},
		{Family: "extension", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL extension blocks parse; create, drop, version update, and comments implemented"},
		{Family: "domain", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL domain blocks parse; create, drop, replacement, and comments implemented"},
		{Family: "composite", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL composite blocks parse; create, drop, replacement, and comments implemented"},
		{Family: "range", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL range blocks parse; create, drop, replacement, and comments implemented"},
		{Family: "collation", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL collation blocks parse; create, drop, replacement, and comments implemented"},
		{Family: "cast", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL cast blocks parse; create, drop, replacement, and comments implemented"},
		{Family: "view", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL view blocks parse; create, replace, drop, and comments implemented"},
		{Family: "materialized", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL materialized view blocks parse; create, drop, replacement, and comments implemented"},
		{Family: "function", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL function blocks parse; create or replace, drop, and comments implemented"},
		{Family: "procedure", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL procedure blocks parse; create or replace, drop, and comments implemented"},
		{Family: "aggregate", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL aggregate blocks parse; create, drop, replacement, and comments implemented"},
		{Family: "trigger", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL trigger blocks parse; create, drop, and replace-on-change implemented"},
		{Family: "event_trigger", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL event trigger blocks parse; create, drop, replacement, and comments implemented"},
		{Family: "policy", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL policy blocks parse; create, drop, replacement, and comments implemented"},
		{Family: "permission", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL permission blocks parse; GRANT/REVOKE generation and table/view/materialized view/sequence/schema/routine/type/database/foreign server ACL inspection implemented"},
		{Family: "default_permission", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL default_permission blocks parse with object/privilege validation; ALTER DEFAULT PRIVILEGES grant/revoke generation with reverses, inspection, and live round-trip coverage implemented"},
		{Family: "server", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL foreign server blocks parse; create, drop, replacement, and comments implemented"},
		{Family: "foreign_table", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL foreign table blocks parse; create, drop, replacement, and comments implemented"},
		{Family: "user_mapping", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL user mapping blocks parse; create/drop generation implemented; secret options rejected"},
		{Family: "role", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL role blocks parse; create, alter, drop, and comments implemented behind --manage-roles"},
		{Family: "user", Owner: OwnerEdvat, Status: StatusExperimental, Notes: "HCL user blocks parse; create, alter, drop, and comments implemented behind --manage-users; passwords rejected"},
	}
}

func Text(caps []Capability) string {
	out := "FAMILY\tOWNER\tSTATUS\tNOTES\n"
	for _, cap := range caps {
		out += fmt.Sprintf("%s\t%s\t%s\t%s\n", cap.Family, cap.Owner, cap.Status, cap.Notes)
	}
	return out
}
