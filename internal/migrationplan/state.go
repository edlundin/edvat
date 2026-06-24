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
