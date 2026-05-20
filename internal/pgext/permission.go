package pgext

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/edlundin/edvat/internal/baseatlas"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

type Permission struct {
	Name       string
	Target     string
	Grantee    string
	Privileges []string
	Grantable  bool
}

type PermissionState map[string]Permission

func ParsePermissionFiles(paths []string) (PermissionState, error) {
	return parseStateFiles(paths, "permission", ParsePermissionsHCL)
}

func ParsePermissionsHCL(src []byte, filename string) (PermissionState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse permission hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse permission hcl: unexpected body type %T", file.Body)
	}

	state := PermissionState{}
	for _, block := range body.Blocks {
		if block.Type != "permission" {
			continue
		}
		permission := Permission{}
		if len(block.Labels) > 0 {
			permission.Name = block.Labels[0]
		}
		attrs := block.Body.Attributes
		if attr, ok := attrs["on"]; ok {
			target, err := targetExpr(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode permission.on: %w", err)
			}
			permission.Target = target
		}
		if attr, ok := attrs["to"]; ok {
			grantee, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode permission.to: %w", err)
			}
			permission.Grantee = grantee
		}
		if attr, ok := attrs["privileges"]; ok {
			privileges, err := symbolOrStringList(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode permission.privileges: %w", err)
			}
			for _, privilege := range privileges {
				permission.Privileges = append(permission.Privileges, strings.ToUpper(privilege))
			}
		}
		if attr, ok := attrs["grantable"]; ok {
			grantable, err := boolExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode permission.grantable: %w", err)
			}
			permission.Grantable = grantable
		}
		if permission.Target == "" {
			return nil, fmt.Errorf("permission requires on")
		}
		if permission.Grantee == "" {
			return nil, fmt.Errorf("permission %s requires to", permission.Target)
		}
		if len(permission.Privileges) == 0 {
			return nil, fmt.Errorf("permission %s requires privileges", permission.Target)
		}
		state[permissionID(permission)] = permission
	}
	return state, nil
}

func InspectPermissionsURL(ctx context.Context, url string) (PermissionState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectPermissions(ctx, db)
}

