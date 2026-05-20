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

type Partition struct {
	Name    string
	Schema  string
	Of      string
	For     string
	Comment string
}

type PartitionState map[string]Partition

func ParsePartitionFiles(paths []string) (PartitionState, error) {
	return parseStateFiles(paths, "partition", ParsePartitionsHCL)
}

func ParsePartitionsHCL(src []byte, filename string) (PartitionState, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse partition hcl: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse partition hcl: unexpected body type %T", file.Body)
	}
	state := PartitionState{}
	for _, block := range body.Blocks {
		if block.Type != "partition" || len(block.Labels) != 1 {
			continue
		}
		partition := Partition{Name: block.Labels[0]}
		attrs := block.Body.Attributes
		if attr, ok := attrs["schema"]; ok {
			schemaName, err := schemaExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode partition.%s.schema: %w", partition.Name, err)
			}
			partition.Schema = schemaName
		}
		if attr, ok := attrs["of"]; ok {
			of, err := relationExpr(attr.Expr, src)
			if err != nil {
				return nil, fmt.Errorf("decode partition.%s.of: %w", partition.Name, err)
			}
			partition.Of = of
		}
		if attr, ok := attrs["for"]; ok {
			forValue, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode partition.%s.for: %w", partition.Name, err)
			}
			partition.For = strings.TrimSpace(forValue)
		}
		if partition.For == "" {
			for _, child := range block.Body.Blocks {
				forValue, err := partitionForBlock(partition.Name, child)
				if err != nil {
					return nil, err
				}
				if forValue != "" {
					partition.For = forValue
					break
				}
			}
		}
		if partition.Schema != "" && strings.Count(partition.Of, `"`) == 2 && !strings.Contains(partition.Of, ".") {
			partition.Of = qualifiedIdent(partition.Schema, strings.Trim(partition.Of, `"`))
		}
		if attr, ok := attrs["comment"]; ok {
			comment, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode partition.%s.comment: %w", partition.Name, err)
			}
			partition.Comment = comment
		}
		if partition.Of == "" {
			return nil, fmt.Errorf("partition.%s requires of", partition.Name)
		}
		if partition.For == "" {
			return nil, fmt.Errorf("partition.%s requires for", partition.Name)
		}
		state[partitionID(partition)] = partition
	}
	return state, nil
}

func InspectPartitionsURL(ctx context.Context, url string) (PartitionState, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	return InspectPartitions(ctx, db)
}

func InspectPartitions(ctx context.Context, db *sql.DB) (PartitionState, error) {
	rows, err := db.QueryContext(ctx, `
SELECT cn.nspname,
       c.relname,
       pn.nspname || '.' || p.relname,
       COALESCE(pg_get_expr(c.relpartbound, c.oid), ''),
       COALESCE(d.description, '')
FROM pg_class c
JOIN pg_namespace cn ON cn.oid = c.relnamespace
JOIN pg_inherits i ON i.inhrelid = c.oid
JOIN pg_class p ON p.oid = i.inhparent
JOIN pg_namespace pn ON pn.oid = p.relnamespace
LEFT JOIN pg_description d ON d.objoid = c.oid AND d.classoid = 'pg_class'::regclass AND d.objsubid = 0
WHERE c.relispartition
  AND c.relkind IN ('r', 'p')
  AND cn.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY cn.nspname, c.relname`)
	if err != nil {
		return nil, fmt.Errorf("inspect postgres partitions: %w", err)
	}
	defer rows.Close()
	state := PartitionState{}
	for rows.Next() {
		var partition Partition
		if err := rows.Scan(&partition.Schema, &partition.Name, &partition.Of, &partition.For, &partition.Comment); err != nil {
			return nil, fmt.Errorf("scan postgres partition: %w", err)
		}
		partition.Of = normalizeInspectedPartitionRelation(partition.Of)
		partition.For = strings.TrimSpace(strings.TrimPrefix(partition.For, "FOR VALUES"))
		state[partitionID(partition)] = partition
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect postgres partitions: %w", err)
	}
	return state, nil
}

func DiffPartitions(current, desired PartitionState) []baseatlas.Statement {
	if current == nil {
		current = PartitionState{}
	}
	if desired == nil {
		desired = PartitionState{}
	}
	ids := stateIDs(current, desired)
	var statements []baseatlas.Statement
	for _, id := range ids {
		cur, hasCurrent := current[id]
		des, hasDesired := desired[id]
		switch {
		case !hasCurrent && hasDesired:
			statements = append(statements, createPartitionStatements(des)...)
		case hasCurrent && !hasDesired:
			statements = append(statements, dropPartitionStatement(cur, "drop partition "+partitionID(cur)+" (destructive)"))
		case hasCurrent && hasDesired:
			if cur.Of != des.Of || !strings.EqualFold(normalizeSQL(cur.For), normalizeSQL(des.For)) {
				statements = append(statements, dropPartitionStatement(cur, "drop partition "+partitionID(cur)+" for replacement (destructive)"))
				statements = append(statements, createPartitionStatements(des)...)
				continue
			}
			if cur.Comment != des.Comment {
				statements = append(statements, baseatlas.Statement{Comment: "set comment on partition " + partitionID(des), SQL: "COMMENT ON TABLE " + qualifiedIdent(des.Schema, des.Name) + " IS " + nullableLiteral(des.Comment), Reverse: "COMMENT ON TABLE " + qualifiedIdent(cur.Schema, cur.Name) + " IS " + nullableLiteral(cur.Comment)})
			}
		}
	}
	return statements
}

