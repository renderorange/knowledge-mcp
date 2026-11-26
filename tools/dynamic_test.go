package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
	"github.com/renderorange/knowledge-mcp/search"
)

// A project that resolves dynamically resolves to a path the index was never
// built for. The resolver's fallback returns Address: name for any directory
// under a root, with no entry in the startup snapshot, so the index holds no
// load failures under that address and a corrupt file inside it is reported as
// a clean absence.
//
// Reproduced live 2026-11-26: list_knowledge correctly warned about the corrupt
// file while query_knowledge answered "no matching knowledge entries found".
func TestQueryDynamicProjectReportsCorruptFile(t *testing.T) {
	root := t.TempDir()

	// The project exists on disk but is created before projects.Build only if
	// it is a git repo. A bare directory is skipped by the startup scan and
	// reached solely through the dynamic fallback.
	proj := filepath.Join(root, "late-project")
	agentsDir := filepath.Join(proj, ".agents")
	if err := knowledge.EnsureDir(agentsDir); err != nil {
		t.Fatalf("EnsureDir() error: %v", err)
	}
	corrupt := "project: late-project\nversion: 1\nentries:\n\t- id: conv-001\n      summary: broken\n"
	if err := os.WriteFile(knowledge.CategoryFilePath(agentsDir, "conventions"), []byte(corrupt), 0644); err != nil {
		t.Fatalf("write corrupt conventions: %v", err)
	}

	// Build the resolver while the directory is hidden, then expose it, so the
	// name cannot be in the startup snapshot and must resolve dynamically.
	hidden := filepath.Join(root, ".hidden-late-project")
	if err := os.Rename(proj, hidden); err != nil {
		t.Fatalf("hide project: %v", err)
	}
	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}
	if err := os.Rename(hidden, proj); err != nil {
		t.Fatalf("unhide project: %v", err)
	}

	idx, err := search.NewIndex()
	if err != nil {
		t.Fatalf("search.NewIndex() error: %v", err)
	}
	go idx.IndexAll(resolver)
	idx.WaitReady()
	defer idx.Close()

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": "late-project",
		"query":   "zzzznosuchterm",
	}
	result, err := QueryHandler(resolver, idx)(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	text := extractTextContent(t, result)
	if !strings.Contains(text, "conventions.yaml") {
		t.Errorf("corrupt file in a dynamically resolved project is invisible:\n%s", text)
	}
	if !result.IsError {
		t.Errorf("absence from an unreadable store must be an error result, got a success:\n%s", text)
	}
}
