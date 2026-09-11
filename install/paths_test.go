package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDirEnvOverride(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	t.Setenv("KNM_CONFIG_DIR", dir)
	if got := ConfigDir(); got != dir {
		t.Fatalf("ConfigDir() = %q, want %q", got, dir)
	}
	if got := OpencodeDir(); got != filepath.Join(filepath.Dir(dir), "opencode") {
		t.Fatalf("OpencodeDir() = %q, want %q", got, filepath.Join(filepath.Dir(dir), "opencode"))
	}
}

func TestConfigDirDefault(t *testing.T) {
	t.Setenv("KNM_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	t.Setenv("HOME", "/home/u")
	if got := ConfigDir(); got != filepath.Join("/xdg", "knowledge-mcp") {
		t.Fatalf("ConfigDir() = %q, want %q", got, filepath.Join("/xdg", "knowledge-mcp"))
	}
	if got := OpencodeDir(); got != filepath.Join("/xdg", "opencode") {
		t.Fatalf("OpencodeDir() = %q, want %q", got, filepath.Join("/xdg", "opencode"))
	}
}

func TestOpencodeConfigPathPreference(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("KNM_CONFIG_DIR", filepath.Join(cfg, "knowledge-mcp"))
	opencodeDir := OpencodeDir()

	got, err := OpencodeConfigPath()
	if err != nil {
		t.Fatalf("OpencodeConfigPath() error: %v", err)
	}
	if got != filepath.Join(opencodeDir, "opencode.jsonc") {
		t.Fatalf("OpencodeConfigPath() = %q, want jsonc proposal", got)
	}

	if err := os.MkdirAll(opencodeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opencodeDir, "opencode.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err = OpencodeConfigPath()
	if err != nil {
		t.Fatalf("OpencodeConfigPath() error: %v", err)
	}
	if got != filepath.Join(opencodeDir, "opencode.json") {
		t.Fatalf("OpencodeConfigPath() = %q, want json", got)
	}

	if err := os.WriteFile(filepath.Join(opencodeDir, "opencode.jsonc"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	got, _ = OpencodeConfigPath()
	if got != filepath.Join(opencodeDir, "opencode.jsonc") {
		t.Fatalf("OpencodeConfigPath() = %q, want jsonc", got)
	}
}

func TestRecordRoundTrip(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("KNM_CONFIG_DIR", filepath.Join(cfg, "knowledge-mcp"))
	rec := Record{
		Version: "v1.0.0", BinPath: "/bin/k", Roots: []string{"/r1", "/r2"},
		Projects: []string{"/p"}, Global: "/g", Index: "/i",
		Artifacts: []Artifact{{Path: "/a", Kind: "skill"}},
	}
	if err := SaveRecord(rec); err != nil {
		t.Fatalf("SaveRecord() error: %v", err)
	}
	loaded, ok := LoadRecord()
	if !ok {
		t.Fatal("LoadRecord() not ok")
	}
	if loaded.Version != rec.Version || len(loaded.Roots) != 2 || loaded.Artifacts[0].Kind != "skill" {
		t.Fatalf("round trip mismatch: %+v", loaded)
	}

	t.Setenv("KNM_CONFIG_DIR", filepath.Join(t.TempDir(), "none"))
	if _, ok := LoadRecord(); ok {
		t.Fatal("LoadRecord() ok for missing record")
	}
}