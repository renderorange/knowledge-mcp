package knowledge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNextID(t *testing.T) {
	tests := []struct {
		name     string
		entries  []Entry
		prefix   string
		expected string
	}{
		{
			name:     "empty entries",
			entries:  []Entry{},
			prefix:   "conv",
			expected: "conv-001",
		},
		{
			name: "sequential entries",
			entries: []Entry{
				{ID: "conv-001"},
				{ID: "conv-002"},
			},
			prefix:   "conv",
			expected: "conv-003",
		},
		{
			name: "non-sequential entries",
			entries: []Entry{
				{ID: "conv-001"},
				{ID: "conv-005"},
			},
			prefix:   "conv",
			expected: "conv-006",
		},
		{
			name: "mixed prefixes ignored",
			entries: []Entry{
				{ID: "conv-001"},
				{ID: "sub-001"},
				{ID: "dec-001"},
			},
			prefix:   "conv",
			expected: "conv-002",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NextID(tt.entries, tt.prefix)
			if result != tt.expected {
				t.Errorf("NextID() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestLoadSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")

	kf := &KnowledgeFile{
		Project: "test-project",
		Version: 1,
		Entries: []Entry{
			{
				ID:      "conv-001",
				Summary: "Test entry",
				Detail:  "Some detail",
				Source:  "test",
				Date:    "2026-09-04",
			},
		},
	}

	if err := Save(path, kf); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if loaded.Project != kf.Project {
		t.Errorf("Project = %q, want %q", loaded.Project, kf.Project)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("len(Entries) = %d, want 1", len(loaded.Entries))
	}
	if loaded.Entries[0].ID != "conv-001" {
		t.Errorf("Entry ID = %q, want %q", loaded.Entries[0].ID, "conv-001")
	}
}

func TestLoadOrCreate(t *testing.T) {
	dir := t.TempDir()

	// Non-existent file should create empty
	kf, err := LoadOrCreate(filepath.Join(dir, "missing.yaml"), "proj")
	if err != nil {
		t.Fatalf("LoadOrCreate() error: %v", err)
	}
	if kf.Project != "proj" {
		t.Errorf("Project = %q, want %q", kf.Project, "proj")
	}
	if len(kf.Entries) != 0 {
		t.Errorf("len(Entries) = %d, want 0", len(kf.Entries))
	}

	// Existing file should load
	kf.Entries = append(kf.Entries, Entry{ID: "conv-001", Summary: "test"})
	if err := Save(filepath.Join(dir, "exists.yaml"), kf); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	loaded, err := LoadOrCreate(filepath.Join(dir, "exists.yaml"), "proj")
	if err != nil {
		t.Fatalf("LoadOrCreate() error: %v", err)
	}
	if len(loaded.Entries) != 1 {
		t.Errorf("len(Entries) = %d, want 1", len(loaded.Entries))
	}
}

func TestValidationHelpers(t *testing.T) {
	if !IsValidCategory("conventions") {
		t.Error("conventions should be valid")
	}
	if IsValidCategory("invalid") {
		t.Error("invalid should not be valid")
	}
	if CategoryPrefix("conventions") != "conv" {
		t.Errorf("prefix = %q, want %q", CategoryPrefix("conventions"), "conv")
	}
}

func TestEnsureDir(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".agents")

	if err := EnsureDir(agentsDir); err != nil {
		t.Fatalf("EnsureDir() error: %v", err)
	}

	info, err := os.Stat(agentsDir)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}

	// Calling again should not error
	if err := EnsureDir(agentsDir); err != nil {
		t.Fatalf("EnsureDir() second call error: %v", err)
	}
}
