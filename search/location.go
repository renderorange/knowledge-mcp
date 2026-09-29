package search

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Location picks the bleve index path: explicit override, a --store
// default (<store>/.index), legacy per-config locations for single-entry
// configs, or a hashed XDG state dir for multi-entry configs.
func Location(override, store string, roots, projs, canonicalEntries []string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	if store != "" {
		return filepath.Join(store, ".index"), nil
	}
	if len(roots)+len(projs) == 1 {
		single := roots
		if len(single) == 0 {
			single = projs
		}
		return filepath.Join(single[0], ".agents", ".index"), nil
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve state dir: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	sum := sha256.Sum256([]byte(strings.Join(canonicalEntries, "\x00")))
	return filepath.Join(base, "knowledge-mcp", hex.EncodeToString(sum[:8])+".index"), nil
}
