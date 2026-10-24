package search

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
)

// saveEntries writes a category file directly, bypassing Index.Add. This is
// the out-of-band edit (git checkout, migration script, manual edit) that
// query_knowledge must notice.
func saveEntries(t *testing.T, agentsDir, category string, entries ...knowledge.Entry) {
	t.Helper()
	if err := knowledge.EnsureDir(agentsDir); err != nil {
		t.Fatalf("EnsureDir() error: %v", err)
	}
	kf := &knowledge.KnowledgeFile{Project: "test", Version: 1, Entries: entries}
	if err := knowledge.Save(knowledge.CategoryFilePath(agentsDir, category), kf); err != nil {
		t.Fatalf("knowledge.Save() error: %v", err)
	}
}

func testEntry(id, summary, detail string) knowledge.Entry {
	return knowledge.Entry{
		ID: id, Summary: summary, Detail: detail, Source: "test", Date: "2026-10-23",
	}
}

func buildProjectIndex(t *testing.T, proj string) *Index {
	t.Helper()
	resolver, _, err := projects.BuildWithStore(nil, []string{proj}, "", "")
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}
	idx := newIndex(t)
	idx.IndexAll(resolver)
	return idx
}

func TestQueryRefreshesAfterDirectEdit(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "refreshproj")
	agents := filepath.Join(proj, ".agents")
	first := testEntry("sub-001", "Storage backend", "RustFS keeps objects")
	saveEntries(t, agents, "subsystems", first)

	idx := buildProjectIndex(t, proj)
	defer idx.Close()

	results, err := idx.Query("refreshproj", "RustFS", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	// Direct edit: add a second entry without calling Index.Add.
	second := testEntry("sub-002", "Trash sweep", "SweepTrash purges stale rows")
	saveEntries(t, agents, "subsystems", first, second)

	results, err = idx.Query("refreshproj", "SweepTrash", "", 10)
	if err != nil {
		t.Fatalf("Query() after direct edit error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) after direct edit = %d, want 1", len(results))
	}
	if results[0].ID != "sub-002" {
		t.Errorf("ID = %q, want %q", results[0].ID, "sub-002")
	}
}

func TestQueryDropsRemovedEntry(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "deleteproj")
	agents := filepath.Join(proj, ".agents")
	keep := testEntry("sub-001", "Storage backend", "RustFS keeps objects")
	remove := testEntry("sub-002", "Trash sweep", "SweepTrash purges stale rows")
	saveEntries(t, agents, "subsystems", keep, remove)

	idx := buildProjectIndex(t, proj)
	defer idx.Close()

	results, err := idx.Query("deleteproj", "SweepTrash", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	// Delete the entry by rewriting the file directly.
	saveEntries(t, agents, "subsystems", keep)

	results, err = idx.Query("deleteproj", "SweepTrash", "", 10)
	if err != nil {
		t.Fatalf("Query() after delete error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("len(results) after delete = %d, want 0; stale doc still indexed: %+v", len(results), results)
	}
}

func TestRefreshReportsWhetherStoreChanged(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "refreshflag")
	agents := filepath.Join(proj, ".agents")
	first := testEntry("sub-001", "Storage backend", "RustFS keeps objects")
	saveEntries(t, agents, "subsystems", first)

	idx := buildProjectIndex(t, proj)
	defer idx.Close()

	changed, err := idx.Refresh()
	if err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	if changed {
		t.Error("Refresh() reported a change for an unchanged store")
	}

	saveEntries(t, agents, "subsystems", first, testEntry("sub-002", "Trash sweep", "SweepTrash"))

	changed, err = idx.Refresh()
	if err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	if !changed {
		t.Error("Refresh() did not report a change after a direct edit")
	}

	changed, err = idx.Refresh()
	if err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	if changed {
		t.Error("Refresh() reported a second change with no intervening edit")
	}
}

func TestRebuildRefreshesIndex(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "rebuildproj")
	agents := filepath.Join(proj, ".agents")
	first := testEntry("sub-001", "Storage backend", "RustFS keeps objects")
	saveEntries(t, agents, "subsystems", first)

	idx := buildProjectIndex(t, proj)
	defer idx.Close()

	saveEntries(t, agents, "subsystems", first, testEntry("sub-002", "Trash sweep", "SweepTrash"))

	if err := idx.Rebuild(); err != nil {
		t.Fatalf("Rebuild() error: %v", err)
	}

	count, err := idx.DocCount()
	if err != nil {
		t.Fatalf("DocCount() error: %v", err)
	}
	if count != 2 {
		t.Errorf("DocCount() = %d, want 2", count)
	}
}

func TestQueryConcurrentWithRefresh(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "raceproj")
	agents := filepath.Join(proj, ".agents")
	base := testEntry("sub-001", "Storage backend", "RustFS keeps objects")
	saveEntries(t, agents, "subsystems", base)

	idx := buildProjectIndex(t, proj)
	defer idx.Close()

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 25; n++ {
				if _, err := idx.Query("raceproj", "RustFS", "", 10); err != nil {
					errs <- err
					return
				}
			}
		}()
	}

	entries := []knowledge.Entry{base}
	for n := 0; n < 10; n++ {
		entries = append(entries, testEntry(fmt.Sprintf("sub-%03d", n+2), fmt.Sprintf("Filler %d", n), "filler detail"))
		saveEntries(t, agents, "subsystems", entries...)
		if _, err := idx.Refresh(); err != nil {
			t.Fatalf("Refresh() error: %v", err)
		}
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Query() error: %v", err)
	}
}

func TestIndexOrgKnowledgeIndexesAllMarkdown(t *testing.T) {
	root := t.TempDir()
	knowledgeDir := filepath.Join(root, ".agents", "knowledge")
	if err := knowledge.EnsureDir(knowledgeDir); err != nil {
		t.Fatalf("EnsureDir() error: %v", err)
	}
	files := map[string]string{
		"architecture.md": "## Build\nmake build\n",
		"review.md":       "## Overview\nchaon-review\n",
		"notes.md":        "## Takedown\npurge_requested_at drives takedown\n",
		"ignored.txt":     "## Not Markdown\nSweepTrash\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(knowledgeDir, name), []byte(body), 0644); err != nil {
			t.Fatalf("WriteFile(%s) error: %v", name, err)
		}
	}

	resolver, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	rootName := filepath.Base(root)

	idx := newIndex(t)
	defer idx.Close()
	idx.IndexAll(resolver)

	results, err := idx.Query(rootName, "takedown", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1 (notes.md section)", len(results))
	}
	if want := "org-notes.md::Takedown"; results[0].ID != want {
		t.Errorf("ID = %q, want %q", results[0].ID, want)
	}

	results, err = idx.Query(rootName, "SweepTrash", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("len(results) = %d for non-markdown file, want 0", len(results))
	}
}