func InspectPermissions(ctx context.Context, db *sql.DB) (PermissionState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT CASE WHEN c.relkind = 'S' THEN 'SEQUENCE' ELSE 'TABLE' END AS target_kind,
       n.nspname,
       c.relname,
       CASE WHEN acl.grantee = 0 THEN 'public' ELSE grantee.rolname END AS grantee,
       acl.privilege_type,
       acl.is_grantable
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN LATERAL aclexplode(c.relacl) acl ON true
LEFT JOIN pg_roles grantee ON grantee.oid = acl.grantee
WHERE c.relkind IN ('r', 'p', 'v', 'm', 'S')
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND acl.grantee <> c.relowner
ORDER BY target_kind, n.nspname, c.relname, grantee, acl.is_grantable, acl.privilege_type`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres relation permissions: %w", err)
	}
	defer rows.Close()
	state := PermissionState{}
	for rows.Next() {
		var targetKind, schemaName, objectName, grantee, privilege string
		var grantable bool
		if err := rows.Scan(&targetKind, &schemaName, &objectName, &grantee, &privilege, &grantable); err != nil {
			return nil, fmt.Errorf("scan postgres relation permission: %w", err)
		}
		addInspectedPermission(state, Permission{Target: targetKind + " " + qualifiedIdent(schemaName, objectName), Grantee: grantee, Privileges: []string{strings.ToUpper(privilege)}, Grantable: grantable})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres relation permissions: %w", err)
	}

	schemaRows, err := db.QueryContext(ctx, `
SELECT n.nspname,
       CASE WHEN acl.grantee = 0 THEN 'public' ELSE grantee.rolname END AS grantee,
       acl.privilege_type,
       acl.is_grantable
FROM pg_namespace n
JOIN LATERAL aclexplode(n.nspacl) acl ON true
LEFT JOIN pg_roles grantee ON grantee.oid = acl.grantee
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND acl.grantee <> n.nspowner
  AND NOT (n.nspname = 'public' AND acl.grantee = 0)
ORDER BY n.nspname, grantee, acl.is_grantable, acl.privilege_type`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres schema permissions: %w", err)
	}
	defer schemaRows.Close()
	for schemaRows.Next() {
		var schemaName, grantee, privilege string
		var grantable bool
		if err := schemaRows.Scan(&schemaName, &grantee, &privilege, &grantable); err != nil {
			return nil, fmt.Errorf("scan postgres schema permission: %w", err)
		}
		addInspectedPermission(state, Permission{Target: "SCHEMA " + quoteIdent(schemaName), Grantee: grantee, Privileges: []string{strings.ToUpper(privilege)}, Grantable: grantable})
	}
	if err := schemaRows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres schema permissions: %w", err)
	}

	routineRows, err := db.QueryContext(ctx, `
SELECT CASE p.prokind WHEN 'p' THEN 'PROCEDURE' ELSE 'FUNCTION' END AS target_kind,
       n.nspname,
       p.proname,
       pg_get_function_identity_arguments(p.oid),
       CASE WHEN acl.grantee = 0 THEN 'public' ELSE grantee.rolname END AS grantee,
       acl.privilege_type,
       acl.is_grantable
FROM pg_proc p
JOIN pg_namespace n ON n.oid = p.pronamespace
JOIN LATERAL aclexplode(p.proacl) acl ON true
LEFT JOIN pg_roles grantee ON grantee.oid = acl.grantee
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND acl.grantee <> p.proowner
  AND acl.grantee <> 0
ORDER BY target_kind, n.nspname, p.proname, pg_get_function_identity_arguments(p.oid), grantee, acl.is_grantable, acl.privilege_type`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres routine permissions: %w", err)
	}
	defer routineRows.Close()
	for routineRows.Next() {
		var targetKind, schemaName, routineName, args, grantee, privilege string
		var grantable bool
		if err := routineRows.Scan(&targetKind, &schemaName, &routineName, &args, &grantee, &privilege, &grantable); err != nil {
			return nil, fmt.Errorf("scan postgres routine permission: %w", err)
		}
		addInspectedPermission(state, Permission{Target: targetKind + " " + qualifiedIdent(schemaName, routineName) + "(" + argTypeList(parseIdentityArgs(args)) + ")", Grantee: grantee, Privileges: []string{strings.ToUpper(privilege)}, Grantable: grantable})
	}
	if err := routineRows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres routine permissions: %w", err)
	}

	typeRows, err := db.QueryContext(ctx, `
SELECT n.nspname,
       t.typname,
       CASE WHEN acl.grantee = 0 THEN 'public' ELSE grantee.rolname END AS grantee,
       acl.privilege_type,
       acl.is_grantable
FROM pg_type t
JOIN pg_namespace n ON n.oid = t.typnamespace
JOIN LATERAL aclexplode(t.typacl) acl ON true
LEFT JOIN pg_roles grantee ON grantee.oid = acl.grantee
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND t.typtype IN ('b', 'c', 'd', 'e', 'r', 'm')
  AND t.typrelid = 0
  AND acl.grantee <> t.typowner
  AND acl.grantee <> 0
ORDER BY n.nspname, t.typname, grantee, acl.is_grantable, acl.privilege_type`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres type permissions: %w", err)
	}
	defer typeRows.Close()
	for typeRows.Next() {
		var schemaName, typeName, grantee, privilege string
		var grantable bool
		if err := typeRows.Scan(&schemaName, &typeName, &grantee, &privilege, &grantable); err != nil {
			return nil, fmt.Errorf("scan postgres type permission: %w", err)
		}
		addInspectedPermission(state, Permission{Target: "TYPE " + qualifiedIdent(schemaName, typeName), Grantee: grantee, Privileges: []string{strings.ToUpper(privilege)}, Grantable: grantable})
	}
	if err := typeRows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres type permissions: %w", err)
	}

	databaseRows, err := db.QueryContext(ctx, `
SELECT d.datname,
       CASE WHEN acl.grantee = 0 THEN 'public' ELSE grantee.rolname END AS grantee,
       acl.privilege_type,
       acl.is_grantable
FROM pg_database d
JOIN LATERAL aclexplode(d.datacl) acl ON true
LEFT JOIN pg_roles grantee ON grantee.oid = acl.grantee
WHERE d.datname = current_database()
  AND acl.grantee <> d.datdba
  AND acl.grantee <> 0
ORDER BY d.datname, grantee, acl.is_grantable, acl.privilege_type`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres database permissions: %w", err)
	}
	defer databaseRows.Close()
	for databaseRows.Next() {
		var databaseName, grantee, privilege string
		var grantable bool
		if err := databaseRows.Scan(&databaseName, &grantee, &privilege, &grantable); err != nil {
			return nil, fmt.Errorf("scan postgres database permission: %w", err)
		}
		addInspectedPermission(state, Permission{Target: "DATABASE " + quoteIdent(databaseName), Grantee: grantee, Privileges: []string{strings.ToUpper(privilege)}, Grantable: grantable})
	}
	if err := databaseRows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres database permissions: %w", err)
	}

	serverRows, err := db.QueryContext(ctx, `
SELECT s.srvname,
       CASE WHEN acl.grantee = 0 THEN 'public' ELSE grantee.rolname END AS grantee,
       acl.privilege_type,
       acl.is_grantable
FROM pg_foreign_server s
JOIN LATERAL aclexplode(s.srvacl) acl ON true
LEFT JOIN pg_roles grantee ON grantee.oid = acl.grantee
WHERE acl.grantee <> s.srvowner
ORDER BY s.srvname, grantee, acl.is_grantable, acl.privilege_type`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres foreign server permissions: %w", err)
	}
	defer serverRows.Close()
	for serverRows.Next() {
		var serverName, grantee, privilege string
		var grantable bool
		if err := serverRows.Scan(&serverName, &grantee, &privilege, &grantable); err != nil {
			return nil, fmt.Errorf("scan postgres foreign server permission: %w", err)
		}
		addInspectedPermission(state, Permission{Target: "FOREIGN SERVER " + quoteIdent(serverName), Grantee: grantee, Privileges: []string{strings.ToUpper(privilege)}, Grantable: grantable})
	}
	if err := serverRows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres foreign server permissions: %w", err)
	}
	return state, nil
}

func addInspectedPermission(state PermissionState, permission Permission) {
	id := permissionID(permission)
	if existing, ok := state[id]; ok && existing.Grantable == permission.Grantable {
		existing.Privileges = append(existing.Privileges, permission.Privileges...)
		state[id] = existing
		return
	}
	state[id] = permission
}

func DiffPermissions(current, desired PermissionState) []baseatlas.Statement {
	if current == nil {
		current = PermissionState{}
	}
	if desired == nil {
		desired = PermissionState{}
	}
	ids := stateIDs(current, desired)

	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, grantPermissionStatement(des))
		case hasCurrent && !hasDesired:
			statements = append(statements, revokePermissionStatement(cur))
		case hasCurrent && hasDesired:
			if !samePermission(cur, des) {
				statements = append(statements, revokePermissionStatement(cur), grantPermissionStatement(des))
			}
		}
	}
	return statements
}

func grantPermissionStatement(permission Permission) baseatlas.Statement {
	grantOption := ""
	if permission.Grantable {
		grantOption = " WITH GRANT OPTION"
	}
	return baseatlas.Statement{Comment: "grant permission " + permissionID(permission), SQL: "GRANT " + strings.Join(permission.Privileges, ", ") + " ON " + permission.Target + " TO " + roleSQL(permission.Grantee) + grantOption, Reverse: revokePermissionStatement(permission).SQL}
}

func revokePermissionStatement(permission Permission) baseatlas.Statement {
	return baseatlas.Statement{Comment: "revoke permission " + permissionID(permission) + " (destructive)", SQL: "REVOKE " + strings.Join(permission.Privileges, ", ") + " ON " + permission.Target + " FROM " + roleSQL(permission.Grantee)}
}

func samePermission(a, b Permission) bool {
	return samePrivileges(a.Privileges, b.Privileges) && a.Grantable == b.Grantable
}

func samePrivileges(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa := append([]string(nil), a...)
	bb := append([]string(nil), b...)
	sort.Strings(aa)
	sort.Strings(bb)
	return strings.Join(aa, ",") == strings.Join(bb, ",")
}

func permissionID(permission Permission) string {
	return permission.Target + " TO " + permission.Grantee
}

func targetExpr(expr hclsyntax.Expression, src []byte) (string, error) {
	if traversal, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		parts := traversalParts(traversal.Traversal)
		if len(parts) == 2 && parts[0] == "table" {
			return "TABLE " + quoteIdent(parts[1]), nil
		}
		if len(parts) == 2 && parts[0] == "schema" {
			return "SCHEMA " + quoteIdent(parts[1]), nil
		}
		if len(parts) == 2 && parts[0] == "sequence" {
			return "SEQUENCE " + quoteIdent(parts[1]), nil
		}
		if len(parts) == 2 && (parts[0] == "type" || parts[0] == "domain") {
			return "TYPE " + quoteIdent(parts[1]), nil
		}
		if len(parts) == 2 && parts[0] == "database" {
			return "DATABASE " + quoteIdent(parts[1]), nil
		}
		if len(parts) == 2 && parts[0] == "server" {
			return "FOREIGN SERVER " + quoteIdent(parts[1]), nil
		}
		if len(parts) == 3 && parts[0] == "schema" {
			return "TABLE " + qualifiedIdent(parts[1], parts[2]), nil
		}
		if len(parts) == 3 && parts[0] == "sequence" {
			return "SEQUENCE " + qualifiedIdent(parts[1], parts[2]), nil
		}
		if len(parts) == 3 && (parts[0] == "type" || parts[0] == "domain") {
			return "TYPE " + qualifiedIdent(parts[1], parts[2]), nil
		}
	}
	return symbolOrString(expr, src)
}

func roleSQL(role string) string {
	if strings.EqualFold(role, "public") {
		return "PUBLIC"
	}
	return quoteIdent(role)
}
