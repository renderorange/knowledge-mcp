package install

import (
	"os"
	"strings"
	"testing"
)

const testBin = "/usr/local/bin/knowledge-mcp"

var testFlags = []string{"--root", "/org", "--global", "/global"}

func TestMergeMCPEntryIntoExistingMCP(t *testing.T) {
	existing := []byte("{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  // a comment\n  \"mcp\": {\n    \"codebase-memory-mcp\": { \"command\": [\"/x\"], \"type\": \"local\" }\n  },\n  \"model\": \"a/b\"\n}\n")
	out, changed, err := MergeMCPEntry(existing, testBin, testFlags)
	if err != nil {
		t.Fatalf("MergeMCPEntry() error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	s := string(out)
	for _, want := range []string{
		"$schema", "// a comment", "codebase-memory-mcp", "\"model\": \"a/b\"",
		"// knowledge-mcp:start", "// knowledge-mcp:end",
		"\"command\": [\"/usr/local/bin/knowledge-mcp\", \"--root\", \"/org\", \"--global\", \"/global\"]",
		"\"type\": \"local\"",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q:\n%s", want, s)
		}
	}
}

func TestMergeMCPEntryNoMCPKey(t *testing.T) {
	existing := []byte("{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  \"model\": \"a/b\"\n}\n")
	out, _, err := MergeMCPEntry(existing, testBin, testFlags)
	if err != nil {
		t.Fatalf("MergeMCPEntry() error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "\"mcp\": {") {
		t.Errorf("mcp object not appended:\n%s", s)
	}
	if strings.LastIndex(s, "\"mcp\"") < strings.LastIndex(s, "\"model\"") {
		t.Errorf("mcp should come after existing keys:\n%s", s)
	}
}

func TestMergeMCPEntryReplacesMarkedRegion(t *testing.T) {
	existing := []byte("{\n  \"mcp\": {\n    // knowledge-mcp:start\n    \"knowledge-mcp\": { \"command\": [\"/old/bin\"], \"type\": \"local\" },\n    // knowledge-mcp:end\n    \"other\": { \"command\": [\"/o\"], \"type\": \"local\" }\n  }\n}\n")
	out, changed, err := MergeMCPEntry(existing, testBin, testFlags)
	if err != nil {
		t.Fatalf("MergeMCPEntry() error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	s := string(out)
	if strings.Contains(s, "/old/bin") {
		t.Errorf("old entry not replaced:\n%s", s)
	}
	if strings.Count(s, "knowledge-mcp:start") != 1 || strings.Count(s, "knowledge-mcp:end") != 1 {
		t.Errorf("markers duplicated:\n%s", s)
	}
	if !strings.Contains(s, "\"other\"") {
		t.Errorf("sibling entry lost:\n%s", s)
	}
}

func TestMergeMCPEntryHandWrittenEntryBlocks(t *testing.T) {
	existing := []byte("{\n  \"mcp\": {\n    \"knowledge-mcp\": { \"command\": [\"/manual\"], \"type\": \"local\" }\n  }\n}\n")
	_, _, err := MergeMCPEntry(existing, testBin, testFlags)
	if err == nil {
		t.Fatal("expected error for unmarked hand-written entry")
	}
	if !strings.Contains(err.Error(), "without knowledge-mcp markers") {
		t.Fatalf("error should explain the ownership conflict: %v", err)
	}
}

func TestMergeMCPEntryUnbalancedBraces(t *testing.T) {
	existing := []byte("{ \"mcp\": { }")
	_, _, err := MergeMCPEntry(existing, testBin, testFlags)
	if err == nil {
		t.Fatal("expected sanity error for unbalanced braces")
	}
	if strings.Contains(err.Error(), "without knowledge-mcp markers") {
		t.Fatalf("wrong error: %v", err)
	}
}

func TestMergeMCPEntryEmptyFile(t *testing.T) {
	out, _, err := MergeMCPEntry(nil, testBin, testFlags)
	if err != nil {
		t.Fatalf("MergeMCPEntry() error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "\"$schema\"") || !strings.Contains(s, "\"mcp\"") {
		t.Fatalf("new file should be a full config with mcp:\n%s", s)
	}
}

func TestMergePermissionRuleIntoExistingConfig(t *testing.T) {
	home := os.Getenv("HOME")
	stateDir := home + "/.local/state/knowledge-mcp"
	existing := []byte("{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  \"mcp\": {}\n}\n")
	out, changed, err := MergePermissionRule(existing, stateDir, home)
	if err != nil {
		t.Fatalf("MergePermissionRule() error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	s := string(out)
	for _, want := range []string{
		"// knowledge-mcp:permission:start",
		"// knowledge-mcp:permission:end",
		"\"external_directory\"",
		"\"~/.local/state/knowledge-mcp/**\": \"allow\"",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q:\n%s", want, s)
		}
	}
}

func TestMergePermissionRuleAlreadyPresent(t *testing.T) {
	home := os.Getenv("HOME")
	stateDir := home + "/.local/state/knowledge-mcp"
	existing := []byte("{\n  \"permission\": {\n    // knowledge-mcp:permission:start\n    \"external_directory\": { \"~/.local/state/knowledge-mcp/**\": \"allow\" },\n    // knowledge-mcp:permission:end\n  }\n}\n")
	out, changed, err := MergePermissionRule(existing, stateDir, home)
	if err != nil {
		t.Fatalf("MergePermissionRule() error: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false for identical rule")
	}
	_ = out
}

func TestMergePermissionRuleReplacesMarkedRegion(t *testing.T) {
	home := os.Getenv("HOME")
	newStateDir := home + "/different/state/knowledge-mcp"
	// Seed with the old state dir's rule in the marked region.
	existing := []byte("{\n  \"permission\": {\n    // knowledge-mcp:permission:start\n    \"external_directory\": { \"~/.local/state/knowledge-mcp/**\": \"allow\" },\n    // knowledge-mcp:permission:end\n  }\n}\n")
	out, changed, err := MergePermissionRule(existing, newStateDir, home)
	if err != nil {
		t.Fatalf("MergePermissionRule() error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true for different state dir")
	}
	s := string(out)
	if strings.Contains(s, "~/.local/state/knowledge-mcp/**") {
		t.Errorf("old rule should be gone after replacement:\n%s", s)
	}
	if !strings.Contains(s, "~/different/state/knowledge-mcp/**") {
		t.Errorf("new rule not present:\n%s", s)
	}
	if strings.Count(s, "knowledge-mcp:permission:start") != 1 || strings.Count(s, "knowledge-mcp:permission:end") != 1 {
		t.Errorf("markers duplicated:\n%s", s)
	}
}

func TestMergePermissionRuleNoPermissionKey(t *testing.T) {
	home := os.Getenv("HOME")
	stateDir := home + "/state/km"
	existing := []byte("{\n  \"mcp\": {}\n}\n")
	out, _, err := MergePermissionRule(existing, stateDir, home)
	if err != nil {
		t.Fatalf("MergePermissionRule() error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "\"permission\"") {
		t.Errorf("permission object not added:\n%s", s)
	}
}

func TestMergePermissionRuleEmptyFile(t *testing.T) {
	home := os.Getenv("HOME")
	stateDir := home + "/state/km"
	out, _, err := MergePermissionRule(nil, stateDir, home)
	if err != nil {
		t.Fatalf("MergePermissionRule() error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "\"$schema\"") || !strings.Contains(s, "\"permission\"") {
		t.Errorf("new file should be full config with permission:\n%s", s)
	}
}
