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
	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
	"github.com/renderorange/knowledge-mcp/search"
)

func TestInitHandler(t *testing.T) {
	dir := t.TempDir()

	resolver, _, err := projects.Build(nil, []string{dir}, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project_path": dir,
	}

	result, err := InitHandler(resolver)(context.Background(), req)
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
	result2, err := InitHandler(resolver)(context.Background(), req)
	if err != nil {
		t.Fatalf("second InitHandler() error: %v", err)
	}
	if result2 == nil {
		t.Fatal("second InitHandler() returned nil")
	}
}

func TestWriteHandler(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "test")
	knowledge.EnsureDir(filepath.Join(dir, ".agents"))

	kf := &knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{}}
	knowledge.Save(knowledge.CategoryFilePath(filepath.Join(dir, ".agents"), "conventions"), kf)

	indexPath := filepath.Join(dir, ".index")
	idx, _ := search.NewIndex(indexPath, []string{"test"})
	defer idx.Close()

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	handler := WriteHandler(resolver, idx)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project":  "test",
		"category": "conventions",
		"summary":  "Test convention",
		"detail":   "Some detail about the convention",
		"source":   "test",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("WriteHandler() error: %v", err)
	}
	if result == nil {
		t.Fatal("WriteHandler() returned nil")
	}

	// Verify entry was written
	loaded, _ := knowledge.Load(knowledge.CategoryFilePath(filepath.Join(dir, ".agents"), "conventions"))
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
	root := t.TempDir()
	dir := filepath.Join(root, "test")
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"),
		&knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{}})

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	handler := WriteHandler(resolver, nil)

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "invalid category",
			args: map[string]interface{}{
				"project": "test", "category": "invalid", "summary": "s",
				"detail": "d", "source": "s",
			},
		},
		{
			name: "summary too long",
			args: map[string]interface{}{
				"project": "test", "category": "conventions",
				"summary": "this is a very long summary that exceeds one hundred characters and should be rejected by the validation logic in the handler",
				"detail":  "d", "source": "s",
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
	root := t.TempDir()
	dir := filepath.Join(root, "test")
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"),
		&knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{}})

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	handler := WriteHandler(resolver, nil)

	// Create a detail string exceeding 1MB
	bigDetail := strings.Repeat("x", maxDetailSize+1)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": "test", "category": "conventions", "summary": "s",
		"detail": bigDetail, "source": "s",
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
	root := t.TempDir()
	dir := filepath.Join(root, "test")
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)

	kf := &knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{}}
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"), kf)

	indexPath := filepath.Join(dir, ".index")
	idx, _ := search.NewIndex(indexPath, []string{"test"})
	defer idx.Close()

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	handler := QueryHandler(resolver, idx)

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
}

