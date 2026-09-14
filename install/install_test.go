package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupConfig(t *testing.T) (string, string) {
	t.Helper()
	cfgRoot := t.TempDir()
	opencodeDir := filepath.Join(cfgRoot, "opencode")
	t.Setenv("KNM_CONFIG_DIR", filepath.Join(cfgRoot, "knowledge-mcp"))
	return cfgRoot, opencodeDir
}

func writeAgents(t *testing.T, opencodeDir, content string) {
	t.Helper()
	if err := os.MkdirAll(opencodeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opencodeDir, "AGENTS.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRunInstallWritesAllArtifacts(t *testing.T) {
	_, opencodeDir := setupConfig(t)
	writeAgents(t, opencodeDir, "# Existing\n\nuser rules\n")

	root := t.TempDir()
	global := t.TempDir()
	cfg := installConfig{
		Roots: []string{root}, Global: global,
		Version: "v1.2.3", BinPath: filepath.Join(t.TempDir(), "knowledge-mcp"),
	}
	reports, err := runInstall(cfg)
	if err != nil {
		t.Fatalf("runInstall() error: %v", err)
	}
	if len(reports) == 0 {
		t.Fatal("no reports")
	}

	for _, want := range []string{
		filepath.Join(opencodeDir, "skills", "knowledge-mcp", "SKILL.md"),
		filepath.Join(opencodeDir, "plugins", "knowledge-mcp.ts"),
	} {
		if _, statErr := os.Stat(want); statErr != nil {
			t.Errorf("artifact missing: %s (%v)", want, statErr)
		}
	}
	agents, _ := os.ReadFile(filepath.Join(opencodeDir, "AGENTS.md"))
	if !strings.Contains(string(agents), "knowledge-mcp:start") {
		t.Error("AGENTS.md block not upserted")
	}
	if !strings.HasPrefix(string(agents), "# Existing") {
		t.Error("user AGENTS.md content lost")
	}
	cfgPath, _ := OpencodeConfigPath()
	cfgData, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(cfgData), "\"type\": \"local\"") {
		t.Errorf("opencode config entry missing:\n%s", cfgData)
	}
	if !strings.Contains(string(cfgData), "--root") {
		t.Errorf("flags missing from config entry:\n%s", cfgData)
	}
	rec, ok := LoadRecord()
	if !ok {
		t.Fatal("record not saved")
	}
	if rec.Version != "v1.2.3" || len(rec.Artifacts) != 4 {
		t.Errorf("record mismatch: %+v", rec)
	}
	if len(rec.Roots) != 1 || rec.Roots[0] != root {
		t.Errorf("record roots mismatch: %+v", rec)
	}
}

func TestRunInstallIdempotent(t *testing.T) {
	_, opencodeDir := setupConfig(t)
	writeAgents(t, opencodeDir, "# Existing\n")
	root := t.TempDir()

	run2 := func() {
		t.Helper()
		if _, err := runInstall(installConfig{
			Roots: []string{root}, Version: "v1", BinPath: "/bin/k",
		}); err != nil {
			t.Fatalf("runInstall() error: %v", err)
		}
	}
	run2()

	snapshot := func() map[string]string {
		out := map[string]string{}
		cfgPath, _ := OpencodeConfigPath()
		for _, p := range []string{
			cfgPath,
			filepath.Join(opencodeDir, "AGENTS.md"),
			filepath.Join(ConfigDir(), "install.json"),
			filepath.Join(opencodeDir, "skills", "knowledge-mcp", "SKILL.md"),
			filepath.Join(opencodeDir, "plugins", "knowledge-mcp.ts"),
		} {
			b, _ := os.ReadFile(p)
			out[p] = string(b)
		}
		return out
	}
	before := snapshot()
	run2()
	after := snapshot()
	for p, want := range before {
		if after[p] != want {
			t.Errorf("non-idempotent artifact %s\n--- before ---\n%s\n--- after ---\n%s", p, want, after[p])
		}
	}
}

func TestRunInstallRejectsMissingRoot(t *testing.T) {
	setupConfig(t)
	cfg := installConfig{Roots: []string{filepath.Join(t.TempDir(), "does-not-exist")}, Version: "v", BinPath: "/bin/k"}
	if _, err := runInstall(cfg); err == nil {
		t.Fatal("expected error for nonexistent root")
	}
}

func TestRunInstallWritesPermissionRule(t *testing.T) {
	_, opencodeDir := setupConfig(t)
	writeAgents(t, opencodeDir, "# Existing\n")
	root := t.TempDir()

	if _, err := runInstall(installConfig{
		Roots: []string{root}, Version: "v1", BinPath: "/bin/k",
	}); err != nil {
		t.Fatalf("runInstall() error: %v", err)
	}

	cfgPath, _ := OpencodeConfigPath()
	cfgData, _ := os.ReadFile(cfgPath)
	s := string(cfgData)
	for _, want := range []string{
		"// knowledge-mcp:permission:start",
		"\"external_directory\"",
		"\"~/.local/state/knowledge-mcp/**\": \"allow\"",
		"// knowledge-mcp:permission:end",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("opencode config missing %q:\n%s", want, s)
		}
	}
}

func TestRunInstallRequiresFlags(t *testing.T) {
	setupConfig(t)
	if _, err := runInstall(installConfig{Version: "v", BinPath: "/bin/k"}); err == nil {
		t.Fatal("expected error when no roots/projects/global given")
	}
}
