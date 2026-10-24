package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
)

func TestReindexHandlerRebuildsIndex(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "reindexproj")
	agents := filepath.Join(proj, ".agents")
	if err := knowledge.EnsureDir(agents); err != nil {
		t.Fatalf("EnsureDir() error: %v", err)
	}
	kf := &knowledge.KnowledgeFile{Project: "reindexproj", Version: 1, Entries: []knowledge.Entry{
		{ID: "sub-001", Summary: "Storage backend", Detail: "RustFS keeps objects", Source: "test", Date: "2026-10-23"},
		{ID: "sub-002", Summary: "Trash sweep", Detail: "SweepTrash purges rows", Source: "test", Date: "2026-10-23"},
	}}
	if err := knowledge.Save(knowledge.CategoryFilePath(agents, "subsystems"), kf); err != nil {
		t.Fatalf("knowledge.Save() error: %v", err)
	}

	resolver, _, err := projects.BuildWithStore(nil, []string{proj}, "", "")
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	idx := newIndexReady(t)
	defer idx.Close()
	idx.IndexAll(resolver)

	handler := ReindexHandler(idx)
	result, err := handler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("ReindexHandler() error: %v", err)
	}
	if result.IsError {
		t.Fatalf("ReindexHandler() returned error result: %s", extractTextContent(t, result))
	}

	content := extractTextContent(t, result)
	if !strings.Contains(content, "2 documents") {
		t.Errorf("content = %q, want it to report 2 documents", content)
	}
}

func TestReindexHandlerWithoutResolver(t *testing.T) {
	idx := newIndexReady(t)
	defer idx.Close()

	result, err := ReindexHandler(idx)(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("ReindexHandler() error: %v", err)
	}
	if !result.IsError {
		t.Error("ReindexHandler() without a resolver should return an error result")
	}
}

func TestReindexHandlerNilIndex(t *testing.T) {
	result, err := ReindexHandler(nil)(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("ReindexHandler() error: %v", err)
	}
	if !result.IsError {
		t.Error("ReindexHandler(nil) should return an error result")
	}
}

// TestWriteThenQueryOnLiveIndex covers the server flow: the index was built
// from a resolver, write_knowledge saves the file and Adds the doc, and the
// next query must see the entry even though the store fingerprint changed.
func TestWriteThenQueryOnLiveIndex(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "liveproj")
	if err := knowledge.EnsureDir(filepath.Join(proj, ".agents")); err != nil {
		t.Fatalf("EnsureDir() error: %v", err)
	}

	resolver, _, err := projects.BuildWithStore(nil, []string{proj}, "", "")
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	idx := newIndexReady(t)
	defer idx.Close()
	idx.IndexAll(resolver)

	writeReq := mcp.CallToolRequest{}
	writeReq.Params.Arguments = map[string]interface{}{
		"project":  "liveproj",
		"category": "subsystems",
		"summary":  "Trash sweep",
		"detail":   "SweepTrash purges stale rows",
		"source":   "test",
	}
	writeResult, err := WriteHandler(resolver, idx)(context.Background(), writeReq)
	if err != nil {
		t.Fatalf("WriteHandler() error: %v", err)
	}
	if writeResult.IsError {
		t.Fatalf("WriteHandler() error result: %s", extractTextContent(t, writeResult))
	}

	queryReq := mcp.CallToolRequest{}
	queryReq.Params.Arguments = map[string]interface{}{
		"project": "liveproj",
		"query":   "SweepTrash",
	}
	queryResult, err := QueryHandler(resolver, idx)(context.Background(), queryReq)
	if err != nil {
		t.Fatalf("QueryHandler() error: %v", err)
	}
	content := extractTextContent(t, queryResult)
	if !strings.Contains(content, "sub-001") {
		t.Errorf("query after write did not find the entry: %s", content)
	}
}
