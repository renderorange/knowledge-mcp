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

// corruptStore builds a project whose conventions file cannot be parsed, next
// to a decisions file that can. The tab indent is the same shape as the real
// incident (an unresolved git conflict marker), and is a guaranteed YAML error.
func corruptStore(t *testing.T) (*projects.Resolver, *search.Index) {
	t.Helper()

	root := t.TempDir()
	agentsDir := filepath.Join(root, "test", ".agents")
	if err := knowledge.EnsureDir(agentsDir); err != nil {
		t.Fatalf("EnsureDir() error: %v", err)
	}

	corrupt := "project: test\nversion: 1\nentries:\n\t- id: conv-001\n      summary: broken\n"
	if err := os.WriteFile(knowledge.CategoryFilePath(agentsDir, "conventions"), []byte(corrupt), 0644); err != nil {
		t.Fatalf("write corrupt conventions: %v", err)
	}

	valid := "project: test\nversion: 1\nentries:\n  - id: dec-001\n    summary: Commit gate\n    detail: A mechanical checkpoint before every commit.\n    rule: Never commit without the checkpoint\n    category: decisions\n"
	if err := os.WriteFile(knowledge.CategoryFilePath(agentsDir, "decisions"), []byte(valid), 0644); err != nil {
		t.Fatalf("write valid decisions: %v", err)
	}

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	idx, err := search.NewIndex()
	if err != nil {
		t.Fatalf("search.NewIndex() error: %v", err)
	}
	go idx.IndexAll(resolver)
	idx.WaitReady()

	return resolver, idx
}

// The failure this guards: a store file that cannot be parsed must never make
// an absent rule look like an absent fact. The response is the only channel the
// agent reads; a stderr log does not reach it. Silence is not enough either: an
// assertion of absence from a partly loaded store is a failed query, so it is
// reported as one.
func TestQuerySurfacesCorruptStoreFile(t *testing.T) {
	resolver, idx := corruptStore(t)
	defer idx.Close()

	handler := QueryHandler(resolver, idx)

	t.Run("no results is an error, not a clean absence", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project": "test",
			"query":   "zzzznosuchterm",
		}
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		text := extractTextContent(t, result)
		if !strings.Contains(text, "conventions.yaml") {
			t.Errorf("zero-result text does not name the corrupt file:\n%s", text)
		}
		if !result.IsError {
			t.Errorf("absence from a partly loaded store must be an error result, got a success:\n%s", text)
		}
	})

	t.Run("partial results lead with the warning", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project": "test",
			"query":   "commit gate",
		}
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		text := extractTextContent(t, result)
		if result.IsError {
			t.Fatalf("found results must not be an error result:\n%s", text)
		}
		if !strings.Contains(text, "dec-001") {
			t.Fatalf("expected the valid entry to be returned:\n%s", text)
		}
		if !strings.HasPrefix(text, "warning:") {
			t.Errorf("warning must be the first line, not a trailing paragraph:\n%s", text)
		}
	})
}

func TestListSurfacesCorruptStoreFile(t *testing.T) {
	resolver, idx := corruptStore(t)
	defer idx.Close()

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": "test",
	}
	result, err := ListHandler(resolver, idx)(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := extractTextContent(t, result)
	if !strings.Contains(text, "conventions.yaml") {
		t.Errorf("list text does not name the corrupt file:\n%s", text)
	}
	if !strings.HasPrefix(text, "warning:") {
		t.Errorf("list warning must be the first line:\n%s", text)
	}
}

// reindex_knowledge is the health check a human or agent runs when the store is
// suspected. Reporting only a document count makes a broken file invisible
// there, which is the one place it must not be.
func TestReindexReportsLoadFailures(t *testing.T) {
	_, idx := corruptStore(t)
	defer idx.Close()

	result, err := ReindexHandler(idx)(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := extractTextContent(t, result)
	if !strings.Contains(text, "conventions.yaml") {
		t.Errorf("reindex output does not name the corrupt file:\n%s", text)
	}
	if !result.IsError {
		t.Errorf("a reindex over an incomplete store must report failure:\n%s", text)
	}
}

// A corrupt file must never be overwritten by the next write: the parse error
// is the only thing standing between the user and silent data loss.
func TestWriteRefusesToCorruptFile(t *testing.T) {
	resolver, idx := corruptStore(t)
	defer idx.Close()

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project":  "test",
		"category": "conventions",
		"summary":  "Should never be written",
		"detail":   "The corrupt file must not be clobbered.",
		"source":   "test",
	}
	result, err := WriteHandler(resolver, idx)(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Errorf("write over a corrupt file must fail, got success:\n%s", extractTextContent(t, result))
	}

	ref := mustResolve(t, resolver, "test")
	path := knowledge.CategoryFilePath(resolver.AgentsDir(ref), "conventions")
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read back corrupt file: %v", readErr)
	}
	if !strings.Contains(string(data), "broken") {
		t.Errorf("corrupt file was modified:\n%s", data)
	}
}

// A healthy store must stay silent: no warning when every file parses.
func TestNoWarningWhenStoreIsHealthy(t *testing.T) {
	resolver, idx := corruptStore(t)
	defer idx.Close()

	agentsDir := filepath.Join(resolver.AgentsDir(mustResolve(t, resolver, "test")))
	if err := os.Remove(knowledge.CategoryFilePath(agentsDir, "conventions")); err != nil {
		t.Fatalf("remove corrupt file: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{"project": "test", "query": "commit gate"}
	result, err := QueryHandler(resolver, idx)(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := extractTextContent(t, result)
	if strings.Contains(text, "failed to load") || strings.Contains(text, "may be incomplete") {
		t.Errorf("healthy store produced a warning:\n%s", text)
	}
}

func mustResolve(t *testing.T, res *projects.Resolver, name string) projects.Ref {
	t.Helper()
	ref, err := res.Resolve(name)
	if err != nil {
		t.Fatalf("Resolve(%q) error: %v", name, err)
	}
	return ref
}