func TestListHandler(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "test")
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)

	kf := &knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{}}
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"), kf)

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	indexPath := filepath.Join(root, ".index")
	idx, err := search.NewIndex(indexPath, []string{"test"})
	if err != nil {
		t.Fatalf("search.NewIndex() error: %v", err)
	}
	defer idx.Close()

	handler := ListHandler(resolver, idx)

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
	root := t.TempDir()
	dir := filepath.Join(root, "test")
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)

	entry := knowledge.Entry{
		ID:      "conv-001",
		Summary: "Test entry",
		Detail:  "Some detail",
		Source:  "test",
		Date:    "2024-01-01",
	}
	kf := &knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{entry}}
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"), kf)

	indexPath := filepath.Join(dir, ".index")
	idx, _ := search.NewIndex(indexPath, []string{"test"})
	defer idx.Close()

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	handler := UpdateHandler(resolver, idx)

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
		if loaded.Entries[0].Date != knowledge.Today() {
			t.Errorf("date = %q, want %q (should be refreshed on update)", loaded.Entries[0].Date, knowledge.Today())
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
			"project":  "test",
			"category": "conventions",
			"id":       "nonexistent",
			"summary":  "Updated",
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
			"project":  "nonexistent",
			"category": "conventions",
			"id":       "conv-001",
			"summary":  "Updated",
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
	root := t.TempDir()
	dir := filepath.Join(root, "test")
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)

	kf := &knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{}}
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"), kf)

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	handler := WriteHandler(resolver, nil)

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
	root := t.TempDir()
	dir := filepath.Join(root, "test")
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)

	entry := knowledge.Entry{
		ID:      "conv-001",
		Summary: "Original",
		Detail:  "Original detail",
		Source:  "test",
		Date:    "2024-01-01",
	}
	kf := &knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{entry}}
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"), kf)

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	writeHandler := WriteHandler(resolver, nil)
	updateHandler := UpdateHandler(resolver, nil)

	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]interface{}{
				"project":  "test",
				"category": "conventions",
				"summary":  fmt.Sprintf("write %d", n),
				"detail":   "detail",
				"source":   "test",
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
				"project":  "test",
				"category": "conventions",
				"id":       "conv-001",
				"summary":  fmt.Sprintf("update %d", n),
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

func TestOrgRefWriteGuard(t *testing.T) {
	root := t.TempDir()
	knowledge.EnsureDir(filepath.Join(root, ".agents", "knowledge"))

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}
	rootName := filepath.Base(root)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project":  rootName,
		"category": "conventions",
		"summary":  "s",
		"detail":   "d",
		"source":   "test",
	}
	result, _ := WriteHandler(resolver, nil)(context.Background(), req)
	if result == nil || !result.IsError {
		t.Fatal("write to org root should be an error")
	}
	content := extractTextContent(t, result)
	if !strings.Contains(content, "org root") {
		t.Errorf("error should mention org root, got: %s", content)
	}
}

func TestAmbiguousProjectError(t *testing.T) {
	r1 := t.TempDir()
	r2 := t.TempDir()
	for _, r := range []string{r1, r2} {
		knowledge.EnsureDir(filepath.Join(r, "api", ".agents"))
	}

	resolver, _, err := projects.Build([]string{r1, r2}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project":  "api",
		"category": "conventions",
		"summary":  "s",
		"detail":   "d",
		"source":   "test",
	}
	result, _ := WriteHandler(resolver, nil)(context.Background(), req)
	if result == nil || !result.IsError {
		t.Fatal("write to ambiguous bare name should be an error")
	}
	content := extractTextContent(t, result)
	if !strings.Contains(content, "ambiguous") {
		t.Errorf("error should say ambiguous, got: %s", content)
	}
}

