package diag

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/renderorange/knowledge-mcp/internal/update"
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

// makeGitRepo creates a temp git checkout with a tag, returning its dir.
func makeGitRepo(t *testing.T, tag string) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	// A commit is required before a tag can be created.
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "-C", dir, "add", "f")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	if tag != "" {
		cmd = exec.Command("git", "-C", dir, "tag", tag)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git tag: %v\n%s", err, out)
		}
	}
	return dir
}

func TestRunDoctorReportsVersionLocalAndSkew(t *testing.T) {
	dir := makeGitRepo(t, "v1.7.1")
	exe := filepath.Join(dir, "knowledge-mcp")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Point osExecutable at the temp checkout; gitDescribe runs for real
	// against it.
	oldExe := osExecutable
	osExecutable = func() (string, error) { return exe, nil }
	defer func() { osExecutable = oldExe }()

	proj := t.TempDir()
	agents := filepath.Join(proj, ".agents")
	if err := os.MkdirAll(agents, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents, "conventions.yaml"), []byte(healthyYAML), 0644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		code := RunDoctor([]string{"--project", proj, "--timeout", "10s"}, "v1.7.0")
		// Embedded v1.7.0 vs checkout v1.7.1 must be a warn, not a fail.
		if code != 0 {
			t.Errorf("skew must be warn, not fail; code = %d", code)
		}
	})
	if !strings.Contains(out, "version.skew") || !strings.Contains(out, "v1.7.0") || !strings.Contains(out, "v1.7.1") {
		t.Errorf("missing version.skew finding:\n%s", out)
	}
	if !strings.Contains(out, "version.local") {
		t.Errorf("missing version.local finding:\n%s", out)
	}
}

func TestRunDoctorNoSkewWhenVersionsAgree(t *testing.T) {
	dir := makeGitRepo(t, "v1.7.1")
	exe := filepath.Join(dir, "knowledge-mcp")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	oldExe := osExecutable
	osExecutable = func() (string, error) { return exe, nil }
	defer func() { osExecutable = oldExe }()

	proj := t.TempDir()
	agents := filepath.Join(proj, ".agents")
	os.MkdirAll(agents, 0755)
	os.WriteFile(filepath.Join(agents, "conventions.yaml"), []byte(healthyYAML), 0644)

	out := captureStdout(t, func() {
		if code := RunDoctor([]string{"--project", proj, "--timeout", "10s"}, "v1.7.1"); code != 0 {
			t.Errorf("RunDoctor() = %d, want 0", code)
		}
	})
	if strings.Contains(out, "version.skew") {
		t.Errorf("unexpected skew when versions agree:\n%s", out)
	}
}

func TestRunDoctorReportsStoreLoadFailure(t *testing.T) {
	proj := t.TempDir()
	agents := filepath.Join(proj, ".agents")
	if err := os.MkdirAll(agents, 0755); err != nil {
		t.Fatal(err)
	}
	// A deliberately corrupt category file must surface as store.load.
	corrupt := "project: test\nversion: 1\nentries:\n\t- id: conv-001\n      summary: broken\n"
	if err := os.WriteFile(filepath.Join(agents, "conventions.yaml"), []byte(corrupt), 0644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		RunDoctor([]string{"--project", proj, "--timeout", "10s"}, "test")
	})
	if !strings.Contains(out, "store.load") || !strings.Contains(out, "conventions.yaml") {
		t.Errorf("missing store.load finding:\n%s", out)
	}
}

// isolateCache points the update cache at a fresh temp dir, so a real cache
// left by a manual run (or another test) can never satisfy the TTL and skip
// the stubbed network path.
func isolateCache(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
}

func TestRunDoctorCheckUpdatesNoRequestWhenOff(t *testing.T) {
	isolateCache(t)
	oldFn := checkUpdateFn
	checkUpdateFn = func(client update.Client, version string) (string, error) {
		t.Error("checkUpdateFn called without --check-updates")
		return "", nil
	}
	defer func() { checkUpdateFn = oldFn }()

	proj := t.TempDir()
	agents := filepath.Join(proj, ".agents")
	os.MkdirAll(agents, 0755)
	os.WriteFile(filepath.Join(agents, "conventions.yaml"), []byte(healthyYAML), 0644)

	captureStdout(t, func() {
		if code := RunDoctor([]string{"--project", proj, "--timeout", "10s"}, "v1.7.1"); code != 0 {
			t.Errorf("RunDoctor() = %d, want 0", code)
		}
	})
}

func TestRunDoctorCheckUpdatesBehind(t *testing.T) {
	isolateCache(t)
	oldFn := checkUpdateFn
	checkUpdateFn = func(client update.Client, version string) (string, error) {
		return "v1.7.2", nil
	}
	defer func() { checkUpdateFn = oldFn }()

	proj := t.TempDir()
	agents := filepath.Join(proj, ".agents")
	os.MkdirAll(agents, 0755)
	os.WriteFile(filepath.Join(agents, "conventions.yaml"), []byte(healthyYAML), 0644)

	out := captureStdout(t, func() {
		code := RunDoctor([]string{"--project", proj, "--check-updates", "--timeout", "10s"}, "v1.7.1")
		if code != 0 {
			t.Errorf("update.available must be warn, not fail; code = %d", code)
		}
	})
	if !strings.Contains(out, "update.available") || !strings.Contains(out, "v1.7.2") {
		t.Errorf("missing update.available finding:\n%s", out)
	}
}

