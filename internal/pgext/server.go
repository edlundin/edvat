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

type Server struct {
	Name    string
	FDW     string
	Type    string
	Version string
	Options map[string]string
	Comment string
}

type ServerState map[string]Server

func ParseServerFiles(paths []string) (ServerState, error) {
	return parseStateFiles(paths, "server", ParseServersHCL)
}

func ParseServersHCL(src []byte, filename string) (ServerState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse server hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse server hcl: unexpected body type %T", file.Body)
	}
	state := ServerState{}
	for _, block := range body.Blocks {
		if block.Type != "server" || len(block.Labels) != 1 {
			continue
		}
		server := Server{Name: block.Labels[0], Options: map[string]string{}}
		attrs := block.Body.Attributes
		if attr, ok := attrs["fdw"]; ok {
			fdw, err := symbolOrString(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode server.%s.fdw: %w", server.Name, err)
			}
			server.FDW = fdw
		}
		for attrName, target := range map[string]*string{"type": &server.Type, "version": &server.Version, "comment": &server.Comment} {
			if attr, ok := attrs[attrName]; ok {
				value, err := stringExpr(attr.Expr)
				if err != nil {
					return nil, fmt.Errorf("decode server.%s.%s: %w", server.Name, attrName, err)
				}
				*target = value
			}
		}
		if attr, ok := attrs["options"]; ok {
			options, err := stringMapExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode server.%s.options: %w", server.Name, err)
			}
			server.Options = options
		}
		if server.FDW == "" {
			return nil, fmt.Errorf("server.%s requires fdw", server.Name)
		}
		state[server.Name] = server
	}
	return state, nil
}

func InspectServersURL(ctx context.Context, url string) (ServerState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectServers(ctx, db)
}

func InspectServers(ctx context.Context, db *sql.DB) (ServerState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT s.srvname, f.fdwname, COALESCE(s.srvtype, ''), COALESCE(s.srvversion, ''), COALESCE(array_to_string(s.srvoptions, ','), ''), COALESCE(d.description, '')
FROM pg_foreign_server s
JOIN pg_foreign_data_wrapper f ON f.oid = s.srvfdw
LEFT JOIN pg_description d ON d.objoid = s.oid AND d.classoid = 'pg_foreign_server'::regclass AND d.objsubid = 0
ORDER BY s.srvname`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres servers: %w", err)
	}
	defer rows.Close()
	state := ServerState{}
	for rows.Next() {
		var server Server
		var options string
		if err := rows.Scan(&server.Name, &server.FDW, &server.Type, &server.Version, &options, &server.Comment); err != nil {
			return nil, fmt.Errorf("scan postgres server: %w", err)
		}
		server.Options = parseOptionCSV(options)
		state[server.Name] = server
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres servers: %w", err)
	}
	return state, nil
}

func DiffServers(current, desired ServerState) []baseatlas.Statement {
	if current == nil {
		current = ServerState{}
	}
	if desired == nil {
		desired = ServerState{}
	}
	ids := stateIDs(current, desired)
	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createServerStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, baseatlas.Statement{Comment: "drop server " + cur.Name + " (destructive)", SQL: dropServerSQL(cur), Reverse: strings.Join(serverSQL(cur), ";\n")})
		case hasCurrent && hasDesired:
			if !sameServerDefinition(cur, des) {
				statements = append(statements, baseatlas.Statement{Comment: "drop server " + cur.Name + " for replacement (destructive)", SQL: "DROP SERVER " + quoteIdent(cur.Name)})
				statements = append(statements, createServerStatements(des)...)
				continue
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on server " + des.Name, SQL: "COMMENT ON SERVER " + quoteIdent(des.Name) + " IS " + nullableLiteral(des.Comment), Reverse: "COMMENT ON SERVER " + quoteIdent(cur.Name) + " IS " + nullableLiteral(cur.Comment)})
			}
		}
	}
	return statements
}

func createServerStatements(server Server) []baseatlas.Statement {
	statements := []baseatlas.Statement{{Comment: "create server " + server.Name, SQL: createServerSQL(server), Reverse: dropServerSQL(server)}}
	if server.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on server " + server.Name, SQL: "COMMENT ON SERVER " + quoteIdent(server.Name) + " IS " + literal(server.Comment), Reverse: "COMMENT ON SERVER " + quoteIdent(server.Name) + " IS NULL"})
	}
	return statements
}

func serverSQL(server Server) []string {
	statements := []string{createServerSQL(server)}
	if server.Comment != "" {
		statements = append(statements, "COMMENT ON SERVER "+quoteIdent(server.Name)+" IS "+literal(server.Comment))
	}
	return statements
}

func dropServerSQL(server Server) string {
	return "DROP SERVER " + quoteIdent(server.Name)
}

func createServerSQL(server Server) string {
	var b strings.Builder
	b.WriteString("CREATE SERVER ")
	b.WriteString(quoteIdent(server.Name))
	if server.Type != "" {
		b.WriteString(" TYPE ")
		b.WriteString(literal(server.Type))
	}
	if server.Version != "" {
		b.WriteString(" VERSION ")
		b.WriteString(literal(server.Version))
	}
	b.WriteString(" FOREIGN DATA WRAPPER ")
	b.WriteString(quoteIdent(server.FDW))
	if len(server.Options) > 0 {
		b.WriteString(" OPTIONS (")
		b.WriteString(optionsSQL(server.Options))
		b.WriteString(")")
	}
	return b.String()
}

func sameServerDefinition(a, b Server) bool {
	return a.FDW == b.FDW && a.Type == b.Type && a.Version == b.Version && stringMapEqual(a.Options, b.Options)
}

func stringMapExpr(expr hclsyntax.Expression) (map[string]string, error) {
	value, diags := expr.Value(nil)
	if diags.HasErrors() {
		return nil, fmt.Errorf("%s", diags.Error())
	}
	out := map[string]string{}
	it := value.ElementIterator()
	for it.Next() {
		k, v := it.Element()
		out[k.AsString()] = v.AsString()
	}
	return out, nil
}

func optionsSQL(options map[string]string) string {
	keys := make([]string, 0, len(options))
	for k := range options {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, quoteIdent(k)+" "+literal(options[k]))
	}
	return strings.Join(parts, ", ")
}

func stringMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func parseOptionCSV(value string) map[string]string {
	out := map[string]string{}
	for _, part := range splitCSV(value) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			out[kv[0]] = kv[1]
		}
	}
	return out
}
