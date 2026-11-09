package baseatlas

import "testing"

func TestScopeIncludes(t *testing.T) {
	cases := []struct {
		name   string
		scope  Scope
		typ    string
		schema string
		path   []string
		want   bool
	}{
		{"empty scope", Scope{}, "function", "river", []string{"f"}, true},
		{"schemas keeps listed", Scope{Schemas: []string{"public"}}, "function", "public", []string{"f"}, true},
		{"schemas drops others", Scope{Schemas: []string{"public"}}, "function", "river", []string{"f"}, false},
		{"unqualified always kept", Scope{Schemas: []string{"public"}}, "permission", "", nil, true},
		{"schema glob drops schema", Scope{Exclude: []string{"river"}}, "permission", "river", nil, false},
		{"schema glob drops children", Scope{Exclude: []string{"riv*"}}, "trigger", "river", []string{"job", "t"}, false},
		{"schema.* keeps schema", Scope{Exclude: []string{"river.*"}}, "permission", "river", nil, true},
		{"schema.* drops objects", Scope{Exclude: []string{"river.*"}}, "sequence", "river", []string{"s"}, false},
		{"table drops table children", Scope{Exclude: []string{"public.some_table"}}, "policy", "public", []string{"some_table", "p"}, false},
		{"table keeps other tables", Scope{Exclude: []string{"public.some_table"}}, "policy", "public", []string{"users", "p"}, true},
		{"table child", Scope{Exclude: []string{"public.users.audit"}}, "trigger", "public", []string{"users", "audit"}, false},
		{"type selector matches", Scope{Exclude: []string{"river.*[type=function]"}}, "function", "river", []string{"f"}, false},
		{"type selector skips", Scope{Exclude: []string{"river.*[type=function]"}}, "sequence", "river", []string{"s"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.scope.Includes(tc.typ, tc.schema, tc.path...); got != tc.want {
				t.Fatalf("Includes(%q, %q, %q) = %v, want %v", tc.typ, tc.schema, tc.path, got, tc.want)
			}
		})
	}
}
