package search

import (
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
				Summary:    "DSP interface pattern: PedalParams struct",
				Detail:     "Every pedal exposes pedal_init, pedal_set_params, pedal_process",
				Category:   "conventions",
				Confidence: "high",
				Project:    "test",
			},
		},
		{
			id: "sub-001",
			doc: SearchDocument{
				Summary:    "Reverb uses Schroeder allpass chain",
				Detail:     "The reverb engine uses a Schroeder allpass chain with 2048-sample buffer",
				Category:   "subsystems",
				Confidence: "high",
				Project:    "test",
			},
		},
		{
			id: "dec-001",
			doc: SearchDocument{
				Summary:    "Sag floor 2.0V pins analog sag",
				Detail:     "The sag circuit uses a series resistor with load-ratio model",
				Category:   "decisions",
				Confidence: "medium",
				Project:    "test",
			},
		},
	}

	for _, d := range docs {
		if err := idx.Add(d.id, d.doc); err != nil {
			t.Fatalf("Add(%s) error: %v", d.id, err)
		}
	}

	results, err := idx.Query("test", "reverb allpass", "", "", 10)
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

	results, err = idx.Query("test", "", "conventions", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].ID != "conv-001" {
		t.Errorf("result ID = %q, want %q", results[0].ID, "conv-001")
	}

	results, err = idx.Query("test", "", "", "medium", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].ID != "dec-001" {
		t.Errorf("result ID = %q, want %q", results[0].ID, "dec-001")
	}

	results, err = idx.Query("test", "DSP", "conventions", "high", 10)
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
		Category: "conventions", Confidence: "high", Project: "alpha",
	}); err != nil {
		t.Fatalf("Add(alpha) error: %v", err)
	}
	if err := idx.Add("beta/conv-001", SearchDocument{
		Summary: "beta convention", Detail: "beta detail",
		Category: "conventions", Confidence: "high", Project: "beta",
	}); err != nil {
		t.Fatalf("Add(beta) error: %v", err)
	}

	alphaResults, err := idx.Query("alpha", "", "", "", 10)
	if err != nil {
		t.Fatalf("Query(alpha) error: %v", err)
	}
	if len(alphaResults) != 1 || alphaResults[0].Summary != "alpha convention" {
		t.Fatalf("alpha query = %+v, want exactly the alpha doc", alphaResults)
	}

	betaResults, err := idx.Query("beta", "", "", "", 10)
	if err != nil {
		t.Fatalf("Query(beta) error: %v", err)
	}
	if len(betaResults) != 1 || betaResults[0].Summary != "beta convention" {
		t.Fatalf("beta query = %+v, want exactly the beta doc", betaResults)
	}

	// Unfiltered query sees both docs (no data loss from key collision).
	all, err := idx.Query("", "", "", "", 10)
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

	results, err := idx2.Query("test", "persist", "", "", 10)
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
	results, err := idx2.Query("alpha", "", "", "", 10)
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
	results, err = idx3.Query("alpha", "", "", "", 10)
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

	results, err := idx.Query("test", "", "", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("len(results) = %d, want 1", len(results))
	}
}
