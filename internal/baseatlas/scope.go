package baseatlas

import (
	"encoding/csv"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"ariga.io/atlas/sql/schema"
)

// Scope limits the objects that take part in a diff, following the `schemas`
// and `exclude` attributes of an Atlas env block. The zero value keeps everything.
type Scope struct {
	// Schemas lists the only schemas to inspect and diff. Empty means all.
	Schemas []string
	// Exclude holds Atlas glob patterns such as "river", "river.*" or
	// "public.users", optionally with a "[type=...]" selector on a part.
	Exclude []string
}

func (s Scope) inspectOption() *schema.InspectRealmOption {
	if len(s.Schemas) == 0 && len(s.Exclude) == 0 {
		return nil
	}
	return &schema.InspectRealmOption{Schemas: s.Schemas, Exclude: s.Exclude}
}

// Realm removes out-of-scope schemas and objects from realm in place.
func (s Scope) Realm(realm *schema.Realm) (*schema.Realm, error) {
	if realm == nil {
		return nil, nil
	}
	if len(s.Schemas) > 0 {
		kept := realm.Schemas[:0]
		for _, sc := range realm.Schemas {
			if slices.Contains(s.Schemas, sc.Name) {
				kept = append(kept, sc)
			}
		}
		realm.Schemas = kept
	}
	realm, err := schema.ExcludeRealm(realm, s.Exclude)
	if err != nil {
		return nil, fmt.Errorf("apply env exclude: %w", err)
	}
	return realm, nil
}

// Includes reports whether an object Edvat manages outside the Atlas realm is in
// scope. typ is the object's HCL block name, used by "[type=...]" selectors.
// path is the object's address below its schema: the object name, or the table
// name followed by the object name for table-owned objects such as triggers.
// An empty path addresses the schema itself. Objects without a schema, such as
// roles or casts, are always in scope.
//
// The matching mirrors schema.ExcludeRealm: a pattern part matches the schema,
// then the table or schema-level object, then the table child.
func (s Scope) Includes(typ, schemaName string, path ...string) bool {
	if schemaName == "" {
		return true
	}
	if len(s.Schemas) > 0 && !slices.Contains(s.Schemas, schemaName) {
		return false
	}
	for _, pattern := range s.Exclude {
		if excludesObject(pattern, typ, schemaName, path) {
			return false
		}
	}
	return true
}

func excludesObject(pattern, typ, schemaName string, path []string) bool {
	r := csv.NewReader(strings.NewReader(pattern))
	r.Comma = '.'
	records, err := r.ReadAll()
	// Malformed patterns are reported by Realm, which always runs first.
	if err != nil || len(records) != 1 || len(records[0]) == 0 {
		return false
	}
	parts := records[0]
	if !globMatches(parts[0], "schema", schemaName) {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	if len(path) == 0 {
		return false
	}
	if len(path) == 1 {
		return len(parts) == 2 && globMatches(parts[1], typ, path[0])
	}
	// Table-owned object: the second part selects the table, the third its child.
	if !globMatches(parts[1], "table", path[0]) {
		return false
	}
	return len(parts) == 2 || globMatches(parts[2], typ, path[1])
}

var typeSelector = regexp.MustCompile(`\[type=([a-z|_]+)+\]$`)

func globMatches(glob, typ, name string) bool {
	if m := typeSelector.FindStringSubmatch(glob); len(m) == 2 {
		if !slices.Contains(strings.Split(m[1], "|"), typ) {
			return false
		}
		glob = strings.TrimSuffix(glob, m[0])
	}
	match, err := filepath.Match(glob, name)
	return err == nil && match
}
