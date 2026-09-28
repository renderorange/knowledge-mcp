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
