package install

import (
	"os"
	"path/filepath"
)

// ConfigDir returns the knowledge-mcp install/config directory.
// KNM_CONFIG_DIR overrides it (tests, containers); otherwise it follows
// XDG (or the OS user-config dir).
func ConfigDir() string {
	if d := os.Getenv("KNM_CONFIG_DIR"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		if home, herr := os.UserHomeDir(); herr == nil {
			base = filepath.Join(home, ".config")
		} else {
			base = "."
		}
	}
	return filepath.Join(base, "knowledge-mcp")
}

// OpencodeDir returns the opencode config directory (sibling of ConfigDir).
// opencode reads its config from <config-root>/opencode on Linux/macOS.
func OpencodeDir() string {
	return filepath.Join(filepath.Dir(ConfigDir()), "opencode")
}

// OpencodeConfigPath picks the existing opencode config file (jsonc
// preferred), or proposes a new opencode.jsonc path when neither exists.
func OpencodeConfigPath() (string, error) {
	dir := OpencodeDir()
	jsonc := filepath.Join(dir, "opencode.jsonc")
	json := filepath.Join(dir, "opencode.json")
	switch {
	case fileExists(jsonc):
		return jsonc, nil
	case fileExists(json):
		return json, nil
	default:
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
		return jsonc, nil
	}
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
