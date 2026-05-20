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

type UserMapping struct {
	User    string
	Server  string
	Options map[string]string
}

type UserMappingState map[string]UserMapping

func ParseUserMappingFiles(paths []string) (UserMappingState, error) {
	return parseStateFiles(paths, "user mapping", ParseUserMappingsHCL)
}

func ParseUserMappingsHCL(src []byte, filename string) (UserMappingState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse user mapping hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse user mapping hcl: unexpected body type %T", file.Body)
	}
	state := UserMappingState{}
	for _, block := range body.Blocks {
		if block.Type != "user_mapping" {
			continue
		}
		mapping := UserMapping{Options: map[string]string{}}
		attrs := block.Body.Attributes
		if attr, ok := attrs["user"]; ok {
			user, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode user_mapping.user: %w", err)
			}
			mapping.User = user
		}
		if attr, ok := attrs["server"]; ok {
			server, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode user_mapping.server: %w", err)
			}
			mapping.Server = server
		}
		if attr, ok := attrs["options"]; ok {
			options, err := stringMapExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode user_mapping.options: %w", err)
			}
			if err := rejectSecretOptions(options); err != nil {
				return nil, fmt.Errorf("decode user_mapping.options: %w", err)
			}
			mapping.Options = options
		}
		if mapping.User == "" {
			return nil, fmt.Errorf("user_mapping requires user")
		}
		if mapping.Server == "" {
			return nil, fmt.Errorf("user_mapping for %s requires server", mapping.User)
		}
		state[userMappingID(mapping)] = mapping
	}
	return state, nil
}

func InspectUserMappingsURL(ctx context.Context, url string) (UserMappingState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectUserMappings(ctx, db)
}

func InspectUserMappings(ctx context.Context, db *sql.DB) (UserMappingState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT CASE WHEN u.umuser = 0 THEN 'PUBLIC' ELSE r.rolname END,
       s.srvname,
       COALESCE(array_to_string(u.umoptions, ','), '')
FROM pg_user_mapping u
JOIN pg_foreign_server s ON s.oid = u.umserver
LEFT JOIN pg_roles r ON r.oid = u.umuser
ORDER BY 1, 2`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres user mappings: %w", err)
	}
	defer rows.Close()
	state := UserMappingState{}
	for rows.Next() {
		var mapping UserMapping
		var options string
		if err := rows.Scan(&mapping.User, &mapping.Server, &options); err != nil {
			return nil, fmt.Errorf("scan postgres user mapping: %w", err)
		}
		mapping.Options = parseOptionCSV(options)
		state[userMappingID(mapping)] = mapping
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres user mappings: %w", err)
	}
	return state, nil
}

func DiffUserMappings(current, desired UserMappingState) []baseatlas.Statement {
	if current == nil {
		current = UserMappingState{}
	}
	if desired == nil {
		desired = UserMappingState{}
	}
	ids := stateIDs(current, desired)
	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, baseatlas.Statement{Comment: "create user mapping " + userMappingID(des), SQL: createUserMappingSQL(des), Reverse: dropUserMappingSQL(des)})
		case hasCurrent && !hasDesired:
			statements = append(statements, dropUserMappingStatement(cur))
		case hasCurrent && hasDesired:
			if !stringMapEqual(cur.Options, des.Options) {
				statements = append(statements, dropUserMappingStatement(cur), baseatlas.Statement{Comment: "create user mapping " + userMappingID(des), SQL: createUserMappingSQL(des), Reverse: dropUserMappingSQL(des)})
			}
		}
	}
	return statements
}

func createUserMappingSQL(mapping UserMapping) string {
	sql := "CREATE USER MAPPING FOR " + roleSQL(mapping.User) + " SERVER " + quoteIdent(mapping.Server)
	if len(mapping.Options) > 0 {
		sql += " OPTIONS (" + optionsSQL(mapping.Options) + ")"
	}
	return sql
}

func dropUserMappingStatement(mapping UserMapping) baseatlas.Statement {
	return baseatlas.Statement{Comment: "drop user mapping " + userMappingID(mapping) + " (destructive)", SQL: dropUserMappingSQL(mapping), Reverse: createUserMappingSQL(mapping)}
}

func dropUserMappingSQL(mapping UserMapping) string {
	return "DROP USER MAPPING FOR " + roleSQL(mapping.User) + " SERVER " + quoteIdent(mapping.Server)
}

func userMappingID(mapping UserMapping) string { return mapping.User + " SERVER " + mapping.Server }

func rejectSecretOptions(options map[string]string) error {
	for key := range options {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "key") {
			return fmt.Errorf("option %q looks secret; user mapping secrets are not emitted to migrations", key)
		}
	}
	return nil
}
