package diag

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/renderorange/knowledge-mcp/search"
)

func TestWatchdogBlocked(t *testing.T) {
	err := watchdog(50*time.Millisecond, func() error {
		<-make(chan struct{})
		return nil
	})
	if err != ErrQueryBlocked {
		t.Errorf("watchdog() = %v, want ErrQueryBlocked", err)
	}
}

func TestWatchdogPassThrough(t *testing.T) {
	err := watchdog(time.Second, func() error { return nil })
	if err != nil {
		t.Errorf("watchdog() = %v, want nil", err)
	}
}

func TestRunDoctorEmptyArgsFails(t *testing.T) {
	code := RunDoctor([]string{}, "test")
	if code != 1 {
		t.Errorf("RunDoctor() = %d, want 1", code)
	}
}

func TestRunDoctorHealthyFixture(t *testing.T) {
	proj := t.TempDir()
	agents := filepath.Join(proj, ".agents")
	if err := os.MkdirAll(agents, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "conventions.yaml"), []byte("project: test\nversion: 1\nentries:\n    - id: conv-001\n      summary: test entry\n      detail: body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		code := RunDoctor([]string{"--project", proj, "--timeout", "10s"}, "test")
		if code != 0 {
			t.Errorf("RunDoctor() = %d, want 0", code)
		}
	})
	for _, want := range []string{"== Resolution ==", "== Startup trail ==", "== Environment ==", "findings:", "query.blocked"} {
		if want == "query.blocked" {
			if strings.Contains(out, want) {
				t.Errorf("unexpected blocking finding: %s", out)
			}
			continue
		}
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestRunDoctorUnresolvedPathFails(t *testing.T) {
	out := captureStdout(t, func() {
		code := RunDoctor([]string{"--project", filepath.Join(t.TempDir(), "does-not-exist")}, "test")
		if code != 1 {
			t.Errorf("RunDoctor() = %d, want 1", code)
		}
	})
	if !strings.Contains(out, "fail") {
		t.Errorf("missing fail finding:\n%s", out)
	}
}

func TestRunDoctorSanitizesNothingBodies(t *testing.T) {
	proj := t.TempDir()
	agents := filepath.Join(proj, ".agents")
	os.MkdirAll(agents, 0755)
	os.WriteFile(filepath.Join(agents, "decisions.yaml"), []byte("project: test\nversion: 1\nentries:\n    - id: dec-001\n      summary: s\n      detail: MEGASECRET\n"), 0644)
	out := captureStdout(t, func() {
		RunDoctor([]string{"--project", proj, "--timeout", "10s"}, "test")
	})
	if strings.Contains(out, "MEGASECRET") {
		t.Errorf("body leaked into doctor report:\n%s", out)
	}
}

func TestRunDoctorEmptyEntriesStaysFresh(t *testing.T) {
	name := "freshproj"
	proj := filepath.Join(t.TempDir(), name)
	agents := filepath.Join(proj, ".agents")
	if err := os.MkdirAll(agents, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "conventions.yaml"), []byte("project: test\nversion: 1\nentries: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		code := RunDoctor([]string{"--project", proj, "--timeout", "10s"}, "test")
		if code != 0 {
			t.Errorf("RunDoctor() = %d, want 0", code)
		}
	})
	if !strings.Contains(out, "index freshness: fresh") {
		t.Errorf("missing fresh line in output:\n%s", out)
	}
	if strings.Contains(out, "index.stale") || strings.Contains(out, "STALE") {
		t.Errorf("unexpected stale finding on empty store:\n%s", out)
	}
}

func TestRunDoctorWarmIndex(t *testing.T) {
	name := "warmproj"
	proj := filepath.Join(t.TempDir(), name)
	agents := filepath.Join(proj, ".agents")
	if err := os.MkdirAll(agents, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "conventions.yaml"), []byte("project: test\nversion: 1\nentries:\n    - id: conv-001\n      summary: test entry\n      detail: body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	idx, err := search.NewIndex(filepath.Join(agents, ".index"), []string{name})
	if err != nil {
		t.Fatalf("pre-populate warm index: %v", err)
	}
	if err := idx.Add(name+"/conv-001", search.SearchDocument{Summary: "test entry", Detail: "body", Category: "conventions", Project: name}); err != nil {
		t.Fatalf("pre-populate add: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("pre-populate close: %v", err)
	}
	out := captureStdout(t, func() {
		code := RunDoctor([]string{"--project", proj, "--timeout", "10s"}, "test")
		if code != 0 {
			t.Errorf("RunDoctor() = %d, want 0", code)
		}
	})
	trail := strings.Index(out, "== Startup trail ==")
	kick := strings.Index(out, "index.kick done")
	query := strings.Index(out, "self-query")
	env := strings.Index(out, "== Environment ==")
	findings := strings.Index(out, "findings:")
	if trail < 0 || kick < 0 || query < 0 || env < 0 || findings < 0 {
		t.Fatalf("missing markers in output:\n%s", out)
	}
	if !(trail < kick && kick < query && kick < env && kick < findings) {
		t.Errorf("index.kick done out of order (trail=%d kick=%d query=%d env=%d findings=%d):\n%s", trail, kick, query, env, findings, out)
	}
	if strings.Contains(out, "index.stale") || strings.Contains(out, "STALE") {
		t.Errorf("unexpected stale finding on warm store:\n%s", out)
	}
}

func TestRunDoctorIndexLocked(t *testing.T) {
	name := "lockproj"
	proj := filepath.Join(t.TempDir(), name)
	agents := filepath.Join(proj, ".agents")
	if err := os.MkdirAll(agents, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "conventions.yaml"), []byte("project: test\nversion: 1\nentries:\n    - id: conv-001\n      summary: test entry\n      detail: body\n"), 0644); err != nil {
		t.Fatal(err)
	}
	holder, err := search.NewIndex(filepath.Join(agents, ".index"), []string{name})
	if err != nil {
		t.Fatalf("hold index lock: %v", err)
	}
	t.Cleanup(func() { holder.Close() })
	out := captureStdout(t, func() {
		code := RunDoctor([]string{"--project", proj, "--timeout", "500ms"}, "test")
		if code != 1 {
			t.Errorf("RunDoctor() = %d, want 1", code)
		}
	})
	if !strings.Contains(out, "fail") || !strings.Contains(out, "index.locked") {
		t.Errorf("missing fail index.locked finding:\n%s", out)
	}
}

func TestRunDoctorNoIndexOnStartupWarnsQueryBlocked(t *testing.T) {
	name := "warnproj"
	proj := filepath.Join(t.TempDir(), name)
	agents := filepath.Join(proj, ".agents")
	if err := os.MkdirAll(agents, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "conventions.yaml"), []byte("project: test\nversion: 1\nentries: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		code := RunDoctor([]string{"--project", proj, "--no-index-on-startup", "--timeout", "300ms"}, "test")
		if code != 0 {
			t.Errorf("RunDoctor() = %d, want 0", code)
		}
	})
	for _, want := range []string{"warn query.blocked reason=no-index-on-startup", "findings:"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

// captureStdout runs f while capturing os.Stdout (RunDoctor writes the
// report there).
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	f()
	w.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