func TestQueryProjectIsolation(t *testing.T) {
	orgDir := t.TempDir()
	for _, p := range []string{"projectA", "projectB"} {
		knowledge.EnsureDir(filepath.Join(orgDir, p, ".agents"))
	}

	resolver, _, err := projects.Build([]string{orgDir}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	idx, err := search.NewIndex(filepath.Join(orgDir, ".index"), []string{"projectA", "projectB"})
	if err != nil {
		t.Fatalf("NewIndex() error: %v", err)
	}
	defer idx.Close()

	write := WriteHandler(resolver, idx)
	for _, p := range []string{"projectA", "projectB"} {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{
			"project":  p,
			"category": "conventions",
			"summary":  p + " convention",
			"detail":   p + " detail",
			"source":   "test",
		}
		result, _ := write(context.Background(), req)
		if result == nil || result.IsError {
			t.Fatalf("write to %s failed: %+v", p, result)
		}
	}

	query := QueryHandler(resolver, idx)
	reqA := mcp.CallToolRequest{}
	reqA.Params.Arguments = map[string]interface{}{"project": "projectA"}
	resultA, _ := query(context.Background(), reqA)
	contentA := extractTextContent(t, resultA)
	if strings.Contains(contentA, "projectB convention") {
		t.Errorf("projectA query leaked projectB data: %s", contentA)
	}
	if !strings.Contains(contentA, "projectA convention") {
		t.Errorf("projectA query missing its own data: %s", contentA)
	}
}

func TestInitHandlerResolvabilityWarning(t *testing.T) {
	root := t.TempDir()
	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	// Init a project NOT under any configured root
	outside := t.TempDir()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{"project_path": outside}
	result, err := InitHandler(resolver)(context.Background(), req)
	if err != nil {
		t.Fatalf("InitHandler() error: %v", err)
	}
	content := extractTextContent(t, result)
	if !strings.Contains(content, "not under any configured") {
		t.Errorf("expected resolvability warning, got: %s", content)
	}

	// Init a project under the root: no warning
	inside := filepath.Join(root, "inside")
	knowledge.EnsureDir(inside)
	req2 := mcp.CallToolRequest{}
	req2.Params.Arguments = map[string]interface{}{"project_path": inside}
	result2, err := InitHandler(resolver)(context.Background(), req2)
	if err != nil {
		t.Fatalf("InitHandler() error: %v", err)
	}
	content2 := extractTextContent(t, result2)
	if strings.Contains(content2, "not under any configured") {
		t.Errorf("unexpected warning for covered path: %s", content2)
	}
}

func TestQueryWithGlobalMerge(t *testing.T) {
	// Set up a project and a global store (global must be outside root)
	root := t.TempDir()
	projDir := filepath.Join(root, "myproj")
	globalDir := t.TempDir() // separate from root
	knowledge.EnsureDir(filepath.Join(projDir, ".agents"))
	knowledge.EnsureDir(filepath.Join(globalDir, ".agents"))

	// Create knowledge files for both
	projKF := &knowledge.KnowledgeFile{Project: "myproj", Version: 1, Entries: []knowledge.Entry{}}
	knowledge.Save(knowledge.CategoryFilePath(filepath.Join(projDir, ".agents"), "conventions"), projKF)

	globalKF := &knowledge.KnowledgeFile{Project: "_global", Version: 1, Entries: []knowledge.Entry{}}
	knowledge.Save(knowledge.CategoryFilePath(filepath.Join(globalDir, ".agents"), "conventions"), globalKF)

	resolver, _, err := projects.Build([]string{root}, nil, globalDir)
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	indexPath := filepath.Join(root, ".index")
	idx, err := search.NewIndex(indexPath, []string{"myproj", "_global"})
	if err != nil {
		t.Fatalf("search.NewIndex() error: %v", err)
	}
	defer idx.Close()

	// Write to project
	write := WriteHandler(resolver, idx)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project":  "myproj",
		"category": "conventions",
		"summary":  "Project convention",
		"detail":   "project detail",
		"source":   "test",
	}
	result, _ := write(context.Background(), req)
	if result == nil || result.IsError {
		t.Fatalf("write to myproj failed: %+v", result)
	}

	// Write to global
	globalReq := mcp.CallToolRequest{}
	globalReq.Params.Arguments = map[string]interface{}{
		"project":  "_global",
		"category": "conventions",
		"summary":  "Global convention",
		"detail":   "global detail",
		"source":   "test",
	}
	result2, _ := write(context.Background(), globalReq)
	if result2 == nil || result2.IsError {
		t.Fatalf("write to _global failed: %+v", result2)
	}

	// Query project — should include both project and global results
	query := QueryHandler(resolver, idx)
	queryReq := mcp.CallToolRequest{}
	queryReq.Params.Arguments = map[string]interface{}{
		"project": "myproj",
	}
	queryResult, _ := query(context.Background(), queryReq)
	content := extractTextContent(t, queryResult)

	if !strings.Contains(content, "Project convention") {
		t.Errorf("project query missing project result: %s", content)
	}
	if !strings.Contains(content, "Global convention") {
		t.Errorf("project query missing global result: %s", content)
	}

	// Query global directly — should only have global results
	globalQueryReq := mcp.CallToolRequest{}
	globalQueryReq.Params.Arguments = map[string]interface{}{
		"project": "_global",
	}
	globalQueryResult, _ := query(context.Background(), globalQueryReq)
	globalContent := extractTextContent(t, globalQueryResult)

	if strings.Contains(globalContent, "Project convention") {
		t.Errorf("global query should not include project results: %s", globalContent)
	}
	if !strings.Contains(globalContent, "Global convention") {
		t.Errorf("global query missing global result: %s", globalContent)
	}
}

