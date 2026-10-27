package knowledge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendOrgSection(t *testing.T) {
	data := "# Title\n\nintro\n"
	got, err := AppendOrgSection(data, "Cost", "cycles block the deadline")
	if err != nil {
		t.Fatalf("AppendOrgSection() error: %v", err)
	}
	if !strings.Contains(got, "# Title\n\nintro\n\n## Cost\n\ncycles block the deadline\n") {
		t.Errorf("AppendOrgSection() = %q", got)
	}
}

func TestAppendOrgSectionEmptyFile(t *testing.T) {
	got, err := AppendOrgSection("", "Cost", "cycles block the deadline")
	if err != nil {
		t.Fatalf("AppendOrgSection() error: %v", err)
	}
	want := "## Cost\n\ncycles block the deadline\n"
	if got != want {
		t.Errorf("AppendOrgSection() = %q, want %q", got, want)
	}
}

func TestAppendOrgSectionExisting(t *testing.T) {
	_, err := AppendOrgSection("## Cost\nold\n", "Cost", "new")
	if !errors.Is(err, ErrOrgSectionExists) {
		t.Fatalf("AppendOrgSection() error = %v, want ErrOrgSectionExists", err)
	}
}

func TestAppendOrgSectionDuplicateHeading(t *testing.T) {
	_, err := AppendOrgSection("## Cost\none\n\n## Cost\ntwo\n", "Cost", "new")
	if !errors.Is(err, ErrOrgDuplicateSection) {
		t.Fatalf("AppendOrgSection() error = %v, want ErrOrgDuplicateSection", err)
	}
}

func TestOrgSectionBody(t *testing.T) {
	data := "# Title\n\nintro\n\n## First\none\n\n### Sub\nsub body\n\n## Second\ntwo\n"

	body, err := OrgSectionBody(data, "First")
	if err != nil {
		t.Fatalf("OrgSectionBody() error: %v", err)
	}
	if body != "one\n\n### Sub\nsub body" {
		t.Errorf("body = %q", body)
	}

	body, err = OrgSectionBody(data, "Second")
	if err != nil {
		t.Fatalf("OrgSectionBody() error: %v", err)
	}
	if body != "two" {
		t.Errorf("body = %q", body)
	}

	if _, err := OrgSectionBody(data, "Missing"); !errors.Is(err, ErrOrgSectionNotFound) {
		t.Fatalf("OrgSectionBody() error = %v, want ErrOrgSectionNotFound", err)
	}
}

func TestReplaceOrgSection(t *testing.T) {
	data := "# Title\n\nintro\n\n## First\none\n\n### Sub\nsub body\n\n## Second\ntwo\n"

	got, err := ReplaceOrgSection(data, "First", "new body\nline two")
	if err != nil {
		t.Fatalf("ReplaceOrgSection() error: %v", err)
	}
	if strings.Contains(got, "one") || strings.Contains(got, "sub body") {
		t.Errorf("old section body survived: %q", got)
	}
	for _, want := range []string{"# Title", "intro", "## First", "new body\nline two", "## Second", "two"} {
		if !strings.Contains(got, want) {
			t.Errorf("ReplaceOrgSection() lost %q: %q", want, got)
		}
	}
}

func TestReplaceOrgSectionLast(t *testing.T) {
	got, err := ReplaceOrgSection("## First\none\n\n## Second\ntwo\n", "Second", "three")
	if err != nil {
		t.Fatalf("ReplaceOrgSection() error: %v", err)
	}
	want := "## First\none\n\n## Second\n\nthree\n"
	if got != want {
		t.Errorf("ReplaceOrgSection() = %q, want %q", got, want)
	}
}

func TestReplaceOrgSectionNotFound(t *testing.T) {
	_, err := ReplaceOrgSection("## First\none\n", "Missing", "new")
	if !errors.Is(err, ErrOrgSectionNotFound) {
		t.Fatalf("ReplaceOrgSection() error = %v, want ErrOrgSectionNotFound", err)
	}
}

func TestReplaceOrgSectionDuplicateHeading(t *testing.T) {
	_, err := ReplaceOrgSection("## Cost\none\n\n## Cost\ntwo\n", "Cost", "new")
	if !errors.Is(err, ErrOrgDuplicateSection) {
		t.Fatalf("ReplaceOrgSection() error = %v, want ErrOrgDuplicateSection", err)
	}
}

func TestSaveText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")

	if err := SaveText(path, "## Heading\n\nbody\n"); err != nil {
		t.Fatalf("SaveText() error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(data) != "## Heading\n\nbody\n" {
		t.Errorf("content = %q", data)
	}

	leftovers, err := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if err != nil {
		t.Fatalf("Glob() error: %v", err)
	}
	if len(leftovers) != 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
}

func TestOrgKnowledgeFilesRegularOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("## A\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.md"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "a.md"), filepath.Join(dir, "link.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing.md"), filepath.Join(dir, "broken.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, filepath.Join(dir, "dirlink.md")); err != nil {
		t.Fatal(err)
	}

	files, err := OrgKnowledgeFiles(dir)
	if err != nil {
		t.Fatalf("OrgKnowledgeFiles() error: %v", err)
	}
	want := []string{"a.md", "link.md"}
	if len(files) != len(want) {
		t.Fatalf("OrgKnowledgeFiles() = %v, want %v", files, want)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("OrgKnowledgeFiles() = %v, want %v", files, want)
		}
	}
}
