package diag

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
	// 60 entries across three files (30/20/10): big enough that the
	// background IndexAll is still adding when RunDoctor finishes its
	// report and hits teardown, so a Close/IndexAll race surfaces as
	// stderr warnings instead of winning the scheduling race. A single
	// entry is indexed before teardown and proves nothing.
	for _, f := range []struct {
		cat, prefix string
		n           int
	}{
		{"conventions", "conv", 30},
		{"decisions", "dec", 20},
		{"subsystems", "sub", 10},
	} {
		path := filepath.Join(agents, f.cat+".yaml")
		if err := os.WriteFile(path, []byte(knowledgeYAML(f.prefix, f.n)), 0644); err != nil {
			t.Fatal(err)
		}
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
	var logBuf syncBuffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)
	out := captureStdout(t, func() {
		code := RunDoctor([]string{"--project", proj, "--timeout", "10s"}, "test")
		if code != 0 {
			t.Errorf("RunDoctor() = %d, want 0", code)
		}
	})
	// Drain the leaked IndexAll writer before the test returns (bounded):
	// the warm-path leak keeps it in flight past RunDoctor, and if
	// t.TempDir() cleanup deletes its files under it, bleve emits
	// persist-err warnings and can panic (zapx nil-map on the failed
	// segment), taking the whole test binary down. A silent writer has
	// no completion signal in the log, so poll its goroutine frame.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && indexAllRunning() {
		time.Sleep(10 * time.Millisecond)
	}
	// Then let the leaked index's background persistence quiesce: scorch
	// persister/merger goroutines outlive IndexAll, and a late .zap write
	// racing t.TempDir() RemoveAll fails cleanup with "directory not
	// empty". 200ms of index-tree quiet, bounded at 2s.
	indexPath := filepath.Join(agents, ".index")
	qDeadline := time.Now().Add(2 * time.Second)
	prev := treeMTime(indexPath)
	quietSince := time.Now()
	for time.Now().Before(qDeadline) {
		time.Sleep(50 * time.Millisecond)
		cur := treeMTime(indexPath)
		if cur.After(prev) {
			prev = cur
			quietSince = time.Now()
			continue
		}
		if time.Since(quietSince) >= 200*time.Millisecond {
			break
		}
	}
	stderr := logBuf.String()
	var tolerated []string
	failed := false
	if strings.Contains(stderr, "index is closed") {
		t.Errorf("stderr contains %q: index closed while IndexAll in flight", "index is closed")
		failed = true
	}
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.Contains(line, "failed to index") {
			continue
		}
		if strings.Contains(line, "persist err:") {
			// The one tolerated class: leaked writer racing t.TempDir()
			// cleanup (this or a prior run's leaked goroutine). Never
			// tolerate index is closed or a non-persist failure.
			tolerated = append(tolerated, line)
			continue
		}
		t.Errorf("unexpected index failure on stderr: %s", line)
		failed = true
	}
	if failed || len(tolerated) > 0 {
		t.Logf("captured stderr (%d tolerated persist-err lines):\n%s", len(tolerated), stderr)
	}
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

// knowledgeYAML renders n wrapper-form knowledge entries under the given
// id prefix (a bare YAML list loads as zero entries).
func knowledgeYAML(prefix string, n int) string {
	var b strings.Builder
	b.WriteString("project: test\nversion: 1\nentries:\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "    - id: %s-%03d\n      summary: warm entry %s-%03d\n      detail: body %d\n", prefix, i, prefix, i, i)
	}
	return b.String()
}

// syncBuffer is a goroutine-safe log sink: the leaked IndexAll writer may
// log while the test polls and reads the captured stderr.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// indexAllRunning reports whether any goroutine is still inside
// search.(*Index).IndexAll — the leaked warm-path writer.
func indexAllRunning() bool {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return strings.Contains(string(buf[:n]), "search.(*Index).IndexAll")
		}
		buf = make([]byte, 2*len(buf))
	}
}
