package hook

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
)

func seedEntry(t *testing.T, dir, cat string, entries ...knowledge.Entry) {
	t.Helper()
	if err := knowledge.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	kf := &knowledge.KnowledgeFile{Project: "p", Version: 1, Entries: entries}
	if err := knowledge.Save(knowledge.CategoryFilePath(dir, cat), kf); err != nil {
		t.Fatal(err)
	}
}

func lookupRef(t *testing.T, res *projects.Resolver, path string) *projects.Ref {
	t.Helper()
	ref, ok := res.RefForPath(path)
	if !ok {
		t.Fatalf("RefForPath(%s) not ok", path)
	}
	return &ref
}

func TestSearchProjectHits(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	seedEntry(t, filepath.Join(proj, ".agents"), "conventions",
		knowledge.Entry{ID: "conv-001", Summary: "Tmp directory rules", Detail: "All project docs go in ./tmp/docs/ only", Source: "user", Date: knowledge.Today()},
		knowledge.Entry{ID: "conv-002", Summary: "Unrelated", Detail: "nothing here", Source: "user", Date: knowledge.Today()},
	)
	res, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	hits := Search(res, lookupRef(t, res, proj), nil, "tmp directory", 3)
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1 (%+v)", len(hits), hits)
	}
	if hits[0].ID != "conv-001" || hits[0].Address != "proj" {
		t.Errorf("hit = %+v", hits[0])
	}
}

func TestSearchWholePatternSubstring(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	seedEntry(t, filepath.Join(proj, ".agents"), "decisions",
		knowledge.Entry{ID: "dec-001", Summary: "Session compression", Detail: "Use sessioncompress for compaction", Source: "test", Date: knowledge.Today()},
	)
	res, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	hits := Search(res, lookupRef(t, res, proj), nil, "sessioncompress", 3)
	if len(hits) != 1 || hits[0].ID != "dec-001" {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestSearchIncludesOrgAndGlobal(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	seedEntry(t, filepath.Join(proj, ".agents"), "conventions",
		knowledge.Entry{ID: "conv-001", Summary: "Project one", Detail: "compression project", Source: "t", Date: knowledge.Today()},
	)
	orgKnowledge := filepath.Join(root, ".agents", "knowledge")
	if err := os.MkdirAll(orgKnowledge, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orgKnowledge, "architecture.md"),
		[]byte("## Compression\norg-level compression notes\n"), 0644); err != nil {
		t.Fatal(err)
	}
	globalDir := filepath.Join(t.TempDir(), "global")
	seedEntry(t, filepath.Join(globalDir, ".agents"), "conventions",
		knowledge.Entry{ID: "conv-009", Summary: "Global compression rule", Detail: "handle compression globally", Source: "t", Date: knowledge.Today()},
	)
	res, _, err := projects.Build([]string{root}, nil, globalDir)
	if err != nil {
		t.Fatal(err)
	}
	hits := Search(res, lookupRef(t, res, proj), orgRef(t, res), "compression", 5)
	var addresses []string
	for _, h := range hits {
		addresses = append(addresses, h.Address)
	}
	found := func(addr string) bool {
		for _, a := range addresses {
			if a == addr {
				return true
			}
		}
		return false
	}
	if !found("proj") || !found("_global") {
		t.Errorf("hits missing sources: %+v", hits)
	}
}

func orgRef(t *testing.T, res *projects.Resolver) *projects.Ref {
	t.Helper()
	for _, ref := range res.Snapshot() {
		if ref.Kind == projects.KindOrg {
			return &ref
		}
	}
	t.Fatal("no org ref")
	return nil
}

func TestSearchRankingAndLimit(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	seedEntry(t, filepath.Join(proj, ".agents"), "conventions",
		knowledge.Entry{ID: "conv-001", Summary: "Alpha", Detail: "zilch zilch zilch", Source: "t", Date: knowledge.Today()},
		knowledge.Entry{ID: "conv-002", Summary: "Beta", Detail: "zilch zilch", Source: "t", Date: knowledge.Today()},
		knowledge.Entry{ID: "conv-003", Summary: "Gamma", Detail: "zilch", Source: "t", Date: knowledge.Today()},
		knowledge.Entry{ID: "conv-004", Summary: "Delta", Detail: "unrelated", Source: "t", Date: knowledge.Today()},
	)
	res, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	hits := Search(res, lookupRef(t, res, proj), nil, "zilch", 2)
	if len(hits) != 2 {
		t.Fatalf("len(hits) = %d, want 2", len(hits))
	}
	if hits[0].ID != "conv-001" || hits[1].ID != "conv-002" {
		t.Errorf("ranking wrong: %+v", hits)
	}
}

func TestResolveCwdMatrix(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	sub := filepath.Join(proj, "deep", "dir")
	other := t.TempDir()
	globalDir := filepath.Join(t.TempDir(), "global")
	for _, d := range []string{sub, other, filepath.Join(root, ".agents", "knowledge"), filepath.Join(globalDir, ".agents")} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	res, _, err := projects.Build([]string{root}, nil, globalDir)
	if err != nil {
		t.Fatal(err)
	}

	pRef, oRef := resolveCwd(res, proj)
	if pRef == nil || pRef.Address != "proj" {
		t.Errorf("exact project: got %+v", pRef)
	}
	if oRef == nil || oRef.Kind != projects.KindOrg {
		t.Errorf("exact project: expected containing org ref, got %+v", oRef)
	}

	pRef, oRef = resolveCwd(res, sub)
	if pRef == nil || pRef.Address != "proj" {
		t.Errorf("nested under project: got %+v", pRef)
	}

	pRef, _ = resolveCwd(res, other)
	if pRef != nil {
		t.Errorf("outside roots: got %+v", pRef)
	}

	pRef, oRef = resolveCwd(res, root)
	if pRef != nil || oRef == nil || oRef.Kind != projects.KindOrg {
		t.Errorf("org root cwd: got pRef=%+v oRef=%+v", pRef, oRef)
	}
}
