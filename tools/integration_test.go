package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
	"github.com/renderorange/knowledge-mcp/search"
	"gopkg.in/yaml.v3"
)

func TestEndToEnd(t *testing.T) {
	dir := t.TempDir()
	projectName := filepath.Base(dir)

	resolver, _, err := projects.Build(nil, []string{dir}, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	idx := newIndexReady(t)
	defer idx.Close()

	// Step 1: Init
	initReq := mcp.CallToolRequest{}
	initReq.Params.Arguments = map[string]interface{}{
		"project_path": dir,
	}
	initResult, err := InitHandler(resolver)(context.Background(), initReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if initResult == nil {
		t.Fatal("init returned nil")
	}

	// Step 2: Write conventions
	writeHandler := WriteHandler(resolver, idx)
	writeReq := mcp.CallToolRequest{}
	writeReq.Params.Arguments = map[string]interface{}{
		"project":  projectName,
		"category": "conventions",
		"summary":  "Build with make test",
		"detail":   "Run make test before committing. Also make test-stability and make lint.",
		"source":   ".agents/knowledge/architecture.md",
	}
	writeResult, err := writeHandler(context.Background(), writeReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if writeResult == nil {
		t.Fatal("write conventions returned nil")
	}

	// Step 3: Write subsystem
	writeReq2 := mcp.CallToolRequest{}
	writeReq2.Params.Arguments = map[string]interface{}{
		"project":  projectName,
		"category": "subsystems",
		"summary":  "Reverb uses Schroeder allpass",
		"detail":   "The reverb engine uses a Schroeder allpass chain with 2048-sample buffer.",
		"source":   "code analysis",
	}
	writeResult2, err := writeHandler(context.Background(), writeReq2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if writeResult2 == nil {
		t.Fatal("write subsystem returned nil")
	}

	// Step 4: Write decision
	writeReq3 := mcp.CallToolRequest{}
	writeReq3.Params.Arguments = map[string]interface{}{
		"project":  projectName,
		"category": "decisions",
		"summary":  "Sag floor 2.0V",
		"detail":   "The sag floor is pinned at 2.0V to keep analog sag audible at starve >= 9.5.",
		"source":   "adversarial analysis",
	}
	writeResult3, err := writeHandler(context.Background(), writeReq3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if writeResult3 == nil {
		t.Fatal("write decision returned nil")
	}

	// Step 5: Query — full text, verify result content
	queryHandler := QueryHandler(resolver, idx)
	queryReq := mcp.CallToolRequest{}
	queryReq.Params.Arguments = map[string]interface{}{
		"project": projectName,
		"query":   "reverb allpass",
	}
	queryResult, err := queryHandler(context.Background(), queryReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if queryResult == nil {
		t.Fatal("query returned nil")
	}

	// Parse and verify query results
	queryContent := extractTextContent(t, queryResult)
	var queryResults []search.SearchResult
	if err := json.Unmarshal([]byte(queryContent), &queryResults); err != nil {
		t.Fatalf("failed to parse query results: %v", err)
	}
	if len(queryResults) == 0 {
		t.Fatal("query returned 0 results")
	}
	if queryResults[0].ID != "sub-001" {
		t.Errorf("top result ID = %q, want %q", queryResults[0].ID, "sub-001")
	}
	if queryResults[0].Project != projectName {
		t.Errorf("top result Project = %q, want %q", queryResults[0].Project, projectName)
	}

	// Step 6: Query — category filter
	queryReq2 := mcp.CallToolRequest{}
	queryReq2.Params.Arguments = map[string]interface{}{
		"project":  projectName,
		"category": "conventions",
	}
	queryResult2, err := queryHandler(context.Background(), queryReq2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if queryResult2 == nil {
		t.Fatal("query by category returned nil")
	}

	catContent := extractTextContent(t, queryResult2)
	var catResults []search.SearchResult
	if err := json.Unmarshal([]byte(catContent), &catResults); err != nil {
		t.Fatalf("failed to parse category results: %v", err)
	}
	if len(catResults) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(catResults))
	}
	if catResults[0].ID != "conv-001" {
		t.Errorf("result ID = %q, want %q", catResults[0].ID, "conv-001")
	}

	// Step 7: List, verify output contains entries
	listHandler := ListHandler(resolver, idx)
	listReq := mcp.CallToolRequest{}
	listReq.Params.Arguments = map[string]interface{}{
		"project": projectName,
	}
	listResult, err := listHandler(context.Background(), listReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if listResult == nil {
		t.Fatal("list returned nil")
	}

	listContent := extractTextContent(t, listResult)
	if !strings.Contains(listContent, "conv-001") {
		t.Error("list output missing conv-001")
	}
	if !strings.Contains(listContent, "sub-001") {
		t.Error("list output missing sub-001")
	}
	if !strings.Contains(listContent, "dec-001") {
		t.Error("list output missing dec-001")
	}

	// Step 8: Update
	updateHandler := UpdateHandler(resolver, idx)
	updateReq := mcp.CallToolRequest{}
	updateReq.Params.Arguments = map[string]interface{}{
		"project":  projectName,
		"category": "decisions",
		"id":       "dec-001",
		"summary":  "Updated sag floor",
	}
	updateResult, err := updateHandler(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updateResult == nil {
		t.Fatal("update returned nil")
	}

	// Verify update persisted and date refreshed
	agentsDir := filepath.Join(dir, ".agents")
	loaded, _ := knowledge.Load(knowledge.CategoryFilePath(agentsDir, "decisions"))
	if loaded.Entries[0].Summary != "Updated sag floor" {
		t.Errorf("summary = %q, want %q", loaded.Entries[0].Summary, "Updated sag floor")
	}
	if loaded.Entries[0].Date != knowledge.Today() {
		t.Errorf("date = %q, want %q (should be refreshed on update)", loaded.Entries[0].Date, knowledge.Today())
	}
}

func TestOrgWideMode(t *testing.T) {
	orgDir := t.TempDir()

	// Create org-level .agents/knowledge/ with test files
	orgAgentsDir := filepath.Join(orgDir, ".agents", "knowledge")
	knowledge.EnsureDir(orgAgentsDir)

	// Create two project directories
	projA := filepath.Join(orgDir, "projectA")
	projB := filepath.Join(orgDir, "projectB")
	knowledge.EnsureDir(filepath.Join(projA, ".agents"))
	knowledge.EnsureDir(filepath.Join(projB, ".agents"))

	resolver, _, err := projects.Build([]string{orgDir}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	// Init both projects
	initReqA := mcp.CallToolRequest{}
	initReqA.Params.Arguments = map[string]interface{}{"project_path": projA}
	InitHandler(resolver)(context.Background(), initReqA)

	initReqB := mcp.CallToolRequest{}
	initReqB.Params.Arguments = map[string]interface{}{"project_path": projB}
	InitHandler(resolver)(context.Background(), initReqB)

	idx, err := search.NewIndex()
	if err != nil {
		t.Fatalf("NewIndex() error: %v", err)
	}
	defer idx.Close()

	// Write to projectA
	writeHandler := WriteHandler(resolver, idx)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project":  "projectA",
		"category": "conventions",
		"summary":  "Project A convention",
		"detail":   "detail",
		"source":   "test",
	}
	result, err := writeHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if result.IsError {
		t.Fatal("write returned error")
	}

	// List projectA
	listHandler := ListHandler(resolver, idx)
	listReq := mcp.CallToolRequest{}
	listReq.Params.Arguments = map[string]interface{}{"project": "projectA"}
	listResult, _ := listHandler(context.Background(), listReq)
	listContent := extractTextContent(t, listResult)
	if !strings.Contains(listContent, "conv-001") {
		t.Error("list projectA missing conv-001")
	}

	// projectB should have no entries
	listReqB := mcp.CallToolRequest{}
	listReqB.Params.Arguments = map[string]interface{}{"project": "projectB"}
	listResultB, _ := listHandler(context.Background(), listReqB)
	listContentB := extractTextContent(t, listResultB)
	if strings.Contains(listContentB, "conv-001") {
		t.Error("projectB should not have projectA's entries")
	}
}

func TestFullWorkflow(t *testing.T) {
	orgRoot := t.TempDir()

	// Create org with two projects
	for _, proj := range []string{"app", "lib"} {
		agentsDir := filepath.Join(orgRoot, proj, ".agents")
		os.MkdirAll(agentsDir, 0755)
		meta := knowledge.Meta{
			Project:          proj,
			KnowledgeVersion: 1,
			Created:          "2026-01-01",
			LastUpdated:      "2026-01-01",
			Categories:       knowledge.ValidCategories(),
		}
		data, _ := yaml.Marshal(meta)
		os.WriteFile(filepath.Join(agentsDir, "_meta.yaml"), data, 0644)
	}

	resolver, _, err := projects.Build([]string{orgRoot}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	idx := newIndexReady(t)
	defer idx.Close()

	// 1. List projects
	listHandler := ListProjectsHandler(resolver)
	listResult, err := listHandler(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("list_projects error: %v", err)
	}
	listText := extractTextContent(t, listResult)
	if !strings.Contains(listText, "- app") || !strings.Contains(listText, "- lib") {
		t.Fatalf("list_projects failed: %s", listText)
	}

	// 2. Write knowledge
	writeHandler := WriteHandler(resolver, idx)
	writeReq := mcp.CallToolRequest{}
	writeReq.Params.Arguments = map[string]interface{}{
		"project":  "app",
		"category": "conventions",
		"summary":  "Use tabs",
		"detail":   "All files use tabs for indentation",
		"source":   "manual review",
	}
	writeResult, err := writeHandler(context.Background(), writeReq)
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	writeText := extractTextContent(t, writeResult)
	if !strings.Contains(writeText, "wrote conv-001") {
		t.Fatalf("write failed: %s", writeText)
	}

	// 3. Query
	queryHandler := QueryHandler(resolver, idx)
	queryReq := mcp.CallToolRequest{}
	queryReq.Params.Arguments = map[string]interface{}{
		"project": "app",
		"query":   "tabs",
	}
	queryResult, err := queryHandler(context.Background(), queryReq)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	queryText := extractTextContent(t, queryResult)
	if !strings.Contains(queryText, "conv-001") {
		t.Fatalf("query failed: %s", queryText)
	}
}

func TestStoreModeEndToEnd(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	os.MkdirAll(proj, 0755)

	// Seed an in-tree store that must be ignored under --store.
	agentsDir := filepath.Join(proj, ".agents")
	os.MkdirAll(agentsDir, 0755)
	inTree := &knowledge.KnowledgeFile{
		Project: "proj", Version: 1,
		Entries: []knowledge.Entry{
			{ID: "conv-001", Summary: "stale in-tree entry", Source: "test", Date: knowledge.Today()},
		},
	}
	if err := knowledge.Save(knowledge.CategoryFilePath(agentsDir, "conventions"), inTree); err != nil {
		t.Fatalf("seed in-tree store: %v", err)
	}

	store := t.TempDir()
	resolver, warnings, err := projects.BuildWithStore([]string{root}, nil, "", store)
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	warned := false
	for _, w := range warnings {
		if strings.Contains(w, "ignoring in-tree .agents") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("expected in-tree ignore warning, got %v", warnings)
	}

	idx := newIndexReady(t)
	defer idx.Close()

	// Init writes into the store.
	initReq := mcp.CallToolRequest{}
	initReq.Params.Arguments = map[string]interface{}{"project_path": proj}
	initResult, err := InitHandler(resolver)(context.Background(), initReq)
	if err != nil {
		t.Fatalf("init error: %v", err)
	}
	if initResult.IsError {
		t.Fatalf("init returned error: %v", extractTextContent(t, initResult))
	}
	if !knowledge.FileExists(filepath.Join(store, "proj", ".agents", "conventions.yaml")) {
		t.Fatal("init did not create the central store")
	}

	// Write goes to the store.
	writeHandler := WriteHandler(resolver, idx)
	writeReq := mcp.CallToolRequest{}
	writeReq.Params.Arguments = map[string]interface{}{
		"project":  "proj",
		"category": "conventions",
		"summary":  "fresh central entry",
		"detail":   "centralized reverb parameters",
		"source":   "test",
	}
	if _, err := writeHandler(context.Background(), writeReq); err != nil {
		t.Fatalf("write error: %v", err)
	}

	// Query hits the store entry, not the in-tree one.
	queryHandler := QueryHandler(resolver, idx)
	queryReq := mcp.CallToolRequest{}
	queryReq.Params.Arguments = map[string]interface{}{"project": "proj", "query": "reverb"}
	queryResult, err := queryHandler(context.Background(), queryReq)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	queryContent := extractTextContent(t, queryResult)
	if !strings.Contains(queryContent, "fresh central entry") {
		t.Errorf("query should find the central entry, got: %s", queryContent)
	}
	if strings.Contains(queryContent, "stale in-tree entry") {
		t.Error("query must not find in-tree entries under --store")
	}

	// List shows the store entry only.
	listHandler := ListHandler(resolver, idx)
	listReq := mcp.CallToolRequest{}
	listReq.Params.Arguments = map[string]interface{}{"project": "proj"}
	listResult, err := listHandler(context.Background(), listReq)
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	listContent := extractTextContent(t, listResult)
	if !strings.Contains(listContent, "fresh central entry") {
		t.Errorf("list should show the central entry, got: %s", listContent)
	}
	if strings.Contains(listContent, "stale in-tree entry") {
		t.Error("list must not show in-tree entries under --store")
	}
}

// extractTextContent extracts the text content from an MCP tool result.
func extractTextContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	if tc, ok := result.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	t.Fatalf("unexpected content type: %T", result.Content[0])
	return ""
}