func createPartitionStatements(partition Partition) []baseatlas.Statement {
	statements := []baseatlas.Statement{{Comment: "create partition " + partitionID(partition), SQL: createPartitionSQL(partition), Reverse: "DROP TABLE " + qualifiedIdent(partition.Schema, partition.Name)}}
	if partition.Comment != "" {
		statements = append(statements, baseatlas.Statement{Comment: "set comment on partition " + partitionID(partition), SQL: "COMMENT ON TABLE " + qualifiedIdent(partition.Schema, partition.Name) + " IS " + literal(partition.Comment), Reverse: "COMMENT ON TABLE " + qualifiedIdent(partition.Schema, partition.Name) + " IS NULL"})
	}
	return statements
}

func dropPartitionStatement(partition Partition, comment string) baseatlas.Statement {
	reverse := createPartitionSQL(partition)
	if partition.Comment != "" {
		reverse += ";\nCOMMENT ON TABLE " + qualifiedIdent(partition.Schema, partition.Name) + " IS " + literal(partition.Comment)
	}
	return baseatlas.Statement{Comment: comment, SQL: "DROP TABLE " + qualifiedIdent(partition.Schema, partition.Name), Reverse: reverse}
}

func createPartitionSQL(partition Partition) string {
	return "CREATE TABLE " + qualifiedIdent(partition.Schema, partition.Name) + " PARTITION OF " + partition.Of + " FOR VALUES " + partition.For
}

func partitionID(partition Partition) string { return viewID(partition.Schema, partition.Name) }

func normalizeInspectedPartitionRelation(value string) string {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return value
	}
	return qualifiedIdent(parts[0], parts[1])
}

func partitionForBlock(partitionName string, block *hclsyntax.Block) (string, error) {
	switch block.Type {
	case "default":
		return "DEFAULT", nil
	case "range":
		from, err := partitionStringListAttr(partitionName, block, "from")
		if err != nil {
			return "", err
		}
		to, err := partitionStringListAttr(partitionName, block, "to")
		if err != nil {
			return "", err
		}
		return "FROM (" + strings.Join(from, ", ") + ") TO (" + strings.Join(to, ", ") + ")", nil
	case "list":
		in, err := partitionStringListAttr(partitionName, block, "in")
		if err != nil {
			return "", err
		}
		return "IN (" + strings.Join(in, ", ") + ")", nil
	case "hash":
		modulus, err := partitionIntAttr(partitionName, block, "modulus")
		if err != nil {
			return "", err
		}
		remainder, err := partitionIntAttr(partitionName, block, "remainder")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("WITH (MODULUS %d, REMAINDER %d)", modulus, remainder), nil
	default:
		return "", nil
	}
}

func partitionStringListAttr(partitionName string, block *hclsyntax.Block, name string) ([]string, error) {
	attr, ok := block.Body.Attributes[name]
	if !ok {
		return nil, fmt.Errorf("partition.%s.%s requires %s", partitionName, block.Type, name)
	}
	values, err := stringListExpr(attr.Expr)
	if err != nil {
		return nil, fmt.Errorf("decode partition.%s.%s.%s: %w", partitionName, block.Type, name, err)
	}
	return values, nil
}

func partitionIntAttr(partitionName string, block *hclsyntax.Block, name string) (int64, error) {
	attr, ok := block.Body.Attributes[name]
	if !ok {
		return 0, fmt.Errorf("partition.%s.%s requires %s", partitionName, block.Type, name)
	}
	value, diags := attr.Expr.Value(nil)
	if diags.HasErrors() {
		return 0, fmt.Errorf("decode partition.%s.%s.%s: %s", partitionName, block.Type, name, diags.Error())
	}
	if value.Type().FriendlyName() != "number" {
		return 0, fmt.Errorf("decode partition.%s.%s.%s: expected number, got %s", partitionName, block.Type, name, value.Type().FriendlyName())
	}
	integer, _ := value.AsBigFloat().Int64()
	return integer, nil
}

func relationExpr(expr hclsyntax.Expression, src []byte) (string, error) {
	if traversal, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		parts := traversalParts(traversal.Traversal)
		if len(parts) == 2 && parts[0] == "table" {
			return quoteIdent(parts[1]), nil
		}
		if len(parts) == 3 && parts[0] == "schema" {
			return qualifiedIdent(parts[1], parts[2]), nil
		}
	}
	return symbolOrString(expr, src)
}
