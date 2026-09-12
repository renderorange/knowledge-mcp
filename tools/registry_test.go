package tools

import "testing"

func TestRegistryComplete(t *testing.T) {
	want := map[string]bool{
		"init_knowledge": true, "write_knowledge": true, "query_knowledge": true,
		"list_knowledge": true, "update_knowledge": true, "list_projects": true,
	}
	seen := map[string]bool{}
	for _, spec := range Registry {
		if seen[spec.Name] {
			t.Fatalf("duplicate tool %q", spec.Name)
		}
		seen[spec.Name] = true
		if spec.Description == "" || spec.Purpose == "" {
			t.Errorf("tool %q missing description/purpose", spec.Name)
		}
		if !want[spec.Name] {
			t.Errorf("unexpected tool %q", spec.Name)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("missing tool %q", name)
		}
	}
}
