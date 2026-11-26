package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
	"github.com/renderorange/knowledge-mcp/search"
)

// A query that never reached the index has not learned that anything is absent.
// The resolver already produces "unknown project: %q (known: ...)"
// (projects/resolver.go); these tests pin that the handlers do not throw it
// away and answer with a clean-looking empty result instead.
func TestQueryUnresolvableProjectIsNotAnAbsence(t *testing.T) {
	root := t.TempDir()
	if err := knowledge.EnsureDir(filepath.Join(root, "test", ".agents")); err != nil {
		t.Fatalf("EnsureDir() error: %v", err)
	}
	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	idx := newIndexReady(t)
	defer idx.Close()

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": "no-such-project",
		"query":   "anything",
	}
	result, err := QueryHandler(resolver, idx)(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	text := extractTextContent(t, result)
	if strings.Contains(text, "no matching knowledge entries found") {
		t.Errorf("unresolvable project reported as a clean absence:\n%s", text)
	}
	if !strings.Contains(text, "no-such-project") {
		t.Errorf("response does not name the project that failed to resolve:\n%s", text)
	}
	if !result.IsError {
		t.Errorf("a query that could not run must be an error result:\n%s", text)
	}
}

func TestListUnresolvableProjectIsNotAnAbsence(t *testing.T) {
	root := t.TempDir()
	if err := knowledge.EnsureDir(filepath.Join(root, "test", ".agents")); err != nil {
		t.Fatalf("EnsureDir() error: %v", err)
	}
	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	idx := newIndexReady(t)
	defer idx.Close()

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": "no-such-project",
	}
	result, err := ListHandler(resolver, idx)(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	text := extractTextContent(t, result)
	if strings.Contains(text, "no knowledge entries found") {
		t.Errorf("unresolvable project reported as a clean empty store:\n%s", text)
	}
	if !strings.Contains(text, "no-such-project") {
		t.Errorf("response does not name the project that failed to resolve:\n%s", text)
	}
	if !result.IsError {
		t.Errorf("a listing that could not run must be an error result:\n%s", text)
	}
}

// The counterpart, and the reason this is not simply "make empty results an
// error": a project that resolves and genuinely holds nothing must still read
// as a plain answer. Could not check is not the same as checked, nothing there.
func TestResolvableEmptyProjectStaysAPlainAnswer(t *testing.T) {
	root := t.TempDir()
	if err := knowledge.EnsureDir(filepath.Join(root, "test", ".agents")); err != nil {
		t.Fatalf("EnsureDir() error: %v", err)
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
	defer idx.Close()

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": "test",
		"query":   "anything",
	}
	result, err := QueryHandler(resolver, idx)(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	text := extractTextContent(t, result)
	if result.IsError {
		t.Errorf("an empty but resolvable project must not be an error:\n%s", text)
	}
	if !strings.Contains(text, "no matching knowledge entries found") {
		t.Errorf("expected the plain empty-result text, got:\n%s", text)
	}
}
