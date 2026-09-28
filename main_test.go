package main

import (
	"testing"

	"github.com/renderorange/knowledge-mcp/projects"
	"github.com/renderorange/knowledge-mcp/tools"
)

func TestPathListSet(t *testing.T) {
	var p pathList
	if err := p.Set("/a"); err != nil {
		t.Fatalf("Set(/a) error: %v", err)
	}
	if err := p.Set("/b"); err != nil {
		t.Fatalf("Set(/b) error: %v", err)
	}
	if len(p) != 2 || p[0] != "/a" || p[1] != "/b" {
		t.Errorf("pathList = %v, want [/a /b]", []string(p))
	}
	if err := p.Set(""); err == nil {
		t.Error("Set(\"\") should error")
	}
	if p.String() != "/a,/b" {
		t.Errorf("String() = %q, want %q", p.String(), "/a,/b")
	}
}

func TestBuildHandlersMatchesRegistry(t *testing.T) {
	resolver, _, err := projects.Build(nil, []string{t.TempDir()}, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}
	for _, orgMode := range []bool{false, true} {
		h := buildHandlers(resolver, nil, orgMode)
		for _, spec := range tools.Registry {
			_, ok := h[spec.Name]
			if spec.Name == tools.ToolListProjects && !orgMode {
				if ok {
					t.Errorf("orgMode=false: list_projects must not be registered")
				}
				continue
			}
			if !ok {
				t.Errorf("orgMode=%v: missing handler for %q", orgMode, spec.Name)
			}
		}
	}
}
