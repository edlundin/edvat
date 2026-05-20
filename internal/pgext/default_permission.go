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

type DefaultPermission struct {
	Name       string
	Schema     string
	ForRole    string
	On         string
	Grantee    string
	Privileges []string
	Grantable  bool
}

type DefaultPermissionState map[string]DefaultPermission

func ParseDefaultPermissionFiles(paths []string) (DefaultPermissionState, error) {
	return parseStateFiles(paths, "default permission", ParseDefaultPermissionsHCL)
}

func ParseDefaultPermissionsHCL(src []byte, filename string) (DefaultPermissionState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse default permission hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse default permission hcl: unexpected body type %T", file.Body)
	}
	state := DefaultPermissionState{}
	for _, block := range body.Blocks {
		if block.Type != "default_permission" {
			continue
		}
		permission := DefaultPermission{On: "TABLES"}
		if len(block.Labels) > 0 {
			permission.Name = block.Labels[0]
		}
		attrs := block.Body.Attributes
		if attr, ok := attrs["schema"]; ok {
			schemaName, err := schemaExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode default_permission.%s.schema: %w", permission.Name, err)
			}
			permission.Schema = schemaName
		}
		if attr, ok := attrs["for_role"]; ok {
			role, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode default_permission.%s.for_role: %w", permission.Name, err)
			}
			permission.ForRole = role
		}
		if attr, ok := attrs["on"]; ok {
			on, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode default_permission.%s.on: %w", permission.Name, err)
			}
			permission.On = strings.ToUpper(on)
		}
		if attr, ok := attrs["to"]; ok {
			grantee, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode default_permission.%s.to: %w", permission.Name, err)
			}
			permission.Grantee = grantee
		}
		if attr, ok := attrs["privileges"]; ok {
			privileges, err := symbolOrStringList(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode default_permission.%s.privileges: %w", permission.Name, err)
			}
			for _, privilege := range privileges {
				permission.Privileges = append(permission.Privileges, strings.ToUpper(privilege))
			}
		}
		if attr, ok := attrs["grantable"]; ok {
			grantable, err := boolExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode default_permission.%s.grantable: %w", permission.Name, err)
			}
			permission.Grantable = grantable
		}
		if err := validateDefaultPermission(permission); err != nil {
			return nil, err
		}
		if permission.Schema == "" {
			return nil, fmt.Errorf("default_permission.%s requires schema", permission.Name)
		}
		if permission.Grantee == "" {
			return nil, fmt.Errorf("default_permission.%s requires to", permission.Name)
		}
		if len(permission.Privileges) == 0 {
			return nil, fmt.Errorf("default_permission.%s requires privileges", permission.Name)
		}
		state[defaultPermissionID(permission)] = permission
	}
	return state, nil
}

