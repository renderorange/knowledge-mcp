package install

import (
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
