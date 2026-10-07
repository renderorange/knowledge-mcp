package search

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/renderorange/knowledge-mcp/projects"
)

func newIndex(t *testing.T) *Index {
	t.Helper()
	idx, err := NewIndex()
	if err != nil {
		t.Fatalf("NewIndex() error: %v", err)
	}
	return idx
}

// newIndexReady creates an index and closes the ready channel immediately,
// for unit tests that add documents directly and query without IndexAll.
func newIndexReady(t *testing.T) *Index {
	t.Helper()
	idx := newIndex(t)
	idx.CloseReady()
	return idx
}

func TestIndexCreateAndQuery(t *testing.T) {
	idx := newIndexReady(t)
	defer idx.Close()

	docs := []struct {
		id  string
		doc SearchDocument
	}{
		{
			id: "conv-001",
			doc: SearchDocument{
				Summary:  "DSP interface pattern: PedalParams struct",
				Detail:   "Every pedal exposes pedal_init, pedal_set_params, pedal_process",
				Category: "conventions",
				Project:  "test",
			},
		},
		{
			id: "sub-001",
			doc: SearchDocument{
				Summary:  "Reverb uses Schroeder allpass chain",
				Detail:   "The reverb engine uses a Schroeder allpass chain with 2048-sample buffer",
				Category: "subsystems",
				Project:  "test",
			},
		},
		{
			id: "dec-001",
			doc: SearchDocument{
				Summary:  "Sag floor 2.0V pins analog sag",
				Detail:   "The sag circuit uses a series resistor with load-ratio model",
				Category: "decisions",
				Project:  "test",
			},
		},
	}

	for _, d := range docs {
		if err := idx.Add(d.id, d.doc); err != nil {
			t.Fatalf("Add(%s) error: %v", d.id, err)
		}
	}

	results, err := idx.Query("test", "reverb allpass", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Query() returned 0 results, want > 0")
	}
	if results[0].ID != "sub-001" {
		t.Errorf("top result ID = %q, want %q", results[0].ID, "sub-001")
	}
	if results[0].Project != "test" {
		t.Errorf("top result Project = %q, want %q", results[0].Project, "test")
	}

	results, err = idx.Query("test", "", "conventions", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].ID != "conv-001" {
		t.Errorf("result ID = %q, want %q", results[0].ID, "conv-001")
	}

	results, err = idx.Query("test", "", "decisions", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].ID != "dec-001" {
		t.Errorf("result ID = %q, want %q", results[0].ID, "dec-001")
	}

	results, err = idx.Query("test", "DSP", "conventions", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
}

func TestProjectScopingNoCrossProjectLeakage(t *testing.T) {
	idx := newIndexReady(t)
	defer idx.Close()

	// Same bare entry ID in two projects — must not overwrite each other.
	if err := idx.Add("alpha/conv-001", SearchDocument{
		Summary: "alpha convention", Detail: "alpha detail",
		Category: "conventions", Project: "alpha",
	}); err != nil {
		t.Fatalf("Add(alpha) error: %v", err)
	}
	if err := idx.Add("beta/conv-001", SearchDocument{
		Summary: "beta convention", Detail: "beta detail",
		Category: "conventions", Project: "beta",
	}); err != nil {
		t.Fatalf("Add(beta) error: %v", err)
	}

	alphaResults, err := idx.Query("alpha", "", "", 10)
	if err != nil {
		t.Fatalf("Query(alpha) error: %v", err)
	}
	if len(alphaResults) != 1 || alphaResults[0].Summary != "alpha convention" {
		t.Fatalf("alpha query = %+v, want exactly the alpha doc", alphaResults)
	}

	betaResults, err := idx.Query("beta", "", "", 10)
	if err != nil {
		t.Fatalf("Query(beta) error: %v", err)
	}
	if len(betaResults) != 1 || betaResults[0].Summary != "beta convention" {
		t.Fatalf("beta query = %+v, want exactly the beta doc", betaResults)
	}

	// Unfiltered query sees both docs (no data loss from key collision).
	all, err := idx.Query("", "", "", 10)
	if err != nil {
		t.Fatalf("Query(all) error: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2 (collision would leave 1)", len(all))
	}
}

func TestIndexEmptyQuery(t *testing.T) {
	idx := newIndexReady(t)
	defer idx.Close()

	if err := idx.Add("test/test-001", SearchDocument{
		Summary: "test", Detail: "test detail", Project: "test",
	}); err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	results, err := idx.Query("test", "", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("len(results) = %d, want 1", len(results))
	}
}

func TestQueryEscapesSpecialCharacters(t *testing.T) {
	idx := newIndexReady(t)
	defer idx.Close()

	if err := idx.Add("test/conv-001", SearchDocument{
		Summary:  "GH-1: commit format",
		Detail:   "Commit messages use the GH-1: verb phrase format",
		Category: "conventions",
		Project:  "test",
	}); err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	// Colon and mid-word hyphen must be plain text, not field/wildcard syntax.
	results, err := idx.Query("test", "GH-1: verb", "", 10)
	if err != nil {
		t.Fatalf("Query() with special characters error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	// Parens and a doubled ampersand must not parse as operators.
	results, err = idx.Query("test", "(GH-1) && format", "", 10)
	if err != nil {
		t.Fatalf("Query() with operators error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	// An unbalanced quote must not produce a syntax error.
	if _, err = idx.Query("test", `"unclosed quote`, "", 10); err != nil {
		t.Fatalf("Query() with unbalanced quote error: %v", err)
	}
}

// TestMemoryIndexCreatesNoFiles pins the memory-only contract: creating,
// adding to, and closing an index must never touch the filesystem.
func TestMemoryIndexCreatesNoFiles(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	dir := t.TempDir()

	idx := newIndexReady(t)
	if err := idx.Add("test/conv-001", SearchDocument{Summary: "doc", Project: "test"}); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("index created %d artifacts in the working temp dir, want 0", len(entries))
	}
	if _, err := os.Stat(filepath.Join(stateHome, "knowledge-mcp")); !os.IsNotExist(err) {
		t.Errorf("index created artifacts under XDG_STATE_HOME: %v", err)
	}
}

func TestWaitReadyBlocksWhenEmpty(t *testing.T) {
	idx := newIndex(t)
	defer idx.Close()

	// ready channel should be open (blocking) for an index that has not
	// completed IndexAll or CloseReady.
	select {
	case <-idx.ready:
		t.Error("ready channel should not be closed for empty index")
	default:
		// expected: channel is open (blocking)
	}
}

func TestIndexAllBackground(t *testing.T) {
	dir := t.TempDir()

	idx := newIndex(t)
	defer idx.Close()

	// Create a mock knowledge file
	agentsDir := filepath.Join(dir, "test-project", ".agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	conventionsPath := filepath.Join(agentsDir, "conventions.yaml")
	if err := os.WriteFile(conventionsPath, []byte(`entries:
- id: conv-001
  summary: Test convention
  detail: Test detail
`), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	// Create a resolver with the test project
	resolver, _, err := projects.BuildWithStore(nil, []string{filepath.Join(dir, "test-project")}, "", "")
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	// IndexAll should complete and close ready channel
	go idx.IndexAll(resolver)

	// Wait for indexing to complete (with timeout)
	select {
	case <-idx.ready:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("IndexAll did not complete within timeout")
	}

	// Query should find the indexed document
	results, err := idx.Query("test-project", "convention", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Summary != "Test convention" {
		t.Errorf("summary = %q, want %q", results[0].Summary, "Test convention")
	}
}

// TestIndexAllAfterCloseReadyNoPanic covers the ready-channel double-close
// path: IndexAll must complete without panicking when ready was already
// closed by CloseReady.
func TestIndexAllAfterCloseReadyNoPanic(t *testing.T) {
	idx := newIndexReady(t)
	defer idx.Close()

	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "test-project", ".agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	conventionsPath := filepath.Join(agentsDir, "conventions.yaml")
	if err := os.WriteFile(conventionsPath, []byte(`entries:
- id: conv-001
  summary: Test convention
  detail: Test detail
`), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	resolver, _, err := projects.BuildWithStore(nil, []string{filepath.Join(dir, "test-project")}, "", "")
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	// IndexAll must NOT panic — it should handle the already-closed ready channel.
	done := make(chan struct{})
	go func() {
		defer close(done)
		idx.IndexAll(resolver)
	}()

	select {
	case <-done:
		// success: no panic
	case <-time.After(5 * time.Second):
		t.Fatal("IndexAll did not complete within timeout")
	}

	results, err := idx.Query("test-project", "convention", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
}

func TestBackgroundIndexingIntegration(t *testing.T) {
	dir := t.TempDir()

	// Create index with no data (ready channel should be open)
	idx := newIndex(t)

	// Verify ready is blocking
	select {
	case <-idx.ready:
		t.Fatal("ready should not be closed for empty index")
	default:
		// expected
	}

	// Create knowledge files
	agentsDir := filepath.Join(dir, "project1", ".agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "conventions.yaml"), []byte(`entries:
- id: conv-001
  summary: First convention
  detail: Detail 1
- id: conv-002
  summary: Second convention
  detail: Detail 2
`), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	// Build resolver
	resolver, _, err := projects.BuildWithStore(nil, []string{filepath.Join(dir, "project1")}, "", "")
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	// Start background indexing
	go idx.IndexAll(resolver)

	// WaitReady should block until IndexAll completes
	done := make(chan struct{})
	go func() {
		defer close(done)
		idx.WaitReady()
	}()

	select {
	case <-done:
		// success: WaitReady returned after indexing
	case <-time.After(5 * time.Second):
		t.Fatal("WaitReady did not return within timeout")
	}

	// Verify ready is now closed
	select {
	case <-idx.ready:
		// expected
	default:
		t.Fatal("ready should be closed after IndexAll completes")
	}

	// Query should return the indexed documents
	results, err := idx.Query("project1", "convention", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}

	idx.Close()
}