func TestQueryWithoutGlobal(t *testing.T) {
	// Without --global, querying should work normally (no global merge)
	root := t.TempDir()
	projDir := filepath.Join(root, "myproj")
	knowledge.EnsureDir(filepath.Join(projDir, ".agents"))

	projKF := &knowledge.KnowledgeFile{Project: "myproj", Version: 1, Entries: []knowledge.Entry{}}
	knowledge.Save(knowledge.CategoryFilePath(filepath.Join(projDir, ".agents"), "conventions"), projKF)

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	indexPath := filepath.Join(root, ".index")
	idx, err := search.NewIndex(indexPath, []string{"myproj"})
	if err != nil {
		t.Fatalf("search.NewIndex() error: %v", err)
	}
	defer idx.Close()

	write := WriteHandler(resolver, idx)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project":  "myproj",
		"category": "conventions",
		"summary":  "Project convention",
		"detail":   "detail",
		"source":   "test",
	}
	write(context.Background(), req)

	query := QueryHandler(resolver, idx)
	queryReq := mcp.CallToolRequest{}
	queryReq.Params.Arguments = map[string]interface{}{
		"project": "myproj",
	}
	result, _ := query(context.Background(), queryReq)
	content := extractTextContent(t, result)

	if !strings.Contains(content, "Project convention") {
		t.Errorf("query missing project result: %s", content)
	}
}

func TestWriteWithRule(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "test")
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)

	kf := &knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{}}
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"), kf)

	indexPath := filepath.Join(dir, ".index")
	idx, _ := search.NewIndex(indexPath, []string{"test"})
	defer idx.Close()

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	handler := WriteHandler(resolver, idx)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project":  "test",
		"category": "conventions",
		"summary":  "Tmp directory rules",
		"detail":   "All docs go in ./tmp/docs/",
		"rule":     "NEVER create files outside ./tmp",
		"source":   "test",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("WriteHandler() error: %v", err)
	}
	if result.IsError {
		t.Fatalf("WriteHandler() returned error: %+v", result)
	}

	loaded, _ := knowledge.Load(knowledge.CategoryFilePath(agentsDir, "conventions"))
	if len(loaded.Entries) != 1 {
		t.Fatalf("len(Entries) = %d, want 1", len(loaded.Entries))
	}
	if loaded.Entries[0].Rule != "NEVER create files outside ./tmp" {
		t.Errorf("Rule = %q, want %q", loaded.Entries[0].Rule, "NEVER create files outside ./tmp")
	}

	listHandler := ListHandler(resolver, idx)
	listReq := mcp.CallToolRequest{}
	listReq.Params.Arguments = map[string]interface{}{"project": "test"}
	listResult, _ := listHandler(context.Background(), listReq)
	listContent := extractTextContent(t, listResult)

	if !strings.Contains(listContent, "## constraints") {
		t.Errorf("list output missing constraints section: %s", listContent)
	}
	if !strings.Contains(listContent, "NEVER create files outside ./tmp") {
		t.Errorf("list output missing rule text: %s", listContent)
	}
}

func TestWriteRuleTooLong(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "test")
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"),
		&knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{}})

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	handler := WriteHandler(resolver, nil)
	longRule := strings.Repeat("x", 201)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project":  "test",
		"category": "conventions",
		"summary":  "s",
		"detail":   "d",
		"rule":     longRule,
		"source":   "test",
	}
	result, _ := handler(context.Background(), req)
	if result == nil {
		t.Fatal("handler returned nil")
	}
	if !result.IsError {
		t.Fatal("expected error for oversized rule")
	}
}

func TestUpdateWithRule(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "test")
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)

	entry := knowledge.Entry{
		ID:      "conv-001",
		Summary: "Test entry",
		Detail:  "Some detail",
		Source:  "test",
		Date:    "2024-01-01",
	}
	kf := &knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: []knowledge.Entry{entry}}
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"), kf)

	indexPath := filepath.Join(dir, ".index")
	idx, _ := search.NewIndex(indexPath, []string{"test"})
	defer idx.Close()

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	handler := UpdateHandler(resolver, idx)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project":  "test",
		"category": "conventions",
		"id":       "conv-001",
		"rule":     "NEVER do the thing",
	}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("UpdateHandler() returned error: %+v", result)
	}

	loaded, _ := knowledge.Load(knowledge.CategoryFilePath(agentsDir, "conventions"))
	if loaded.Entries[0].Rule != "NEVER do the thing" {
		t.Errorf("Rule = %q, want %q", loaded.Entries[0].Rule, "NEVER do the thing")
	}
}

