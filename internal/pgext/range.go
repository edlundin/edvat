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

type Range struct {
	Name           string
	Schema         string
	Subtype        string
	SubtypeDiff    string
	MultirangeName string
	Comment        string
}

type RangeState map[string]Range

func ParseRangeFiles(paths []string) (RangeState, error) {
	return parseStateFiles(paths, "range", ParseRangesHCL)
}

func ParseRangesHCL(src []byte, filename string) (RangeState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse range hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse range hcl: unexpected body type %T", file.Body)
	}

	state := RangeState{}
	for _, block := range body.Blocks {
		if block.Type != "range" || len(block.Labels) != 1 {
			continue
		}
		range_ := Range{Name: block.Labels[0]}
		attrs := block.Body.Attributes
		if attr, ok := attrs["schema"]; ok {
			schemaName, err := schemaExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode range.%s.schema: %w", range_.Name, err)
			}
			range_.Schema = schemaName
		}
		if attr, ok := attrs["subtype"]; ok {
			range_.Subtype = typeExpr(attr.Expr, src)
		}
		if attr, ok := attrs["subtype_diff"]; ok {
			subtypeDiff, err := functionExpr(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode range.%s.subtype_diff: %w", range_.Name, err)
			}
			range_.SubtypeDiff = subtypeDiff
		}
		if attr, ok := attrs["multirange_name"]; ok {
			multirangeName, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode range.%s.multirange_name: %w", range_.Name, err)
			}
			range_.MultirangeName = multirangeName
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode range.%s.comment: %w", range_.Name, err)
			}
			range_.Comment = comment
		}
		if range_.Subtype == "" {
			return nil, fmt.Errorf("range.%s requires subtype", range_.Name)
		}
		state[rangeID(range_)] = range_
	}
	return state, nil
}

func InspectRangesURL(ctx context.Context, url string) (RangeState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectRanges(ctx, db)
}

func InspectRanges(ctx context.Context, db *sql.DB) (RangeState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT n.nspname,
       t.typname,
       format_type(r.rngsubtype, NULL),
       COALESCE(pn.nspname || '.' || p.proname, ''),
       COALESCE(mt.typname, ''),
       COALESCE(d.description, '')
FROM pg_range r
JOIN pg_type t ON t.oid = r.rngtypid
JOIN pg_namespace n ON n.oid = t.typnamespace
LEFT JOIN pg_proc p ON p.oid = r.rngsubdiff
LEFT JOIN pg_namespace pn ON pn.oid = p.pronamespace
LEFT JOIN pg_type mt ON mt.oid = r.rngmultitypid
LEFT JOIN pg_description d ON d.objoid = t.oid AND d.classoid = 'pg_type'::regclass AND d.objsubid = 0
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY n.nspname, t.typname`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres ranges: %w", err)
	}
	defer rows.Close()
	state := RangeState{}
	for rows.Next() {
		var range_ Range
		if err := rows.Scan(&range_.Schema, &range_.Name, &range_.Subtype, &range_.SubtypeDiff, &range_.MultirangeName, &range_.Comment); err != nil {
			return nil, fmt.Errorf("scan postgres range: %w", err)
		}
		state[rangeID(range_)] = range_
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres ranges: %w", err)
	}
	return state, nil
}

func DiffRanges(current, desired RangeState) []baseatlas.Statement {
	if current == nil {
		current = RangeState{}
	}
	if desired == nil {
		desired = RangeState{}
	}
	ids := stateIDs(current, desired)

	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createRangeStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, baseatlas.Statement{Comment: "drop range " + rangeID(cur) + " (destructive)", SQL: "DROP TYPE " + qualifiedIdent(cur.Schema, cur.Name), Reverse: strings.Join(rangeSQL(cur), ";\n")})
		case hasCurrent && hasDesired:
			if !sameRangeDefinition(cur, des) {
				statements = append(statements, baseatlas.Statement{Comment: "drop range " + rangeID(cur) + " for replacement (destructive)", SQL: "DROP TYPE " + qualifiedIdent(cur.Schema, cur.Name)})
				statements = append(statements, createRangeStatements(des)...)
				continue
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on range " + rangeID(des), SQL: "COMMENT ON TYPE " + qualifiedIdent(des.Schema, des.Name) + " IS " + nullableLiteral(des.Comment), Reverse: "COMMENT ON TYPE " + qualifiedIdent(cur.Schema, cur.Name) + " IS " + nullableLiteral(cur.Comment)})
			}
		}
	}
	return statements
}

func createRangeStatements(range_ Range) []baseatlas.Statement {
	statements := []baseatlas.Statement{{Comment: "create range " + rangeID(range_), SQL: createRangeSQL(range_), Reverse: "DROP TYPE " + qualifiedIdent(range_.Schema, range_.Name)}}
	if range_.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on range " + rangeID(range_), SQL: "COMMENT ON TYPE " + qualifiedIdent(range_.Schema, range_.Name) + " IS " + literal(range_.Comment), Reverse: "COMMENT ON TYPE " + qualifiedIdent(range_.Schema, range_.Name) + " IS NULL"})
	}
	return statements
}

func rangeSQL(range_ Range) []string {
	statements := []string{createRangeSQL(range_)}
	if range_.Comment != "" {
		statements = append(statements, "COMMENT ON TYPE "+qualifiedIdent(range_.Schema, range_.Name)+" IS "+literal(range_.Comment))
	}
	return statements
}

func createRangeSQL(range_ Range) string {
	var opts []string
	opts = append(opts, "SUBTYPE = "+range_.Subtype)
	if range_.SubtypeDiff != "" {
		opts = append(opts, "SUBTYPE_DIFF = "+range_.SubtypeDiff)
	}
	if range_.MultirangeName != "" {
		opts = append(opts, "MULTIRANGE_TYPE_NAME = "+quoteIdent(range_.MultirangeName))
	}
	return "CREATE TYPE " + qualifiedIdent(range_.Schema, range_.Name) + " AS RANGE (" + strings.Join(opts, ", ") + ")"
}

func sameRangeDefinition(a, b Range) bool {
	return normalizeSQL(a.Subtype) == normalizeSQL(b.Subtype) && normalizeSQL(a.SubtypeDiff) == normalizeSQL(b.SubtypeDiff) && a.MultirangeName == b.MultirangeName
}

func rangeID(range_ Range) string { return viewID(range_.Schema, range_.Name) }
