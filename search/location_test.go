package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocationOverride(t *testing.T) {
	p, err := Location("/custom/index", "", []string{}, []string{}, nil)
	if err != nil {
		t.Fatalf("Location() error: %v", err)
	}
	if p != "/custom/index" {
		t.Errorf("location = %q, want %q", p, "/custom/index")
	}
}

func TestLocationLegacySingleProject(t *testing.T) {
	p, err := Location("", "", []string{}, []string{"/proj"}, nil)
	if err != nil {
		t.Fatalf("Location() error: %v", err)
	}
	if want := filepath.Join("/proj", ".agents", ".index"); p != want {
		t.Errorf("location = %q, want %q", p, want)
	}
}

func TestLocationLegacySingleRoot(t *testing.T) {
	p, err := Location("", "", []string{"/root"}, []string{}, nil)
	if err != nil {
		t.Fatalf("Location() error: %v", err)
	}
	if want := filepath.Join("/root", ".agents", ".index"); p != want {
		t.Errorf("location = %q, want %q", p, want)
	}
}

func TestLocationMultiEntryStateDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	entries := []string{"/rootB", "/rootA", "/projC"}
	p, err := Location("", "", []string{"/rootB", "/rootA"}, []string{"/projC"}, entries)
	if err != nil {
		t.Fatalf("Location() error: %v", err)
	}
	if !strings.Contains(p, "knowledge-mcp") {
		t.Errorf("location = %q, want under knowledge-mcp state dir", p)
	}

	// Same entries -> same location (deterministic).
	p2, err := Location("", "", []string{"/rootA", "/rootB"}, []string{"/projC"}, entries)
	if err != nil {
		t.Fatalf("Location() error: %v", err)
	}
	if p != p2 {
		t.Errorf("location not deterministic: %q vs %q", p, p2)
	}

	// Different entries -> different location.
	entries2 := []string{"/rootB", "/rootA", "/projD"}
	p3, err := Location("", "", []string{"/rootB", "/rootA"}, []string{"/projD"}, entries2)
	if err != nil {
		t.Fatalf("Location() error: %v", err)
	}
	if p == p3 {
		t.Errorf("different entries should hash to different locations: %q", p)
	}
}

func TestLocationXDGDefault(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	p, err := Location("", "", []string{"/a", "/b"}, []string{}, []string{"/a", "/b"})
	if err != nil {
		t.Fatalf("Location() error: %v", err)
	}
	if want := filepath.Join(home, ".local", "state", "knowledge-mcp"); !strings.HasPrefix(p, want) {
		t.Errorf("location = %q, want under %q", p, want)
	}
}

func TestLocationStoreDefault(t *testing.T) {
	p, err := Location("", "/store", []string{"/root"}, []string{}, nil)
	if err != nil {
		t.Fatalf("Location() error: %v", err)
	}
	if want := filepath.Join("/store", ".index"); p != want {
		t.Errorf("location = %q, want %q", p, want)
	}
}

func TestLocationStoreMultiEntry(t *testing.T) {
	p, err := Location("", "/store", []string{"/a", "/b"}, []string{"/c"}, []string{"/a", "/b", "/c"})
	if err != nil {
		t.Fatalf("Location() error: %v", err)
	}
	if want := filepath.Join("/store", ".index"); p != want {
		t.Errorf("location = %q, want %q", p, want)
	}
}

func TestLocationOverrideBeatsStore(t *testing.T) {
	p, err := Location("/custom/index", "/store", []string{}, []string{}, nil)
	if err != nil {
		t.Fatalf("Location() error: %v", err)
	}
	if p != "/custom/index" {
		t.Errorf("location = %q, want %q", p, "/custom/index")
	}
}