func TestListGlobalProvenance(t *testing.T) {
	root := t.TempDir()
	projDir := filepath.Join(root, "myproj")
	globalDir := t.TempDir()
	knowledge.EnsureDir(filepath.Join(projDir, ".agents"))
	knowledge.EnsureDir(filepath.Join(globalDir, ".agents"))

	globalKF := &knowledge.KnowledgeFile{Project: "_global", Version: 1, Entries: []knowledge.Entry{
		{ID: "conv-001", Summary: "Global constraint", Detail: "info", Rule: "NEVER violate this", Source: "test", Date: "2024-01-01"},
		{ID: "conv-002", Summary: "Global regular", Detail: "info", Source: "test", Date: "2024-01-01"},
	}}
	knowledge.Save(knowledge.CategoryFilePath(filepath.Join(globalDir, ".agents"), "conventions"), globalKF)

	resolver, _, err := projects.Build([]string{root}, nil, globalDir)
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	indexPath := filepath.Join(root, ".index")
	idx, _ := search.NewIndex(indexPath, []string{"myproj", "_global"})
	defer idx.Close()

	handler := ListHandler(resolver, idx)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{"project": "myproj"}
	result, _ := handler(context.Background(), req)
	content := extractTextContent(t, result)

	if !strings.Contains(content, "## constraints (global)") {
		t.Errorf("global constraints section missing label: %s", content)
	}
	if !strings.Contains(content, "## conventions (global)") {
		t.Errorf("global conventions section missing label: %s", content)
	}
	if !strings.Contains(content, "NEVER violate this") {
		t.Errorf("global rule text missing: %s", content)
	}
	if !strings.Contains(content, "no project-specific knowledge entries found") {
		t.Errorf("missing empty-project note: %s", content)
	}
}

func TestListOrgGroupedByFile(t *testing.T) {
	root := t.TempDir()
	knowledge.EnsureDir(filepath.Join(root, ".agents", "knowledge"))

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}
	rootName := filepath.Base(root)

	indexPath := filepath.Join(root, ".index")
	idx, _ := search.NewIndex(indexPath, []string{rootName})
	defer idx.Close()

	for _, sec := range []struct{ file, heading, detail string }{
		{"architecture.md", "Build Commands", "make test"},
		{"architecture.md", "Testing", "make test-stability"},
		{"review.md", "Overview", "chaon-review"},
	} {
		id := fmt.Sprintf("%s/org-%s::%s", rootName, sec.file, sec.heading)
		if err := idx.Add(id, search.SearchDocument{
			Summary: sec.file + ": " + sec.heading, Detail: sec.detail,
			Category: "conventions", Project: rootName,
		}); err != nil {
			t.Fatalf("Add(%s) error: %v", id, err)
		}
	}

	handler := ListHandler(resolver, idx)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{"project": rootName}
	result, _ := handler(context.Background(), req)
	content := extractTextContent(t, result)

	if !strings.Contains(content, "### architecture.md (2 sections)") {
		t.Errorf("architecture group missing: %s", content)
	}
	if !strings.Contains(content, "- [architecture.md :: Build Commands]") {
		t.Errorf("section entry missing: %s", content)
	}
	if !strings.Contains(content, "### review.md (1 sections)") {
		t.Errorf("review group missing: %s", content)
	}
}

