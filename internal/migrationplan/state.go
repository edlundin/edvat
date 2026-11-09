package migrationplan

import (
	"context"
	"os"
	"strings"

	"github.com/edlundin/edvat/internal/baseatlas"
	"github.com/edlundin/edvat/internal/pgext"

	"ariga.io/atlas/sql/schema"
)

type migrationState struct {
	Realm              *schema.Realm
	Extensions         pgext.State
	Domains            pgext.DomainState
	Composites         pgext.CompositeState
	Ranges             pgext.RangeState
	Collations         pgext.CollationState
	Sequences          pgext.SequenceState
	Partitions         pgext.PartitionState
	Exclusions         pgext.ExclusionState
	Casts              pgext.CastState
	Views              pgext.ViewState
	MaterializedViews  pgext.MaterializedViewState
	Functions          pgext.FunctionState
	Procedures         pgext.ProcedureState
	Aggregates         pgext.AggregateState
	Triggers           pgext.TriggerState
	EventTriggers      pgext.EventTriggerState
	Policies           pgext.PolicyState
	Permissions        pgext.PermissionState
	DefaultPermissions pgext.DefaultPermissionState
	Servers            pgext.ServerState
	ForeignTables      pgext.ForeignTableState
	UserMappings       pgext.UserMappingState
	Roles              pgext.RoleState
	Users              pgext.UserState
}

func loadDesiredMigrationState(ctx context.Context, engine baseatlas.BaseEngine, schemaPaths []string) (migrationState, error) {
	var desired migrationState
	var err error
	desired.Realm, err = engine.LoadDesired(ctx, baseatlas.ProjectConfig{SchemaPaths: schemaPaths})
	if err != nil {
		return migrationState{}, err
	}
	desired.Extensions, err = pgext.ParseFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Domains, err = pgext.ParseDomainFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Composites, err = pgext.ParseCompositeFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Ranges, err = pgext.ParseRangeFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Collations, err = pgext.ParseCollationFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Sequences, err = pgext.ParseSequenceFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Partitions, err = pgext.ParsePartitionFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Exclusions, err = pgext.ParseExclusionFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Casts, err = pgext.ParseCastFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Views, err = pgext.ParseViewFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.MaterializedViews, err = pgext.ParseMaterializedViewFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Functions, err = pgext.ParseFunctionFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Procedures, err = pgext.ParseProcedureFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Aggregates, err = pgext.ParseAggregateFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Triggers, err = pgext.ParseTriggerFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.EventTriggers, err = pgext.ParseEventTriggerFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Policies, err = pgext.ParsePolicyFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Permissions, err = pgext.ParsePermissionFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.DefaultPermissions, err = pgext.ParseDefaultPermissionFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Servers, err = pgext.ParseServerFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.ForeignTables, err = pgext.ParseForeignTableFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.UserMappings, err = pgext.ParseUserMappingFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Roles, err = pgext.ParseRoleFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	desired.Users, err = pgext.ParseUserFiles(schemaPaths)
	if err != nil {
		return migrationState{}, err
	}
	return desired, nil
}

