package update

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubClient returns canned responses without the network and counts calls.
type stubClient struct {
	status int
	body   string
	calls  int
	err    error
}

func (s *stubClient) Do(req *http.Request) (*http.Response, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Header:     make(http.Header),
	}, nil
}

func TestCheckHitsNetworkAndCaches(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	c := &stubClient{status: 200, body: `{"tag_name":"v1.7.2"}`}
	tag, err := Check(c, "v1.7.1")
	if err != nil {
		t.Fatalf("Check() error: %v", err)
	}
	if tag != "v1.7.2" {
		t.Errorf("Check() tag = %q, want v1.7.2", tag)
	}
	if c.calls != 1 {
		t.Errorf("Check() made %d network calls, want 1", c.calls)
	}

	// Cache is on disk and fresh.
	st, ok := ReadCache()
	if !ok {
		t.Fatal("cache not readable after Check()")
	}
	if st.LatestTag != "v1.7.2" || st.BinaryVersion != "v1.7.1" {
		t.Errorf("cache = %+v, want latest v1.7.2 binary v1.7.1", st)
	}
	if time.Since(st.CheckedAt) > 5*time.Minute {
		t.Errorf("CheckedAt not recent: %v", st.CheckedAt)
	}
}

func TestCheckNoRequestWhenOff(t *testing.T) {
	// The package API has no "off" switch: the caller simply never calls
	// Check. This pins that reading a cache never touches the network, and
	// that the absence of the file is a cache miss, not a request.
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	if _, ok := ReadCache(); ok {
		t.Fatal("cache reported fresh with no file")
	}
	// No client was even constructed, so no request could have been made.
}

func TestCacheHonoursTTL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	// A fresh cache within TTL is honoured.
	if err := WriteCache(State{BinaryVersion: "v1.7.1", LatestTag: "v1.7.1", CheckedAt: time.Now()}); err != nil {
		t.Fatalf("WriteCache() error: %v", err)
	}
	if _, ok := ReadCache(); !ok {
		t.Error("fresh cache not honoured")
	}

	// A stale cache (older than TTL) is a miss.
	if err := WriteCache(State{BinaryVersion: "v1.7.1", LatestTag: "v1.7.1", CheckedAt: time.Now().Add(-25 * time.Hour)}); err != nil {
		t.Fatalf("WriteCache() stale error: %v", err)
	}
	if _, ok := ReadCache(); ok {
		t.Error("stale cache honoured")
	}
}

func TestCheckNetworkFailureIsErrorNotFabrication(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	c := &stubClient{err: os.ErrDeadlineExceeded}
	if _, err := Check(c, "v1.7.1"); err == nil {
		t.Fatal("Check() with network failure returned no error")
	}
	// A failed check must not leave a fresh cache file.
	if _, ok := ReadCache(); ok {
		t.Error("failed check left a fresh cache")
	}
}

func TestCheckNon200IsErrorWarnNotTag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	c := &stubClient{status: 500, body: `{"message":"boom"}`}
	if _, err := Check(c, "v1.7.1"); err == nil {
		t.Fatal("Check() with non-200 returned no error")
	}
	if _, ok := ReadCache(); ok {
		t.Error("non-200 check left a fresh cache")
	}
}

func TestCachePathUsesXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	if got := CacheDir(); got != filepath.Join(dir, "knowledge-mcp") {
		t.Errorf("CacheDir() = %q, want %q", got, filepath.Join(dir, "knowledge-mcp"))
	}
}
