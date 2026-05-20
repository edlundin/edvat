package migrationplan

import (
	"testing"

	"ariga.io/atlas/sql/schema"
)

func TestInferTableColumnRenames(t *testing.T) {
	old := schema.NewColumn("old_name").SetType(&schema.StringType{T: "text"})
	new := schema.NewColumn("new_name").SetType(&schema.StringType{T: "text"})
	changes := inferTableColumnRenames([]schema.Change{
		&schema.DropColumn{C: old},
		&schema.AddColumn{C: new},
	})
	if len(changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(changes))
	}
	rename, ok := changes[0].(*schema.RenameColumn)
	if !ok {
		t.Fatalf("change = %T, want *schema.RenameColumn", changes[0])
	}
	if rename.From.Name != "old_name" || rename.To.Name != "new_name" {
		t.Fatalf("rename = %s -> %s", rename.From.Name, rename.To.Name)
	}
}
