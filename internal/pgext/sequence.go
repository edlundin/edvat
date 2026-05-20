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

type Sequence struct {
	Name      string
	Schema    string
	Type      string
	Start     string
	Increment string
	Min       string
	Max       string
	Cache     string
	Cycle     bool
	CycleSet  bool
	Comment   string
}

type SequenceState map[string]Sequence

func ParseSequenceFiles(paths []string) (SequenceState, error) {
	return parseStateFiles(paths, "sequence", ParseSequencesHCL)
}

func ParseSequencesHCL(src []byte, filename string) (SequenceState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse sequence hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse sequence hcl: unexpected body type %T", file.Body)
	}
	state := SequenceState{}
	for _, block := range body.Blocks {
		if block.Type != "sequence" || len(block.Labels) != 1 {
			continue
		}
		seq := Sequence{Name: block.Labels[0]}
		attrs := block.Body.Attributes
		if attr, ok := attrs["schema"]; ok {
			schemaName, err := schemaExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode sequence.%s.schema: %w", seq.Name, err)
			}
			seq.Schema = schemaName
		}
		for attrName, target := range map[string]*string{"type": &seq.Type, "start": &seq.Start, "increment": &seq.Increment, "min": &seq.Min, "max": &seq.Max, "cache": &seq.Cache} {
			if attr, ok := attrs[attrName]; ok {
				*target = typeExpr(attr.Expr, src)
			}
		}
		if attr, ok := attrs["cycle"]; ok {
			cycle, err := boolExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode sequence.%s.cycle: %w", seq.Name, err)
			}
			seq.Cycle = cycle
			seq.CycleSet = true
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode sequence.%s.comment: %w", seq.Name, err)
			}
			seq.Comment = comment
		}
		state[sequenceID(seq)] = seq
	}
	return state, nil
}

func InspectSequencesURL(ctx context.Context, url string) (SequenceState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectSequences(ctx, db)
}

func InspectSequences(ctx context.Context, db *sql.DB) (SequenceState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT s.schemaname,
       s.sequencename,
       i.data_type,
       i.start_value,
       i.increment,
       i.minimum_value,
       i.maximum_value,
       s.cache_size::text,
       i.cycle_option
FROM pg_sequences s
JOIN information_schema.sequences i ON i.sequence_schema = s.schemaname AND i.sequence_name = s.sequencename
WHERE s.schemaname NOT IN ('pg_catalog', 'information_schema')
ORDER BY s.schemaname, s.sequencename`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres sequences: %w", err)
	}
	defer rows.Close()
	state := SequenceState{}
	for rows.Next() {
		var seq Sequence
		var cycle string
		if err := rows.Scan(&seq.Schema, &seq.Name, &seq.Type, &seq.Start, &seq.Increment, &seq.Min, &seq.Max, &seq.Cache, &cycle); err != nil {
			return nil, fmt.Errorf("scan postgres sequence: %w", err)
		}
		seq.Cycle = strings.EqualFold(cycle, "YES")
		seq.CycleSet = true
		state[sequenceID(seq)] = seq
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres sequences: %w", err)
	}
	return state, nil
}

func DiffSequences(current, desired SequenceState) []baseatlas.Statement {
	if current == nil {
		current = SequenceState{}
	}
	if desired == nil {
		desired = SequenceState{}
	}
	ids := stateIDs(current, desired)
	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createSequenceStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, baseatlas.Statement{Comment: "drop sequence " + sequenceID(cur) + " (destructive)", SQL: "DROP SEQUENCE " + qualifiedIdent(cur.Schema, cur.Name), Reverse: strings.Join(sequenceSQL(cur), ";\n")})
		case hasCurrent && hasDesired:
			if !sameSequenceDefinition(cur, des) {
				statements = append(statements, baseatlas.Statement{Comment: "alter sequence " + sequenceID(des), SQL: alterSequenceSQL(des), Reverse: alterSequenceSQL(cur)})
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on sequence " + sequenceID(des), SQL: "COMMENT ON SEQUENCE " + qualifiedIdent(des.Schema, des.Name) + " IS " + nullableLiteral(des.Comment), Reverse: "COMMENT ON SEQUENCE " + qualifiedIdent(cur.Schema, cur.Name) + " IS " + nullableLiteral(cur.Comment)})
			}
		}
	}
	return statements
}

func createSequenceStatements(seq Sequence) []baseatlas.Statement {
	statements := []baseatlas.Statement{{Comment: "create sequence " + sequenceID(seq), SQL: createSequenceSQL(seq), Reverse: "DROP SEQUENCE " + qualifiedIdent(seq.Schema, seq.Name)}}
	if seq.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on sequence " + sequenceID(seq), SQL: "COMMENT ON SEQUENCE " + qualifiedIdent(seq.Schema, seq.Name) + " IS " + literal(seq.Comment), Reverse: "COMMENT ON SEQUENCE " + qualifiedIdent(seq.Schema, seq.Name) + " IS NULL"})
	}
	return statements
}

func sequenceSQL(seq Sequence) []string {
	statements := []string{createSequenceSQL(seq)}
	if seq.Comment != "" {
		statements = append(statements, "COMMENT ON SEQUENCE "+qualifiedIdent(seq.Schema, seq.Name)+" IS "+literal(seq.Comment))
	}
	return statements
}

func createSequenceSQL(seq Sequence) string {
	return "CREATE SEQUENCE " + qualifiedIdent(seq.Schema, seq.Name) + sequenceOptionsSQL(seq)
}
func alterSequenceSQL(seq Sequence) string {
	return "ALTER SEQUENCE " + qualifiedIdent(seq.Schema, seq.Name) + sequenceOptionsSQL(seq)
}

func sequenceOptionsSQL(seq Sequence) string {
	var opts []string
	if seq.Type != "" {
		opts = append(opts, "AS "+seq.Type)
	}
	if seq.Increment != "" {
		opts = append(opts, "INCREMENT BY "+seq.Increment)
	}
	if seq.Min != "" {
		opts = append(opts, "MINVALUE "+seq.Min)
	}
	if seq.Max != "" {
		opts = append(opts, "MAXVALUE "+seq.Max)
	}
	if seq.Start != "" {
		opts = append(opts, "START WITH "+seq.Start)
	}
	if seq.Cache != "" {
		opts = append(opts, "CACHE "+seq.Cache)
	}
	if seq.Cycle {
		opts = append(opts, "CYCLE")
	} else if seq.CycleSet {
		opts = append(opts, "NO CYCLE")
	}
	if len(opts) == 0 {
		return ""
	}
	return " " + strings.Join(opts, " ")
}

func sameSequenceDefinition(a, b Sequence) bool {
	return sameOptionalSequenceValue(normalizeSQL(a.Type), normalizeSQL(b.Type)) &&
		sameOptionalSequenceValue(a.Start, b.Start) &&
		sameOptionalSequenceValue(a.Increment, b.Increment) &&
		sameOptionalSequenceValue(a.Min, b.Min) &&
		sameOptionalSequenceValue(a.Max, b.Max) &&
		sameOptionalSequenceValue(a.Cache, b.Cache) &&
		(!b.CycleSet && !b.Cycle || a.Cycle == b.Cycle)
}

func sameOptionalSequenceValue(current, desired string) bool {
	return desired == "" || current == desired
}

func sequenceID(seq Sequence) string { return viewID(seq.Schema, seq.Name) }
