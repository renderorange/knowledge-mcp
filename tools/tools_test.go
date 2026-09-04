package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/agents_knowledge/knowledge"
	"github.com/renderorange/agents_knowledge/search"
)

func TestInitHandler(t *testing.T) {
	dir := t.TempDir()

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project_path": dir,
	}

	result, err := InitHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("InitHandler() error: %v", err)
	}
	if result == nil {
		t.Fatal("InitHandler() returned nil")
	}

	// Verify .agents/ was created
	agentsDir := filepath.Join(dir, ".agents")
	if _, statErr := os.Stat(agentsDir); os.IsNotExist(statErr) {
		t.Error(".agents/ directory not created")
	}

	// Verify category files exist
	for _, cat := range knowledge.ValidCategories() {
		catPath := knowledge.CategoryFilePath(agentsDir, cat)
		if _, statErr := os.Stat(catPath); os.IsNotExist(statErr) {
			t.Errorf("%s.yaml not created", cat)
		}
	}

	// Verify _meta.yaml exists
	metaPath := knowledge.MetaFilePath(agentsDir)
	if _, statErr := os.Stat(metaPath); os.IsNotExist(statErr) {
		t.Error("_meta.yaml not created")
	}

	// Calling again should say "already initialized"
	result2, err := InitHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("second InitHandler() error: %v", err)
	}
	if result2 == nil {
		t.Fatal("second InitHandler() returned nil")
	}
}

func TestWriteHandler(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)

	kf := &knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{}}
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"), kf)

	indexPath := filepath.Join(dir, ".index")
	idx, _ := search.NewIndex(indexPath)
	defer idx.Close()

	projectPathFn := func(project string) string {
		if project == "test" {
			return dir
		}
		return ""
	}

	handler := WriteHandler(projectPathFn, idx)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project":    "test",
		"category":   "conventions",
		"summary":    "Test convention",
		"detail":     "Some detail about the convention",
		"confidence": "high",
		"source":     "test",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("WriteHandler() error: %v", err)
	}
	if result == nil {
		t.Fatal("WriteHandler() returned nil")
	}

	// Verify entry was written
	loaded, _ := knowledge.Load(knowledge.CategoryFilePath(agentsDir, "conventions"))
	if len(loaded.Entries) != 1 {
		t.Fatalf("len(Entries) = %d, want 1", len(loaded.Entries))
	}
	if loaded.Entries[0].ID != "conv-001" {
		t.Errorf("ID = %q, want %q", loaded.Entries[0].ID, "conv-001")
	}
	if loaded.Entries[0].Summary != "Test convention" {
		t.Errorf("Summary = %q, want %q", loaded.Entries[0].Summary, "Test convention")
	}
	if loaded.Entries[0].Date == "" {
		t.Error("Date should not be empty")
	}
}

func TestWriteHandlerValidation(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"),
		&knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{}})

	projectPathFn := func(project string) string { return dir }
	handler := WriteHandler(projectPathFn, nil)

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "invalid category",
			args: map[string]interface{}{
				"project": "test", "category": "invalid", "summary": "s",
				"detail": "d", "confidence": "high", "source": "s",
			},
		},
		{
			name: "summary too long",
			args: map[string]interface{}{
				"project": "test", "category": "conventions",
				"summary": "this is a very long summary that exceeds one hundred characters and should be rejected by the validation logic in the handler",
				"detail": "d", "confidence": "high", "source": "s",
			},
		},
		{
			name: "invalid confidence",
			args: map[string]interface{}{
				"project": "test", "category": "conventions", "summary": "s",
				"detail": "d", "confidence": "maybe", "source": "s",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args
			result, _ := handler(context.Background(), req)
			if result == nil {
				t.Fatal("handler returned nil")
			}
			if !result.IsError {
				t.Fatal("expected error result")
			}
		})
	}
}

func TestWriteHandlerDetailSizeLimit(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"),
		&knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{}})

	projectPathFn := func(project string) string { return dir }
	handler := WriteHandler(projectPathFn, nil)

	// Create a detail string exceeding 1MB
	bigDetail := strings.Repeat("x", maxDetailSize+1)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": "test", "category": "conventions", "summary": "s",
		"detail": bigDetail, "confidence": "high", "source": "s",
	}
	result, _ := handler(context.Background(), req)
	if result == nil {
		t.Fatal("handler returned nil")
	}
	if !result.IsError {
		t.Fatal("expected error for oversized detail")
	}
}

