package search

import (
	"path/filepath"
	"testing"
)

func TestIndexCreateAndQuery(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "test.bleve")

	idx, err := NewIndex(indexPath)
	if err != nil {
		t.Fatalf("NewIndex() error: %v", err)
	}
	defer idx.Close()

	// Index some documents
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
			},
		},
		{
			id: "sub-001",
			doc: SearchDocument{
				Summary:    "Reverb uses Schroeder allpass chain",
				Detail:     "The reverb engine uses a Schroeder allpass chain with 2048-sample buffer",
				Category:   "subsystems",
				Confidence: "high",
			},
		},
		{
			id: "dec-001",
			doc: SearchDocument{
				Summary:    "Sag floor 2.0V pins analog sag",
				Detail:     "The sag circuit uses a series resistor with load-ratio model",
				Category:   "decisions",
				Confidence: "medium",
			},
		},
	}

	for _, d := range docs {
		if err := idx.Add(d.id, d.doc); err != nil {
			t.Fatalf("Add(%s) error: %v", d.id, err)
		}
	}

	// Full-text query
	results, err := idx.Query("reverb allpass", "", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Query() returned 0 results, want > 0")
	}
	if results[0].ID != "sub-001" {
		t.Errorf("top result ID = %q, want %q", results[0].ID, "sub-001")
	}

	// Category filter
	results, err = idx.Query("", "conventions", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].ID != "conv-001" {
		t.Errorf("result ID = %q, want %q", results[0].ID, "conv-001")
	}

	// Confidence filter
	results, err = idx.Query("", "", "medium", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].ID != "dec-001" {
		t.Errorf("result ID = %q, want %q", results[0].ID, "dec-001")
	}

	// Combined filter
	results, err = idx.Query("DSP", "conventions", "high", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
}

func TestIndexEmptyQuery(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "test.bleve")

	idx, err := NewIndex(indexPath)
	if err != nil {
		t.Fatalf("NewIndex() error: %v", err)
	}
	defer idx.Close()

	if err := idx.Add("test-001", SearchDocument{
		Summary: "test",
		Detail:  "test detail",
	}); err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	results, err := idx.Query("", "", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("len(results) = %d, want 1", len(results))
	}
}

func TestIndexReopen(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "test.bleve")

	idx, err := NewIndex(indexPath)
	if err != nil {
		t.Fatalf("NewIndex() error: %v", err)
	}

	if err := idx.Add("test-001", SearchDocument{Summary: "persist"}); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	idx.Close()

	// Reopen and verify persistence
	idx2, err := NewIndex(indexPath)
	if err != nil {
		t.Fatalf("NewIndex() reopen error: %v", err)
	}
	defer idx2.Close()

	results, err := idx2.Query("persist", "", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("len(results) = %d, want 1", len(results))
	}
}