func validateDefaultPermission(permission DefaultPermission) error {
	allowed := map[string]map[string]bool{
		"TABLES":    {"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true, "TRUNCATE": true, "REFERENCES": true, "TRIGGER": true},
		"SEQUENCES": {"USAGE": true, "SELECT": true, "UPDATE": true},
		"FUNCTIONS": {"EXECUTE": true},
		"TYPES":     {"USAGE": true},
		"SCHEMAS":   {"CREATE": true, "USAGE": true},
	}
	privileges, ok := allowed[permission.On]
	if !ok {
		return fmt.Errorf("default_permission.%s unsupported on %q", permission.Name, permission.On)
	}
	for _, privilege := range permission.Privileges {
		if !privileges[privilege] {
			return fmt.Errorf("default_permission.%s unsupported privilege %q for %s", permission.Name, privilege, permission.On)
		}
	}
	return nil
}

func InspectDefaultPermissionsURL(ctx context.Context, url string) (DefaultPermissionState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectDefaultPermissions(ctx, db)
}

func InspectDefaultPermissions(ctx context.Context, db *sql.DB) (DefaultPermissionState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT COALESCE(n.nspname, ''), owner.rolname,
       CASE d.defaclobjtype WHEN 'r' THEN 'TABLES' WHEN 'S' THEN 'SEQUENCES' WHEN 'f' THEN 'FUNCTIONS' WHEN 'T' THEN 'TYPES' WHEN 'n' THEN 'SCHEMAS' ELSE d.defaclobjtype::text END,
       CASE WHEN acl.grantee = 0 THEN 'public' ELSE grantee.rolname END,
       acl.privilege_type,
       acl.is_grantable
FROM pg_default_acl d
JOIN pg_roles owner ON owner.oid = d.defaclrole
LEFT JOIN pg_namespace n ON n.oid = d.defaclnamespace
JOIN LATERAL aclexplode(d.defaclacl) acl ON true
LEFT JOIN pg_roles grantee ON grantee.oid = acl.grantee
ORDER BY COALESCE(n.nspname, ''), owner.rolname, d.defaclobjtype, grantee, acl.is_grantable, acl.privilege_type`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres default permissions: %w", err)
	}
	defer rows.Close()
	state := DefaultPermissionState{}
	for rows.Next() {
		var schemaName, forRole, on, grantee, privilege string
		var grantable bool
		if err := rows.Scan(&schemaName, &forRole, &on, &grantee, &privilege, &grantable); err != nil {
			return nil, fmt.Errorf("scan postgres default permission: %w", err)
		}
		permission := DefaultPermission{Schema: schemaName, ForRole: forRole, On: on, Grantee: grantee, Privileges: []string{strings.ToUpper(privilege)}, Grantable: grantable}
		id := defaultPermissionID(permission)
		if existing, ok := state[id]; ok {
			existing.Privileges = append(existing.Privileges, permission.Privileges...)
			sort.Strings(existing.Privileges)
			state[id] = existing
		} else {
			state[id] = permission
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres default permissions: %w", err)
	}
	return state, nil
}

func DiffDefaultPermissions(current, desired DefaultPermissionState) []baseatlas.Statement {
	if current == nil {
		current = DefaultPermissionState{}
	}
	if desired == nil {
		desired = DefaultPermissionState{}
	}
	ids := stateIDs(current, desired)
	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, grantDefaultPermissionStatement(des))
		case hasCurrent && !hasDesired:
			statements = append(statements, revokeDefaultPermissionStatement(cur))
		case hasCurrent && hasDesired:
			if !equalStringSet(cur.Privileges, des.Privileges) || cur.Grantable != des.Grantable {
				statements = append(statements, revokeDefaultPermissionStatement(cur), grantDefaultPermissionStatement(des))
			}
		}
	}
	return statements
}

func grantDefaultPermissionStatement(permission DefaultPermission) baseatlas.Statement {
	return baseatlas.Statement{Comment: "grant default privileges " + defaultPermissionID(permission), SQL: grantDefaultPermissionSQL(permission), Reverse: revokeDefaultPermissionSQL(permission)}
}

func grantDefaultPermissionSQL(permission DefaultPermission) string {
	grant := "GRANT " + strings.Join(sortedCopy(permission.Privileges), ", ") + " ON " + permission.On + " TO " + roleIdent(permission.Grantee)
	if permission.Grantable {
		grant += " WITH GRANT OPTION"
	}
	return defaultPermissionPrefix(permission) + grant
}

func revokeDefaultPermissionStatement(permission DefaultPermission) baseatlas.Statement {
	return baseatlas.Statement{Comment: "revoke default privileges " + defaultPermissionID(permission) + " (destructive)", SQL: revokeDefaultPermissionSQL(permission), Reverse: grantDefaultPermissionSQL(permission)}
}

func revokeDefaultPermissionSQL(permission DefaultPermission) string {
	return defaultPermissionPrefix(permission) + "REVOKE " + strings.Join(sortedCopy(permission.Privileges), ", ") + " ON " + permission.On + " FROM " + roleIdent(permission.Grantee)
}

func defaultPermissionPrefix(permission DefaultPermission) string {
	parts := []string{"ALTER DEFAULT PRIVILEGES"}
	if permission.ForRole != "" {
		parts = append(parts, "FOR ROLE "+roleIdent(permission.ForRole))
	}
	if permission.Schema != "" {
		parts = append(parts, "IN SCHEMA "+quoteIdent(permission.Schema))
	}
	return strings.Join(parts, " ") + " "
}

func defaultPermissionID(permission DefaultPermission) string {
	forRole := permission.ForRole
	if forRole == "" {
		forRole = "*"
	}
	return permission.Schema + "." + forRole + "." + permission.On + "." + permission.Grantee
}

func roleIdent(role string) string {
	if strings.EqualFold(role, "public") {
		return "PUBLIC"
	}
	return quoteIdent(role)
}

func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func equalStringSet(a, b []string) bool {
	a = sortedCopy(a)
	b = sortedCopy(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