func inspectCurrentMigrationState(ctx context.Context, engine baseatlas.BaseEngine, devURL string, includeRoles, includeUsers bool) (migrationState, error) {
	var current migrationState
	var err error
	current.Realm, err = engine.InspectCurrent(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Extensions, err = pgext.InspectURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Domains, err = pgext.InspectDomainsURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Composites, err = pgext.InspectCompositesURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Ranges, err = pgext.InspectRangesURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Collations, err = pgext.InspectCollationsURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Sequences, err = pgext.InspectSequencesURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Partitions, err = pgext.InspectPartitionsURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Exclusions, err = pgext.InspectExclusionsURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Casts, err = pgext.InspectCastsURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Views, err = pgext.InspectViewsURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.MaterializedViews, err = pgext.InspectMaterializedViewsURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Functions, err = pgext.InspectFunctionsURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Procedures, err = pgext.InspectProceduresURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Aggregates, err = pgext.InspectAggregatesURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Triggers, err = pgext.InspectTriggersURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.EventTriggers, err = pgext.InspectEventTriggersURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Policies, err = pgext.InspectPoliciesURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Permissions, err = pgext.InspectPermissionsURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.DefaultPermissions, err = pgext.InspectDefaultPermissionsURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.Servers, err = pgext.InspectServersURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.ForeignTables, err = pgext.InspectForeignTablesURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	current.UserMappings, err = pgext.InspectUserMappingsURL(ctx, devURL)
	if err != nil {
		return migrationState{}, err
	}
	if includeRoles {
		current.Roles, err = pgext.InspectRolesURL(ctx, devURL)
		if err != nil {
			return migrationState{}, err
		}
	}
	if includeUsers {
		current.Users, err = pgext.InspectUsersURL(ctx, devURL)
		if err != nil {
			return migrationState{}, err
		}
	}
	return current, nil
}

// scopeMigrationState drops schema-qualified pgext objects outside the env scope.
// Realm-level families (extensions, casts, event triggers, servers, user
// mappings, roles, users) are not schema-qualified and stay untouched.
func scopeMigrationState(state migrationState, scope baseatlas.Scope) migrationState {
	if len(scope.Schemas) == 0 && len(scope.Exclude) == 0 {
		return state
	}
	state.Domains = keepInScope(state.Domains, func(o pgext.Domain) bool { return scope.Includes("domain", o.Schema, o.Name) })
	state.Composites = keepInScope(state.Composites, func(o pgext.Composite) bool { return scope.Includes("composite", o.Schema, o.Name) })
	state.Ranges = keepInScope(state.Ranges, func(o pgext.Range) bool { return scope.Includes("range", o.Schema, o.Name) })
	state.Collations = keepInScope(state.Collations, func(o pgext.Collation) bool { return scope.Includes("collation", o.Schema, o.Name) })
	state.Sequences = keepInScope(state.Sequences, func(o pgext.Sequence) bool { return scope.Includes("sequence", o.Schema, o.Name) })
	state.Partitions = keepInScope(state.Partitions, func(o pgext.Partition) bool { return scope.Includes("partition", o.Schema, o.Name) })
	state.Exclusions = keepInScope(state.Exclusions, func(o pgext.ExclusionConstraint) bool {
		return scope.Includes("exclude", o.Schema, o.Table, o.Name)
	})
	state.Views = keepInScope(state.Views, func(o pgext.View) bool { return scope.Includes("view", o.Schema, o.Name) })
	state.MaterializedViews = keepInScope(state.MaterializedViews, func(o pgext.MaterializedView) bool {
		return scope.Includes("materialized", o.Schema, o.Name)
	})
	state.Functions = keepInScope(state.Functions, func(o pgext.Function) bool { return scope.Includes("function", o.Schema, o.Name) })
	state.Procedures = keepInScope(state.Procedures, func(o pgext.Procedure) bool { return scope.Includes("procedure", o.Schema, o.Name) })
	state.Aggregates = keepInScope(state.Aggregates, func(o pgext.Aggregate) bool { return scope.Includes("aggregate", o.Schema, o.Name) })
	state.Triggers = keepInScope(state.Triggers, func(o pgext.Trigger) bool { return scope.Includes("trigger", o.Schema, o.Table, o.Name) })
	state.Policies = keepInScope(state.Policies, func(o pgext.Policy) bool { return scope.Includes("policy", o.Schema, o.Table, o.Name) })
	state.Permissions = keepInScope(state.Permissions, func(o pgext.Permission) bool {
		schemaName, path := permissionTargetPath(o.Target)
		return scope.Includes("permission", schemaName, path...)
	})
	state.DefaultPermissions = keepInScope(state.DefaultPermissions, func(o pgext.DefaultPermission) bool {
		return scope.Includes("default_permission", o.Schema)
	})
	state.ForeignTables = keepInScope(state.ForeignTables, func(o pgext.ForeignTable) bool {
		return scope.Includes("foreign_table", o.Schema, o.Name)
	})
	return state
}

func keepInScope[S ~map[string]V, V any](state S, keep func(V) bool) S {
	for id, object := range state {
		if !keep(object) {
			delete(state, id)
		}
	}
	return state
}

// permissionTargetPath extracts the schema and object name from targets such as
// `TABLE "river"."river_job"`, `FUNCTION "river"."f"(bit)` or `SCHEMA "river"`.
// Database and foreign server targets have no schema.
func permissionTargetPath(target string) (string, []string) {
	kind, rest, ok := strings.Cut(target, ` "`)
	if !ok || kind == "DATABASE" || kind == "FOREIGN SERVER" {
		return "", nil
	}
	first, rest, _ := strings.Cut(rest, `"`)
	if kind == "SCHEMA" {
		return first, nil
	}
	if !strings.HasPrefix(rest, `."`) {
		return "", nil
	}
	name, _, _ := strings.Cut(rest[2:], `"`)
	return first, []string{name}
}

func filterManagedRuntimePermissions(state pgext.PermissionState) pgext.PermissionState {
	if len(state) == 0 {
		return state
	}
	out := pgext.PermissionState{}
	for id, permission := range state {
		if strings.HasPrefix(permission.Target, "DATABASE ") && isManagedRuntimeRole(permission.Grantee) {
			continue
		}
		out[id] = permission
	}
	return out
}

func isManagedRuntimeRole(role string) bool {
	return role == "encore_services" || strings.HasPrefix(role, "encore-")
}

func appendPgExtStatements(statements []baseatlas.Statement, current migrationState, desired migrationState, includeRoles, includeUsers bool) []baseatlas.Statement {
	statements = mergeExtensionStatements(statements, pgext.Diff(current.Extensions, desired.Extensions))
	statements = append(statements, pgext.DiffDomains(current.Domains, desired.Domains)...)
	statements = append(statements, pgext.DiffComposites(current.Composites, desired.Composites)...)
	statements = append(statements, pgext.DiffRanges(current.Ranges, desired.Ranges)...)
	statements = append(statements, pgext.DiffCollations(current.Collations, desired.Collations)...)
	statements = append(statements, pgext.DiffSequences(current.Sequences, desired.Sequences)...)
	statements = append(statements, pgext.DiffPartitions(current.Partitions, desired.Partitions)...)
	statements = append(statements, pgext.DiffExclusions(current.Exclusions, desired.Exclusions)...)
	statements = append(statements, pgext.DiffCasts(current.Casts, desired.Casts)...)
	statements = append(statements, pgext.DiffFunctions(current.Functions, desired.Functions)...)
	statements = append(statements, pgext.DiffProcedures(current.Procedures, desired.Procedures)...)
	statements = append(statements, pgext.DiffAggregates(current.Aggregates, desired.Aggregates)...)
	statements = append(statements, pgext.DiffTriggers(current.Triggers, desired.Triggers)...)
	statements = append(statements, pgext.DiffEventTriggers(current.EventTriggers, desired.EventTriggers)...)
	statements = append(statements, pgext.DiffPolicies(current.Policies, desired.Policies)...)
	statements = append(statements, pgext.DiffPermissions(filterManagedRuntimePermissions(current.Permissions), desired.Permissions)...)
	statements = append(statements, pgext.DiffDefaultPermissions(current.DefaultPermissions, desired.DefaultPermissions)...)
	statements = append(statements, pgext.DiffServers(current.Servers, desired.Servers)...)
	statements = append(statements, pgext.DiffForeignTables(current.ForeignTables, desired.ForeignTables)...)
	statements = append(statements, pgext.DiffUserMappings(current.UserMappings, desired.UserMappings)...)
	if includeRoles {
		statements = append(statements, pgext.DiffRoles(current.Roles, desired.Roles)...)
	}
	if includeUsers {
		statements = append(statements, pgext.DiffUsers(current.Users, desired.Users)...)
	}
	statements = append(statements, pgext.DiffViews(current.Views, desired.Views)...)
	statements = append(statements, pgext.DiffMaterializedViews(current.MaterializedViews, desired.MaterializedViews)...)
	return statements
}

func suppressDropIndexesMentionedInHCL(statements []baseatlas.Statement, schemaPaths []string) []baseatlas.Statement {
	managed := map[string]bool{}
	for _, path := range schemaPaths {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(content), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "index \"") {
				continue
			}
			parts := strings.SplitN(strings.TrimPrefix(line, "index \""), "\"", 2)
			if parts[0] != "" {
				managed[parts[0]] = true
			}
		}
	}
	if len(managed) == 0 {
		return statements
	}
	out := statements[:0]
	for _, statement := range statements {
		if name, ok := dropIndexName(statement.SQL); ok && managed[name] {
			continue
		}
		out = append(out, statement)
	}
	return out
}

