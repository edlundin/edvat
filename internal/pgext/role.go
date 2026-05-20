package pgext

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/edlundin/edvat/internal/baseatlas"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

type Role struct {
	Name        string
	Login       bool
	Superuser   bool
	CreateDB    bool
	CreateRole  bool
	Inherit     bool
	Replication bool
	BypassRLS   bool
	Comment     string
}

type RoleState map[string]Role

func ParseRoleFiles(paths []string) (RoleState, error) {
	return parseStateFiles(paths, "role", ParseRolesHCL)
}

func ParseRolesHCL(src []byte, filename string) (RoleState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse role hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse role hcl: unexpected body type %T", file.Body)
	}
	state := RoleState{}
	for _, block := range body.Blocks {
		if block.Type != "role" || len(block.Labels) != 1 {
			continue
		}
		role := Role{Name: block.Labels[0], Inherit: true}
		attrs := block.Body.Attributes
		if err := rejectUnknownAttrs("role."+role.Name, attrs, map[string]bool{"login": true, "superuser": true, "createdb": true, "createrole": true, "inherit": true, "replication": true, "bypassrls": true, "comment": true}); err != nil {
			return nil, err
		}
		boolAttrs := map[string]*bool{
			"login": &role.Login, "superuser": &role.Superuser, "createdb": &role.CreateDB,
			"createrole": &role.CreateRole, "inherit": &role.Inherit, "replication": &role.Replication, "bypassrls": &role.BypassRLS,
		}
		for name, target := range boolAttrs {
			if attr, ok := attrs[name]; ok {
				value, err := boolExpr(attr.Expr)
				if err != nil {
					return nil, fmt.Errorf("decode role.%s.%s: %w", role.Name, name, err)
				}
				*target = value
			}
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode role.%s.comment: %w", role.Name, err)
			}
			role.Comment = comment
		}
		state[role.Name] = role
	}
	return state, nil
}

func InspectRolesURL(ctx context.Context, url string) (RoleState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectRoles(ctx, db)
}

func InspectRoles(ctx context.Context, db *sql.DB) (RoleState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT r.rolname, r.rolcanlogin, r.rolsuper, r.rolcreatedb, r.rolcreaterole, r.rolinherit, r.rolreplication, r.rolbypassrls, COALESCE(d.description, '')
FROM pg_roles r
LEFT JOIN pg_shdescription d ON d.objoid = r.oid AND d.classoid = 'pg_authid'::regclass
WHERE r.rolname !~ '^pg_'
ORDER BY r.rolname`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres roles: %w", err)
	}
	defer rows.Close()
	state := RoleState{}
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.Name, &role.Login, &role.Superuser, &role.CreateDB, &role.CreateRole, &role.Inherit, &role.Replication, &role.BypassRLS, &role.Comment); err != nil {
			return nil, fmt.Errorf("scan postgres role: %w", err)
		}
		state[role.Name] = role
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres roles: %w", err)
	}
	return state, nil
}

func DiffRoles(current, desired RoleState) []baseatlas.Statement {
	if current == nil {
		current = RoleState{}
	}
	if desired == nil {
		desired = RoleState{}
	}
	ids := stateIDs(current, desired)
	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createRoleStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, baseatlas.Statement{Comment: "drop role " + cur.Name + " (destructive)", SQL: dropRoleSQL(cur), Reverse: strings.Join(roleSQLStatements(cur), ";\n")})
		case hasCurrent && hasDesired:
			if !sameRoleDefinition(cur, des) {
				statements = append(statements, baseatlas.Statement{Comment: "alter role " + des.Name, SQL: alterRoleSQL(des), Reverse: alterRoleSQL(cur)})
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on role " + des.Name, SQL: "COMMENT ON ROLE " + quoteIdent(des.Name) + " IS " + nullableLiteral(des.Comment), Reverse: "COMMENT ON ROLE " + quoteIdent(cur.Name) + " IS " + nullableLiteral(cur.Comment)})
			}
		}
	}
	return statements
}

func createRoleStatements(role Role) []baseatlas.Statement {
	statements := []baseatlas.Statement{{Comment: "create role " + role.Name, SQL: createRoleSQL(role), Reverse: dropRoleSQL(role)}}
	if role.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on role " + role.Name, SQL: "COMMENT ON ROLE " + quoteIdent(role.Name) + " IS " + literal(role.Comment), Reverse: "COMMENT ON ROLE " + quoteIdent(role.Name) + " IS NULL"})
	}
	return statements
}

func roleSQLStatements(role Role) []string {
	statements := []string{createRoleSQL(role)}
	if role.Comment != "" {
		statements = append(statements, "COMMENT ON ROLE "+quoteIdent(role.Name)+" IS "+literal(role.Comment))
	}
	return statements
}

func dropRoleSQL(role Role) string {
	return "DROP ROLE " + quoteIdent(role.Name)
}

func createRoleSQL(role Role) string {
	return "CREATE ROLE " + quoteIdent(role.Name) + roleOptionsSQL(role)
}
func alterRoleSQL(role Role) string {
	return "ALTER ROLE " + quoteIdent(role.Name) + roleOptionsSQL(role)
}

func roleOptionsSQL(role Role) string {
	options := []string{boolKeyword(role.Login, "LOGIN", "NOLOGIN"), boolKeyword(role.Superuser, "SUPERUSER", "NOSUPERUSER"), boolKeyword(role.CreateDB, "CREATEDB", "NOCREATEDB"), boolKeyword(role.CreateRole, "CREATEROLE", "NOCREATEROLE"), boolKeyword(role.Inherit, "INHERIT", "NOINHERIT"), boolKeyword(role.Replication, "REPLICATION", "NOREPLICATION"), boolKeyword(role.BypassRLS, "BYPASSRLS", "NOBYPASSRLS")}
	return " " + strings.Join(options, " ")
}

func boolKeyword(value bool, yes, no string) string {
	if value {
		return yes
	}
	return no
}

func sameRoleDefinition(a, b Role) bool {
	return a.Login == b.Login && a.Superuser == b.Superuser && a.CreateDB == b.CreateDB && a.CreateRole == b.CreateRole && a.Inherit == b.Inherit && a.Replication == b.Replication && a.BypassRLS == b.BypassRLS
}