func TestListConstraintsSeparation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "test")
	agentsDir := filepath.Join(dir, ".agents")
	knowledge.EnsureDir(agentsDir)

	entries := []knowledge.Entry{
		{ID: "conv-001", Summary: "Regular entry", Detail: "info", Source: "test", Date: "2024-01-01"},
		{ID: "conv-002", Summary: "Constrained entry", Detail: "info", Rule: "NEVER violate this", Source: "test", Date: "2024-01-01"},
	}
	kf := &knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: entries}
	knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"), kf)

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	indexPath := filepath.Join(root, ".index")
	idx, _ := search.NewIndex(indexPath, []string{"test"})
	defer idx.Close()

	handler := ListHandler(resolver, idx)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{"project": "test"}
	result, _ := handler(context.Background(), req)
	content := extractTextContent(t, result)

	if !strings.Contains(content, "## constraints") {
		t.Errorf("missing constraints section: %s", content)
	}
	if !strings.Contains(content, "NEVER violate this") {
		t.Errorf("missing rule in constraints: %s", content)
	}
	if !strings.Contains(content, "[conv-002]") {
		t.Errorf("missing constrained entry ID: %s", content)
	}

	constraintsIdx := strings.Index(content, "## constraints")
	regularIdx := strings.Index(content, "## conventions")
	if constraintsIdx >= regularIdx {
		t.Errorf("constraints section should appear before regular entries")
	}
	if strings.Contains(content[regularIdx:], "Constrained entry") {
		t.Errorf("constrained entry should not appear in regular entries section: %s", content[regularIdx:])
	}
	if !strings.Contains(content[regularIdx:], "Regular entry") {
		t.Errorf("regular entry should appear in regular entries section: %s", content[regularIdx:])
	}
}

func TestWriteToCentralStore(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	knowledge.EnsureDir(proj)
	store := filepath.Join(t.TempDir(), "store")
	knowledge.EnsureDir(store)

	resolver, _, err := projects.BuildWithStore([]string{root}, nil, "", store)
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	handler := WriteHandler(resolver, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project":  "proj",
		"category": "conventions",
		"summary":  "central",
		"detail":   "stored centrally",
		"source":   "test",
	}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if result.IsError {
		t.Fatalf("write returned error: %v", extractTextContent(t, result))
	}

	want := filepath.Join(store, "proj", ".agents", "conventions.yaml")
	if !knowledge.FileExists(want) {
		t.Errorf("store file missing: %s", want)
	}
	if knowledge.FileExists(filepath.Join(proj, ".agents", "conventions.yaml")) {
		t.Error("in-tree .agents must not be created under --store")
	}
}

func TestListIgnoresInTreeUnderStore(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	knowledge.EnsureDir(filepath.Join(proj, ".agents"))
	kf := &knowledge.KnowledgeFile{
		Project: "proj", Version: 1,
		Entries: []knowledge.Entry{
			{ID: "conv-001", Summary: "stale in-tree entry", Source: "test", Date: knowledge.Today()},
		},
	}
	if err := knowledge.Save(knowledge.CategoryFilePath(filepath.Join(proj, ".agents"), "conventions"), kf); err != nil {
		t.Fatalf("seed in-tree: %v", err)
	}

	store := filepath.Join(t.TempDir(), "store")
	knowledge.EnsureDir(store)
	resolver, _, err := projects.BuildWithStore([]string{root}, nil, "", store)
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	handler := ListHandler(resolver, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{"project": "proj"}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	content := extractTextContent(t, result)
	if strings.Contains(content, "stale in-tree entry") {
		t.Errorf("list must not surface in-tree entries under --store: %s", content)
	}
}

func TestUpdateInCentralStore(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	knowledge.EnsureDir(proj)
	store := filepath.Join(t.TempDir(), "store")
	knowledge.EnsureDir(store)

	resolver, _, err := projects.BuildWithStore([]string{root}, nil, "", store)
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	want := filepath.Join(store, "proj", ".agents", "conventions.yaml")
	writeHandler := WriteHandler(resolver, nil)
	writeReq := mcp.CallToolRequest{}
	writeReq.Params.Arguments = map[string]interface{}{
		"project": "proj", "category": "conventions",
		"summary": "before", "detail": "before detail", "source": "test",
	}
	if _, err := writeHandler(context.Background(), writeReq); err != nil {
		t.Fatalf("write error: %v", err)
	}

	handler := UpdateHandler(resolver, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": "proj", "category": "conventions", "id": "conv-001", "summary": "after",
	}
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("update error: %v", err)
	}
	if result.IsError {
		t.Fatalf("update returned error: %v", extractTextContent(t, result))
	}

	loaded, loadErr := knowledge.Load(want)
	if loadErr != nil {
		t.Fatalf("load store file: %v", loadErr)
	}
	if loaded.Entries[0].Summary != "after" {
		t.Errorf("summary = %q, want %q", loaded.Entries[0].Summary, "after")
	}
}