func dropIndexName(sql string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(sql))
	if len(fields) < 3 || !strings.EqualFold(fields[0], "DROP") || !strings.EqualFold(fields[1], "INDEX") {
		return "", false
	}
	_, name, ok := splitQualifiedIdent(fields[2])
	if ok {
		return name, true
	}
	return strings.Trim(fields[2], `";`), true
}

func suppressManagedExclusionConstraintDrops(statements []baseatlas.Statement, desired pgext.ExclusionState) []baseatlas.Statement {
	if len(desired) == 0 || len(statements) == 0 {
		return statements
	}
	managed := map[string]bool{}
	for _, exclusion := range desired {
		managed[exclusion.Schema+"."+exclusion.Table+"."+exclusion.Name] = true
	}
	out := statements[:0]
	for _, statement := range statements {
		if key, ok := dropConstraintKey(statement.SQL); ok && managed[key] {
			continue
		}
		out = append(out, statement)
	}
	return out
}

func dropConstraintKey(sql string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(sql))
	if len(fields) < 6 || !strings.EqualFold(fields[0], "ALTER") || !strings.EqualFold(fields[1], "TABLE") || !strings.EqualFold(fields[3], "DROP") || !strings.EqualFold(fields[4], "CONSTRAINT") {
		return "", false
	}
	schemaName, tableName, ok := splitQualifiedIdent(fields[2])
	if !ok {
		return "", false
	}
	constraintName := strings.Trim(fields[5], `";`)
	return schemaName + "." + tableName + "." + constraintName, true
}

func splitQualifiedIdent(value string) (string, string, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.Trim(parts[0], `"`), strings.Trim(parts[1], `"`), true
}

func mergeExtensionStatements(base, extensions []baseatlas.Statement) []baseatlas.Statement {
	if len(extensions) == 0 {
		return base
	}
	idx := 0
	for idx < len(base) && isSchemaStatement(base[idx].SQL) {
		idx++
	}
	merged := make([]baseatlas.Statement, 0, len(base)+len(extensions))
	merged = append(merged, base[:idx]...)
	merged = append(merged, extensions...)
	merged = append(merged, base[idx:]...)
	return merged
}

func isSchemaStatement(sql string) bool {
	return strings.HasPrefix(sql, "CREATE SCHEMA ") || strings.HasPrefix(sql, "COMMENT ON SCHEMA ")
}
