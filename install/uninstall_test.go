package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunUninstallRemovesEverything(t *testing.T) {
	_, opencodeDir := setupConfig(t)
	agentsPath := filepath.Join(opencodeDir, "AGENTS.md")
	writeAgents(t, opencodeDir, "# Keep me\n\nsome rules\n")

	root := t.TempDir()
	binPath := filepath.Join(t.TempDir(), "bin", "knowledge-mcp")
	if err := os.MkdirAll(filepath.Dir(binPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("fake binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := runInstall(installConfig{Roots: []string{root}, Version: "v1", BinPath: binPath}); err != nil {
		t.Fatalf("setup install error: %v", err)
	}

	reports, err := runUninstall(true)
	if err != nil {
		t.Fatalf("runUninstall() error: %v", err)
	}
	if len(reports) == 0 {
		t.Fatal("no reports")
	}

	for _, gone := range []string{
		filepath.Join(opencodeDir, "skills", "knowledge-mcp", "SKILL.md"),
		filepath.Join(opencodeDir, "skills", "knowledge-mcp"),
		filepath.Join(opencodeDir, "plugins", "knowledge-mcp.ts"),
		filepath.Join(ConfigDir(), "install.json"),
		binPath,
	} {
		if _, statErr := os.Stat(gone); statErr == nil {
			t.Errorf("artifact still present: %s", gone)
		}
	}

	agents, _ := os.ReadFile(agentsPath)
	agentsStr := string(agents)
	if strings.Contains(agentsStr, "knowledge-mcp:start") {
		t.Error("AGENTS.md markers not stripped")
	}
	if !strings.Contains(agentsStr, "# Keep me") {
		t.Error("user AGENTS.md content lost on uninstall")
	}

	cfgPath, _ := OpencodeConfigPath()
	cfgData, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(cfgData), "knowledge-mcp:start") {
		t.Error("opencode config markers not stripped")
	}
}

func TestRunUninstallSkipsMissingMarkers(t *testing.T) {
	_, opencodeDir := setupConfig(t)
	agentsPath := filepath.Join(opencodeDir, "AGENTS.md")
	writeAgents(t, opencodeDir, "# plain\n")

	root := t.TempDir()
	if _, err := runInstall(installConfig{Roots: []string{root}, Version: "v", BinPath: "/bin/k"}); err != nil {
		t.Fatalf("setup install error: %v", err)
	}
	// Simulate the user deleting the block themselves.
	if err := os.WriteFile(agentsPath, []byte("# plain\n"), 0644); err != nil {
		t.Fatal(err)
	}

	reports, err := runUninstall(false)
	if err != nil {
		t.Fatalf("runUninstall() error: %v", err)
	}
	found := false
	for _, r := range reports {
		if strings.Contains(r, "skip") && strings.Contains(r, "AGENTS.md") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected skip report for AGENTS.md, got %v", reports)
	}
	agents, _ := os.ReadFile(agentsPath)
	if string(agents) != "# plain\n" {
		t.Errorf("AGENTS.md altered without markers: %q", string(agents))
	}
}

func TestRunUninstallNoRecord(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("KNM_CONFIG_DIR", filepath.Join(cfg, "knowledge-mcp"))
	if _, err := runUninstall(false); err == nil {
		t.Fatal("expected error when no record exists")
	} else if !strings.Contains(err.Error(), "knowledge-mcp") {
		t.Fatalf("error should name the missing record: %v", err)
	}
}
