package migrationplan

import (
	"fmt"

	"ariga.io/atlas/sql/schema"
)

func inferColumnRenames(changes []schema.Change) []schema.Change {
	out := append([]schema.Change(nil), changes...)
	for _, change := range out {
		modify, ok := change.(*schema.ModifyTable)
		if !ok {
			continue
		}
		modify.Changes = inferTableColumnRenames(modify.Changes)
	}
	return out
}

func inferTableColumnRenames(changes []schema.Change) []schema.Change {
	used := make(map[int]bool)
	var renames []schema.Change
	for i, change := range changes {
		drop, ok := change.(*schema.DropColumn)
		if !ok || used[i] {
			continue
		}
		for j, candidate := range changes {
			add, ok := candidate.(*schema.AddColumn)
			if !ok || used[j] || !sameColumnShape(drop.C, add.C) {
				continue
			}
			used[i], used[j] = true, true
			renames = append(renames, &schema.RenameColumn{From: drop.C, To: add.C})
			break
		}
	}
	if len(renames) == 0 {
		return changes
	}
	out := make([]schema.Change, 0, len(changes)-len(renames))
	for i, change := range changes {
		if !used[i] {
			out = append(out, change)
		}
	}
	return append(out, renames...)
}

func sameColumnShape(a, b *schema.Column) bool {
	if a == nil || b == nil || a.Type == nil || b.Type == nil || a.Type.Null != b.Type.Null {
		return false
	}
	if a.Type.Raw != "" || b.Type.Raw != "" {
		return a.Type.Raw == b.Type.Raw
	}
	return fmt.Sprintf("%#v", a.Type.Type) == fmt.Sprintf("%#v", b.Type.Type)
}
