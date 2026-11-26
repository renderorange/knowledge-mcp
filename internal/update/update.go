// Package update checks whether the installed binary is behind the latest
// GitHub release, with a 24h on-disk cache. It is opt-in (callers decide
// when to check) and read-only from the caller's point of view: the only
// side effect is the cache file under $XDG_CACHE_HOME/knowledge-mcp.
package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ErrNoCache is returned when no usable cache file exists (nothing known yet).
var ErrNoCache = errors.New("no update cache")

// CacheTTL is how long a successful check result stays fresh.
const CacheTTL = 24 * time.Hour

// Client is the minimal network dependency. Tests inject a stub to keep the
// package network-free and to prove no request is made when checking is off.
type Client interface {
	// Do performs one request. Returning a non-2xx status is a warn, not an
	// error, for the caller (unreachable network and non-200 both mean "we
	// asked and got nothing").
	Do(req *http.Request) (*http.Response, error)
}

// State is the persisted cache file.
type State struct {
	BinaryVersion string    `json:"binary_version"`
	LatestTag     string    `json:"latest_tag"`
	CheckedAt     time.Time `json:"checked_at"`
}

// CacheDir returns the knowledge-mcp cache directory ($XDG_CACHE_HOME, else
// the OS user cache dir, else $HOME/.cache). Tests override via XDG_CACHE_HOME.
func CacheDir() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "knowledge-mcp")
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "knowledge-mcp")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "knowledge-mcp")
}

func cachePath() string { return filepath.Join(CacheDir(), "update.json") }

// ReadCache loads the cache file. ok is false when absent, unparseable, or
// stale (older than CacheTTL): in every such case the caller must re-check.
func ReadCache() (State, bool) {
	data, err := os.ReadFile(cachePath())
	if err != nil {
		return State{}, false
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, false
	}
	if time.Since(st.CheckedAt) > CacheTTL {
		return State{}, false
	}
	return st, true
}

// WriteCache persists a check result atomically (temp + rename).
func WriteCache(st State) error {
	if err := os.MkdirAll(CacheDir(), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := cachePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, cachePath())
}

// LatestURL is the GitHub releases/latest endpoint for this repo.
const LatestURL = "https://api.github.com/repos/renderorange/knowledge-mcp/releases/latest"

// RequestTimeout bounds a single network check. The statusline calls this on
// every redraw behind a shell command; a wedged network must not hold the bar.
const RequestTimeout = 3 * time.Second

// Check consults the latest release and returns the newest tag. On a network
// failure or non-200 it returns an error (the caller decides warn vs fail);
// it never returns a fabricated tag. It also refreshes the cache on success.
func Check(client Client, binaryVersion string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, LatestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("check updates: github returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("check updates: read response: %w", err)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", fmt.Errorf("check updates: decode response: %w", err)
	}
	if rel.TagName == "" {
		return "", errors.New("check updates: release has no tag_name")
	}

	// Persist the result so later calls need no network until the TTL lapses.
	if wErr := WriteCache(State{
		BinaryVersion: binaryVersion,
		LatestTag:     rel.TagName,
		CheckedAt:     time.Now(),
	}); wErr != nil {
		// A failed cache write is not a failed check.
		return rel.TagName, nil
	}
	return rel.TagName, nil
}
