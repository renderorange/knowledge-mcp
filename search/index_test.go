package search

import (
	"os"
	"path/filepath"
	"testing"
)

func newIndex(t *testing.T, path string, names []string) *Index {
	t.Helper()
	idx, err := NewIndex(path, names)
	if err != nil {
		t.Fatalf("NewIndex() error: %v", err)
	}
	return idx
}

func TestIndexCreateAndQuery(t *testing.T) {
	dir := t.TempDir()
	idx := newIndex(t, filepath.Join(dir, "test.bleve"), []string{"test"})
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
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "test.bleve")
	idx := newIndex(t, indexPath, []string{"alpha", "beta"})
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

func TestIndexReopen(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "test.bleve")

	idx := newIndex(t, indexPath, []string{"test"})
	if err := idx.Add("test/conv-001", SearchDocument{Summary: "persist", Project: "test"}); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	idx.Close()

	idx2 := newIndex(t, indexPath, []string{"test"})
	defer idx2.Close()

	results, err := idx2.Query("test", "persist", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("len(results) = %d, want 1", len(results))
	}
}

func TestIndexRebuildOnNameSetChange(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "test.bleve")

	idx := newIndex(t, indexPath, []string{"alpha"})
	if err := idx.Add("alpha/conv-001", SearchDocument{Summary: "alpha doc", Project: "alpha"}); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	idx.Close()

	// Same name set: no rebuild, doc persists.
	idx2 := newIndex(t, indexPath, []string{"alpha"})
	results, err := idx2.Query("alpha", "", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1 (index was needlessly rebuilt)", len(results))
	}
	idx2.Close()

	// Different name set: rebuild, old docs gone.
	idx3 := newIndex(t, indexPath, []string{"alpha", "beta"})
	defer idx3.Close()
	results, err = idx3.Query("alpha", "", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("len(results) = %d, want 0 (index should have been rebuilt)", len(results))
	}
}

func TestIndexEmptyQuery(t *testing.T) {
	dir := t.TempDir()
	idx := newIndex(t, filepath.Join(dir, "test.bleve"), []string{"test"})
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
	dir := t.TempDir()
	idx := newIndex(t, filepath.Join(dir, "test.bleve"), []string{"test"})
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

func TestIndexRecoverFromCorruption(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "test.bleve")

	// Create index and add a document
	idx := newIndex(t, indexPath, []string{"test"})
	if err := idx.Add("test/conv-001", SearchDocument{
		Summary: "original doc", Project: "test",
	}); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	idx.Close()

	// Corrupt the index by deleting a segment file
	storeDir := filepath.Join(indexPath, "store")
	entries, err := os.ReadDir(storeDir)
	if err != nil {
		t.Fatalf("ReadDir(%s) error: %v", storeDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("no files in store directory")
	}
	// Delete the first segment file to corrupt the index
	for _, e := range entries {
		if e.Name() != "root.bolt" {
			os.Remove(filepath.Join(storeDir, e.Name()))
			break
		}
	}

	// Opening should recover from corruption, not fail
	idx2 := newIndex(t, indexPath, []string{"test"})
	defer idx2.Close()

	// Old data is gone (rebuilt), but server should work
	results, err := idx2.Query("test", "", "", 10)
	if err != nil {
		t.Fatalf("Query() after recovery error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0 (old data should be gone after rebuild)", len(results))
	}

	// Can add new data after recovery
	if err := idx2.Add("test/conv-002", SearchDocument{
		Summary: "new doc after recovery", Project: "test",
	}); err != nil {
		t.Fatalf("Add() after recovery error: %v", err)
	}
	results, err = idx2.Query("test", "recovery", "", 10)
	if err != nil {
		t.Fatalf("Query() after recovery error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("len(results) = %d, want 1", len(results))
	}
}

func TestIndexRecoverFromSilentCorruption(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "test.bleve")

	// Create index and add a document
	idx := newIndex(t, indexPath, []string{"test"})
	if err := idx.Add("test/conv-001", SearchDocument{
		Summary: "original doc", Project: "test",
	}); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	idx.Close()

	// Corrupt root.bolt by truncating it — this simulates the case
	// where the file is partially written or has stale pages that
	// allow Open() to succeed but break internal operations.
	boltPath := filepath.Join(indexPath, "store", "root.bolt")
	info, err := os.Stat(boltPath)
	if err != nil {
		t.Fatalf("Stat(%s) error: %v", boltPath, err)
	}
	if err := os.Truncate(boltPath, info.Size()/2); err != nil {
		t.Fatalf("Truncate() error: %v", err)
	}

	// Opening should detect the unhealthy index and rebuild
	idx2 := newIndex(t, indexPath, []string{"test"})
	defer idx2.Close()

	// Old data is gone (rebuilt), but server should work
	results, err := idx2.Query("test", "", "", 10)
	if err != nil {
		t.Fatalf("Query() after recovery error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0 (old data should be gone after rebuild)", len(results))
	}

	// Can add new data after recovery
	if err := idx2.Add("test/conv-003", SearchDocument{
		Summary: "new doc after silent recovery", Project: "test",
	}); err != nil {
		t.Fatalf("Add() after recovery error: %v", err)
	}
	results, err = idx2.Query("test", "silent", "", 10)
	if err != nil {
		t.Fatalf("Query() after recovery error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("len(results) = %d, want 1", len(results))
	}
}