func TestInitTargetsCentralStore(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	knowledge.EnsureDir(proj)
	store := filepath.Join(t.TempDir(), "store")
	knowledge.EnsureDir(store)

	resolver, _, err := projects.BuildWithStore([]string{root}, nil, "", store)
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{"project_path": proj}
	result, err := InitHandler(resolver)(context.Background(), req)
	if err != nil {
		t.Fatalf("init error: %v", err)
	}
	if result.IsError {
		t.Fatalf("init returned error: %v", extractTextContent(t, result))
	}

	want := filepath.Join(store, "proj", ".agents")
	if !knowledge.FileExists(knowledge.CategoryFilePath(want, "conventions")) {
		t.Errorf("store not initialized at %s", want)
	}
	if knowledge.FileExists(filepath.Join(proj, ".agents", "conventions.yaml")) {
		t.Error("init must not create in-tree .agents under --store")
	}
	content := extractTextContent(t, result)
	if !strings.Contains(content, want) {
		t.Errorf("result should report the store path %q, got: %s", want, content)
	}
}

func TestGlobalOpsUnderStore(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	knowledge.EnsureDir(filepath.Join(proj, ".agents"))
	globalDir := t.TempDir()
	knowledge.EnsureDir(filepath.Join(globalDir, ".agents"))
	store := filepath.Join(t.TempDir(), "store")
	knowledge.EnsureDir(store)

	resolver, _, err := projects.BuildWithStore([]string{root}, nil, globalDir, store)
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	write := WriteHandler(resolver, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": "_global", "category": "conventions",
		"summary": "global under store", "detail": "global detail", "source": "test",
	}
	result, err := write(context.Background(), req)
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if result.IsError {
		t.Fatalf("write error result: %v", extractTextContent(t, result))
	}

	globalCat := knowledge.CategoryFilePath(filepath.Join(globalDir, ".agents"), "conventions")
	if !knowledge.FileExists(globalCat) {
		t.Fatal("write to _global must land in the global store")
	}
	if knowledge.FileExists(filepath.Join(store, "_global", ".agents", "conventions.yaml")) {
		t.Error("write to _global must not create a _global slot in the central store")
	}

	update := UpdateHandler(resolver, nil)
	uReq := mcp.CallToolRequest{}
	uReq.Params.Arguments = map[string]interface{}{
		"project": "_global", "category": "conventions", "id": "conv-001", "summary": "updated global under store",
	}
	uResult, err := update(context.Background(), uReq)
	if err != nil {
		t.Fatalf("update error: %v", err)
	}
	if uResult.IsError {
		t.Fatalf("update error result: %v", extractTextContent(t, uResult))
	}
	loaded, loadErr := knowledge.Load(globalCat)
	if loadErr != nil {
		t.Fatalf("load global file: %v", loadErr)
	}
	if loaded.Entries[0].Summary != "updated global under store" {
		t.Errorf("summary = %q, want %q", loaded.Entries[0].Summary, "updated global under store")
	}

	list := ListHandler(resolver, nil)
	lReq := mcp.CallToolRequest{}
	lReq.Params.Arguments = map[string]interface{}{"project": "_global"}
	lResult, err := list(context.Background(), lReq)
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	content := extractTextContent(t, lResult)
	if !strings.Contains(content, "updated global under store") {
		t.Errorf("list _global should show the global entry: %s", content)
	}
}

func TestInitUnresolvableUnderStore(t *testing.T) {
	root := t.TempDir()
	other := filepath.Join(t.TempDir(), "elsewhere")
	knowledge.EnsureDir(other)
	store := filepath.Join(t.TempDir(), "store")
	knowledge.EnsureDir(store)

	resolver, _, err := projects.BuildWithStore([]string{root}, nil, "", store)
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{"project_path": other}
	result, err := InitHandler(resolver)(context.Background(), req)
	if err != nil {
		t.Fatalf("init error: %v", err)
	}
	if !result.IsError {
		t.Fatal("unresolvable init under --store must return an error")
	}
	content := extractTextContent(t, result)
	if !strings.Contains(content, "--store") {
		t.Errorf("error should explain --store, got: %s", content)
	}
	if knowledge.FileExists(filepath.Join(other, ".agents")) {
		t.Error("unresolvable init must not write in-tree .agents under --store")
	}
}

