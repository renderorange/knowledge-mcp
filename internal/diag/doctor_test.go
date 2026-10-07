package diag

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

const healthyYAML = "project: test\nversion: 1\nentries:\n    - id: conv-001\n      summary: test entry\n      detail: body\n"

func TestRunDoctorHealthyFixture(t *testing.T) {
	proj := t.TempDir()
	agents := filepath.Join(proj, ".agents")
	if err := os.MkdirAll(agents, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "conventions.yaml"), []byte(healthyYAML), 0644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		code := RunDoctor([]string{"--project", proj, "--timeout", "10s"}, "test")
		if code != 0 {
			t.Errorf("RunDoctor() = %d, want 0", code)
		}
	})
	for _, want := range []string{"== Resolution ==", "== Startup trail ==", "== Environment ==", "findings:"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "query.blocked") {
		t.Errorf("unexpected blocking finding: %s", out)
	}
	// The ordering assertion pins synchronous print order: the kick line is
	// printed before the self-query is issued. The ready-channel gate itself
	// is proven by the absence of a query.blocked finding on the real ready
	// channel (Query blocks on <-i.ready).
	trail := strings.Index(out, "== Startup trail ==")
	kick := strings.Index(out, "index.kick started")
	query := strings.Index(out, "self-query")
	if trail < 0 || kick < 0 || query < 0 {
		t.Fatalf("missing trail/kick/self-query markers in output:\n%s", out)
	}
	if !(trail < kick && kick < query) {
		t.Errorf("index.kick started out of order (trail=%d kick=%d query=%d):\n%s", trail, kick, query, out)
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

func TestRunDoctorIndexFlagIgnored(t *testing.T) {
	proj := t.TempDir()
	agents := filepath.Join(proj, ".agents")
	if err := os.MkdirAll(agents, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "conventions.yaml"), []byte(healthyYAML), 0644); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(proj, "should-not-exist.index")
	out := captureStdout(t, func() {
		code := RunDoctor([]string{"--project", proj, "--index", indexPath, "--timeout", "10s"}, "test")
		if code != 0 {
			t.Errorf("RunDoctor() = %d, want 0", code)
		}
	})
	if !strings.Contains(out, "--index is deprecated") {
		t.Errorf("missing --index deprecation warning:\n%s", out)
	}
	if _, err := os.Stat(indexPath); !os.IsNotExist(err) {
		t.Errorf("--index path was created despite being ignored: %v", err)
	}
}

func TestRunDoctorNoIndexOnStartupIgnored(t *testing.T) {
	proj := t.TempDir()
	agents := filepath.Join(proj, ".agents")
	if err := os.MkdirAll(agents, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "conventions.yaml"), []byte(healthyYAML), 0644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		code := RunDoctor([]string{"--project", proj, "--no-index-on-startup", "--timeout", "10s"}, "test")
		if code != 0 {
			t.Errorf("RunDoctor() = %d, want 0", code)
		}
	})
	for _, want := range []string{"--no-index-on-startup is deprecated", "index.kick started"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "query.blocked") {
		t.Errorf("unexpected blocked finding:\n%s", out)
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
