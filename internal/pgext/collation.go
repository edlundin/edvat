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

type Collation struct {
	Name          string
	Schema        string
	Locale        string
	LCType        string
	LCCollate     string
	Provider      string
	Deterministic *bool
	Comment       string
}

type CollationState map[string]Collation

func ParseCollationFiles(paths []string) (CollationState, error) {
	return parseStateFiles(paths, "collation", ParseCollationsHCL)
}

func ParseCollationsHCL(src []byte, filename string) (CollationState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse collation hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse collation hcl: unexpected body type %T", file.Body)
	}

	state := CollationState{}
	for _, block := range body.Blocks {
		if block.Type != "collation" || len(block.Labels) != 1 {
			continue
		}
		collation := Collation{Name: block.Labels[0]}
		attrs := block.Body.Attributes
		if attr, ok := attrs["schema"]; ok {
			schemaName, err := schemaExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode collation.%s.schema: %w", collation.Name, err)
			}
			collation.Schema = schemaName
		}
		for attrName, target := range map[string]*string{"locale": &collation.Locale, "lc_type": &collation.LCType, "lc_collate": &collation.LCCollate, "provider": &collation.Provider} {
			if attr, ok := attrs[attrName]; ok {
				value, err := symbolOrString(attr.Expr, src)
				if err != nil {
					return nil, fmt.Errorf("decode collation.%s.%s: %w", collation.Name, attrName, err)
				}
				*target = value
			}
		}
		if attr, ok := attrs["deterministic"]; ok {
			deterministic, err := boolExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode collation.%s.deterministic: %w", collation.Name, err)
			}
			collation.Deterministic = &deterministic
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode collation.%s.comment: %w", collation.Name, err)
			}
			collation.Comment = comment
		}
		if collation.Locale == "" && (collation.LCType == "" || collation.LCCollate == "") {
			return nil, fmt.Errorf("collation.%s requires locale or both lc_type and lc_collate", collation.Name)
		}
		state[collationID(collation)] = collation
	}
	return state, nil
}

func InspectCollationsURL(ctx context.Context, url string) (CollationState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectCollations(ctx, db)
}

func InspectCollations(ctx context.Context, db *sql.DB) (CollationState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT n.nspname,
       c.collname,
       COALESCE(c.collcollate, ''),
       COALESCE(c.collctype, ''),
       CASE c.collprovider WHEN 'd' THEN 'default' WHEN 'c' THEN 'libc' WHEN 'i' THEN 'icu' ELSE c.collprovider::text END,
       c.collisdeterministic,
       COALESCE(d.description, '')
FROM pg_collation c
JOIN pg_namespace n ON n.oid = c.collnamespace
LEFT JOIN pg_description d ON d.objoid = c.oid AND d.classoid = 'pg_collation'::regclass AND d.objsubid = 0
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY n.nspname, c.collname`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres collations: %w", err)
	}
	defer rows.Close()
	state := CollationState{}
	for rows.Next() {
		var collation Collation
		var deterministic bool
		if err := rows.Scan(&collation.Schema, &collation.Name, &collation.LCCollate, &collation.LCType, &collation.Provider, &deterministic, &collation.Comment); err != nil {
			return nil, fmt.Errorf("scan postgres collation: %w", err)
		}
		collation.Deterministic = &deterministic
		state[collationID(collation)] = collation
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres collations: %w", err)
	}
	return state, nil
}

func DiffCollations(current, desired CollationState) []baseatlas.Statement {
	if current == nil {
		current = CollationState{}
	}
	if desired == nil {
		desired = CollationState{}
	}
	ids := stateIDs(current, desired)

	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createCollationStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, baseatlas.Statement{Comment: "drop collation " + collationID(cur) + " (destructive)", SQL: "DROP COLLATION " + qualifiedIdent(cur.Schema, cur.Name), Reverse: strings.Join(collationSQL(cur), ";\n")})
		case hasCurrent && hasDesired:
			if !sameCollationDefinition(cur, des) {
				statements = append(statements, baseatlas.Statement{Comment: "drop collation " + collationID(cur) + " for replacement (destructive)", SQL: "DROP COLLATION " + qualifiedIdent(cur.Schema, cur.Name)})
				statements = append(statements, createCollationStatements(des)...)
				continue
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on collation " + collationID(des), SQL: "COMMENT ON COLLATION " + qualifiedIdent(des.Schema, des.Name) + " IS " + nullableLiteral(des.Comment), Reverse: "COMMENT ON COLLATION " + qualifiedIdent(cur.Schema, cur.Name) + " IS " + nullableLiteral(cur.Comment)})
			}
		}
	}
	return statements
}

func createCollationStatements(collation Collation) []baseatlas.Statement {
	statements := []baseatlas.Statement{{Comment: "create collation " + collationID(collation), SQL: createCollationSQL(collation), Reverse: "DROP COLLATION " + qualifiedIdent(collation.Schema, collation.Name)}}
	if collation.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on collation " + collationID(collation), SQL: "COMMENT ON COLLATION " + qualifiedIdent(collation.Schema, collation.Name) + " IS " + literal(collation.Comment), Reverse: "COMMENT ON COLLATION " + qualifiedIdent(collation.Schema, collation.Name) + " IS NULL"})
	}
	return statements
}

func collationSQL(collation Collation) []string {
	statements := []string{createCollationSQL(collation)}
	if collation.Comment != "" {
		statements = append(statements, "COMMENT ON COLLATION "+qualifiedIdent(collation.Schema, collation.Name)+" IS "+literal(collation.Comment))
	}
	return statements
}

func createCollationSQL(collation Collation) string {
	var opts []string
	if collation.Provider != "" {
		opts = append(opts, "provider = "+literal(collation.Provider))
	}
	if collation.Locale != "" {
		opts = append(opts, "locale = "+literal(collation.Locale))
	}
	if collation.LCCollate != "" {
		opts = append(opts, "lc_collate = "+literal(collation.LCCollate))
	}
	if collation.LCType != "" {
		opts = append(opts, "lc_ctype = "+literal(collation.LCType))
	}
	if collation.Deterministic != nil {
		opts = append(opts, fmt.Sprintf("deterministic = %t", *collation.Deterministic))
	}
	return "CREATE COLLATION " + qualifiedIdent(collation.Schema, collation.Name) + " (" + strings.Join(opts, ", ") + ")"
}

func sameCollationDefinition(a, b Collation) bool {
	return a.Locale == b.Locale && a.LCType == b.LCType && a.LCCollate == b.LCCollate && a.Provider == b.Provider && boolPtrEqual(a.Deterministic, b.Deterministic)
}

func boolPtrEqual(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func collationID(collation Collation) string { return viewID(collation.Schema, collation.Name) }
