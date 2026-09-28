package search

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/renderorange/knowledge-mcp/projects"
)

// syncBuffer is a goroutine-safe log sink: IndexAll logs from its own
// goroutine while the test reads the captured output.
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

// runIndexAllCapture writes the given files under <dir>/test-project/.agents/,
// runs IndexAll to completion, and returns the captured log output plus the
// index for follow-up queries. Caller closes the index.
func runIndexAllCapture(t *testing.T, dir string, files map[string]string) (string, *Index) {
	t.Helper()

	agentsDir := filepath.Join(dir, "test-project", ".agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(agentsDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile(%s) error: %v", name, err)
		}
	}

	resolver, _, err := projects.BuildWithStore(nil, []string{filepath.Join(dir, "test-project")}, "", "")
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	idx := newIndex(t, filepath.Join(dir, "test.bleve"), []string{"test-project"})

	var logBuf syncBuffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	go idx.IndexAll(resolver)
	select {
	case <-idx.ready:
	case <-time.After(5 * time.Second):
		idx.Close()
		t.Fatal("IndexAll did not complete within timeout")
	}

	return logBuf.String(), idx
}

func TestIndexAllMissingCategoryFilesDoNotWarn(t *testing.T) {
	dir := t.TempDir()
	logged, idx := runIndexAllCapture(t, dir, map[string]string{
		"conventions.yaml": "project: test-project\nversion: 1\nentries:\n  - id: conv-001\n    summary: Test convention\n    detail: Test detail\n",
	})
	defer idx.Close()

	if strings.Contains(logged, "failed to load knowledge file") {
		t.Errorf("log contains %q for missing optional category files:\n%s", "failed to load knowledge file", logged)
	}

	results, err := idx.Query("test-project", "convention", "", 10)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1 (conventions.yaml should still be indexed)", len(results))
	}
}

func TestIndexAllInvalidYAMLWarns(t *testing.T) {
	dir := t.TempDir()
	logged, idx := runIndexAllCapture(t, dir, map[string]string{
		"conventions.yaml": "project: test-project\nversion: 1\nentries:\n\t- id: conv-001\n",
	})
	defer idx.Close()

	want := "failed to load knowledge file"
	if n := strings.Count(logged, want); n != 1 {
		t.Errorf("log contains %d %q warnings, want exactly 1 (corrupt file only):\n%s", n, want, logged)
	}
	if !strings.Contains(logged, "conventions.yaml") {
		t.Errorf("log does not mention conventions.yaml:\n%s", logged)
	}
}