func TestInitStoreFallbackPathVerified(t *testing.T) {
	t.Run("same name in another root must not bind", func(t *testing.T) {
		r1 := t.TempDir()
		r2 := t.TempDir()
		existing := filepath.Join(r2, "api")
		knowledge.EnsureDir(existing) // registered at build with unique bare name "api"

		store := filepath.Join(t.TempDir(), "store")
		knowledge.EnsureDir(store)

		resolver, _, err := projects.BuildWithStore([]string{r1, r2}, nil, "", store)
		if err != nil {
			t.Fatalf("BuildWithStore() error: %v", err)
		}

		created := filepath.Join(r1, "api")
		knowledge.EnsureDir(created) // created after startup, covered by r1

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"project_path": created}
		result, err := InitHandler(resolver)(context.Background(), req)
		if err != nil {
			t.Fatalf("init error: %v", err)
		}
		if !result.IsError {
			t.Fatal("init for a path covered but not resolvable must reject, not write another root's slot")
		}
		content := extractTextContent(t, result)
		if !strings.Contains(content, "--store") {
			t.Errorf("error should explain --store, got: %s", content)
		}
		if knowledge.FileExists(filepath.Join(store, "api", ".agents")) {
			t.Error("init must not write into the other root's store slot")
		}
		if knowledge.FileExists(filepath.Join(created, ".agents")) {
			t.Error("init must not fall back to in-tree .agents under --store")
		}
	})

	t.Run("org ref must not hijack init", func(t *testing.T) {
		orgRoot := filepath.Join(t.TempDir(), "api")
		knowledge.EnsureDir(orgRoot)

		store := filepath.Join(t.TempDir(), "store")
		knowledge.EnsureDir(store)
		// Org ref for "api" exists on the store side.
		knowledge.EnsureDir(filepath.Join(store, "api", ".agents", "knowledge"))

		resolver, _, err := projects.BuildWithStore([]string{orgRoot}, nil, "", store)
		if err != nil {
			t.Fatalf("BuildWithStore() error: %v", err)
		}

		created := filepath.Join(orgRoot, "api")
		knowledge.EnsureDir(created) // a project path under the root named like the org

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"project_path": created}
		result, err := InitHandler(resolver)(context.Background(), req)
		if err != nil {
			t.Fatalf("init error: %v", err)
		}
		if !result.IsError {
			t.Fatal("init must reject when bare-name resolution selects an unrelated org ref")
		}
		content := extractTextContent(t, result)
		if strings.Contains(content, "org roots have no per-project store") {
			t.Errorf("error must not blame the unrelated org ref, got: %s", content)
		}
		if !strings.Contains(content, "--store") {
			t.Errorf("error should explain --store, got: %s", content)
		}
		if knowledge.FileExists(filepath.Join(created, ".agents")) {
			t.Error("init must not write in-tree .agents under --store")
		}
	})

	t.Run("new project under a root still initializes", func(t *testing.T) {
		root := t.TempDir()
		store := filepath.Join(t.TempDir(), "store")
		knowledge.EnsureDir(store)

		resolver, _, err := projects.BuildWithStore([]string{root}, nil, "", store)
		if err != nil {
			t.Fatalf("BuildWithStore() error: %v", err)
		}

		created := filepath.Join(root, "api")
		knowledge.EnsureDir(created) // created after startup

		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]interface{}{"project_path": created}
		result, err := InitHandler(resolver)(context.Background(), req)
		if err != nil {
			t.Fatalf("init error: %v", err)
		}
		if result.IsError {
			t.Fatalf("init returned error: %v", extractTextContent(t, result))
		}
		want := filepath.Join(store, "api", ".agents")
		if !knowledge.FileExists(knowledge.CategoryFilePath(want, "conventions")) {
			t.Errorf("store not initialized at %s", want)
		}
		if knowledge.FileExists(filepath.Join(created, ".agents", "conventions.yaml")) {
			t.Error("init must not create in-tree .agents under --store")
		}
	})
}
