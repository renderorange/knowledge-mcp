package hook

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/renderorange/knowledge-mcp/install"
	"github.com/renderorange/knowledge-mcp/knowledge"
)

func TestRunGolden(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	if err := knowledge.EnsureDir(filepath.Join(proj, ".agents")); err != nil {
		t.Fatal(err)
	}
	kf := &knowledge.KnowledgeFile{Project: "proj", Version: 1, Entries: []knowledge.Entry{
		{ID: "conv-001", Summary: "Tmp directory rules", Detail: "docs in ./tmp/docs", Source: "u", Date: knowledge.Today()},
	}}
	if err := knowledge.Save(knowledge.CategoryFilePath(filepath.Join(proj, ".agents"), "conventions"), kf); err != nil {
		t.Fatal(err)
	}

	cfgRoot := t.TempDir()
	t.Setenv("KNM_CONFIG_DIR", filepath.Join(cfgRoot, "knowledge-mcp"))
	t.Setenv("KNM_HOOK_CWD", proj)
	if err := install.SaveRecord(install.Record{Roots: []string{root}, Version: "v", BinPath: "/x"}); err != nil {
		t.Fatal(err)
	}

	in := bytes.NewBufferString(`{"hook_event_name":"PostToolUse","tool_name":"Grep","tool_input":{"pattern":"tmp directory"}}`)
	var out bytes.Buffer
	if err := Run(in, &out, nil); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "conv-001") || !strings.Contains(s, "query_knowledge") {
		t.Fatalf("output missing hit:\n%s", s)
	}
}

func TestRunNoRecordSilent(t *testing.T) {
	t.Setenv("KNM_CONFIG_DIR", filepath.Join(t.TempDir(), "knowledge-mcp"))
	t.Setenv("KNM_HOOK_CWD", t.TempDir())
	in := bytes.NewBufferString(`{"hook_event_name":"PostToolUse","tool_name":"Grep","tool_input":{"pattern":"x"}}`)
	var out bytes.Buffer
	if err := Run(in, &out, nil); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected empty output, got:\n%s", out.String())
	}
}

func TestRunNoPatternSilent(t *testing.T) {
	t.Setenv("KNM_CONFIG_DIR", filepath.Join(t.TempDir(), "knowledge-mcp"))
	in := bytes.NewBufferString(`{"hook_event_name":"PostToolUse","tool_name":"Grep","tool_input":{}}`)
	var out bytes.Buffer
	if err := Run(in, &out, nil); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected empty output, got %q", out.String())
	}
}