func TestQueryHandler(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)

	kf := &knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{}}
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"), kf)

	indexPath := filepath.Join(dir, ".index")
	idx, _ := search.NewIndex(indexPath)
	defer idx.Close()

	projectPathFn := func(project string) string {
		if project == "test" {
			return dir
		}
		return ""
	}

	handler := QueryHandler(projectPathFn, idx)

	t.Run("unknown project", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project": "nonexistent",
			"query":   "test",
		}
		result, _ := handler(context.Background(), req)
		if result == nil {
			t.Fatal("handler returned nil")
		}
		if !result.IsError {
			t.Fatal("expected error result")
		}
	})

	t.Run("invalid category", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project":  "test",
			"category": "invalid_cat",
		}
		result, _ := handler(context.Background(), req)
		if result == nil {
			t.Fatal("handler returned nil")
		}
		if !result.IsError {
			t.Fatal("expected error for invalid category")
		}
	})

	t.Run("invalid confidence", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project":    "test",
			"confidence": "maybe",
		}
		result, _ := handler(context.Background(), req)
		if result == nil {
			t.Fatal("handler returned nil")
		}
		if !result.IsError {
			t.Fatal("expected error for invalid confidence")
		}
	})

	t.Run("full text query", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project": "test",
			"query":   "build",
		}
		result, _ := handler(context.Background(), req)
		if result == nil {
			t.Fatal("handler returned nil")
		}
	})

	t.Run("category filter", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project":  "test",
			"category": "conventions",
		}
		result, _ := handler(context.Background(), req)
		if result == nil {
			t.Fatal("handler returned nil")
		}
	})

	t.Run("confidence filter", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project":    "test",
			"confidence": "high",
		}
		result, _ := handler(context.Background(), req)
		if result == nil {
			t.Fatal("handler returned nil")
		}
	})
}

func TestListHandler(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)

	kf := &knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{}}
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"), kf)

	projectPathFn := func(project string) string {
		if project == "test" {
			return dir
		}
		return ""
	}

	handler := ListHandler(projectPathFn)

	t.Run("unknown project", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project": "nonexistent",
		}
		result, _ := handler(context.Background(), req)
		if result == nil {
			t.Fatal("handler returned nil")
		}
		if !result.IsError {
			t.Fatal("expected error result")
		}
	})

	t.Run("list all entries", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project": "test",
		}
		result, _ := handler(context.Background(), req)
		if result == nil {
			t.Fatal("handler returned nil")
		}
	})

	t.Run("category filter", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project":  "test",
			"category": "conventions",
		}
		result, _ := handler(context.Background(), req)
		if result == nil {
			t.Fatal("handler returned nil")
		}
	})

	t.Run("invalid category filter", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project":  "test",
			"category": "invalid_cat",
		}
		result, _ := handler(context.Background(), req)
		if result == nil {
			t.Fatal("handler returned nil")
		}
		if !result.IsError {
			t.Fatal("expected error for invalid category")
		}
	})
}

func TestUpdateHandler(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)

	entry := knowledge.Entry{
		ID:         "conv-001",
		Summary:    "Test entry",
		Detail:     "Some detail",
		Confidence: "medium",
		Source:     "test",
		Date:       "2024-01-01",
	}
	kf := &knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{entry}}
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"), kf)

	indexPath := filepath.Join(dir, ".index")
	idx, _ := search.NewIndex(indexPath)
	defer idx.Close()

	projectPathFn := func(project string) string {
		if project == "test" {
			return dir
		}
		return ""
	}

	handler := UpdateHandler(projectPathFn, idx)

	t.Run("update confidence", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project":    "test",
			"category":   "conventions",
			"id":         "conv-001",
			"confidence": "high",
		}
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("handler returned nil")
		}

		// Verify the update persisted and date was refreshed
		loaded, _ := knowledge.Load(knowledge.CategoryFilePath(agentsDir, "conventions"))
		if loaded.Entries[0].Confidence != "high" {
			t.Errorf("confidence = %q, want %q", loaded.Entries[0].Confidence, "high")
		}
		if loaded.Entries[0].Date != knowledge.Today() {
			t.Errorf("date = %q, want %q (should be refreshed on update)", loaded.Entries[0].Date, knowledge.Today())
		}
	})

	t.Run("update summary", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project":  "test",
			"category": "conventions",
			"id":       "conv-001",
			"summary":  "Updated summary",
		}
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("handler returned nil")
		}

		loaded, _ := knowledge.Load(knowledge.CategoryFilePath(agentsDir, "conventions"))
		if loaded.Entries[0].Summary != "Updated summary" {
			t.Errorf("summary = %q, want %q", loaded.Entries[0].Summary, "Updated summary")
		}
	})

	t.Run("update detail", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project":  "test",
			"category": "conventions",
			"id":       "conv-001",
			"detail":   "Updated detail",
		}
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("handler returned nil")
		}

		loaded, _ := knowledge.Load(knowledge.CategoryFilePath(agentsDir, "conventions"))
		if loaded.Entries[0].Detail != "Updated detail" {
			t.Errorf("detail = %q, want %q", loaded.Entries[0].Detail, "Updated detail")
		}
	})

	t.Run("unknown entry", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project":    "test",
			"category":   "conventions",
			"id":         "nonexistent",
			"confidence": "high",
		}
		result, _ := handler(context.Background(), req)
		if result == nil {
			t.Fatal("handler returned nil")
		}
		if !result.IsError {
			t.Fatal("expected error result")
		}
	})

	t.Run("invalid confidence", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project":    "test",
			"category":   "conventions",
			"id":         "conv-001",
			"confidence": "maybe",
		}
		result, _ := handler(context.Background(), req)
		if result == nil {
			t.Fatal("handler returned nil")
		}
		if !result.IsError {
			t.Fatal("expected error result")
		}
	})

	t.Run("unknown project", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project":    "nonexistent",
			"category":   "conventions",
			"id":         "conv-001",
			"confidence": "high",
		}
		result, _ := handler(context.Background(), req)
		if result == nil {
			t.Fatal("handler returned nil")
		}
		if !result.IsError {
			t.Fatal("expected error result")
		}
	})

	t.Run("no-op update rejected", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project":  "test",
			"category": "conventions",
			"id":       "conv-001",
		}
		result, _ := handler(context.Background(), req)
		if result == nil {
			t.Fatal("handler returned nil")
		}
		if !result.IsError {
			t.Fatal("expected error for no-op update")
		}
	})

	t.Run("invalid supersedes", func(t *testing.T) {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project":    "test",
			"category":   "conventions",
			"id":         "conv-001",
			"supersedes": "nonexistent-999",
		}
		result, _ := handler(context.Background(), req)
		if result == nil {
			t.Fatal("handler returned nil")
		}
		if !result.IsError {
			t.Fatal("expected error for invalid supersedes")
		}
	})
}

func TestConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)

	kf := &knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{}}
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"), kf)

	projectPathFn := func(project string) string {
		if project == "test" {
			return dir
		}
		return ""
	}

	handler := WriteHandler(projectPathFn, nil)

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]interface{}{
				"project":    "test",
				"category":   "conventions",
				"summary":    fmt.Sprintf("concurrent entry %d", n),
				"detail":     "detail",
				"confidence": "high",
				"source":     "test",
			}
			result, err := handler(context.Background(), req)
			if err != nil {
				errs <- fmt.Errorf("goroutine %d: %w", n, err)
				return
			}
			if result.IsError {
				errs <- fmt.Errorf("goroutine %d: error result", n)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	// Verify all entries were written (no lost writes)
	loaded, _ := knowledge.Load(knowledge.CategoryFilePath(agentsDir, "conventions"))
	if len(loaded.Entries) != goroutines {
		t.Errorf("len(Entries) = %d, want %d (lost writes due to race)", len(loaded.Entries), goroutines)
	}

	// Verify all IDs are unique
	seen := make(map[string]bool)
	for _, entry := range loaded.Entries {
		if seen[entry.ID] {
			t.Errorf("duplicate ID: %s", entry.ID)
		}
		seen[entry.ID] = true
	}
}

func TestConcurrentWriteAndUpdate(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)

	entry := knowledge.Entry{
		ID:         "conv-001",
		Summary:    "Original",
		Detail:     "Original detail",
		Confidence: "medium",
		Source:     "test",
		Date:       "2024-01-01",
	}
	kf := &knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{entry}}
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"), kf)

	projectPathFn := func(project string) string {
		if project == "test" {
			return dir
		}
		return ""
	}

	writeHandler := WriteHandler(projectPathFn, nil)
	updateHandler := UpdateHandler(projectPathFn, nil)

	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]interface{}{
				"project":    "test",
				"category":   "conventions",
				"summary":    fmt.Sprintf("write %d", n),
				"detail":     "detail",
				"confidence": "high",
				"source":     "test",
			}
			writeHandler(context.Background(), req)
		}(i)
	}

	// Concurrent updates on same entry
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]interface{}{
				"project":    "test",
				"category":   "conventions",
				"id":         "conv-001",
				"confidence": "high",
			}
			updateHandler(context.Background(), req)
		}(i)
	}

	wg.Wait()

	// Verify no corruption — file should be loadable
	loaded, err := knowledge.Load(knowledge.CategoryFilePath(agentsDir, "conventions"))
	if err != nil {
		t.Fatalf("file corrupted after concurrent access: %v", err)
	}
	// Original entry + 10 new writes
	if len(loaded.Entries) != 11 {
		t.Errorf("len(Entries) = %d, want 11", len(loaded.Entries))
	}
}
