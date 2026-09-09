package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitSections(t *testing.T) {
	data := "# Title\n\nintro text\n\n## First Section\nbody one\n\n### Sub Heading\nsub body\n\n## Second Section\nbody two\n"
	sections := splitSections(data)

	want := [][2]string{
		{"Overview", "# Title\n\nintro text"},
		{"First Section", "## First Section\nbody one\n\n### Sub Heading\nsub body"},
		{"Second Section", "## Second Section\nbody two"},
	}
	if len(sections) != len(want) {
		t.Fatalf("len(sections) = %d, want %d (%#v)", len(sections), len(want), sections)
	}
	for i, w := range want {
		if sections[i][0] != w[0] || sections[i][1] != w[1] {
			t.Errorf("section[%d] = %q/%q, want %q/%q", i, sections[i][0], sections[i][1], w[0], w[1])
		}
	}
}

func TestSplitSectionsNoPreamble(t *testing.T) {
	data := "## Only Heading\nbody\n"
	sections := splitSections(data)
	if len(sections) != 1 {
		t.Fatalf("len(sections) = %d, want 1", len(sections))
	}
	if sections[0][0] != "Only Heading" || sections[0][1] != "## Only Heading\nbody" {
		t.Errorf("got %#v", sections)
	}
}

func TestPathListSet(t *testing.T) {
	var p pathList
	if err := p.Set("/a"); err != nil {
		t.Fatalf("Set(/a) error: %v", err)
	}
	if err := p.Set("/b"); err != nil {
		t.Fatalf("Set(/b) error: %v", err)
	}
	if len(p) != 2 || p[0] != "/a" || p[1] != "/b" {
		t.Errorf("pathList = %v, want [/a /b]", []string(p))
	}
	if err := p.Set(""); err == nil {
		t.Error("Set(\"\") should error")
	}
	if p.String() != "/a,/b" {
		t.Errorf("String() = %q, want %q", p.String(), "/a,/b")
	}
}

func TestIndexLocationOverride(t *testing.T) {
	p, err := indexLocation("/custom/index", pathList{}, pathList{}, nil)
	if err != nil {
		t.Fatalf("indexLocation() error: %v", err)
	}
	if p != "/custom/index" {
		t.Errorf("location = %q, want %q", p, "/custom/index")
	}
}

func TestIndexLocationLegacySingleProject(t *testing.T) {
	p, err := indexLocation("", pathList{}, pathList{"/proj"}, nil)
	if err != nil {
		t.Fatalf("indexLocation() error: %v", err)
	}
	if want := filepath.Join("/proj", ".agents", ".index"); p != want {
		t.Errorf("location = %q, want %q", p, want)
	}
}

func TestIndexLocationLegacySingleRoot(t *testing.T) {
	p, err := indexLocation("", pathList{"/root"}, pathList{}, nil)
	if err != nil {
		t.Fatalf("indexLocation() error: %v", err)
	}
	if want := filepath.Join("/root", ".agents", ".index"); p != want {
		t.Errorf("location = %q, want %q", p, want)
	}
}

func TestIndexLocationMultiEntryStateDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	entries := []string{"/rootB", "/rootA", "/projC"}
	p, err := indexLocation("", pathList{"/rootB", "/rootA"}, pathList{"/projC"}, entries)
	if err != nil {
		t.Fatalf("indexLocation() error: %v", err)
	}
	if !strings.Contains(p, "knowledge-mcp") {
		t.Errorf("location = %q, want under knowledge-mcp state dir", p)
	}

	// Same entries -> same location (deterministic).
	p2, err := indexLocation("", pathList{"/rootA", "/rootB"}, pathList{"/projC"}, entries)
	if err != nil {
		t.Fatalf("indexLocation() error: %v", err)
	}
	if p != p2 {
		t.Errorf("location not deterministic: %q vs %q", p, p2)
	}

	// Different entries -> different location.
	entries2 := []string{"/rootB", "/rootA", "/projD"}
	p3, err := indexLocation("", pathList{"/rootB", "/rootA"}, pathList{"/projD"}, entries2)
	if err != nil {
		t.Fatalf("indexLocation() error: %v", err)
	}
	if p == p3 {
		t.Errorf("different entries should hash to different locations: %q", p)
	}
}

func TestIndexLocationXDGDefault(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	p, err := indexLocation("", pathList{"/a", "/b"}, pathList{}, []string{"/a", "/b"})
	if err != nil {
		t.Fatalf("indexLocation() error: %v", err)
	}
	if want := filepath.Join(home, ".local", "state", "knowledge-mcp"); !strings.HasPrefix(p, want) {
		t.Errorf("location = %q, want under %q", p, want)
	}
}
