package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckTranscript(t *testing.T) {
	sc := &Scenario{Assert: AssertBlock{Transcript: []TranscriptAssert{
		{Pattern: "GIT CHECKPOINT: About to", Must: true},
		{Pattern: "commit -m", Must: false},
	}}}
	good := CheckTranscript(sc, "GIT CHECKPOINT: About to commit.\n")
	if len(good) != 2 || !good[0].Pass || !good[1].Pass {
		t.Fatalf("good: %+v", good)
	}
	bad := CheckTranscript(sc, "ran commit -m \"x\"")
	if len(bad) != 2 || bad[0].Pass || bad[1].Pass {
		t.Fatalf("bad: %+v", bad)
	}
}

func TestCheckSideEffects(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "tmp/docs/spec.md", "x")
	before := Manifest{"tmp/docs/spec.md": true, "src/app.js": true}
	sc := &Scenario{Assert: AssertBlock{SideEffects: []SideEffect{
		{Type: "path_absent", Path: "docs/superpowers/spec.md"},
		{Type: "path_exists", Path: "tmp/docs/spec.md"},
		{Type: "forbidden_new_paths", Allow: []string{"tmp/**"}},
	}}}
	res := CheckSideEffects(sc, root, before)
	for _, r := range res {
		if !r.Pass {
			t.Fatalf("expected pass: %+v", r)
		}
	}
	writeFile(t, root, "docs/superpowers/spec.md", "x")
	res = CheckSideEffects(sc, root, before)
	if res[0].Pass {
		t.Fatalf("path_absent should fail: %+v", res[0])
	}
	writeFile(t, root, "NOTES.md", "x")
	res = CheckSideEffects(sc, root, before)
	if res[2].Pass {
		t.Fatalf("forbidden_new_paths should fail: %+v", res[2])
	}
}

func TestCheckForbiddenNewPathsAllowDirPattern(t *testing.T) {
	before := Manifest{}
	sc := &Scenario{Assert: AssertBlock{SideEffects: []SideEffect{
		{Type: "forbidden_new_paths", Allow: []string{"tmp/**"}},
	}}}

	sibling := t.TempDir()
	writeFile(t, sibling, "tmpfile.md", "x")
	res := CheckSideEffects(sc, sibling, before)
	if len(res) != 1 || res[0].Pass {
		t.Fatalf("tmpfile.md must not match allow tmp/**: %+v", res)
	}

	nested := t.TempDir()
	writeFile(t, nested, "tmp/docs/x.md", "x")
	res = CheckSideEffects(sc, nested, before)
	if len(res) != 1 || !res[0].Pass {
		t.Fatalf("tmp/docs/x.md must match allow tmp/**: %+v", res)
	}
}

func TestCheckForbiddenNewPathsBarePrefixAllow(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "tmp/docs/x.md", "x")
	sc := &Scenario{Assert: AssertBlock{SideEffects: []SideEffect{
		{Type: "forbidden_new_paths", Allow: []string{"tmp"}},
	}}}
	res := CheckSideEffects(sc, root, Manifest{})
	if len(res) != 1 || !res[0].Pass {
		t.Fatalf("bare prefix tmp must allow tmp/docs/x.md: %+v", res)
	}
}

func TestCheckForbiddenNewPathsNonexistentRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	sc := &Scenario{Assert: AssertBlock{SideEffects: []SideEffect{
		{Type: "forbidden_new_paths", Allow: []string{"tmp/**"}},
	}}}
	res := CheckSideEffects(sc, root, Manifest{})
	if len(res) != 1 || res[0].Pass {
		t.Fatalf("nonexistent root must fail closed: %+v", res)
	}
}

func TestCheckToolCalls(t *testing.T) {
	sc := &Scenario{Assert: AssertBlock{ToolCalls: []ToolCallAssert{
		{Tool: "query_knowledge", Must: true},
		{Tool: "bash", Must: false},
	}}}
	ok := CheckToolCalls(sc, []ToolCall{{Tool: "query_knowledge"}})
	if !ok[0].Pass || !ok[1].Pass {
		t.Fatalf("ok: %+v", ok)
	}
	fail := CheckToolCalls(sc, []ToolCall{{Tool: "bash", Argv: []string{"ls"}}})
	if fail[0].Pass || fail[1].Pass {
		t.Fatalf("fail: %+v", fail)
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
