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

type User struct {
	Name       string
	CreateDB   bool
	CreateRole bool
	Inherit    bool
	ValidUntil string
	Comment    string
}

type UserState map[string]User

func ParseUserFiles(paths []string) (UserState, error) {
	return parseStateFiles(paths, "user", ParseUsersHCL)
}

func ParseUsersHCL(src []byte, filename string) (UserState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse user hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse user hcl: unexpected body type %T", file.Body)
	}
	state := UserState{}
	for _, block := range body.Blocks {
		if block.Type != "user" || len(block.Labels) != 1 {
			continue
		}
		user := User{Name: block.Labels[0], Inherit: true}
		attrs := block.Body.Attributes
		if err := rejectUnknownAttrs("user."+user.Name, attrs, map[string]bool{"createdb": true, "createrole": true, "inherit": true, "valid_until": true, "comment": true, "password": true}); err != nil {
			return nil, err
		}
		if _, ok := attrs["password"]; ok {
			return nil, fmt.Errorf("user.%s.password is not supported; user secrets are not emitted to migrations", user.Name)
		}
		boolAttrs := map[string]*bool{"createdb": &user.CreateDB, "createrole": &user.CreateRole, "inherit": &user.Inherit}
		for name, target := range boolAttrs {
			if attr, ok := attrs[name]; ok {
				value, err := boolExpr(attr.Expr)
				if err != nil {
					return nil, fmt.Errorf("decode user.%s.%s: %w", user.Name, name, err)
				}
				*target = value
			}
		}
		if attr, ok := attrs["valid_until"]; ok {
			validUntil, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode user.%s.valid_until: %w", user.Name, err)
			}
			user.ValidUntil = validUntil
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode user.%s.comment: %w", user.Name, err)
			}
			user.Comment = comment
		}
		state[user.Name] = user
	}
	return state, nil
}

func InspectUsersURL(ctx context.Context, url string) (UserState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectUsers(ctx, db)
}

func InspectUsers(ctx context.Context, db *sql.DB) (UserState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT r.rolname, r.rolcreatedb, r.rolcreaterole, r.rolinherit, COALESCE(d.description, '')
FROM pg_roles r
LEFT JOIN pg_shdescription d ON d.objoid = r.oid AND d.classoid = 'pg_authid'::regclass
WHERE r.rolcanlogin AND r.rolname !~ '^pg_'
ORDER BY r.rolname`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres users: %w", err)
	}
	defer rows.Close()
	state := UserState{}
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.Name, &user.CreateDB, &user.CreateRole, &user.Inherit, &user.Comment); err != nil {
			return nil, fmt.Errorf("scan postgres user: %w", err)
		}
		state[user.Name] = user
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres users: %w", err)
	}
	return state, nil
}

func DiffUsers(current, desired UserState) []baseatlas.Statement {
	if current == nil {
		current = UserState{}
	}
	if desired == nil {
		desired = UserState{}
	}
	ids := stateIDs(current, desired)
	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createUserStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, baseatlas.Statement{Comment: "drop user " + cur.Name + " (destructive)", SQL: dropUserSQL(cur), Reverse: strings.Join(userSQLStatements(cur), ";\n")})
		case hasCurrent && hasDesired:
			if !sameUserDefinition(cur, des) {
				statements = append(statements, baseatlas.Statement{Comment: "alter user " + des.Name, SQL: alterUserSQL(des), Reverse: alterUserSQL(cur)})
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on user " + des.Name, SQL: "COMMENT ON ROLE " + quoteIdent(des.Name) + " IS " + nullableLiteral(des.Comment), Reverse: "COMMENT ON ROLE " + quoteIdent(cur.Name) + " IS " + nullableLiteral(cur.Comment)})
			}
		}
	}
	return statements
}

func createUserStatements(user User) []baseatlas.Statement {
	statements := []baseatlas.Statement{{Comment: "create user " + user.Name, SQL: createUserSQL(user), Reverse: dropUserSQL(user)}}
	if user.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on user " + user.Name, SQL: "COMMENT ON ROLE " + quoteIdent(user.Name) + " IS " + literal(user.Comment), Reverse: "COMMENT ON ROLE " + quoteIdent(user.Name) + " IS NULL"})
	}
	return statements
}

func userSQLStatements(user User) []string {
	statements := []string{createUserSQL(user)}
	if user.Comment != "" {
		statements = append(statements, "COMMENT ON ROLE "+quoteIdent(user.Name)+" IS "+literal(user.Comment))
	}
	return statements
}

func dropUserSQL(user User) string {
	return "DROP ROLE " + quoteIdent(user.Name)
}

func createUserSQL(user User) string {
	return "CREATE ROLE " + quoteIdent(user.Name) + userOptionsSQL(user)
}
func alterUserSQL(user User) string {
	return "ALTER ROLE " + quoteIdent(user.Name) + userOptionsSQL(user)
}

func userOptionsSQL(user User) string {
	role := Role{Name: user.Name, Login: true, CreateDB: user.CreateDB, CreateRole: user.CreateRole, Inherit: user.Inherit}
	out := roleOptionsSQL(role)
	if user.ValidUntil != "" {
		out += " VALID UNTIL " + literal(user.ValidUntil)
	}
	return out
}

func sameUserDefinition(a, b User) bool {
	return a.CreateDB == b.CreateDB && a.CreateRole == b.CreateRole && a.Inherit == b.Inherit && a.ValidUntil == b.ValidUntil
}