func TestRunDoctorBriefPrintsOneLineWhenBehind(t *testing.T) {
	isolateCache(t)
	oldFn := checkUpdateFn
	checkUpdateFn = func(client update.Client, version string) (string, error) {
		return "v1.7.2", nil
	}
	defer func() { checkUpdateFn = oldFn }()

	out := captureStdout(t, func() {
		if code := RunDoctor([]string{"--brief", "--check-updates"}, "v1.7.1"); code != 0 {
			t.Errorf("RunDoctor() = %d, want 0", code)
		}
	})
	if !strings.Contains(out, "v1.7.1->v1.7.2") {
		t.Errorf("brief mode did not print the one line:\n%q", out)
	}
}

func TestRunDoctorBriefPrintsNothingWhenCurrent(t *testing.T) {
	isolateCache(t)
	oldFn := checkUpdateFn
	checkUpdateFn = func(client update.Client, version string) (string, error) {
		return "v1.7.1", nil
	}
	defer func() { checkUpdateFn = oldFn }()

	out := captureStdout(t, func() {
		if code := RunDoctor([]string{"--brief", "--check-updates"}, "v1.7.1"); code != 0 {
			t.Errorf("RunDoctor() = %d, want 0", code)
		}
	})
	if strings.TrimSpace(out) != "" {
		t.Errorf("brief mode printed when current:\n%q", out)
	}
}

func TestRunDoctorBriefSilentForDevVersion(t *testing.T) {
	// A plain `go build` embeds "dev" (main.go default). An unknown version
	// is not evidence of being behind, so --brief must stay silent.
	isolateCache(t)
	oldFn := checkUpdateFn
	checkUpdateFn = func(client update.Client, version string) (string, error) {
		return "v1.7.2", nil
	}
	defer func() { checkUpdateFn = oldFn }()

	out := captureStdout(t, func() {
		if code := RunDoctor([]string{"--brief", "--check-updates"}, "dev"); code != 0 {
			t.Errorf("RunDoctor() = %d, want 0", code)
		}
	})
	if strings.TrimSpace(out) != "" {
		t.Errorf("brief mode printed for an unknown version:\n%q", out)
	}
}

func TestIsBehindNumericComparison(t *testing.T) {
	// Segments compare numerically, not as bytes: 1.7.10 > 1.7.9.
	cases := []struct {
		installed, latest string
		want              bool
	}{
		{"v1.7.0", "v1.7.1", true},    // ordinary behind
		{"v1.7.2", "v1.7.1", false},   // ahead is not behind
		{"v1.7.1", "v1.7.1", false},   // equal
		{"v1.7.10", "v1.7.9", false},  // numeric, not byte, comparison
		{"1.7.9", "1.7.10", true},     // no v prefix
		{"dev", "v1.7.2", false},      // unknown version never behind
		{"v1.7.2", "dev", false},      // unknown latest never behind
		{"v1.7", "v1.7.1", true},      // missing segments count as 0
		{"v2.0.0", "v1.99.99", false}, // major wins
	}
	for _, c := range cases {
		if got := isBehind(c.installed, c.latest); got != c.want {
			t.Errorf("isBehind(%q, %q) = %v, want %v", c.installed, c.latest, got, c.want)
		}
	}
}

func TestRunDoctorBriefSilentWhenAhead(t *testing.T) {
	// A binary newer than the cached latest is ahead, never "behind": a
	// stale-yet-fresh cache written by an older binary (learned before this
	// release existed) must not make --brief announce a downgrade. The cache
	// is not trusted for a different binary version, so the outgoing check
	// runs and reports the real (current) latest, and the line stays silent.
	isolateCache(t)
	checked := false
	oldFn := checkUpdateFn
	checkUpdateFn = func(client update.Client, version string) (string, error) {
		checked = true
		return "v1.7.2", nil // the network knows the release this binary is
	}
	defer func() { checkUpdateFn = oldFn }()

	// A cache that is fresh (within TTL) but was written by an *older*
	// binary (v1.7.1) that learned "latest v1.7.1".
	if err := update.WriteCache(update.State{
		BinaryVersion: "v1.7.1",
		LatestTag:     "v1.7.1",
		CheckedAt:     time.Now(),
	}); err != nil {
		t.Fatalf("WriteCache() error: %v", err)
	}

	out := captureStdout(t, func() {
		if code := RunDoctor([]string{"--brief", "--check-updates"}, "v1.7.2"); code != 0 {
			t.Errorf("RunDoctor() = %d, want 0", code)
		}
	})
	if strings.TrimSpace(out) != "" {
		t.Errorf("brief mode announced a downgrade for an ahead install:\n%q", out)
	}
	if !checked {
		t.Error("cache written by an older binary must not be trusted; expected a re-check")
	}
}

func TestRunDoctorBriefUsesFreshCacheForSameVersion(t *testing.T) {
	// A fresh cache written by *this* binary version is honored: no network
	// call, and the cached latest drives the line.
	isolateCache(t)
	oldFn := checkUpdateFn
	checkUpdateFn = func(client update.Client, version string) (string, error) {
		t.Error("same-version fresh cache must not trigger a re-check")
		return "", nil
	}
	defer func() { checkUpdateFn = oldFn }()

	if err := update.WriteCache(update.State{
		BinaryVersion: "v1.7.1",
		LatestTag:     "v1.7.2",
		CheckedAt:     time.Now(),
	}); err != nil {
		t.Fatalf("WriteCache() error: %v", err)
	}

	out := captureStdout(t, func() {
		if code := RunDoctor([]string{"--brief", "--check-updates"}, "v1.7.1"); code != 0 {
			t.Errorf("RunDoctor() = %d, want 0", code)
		}
	})
	if !strings.Contains(out, "v1.7.1->v1.7.2") {
		t.Errorf("cached latest not reported:\n%q", out)
	}
}
