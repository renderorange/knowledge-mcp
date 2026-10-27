package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
)

// newOrgFixture builds an org root with .agents/knowledge/ and the given
// markdown files, then resolves it as an org ref.
func newOrgFixture(t *testing.T, files map[string]string) (*projects.Resolver, string, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".agents", "knowledge")
	if err := knowledge.EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir() error: %v", err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile(%s) error: %v", name, err)
		}
	}
	res, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}
	return res, filepath.Base(root), dir
}

func orgRefFor(t *testing.T, res *projects.Resolver) projects.Ref {
	t.Helper()
	for _, ref := range res.Snapshot() {
		if ref.Kind == projects.KindOrg {
			return ref
		}
	}
	t.Fatal("no org ref in resolver")
	return projects.Ref{}
}

func TestWriteHandlerOrgSection(t *testing.T) {
	res, org, dir := newOrgFixture(t, map[string]string{
		"architecture.md": "## Build\nmake test\n",
	})

	idx := newIndexReady(t)
	defer idx.Close()
	idx.IndexAll(res)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": org,
		"summary": "Cost cycles",
		"detail":  "DSP cost cycles block the deadline",
		"source":  "issue #40",
		"file":    "dsp-verification.md",
	}
	result, err := WriteHandler(res, idx)(context.Background(), req)
	if err != nil {
		t.Fatalf("WriteHandler() error: %v", err)
	}
	if result.IsError {
		t.Fatalf("WriteHandler() error result: %s", extractTextContent(t, result))
	}

	data, err := os.ReadFile(filepath.Join(dir, "dsp-verification.md"))
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	content := string(data)
	for _, want := range []string{"## Cost cycles", "DSP cost cycles block the deadline", "Source: issue #40"} {
		if !strings.Contains(content, want) {
			t.Errorf("new section missing %q: %s", want, content)
		}
	}

	queryReq := mcp.CallToolRequest{}
	queryReq.Params.Arguments = map[string]interface{}{
		"project": org,
		"query":   "cycles block the deadline",
	}
	queryResult, err := QueryHandler(res, idx)(context.Background(), queryReq)
	if err != nil {
		t.Fatalf("QueryHandler() error: %v", err)
	}
	queryContent := extractTextContent(t, queryResult)
	if !strings.Contains(queryContent, "org-dsp-verification.md::Cost cycles") {
		t.Errorf("query after org write did not find the section: %s", queryContent)
	}

	listReq := mcp.CallToolRequest{}
	listReq.Params.Arguments = map[string]interface{}{"project": org}
	listResult, err := ListHandler(res, idx)(context.Background(), listReq)
	if err != nil {
		t.Fatalf("ListHandler() error: %v", err)
	}
	listContent := extractTextContent(t, listResult)
	if !strings.Contains(listContent, "### dsp-verification.md (1 sections)") {
		t.Errorf("list after org write did not group the new file: %s", listContent)
	}
}

func TestWriteHandlerOrgTrimsHeading(t *testing.T) {
	res, org, dir := newOrgFixture(t, map[string]string{"architecture.md": "## Build\n"})

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": org,
		"summary": "  Cost cycles  ",
		"detail":  "cost",
		"source":  "test",
		"file":    "dsp-verification.md",
	}
	result, _ := WriteHandler(res, nil)(context.Background(), req)
	if result.IsError {
		t.Fatalf("WriteHandler() error result: %s", extractTextContent(t, result))
	}
	data, err := os.ReadFile(filepath.Join(dir, "dsp-verification.md"))
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if !strings.Contains(string(data), "## Cost cycles\n") {
		t.Errorf("heading was not trimmed: %s", data)
	}
}

func TestWriteHandlerOrgBlankSummary(t *testing.T) {
	res, org, _ := newOrgFixture(t, map[string]string{"architecture.md": "## Build\n"})

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": org,
		"summary": "   ",
		"detail":  "cost",
		"source":  "test",
		"file":    "dsp-verification.md",
	}
	result, _ := WriteHandler(res, nil)(context.Background(), req)
	if result == nil || !result.IsError {
		t.Fatal("blank org heading should be an error")
	}
	if content := extractTextContent(t, result); !strings.Contains(content, "summary must not be blank") {
		t.Errorf("error = %q, want a blank-heading rejection", content)
	}
}

func TestWriteHandlerOrgMissingFileListsAvailable(t *testing.T) {
	res, org, _ := newOrgFixture(t, map[string]string{
		"architecture.md": "## Build\n",
		"review.md":       "## Overview\n",
	})

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": org,
		"summary": "Cost cycles",
		"detail":  "cost",
		"source":  "test",
	}
	result, _ := WriteHandler(res, nil)(context.Background(), req)
	if result == nil || !result.IsError {
		t.Fatal("org write without file should be an error")
	}
	content := extractTextContent(t, result)
	if !strings.Contains(content, "file is required") {
		t.Errorf("error should say file is required: %s", content)
	}
	for _, name := range []string{"architecture.md", "review.md"} {
		if !strings.Contains(content, name) {
			t.Errorf("error should list %s: %s", name, content)
		}
	}
}

func TestWriteHandlerOrgNoFiles(t *testing.T) {
	res, org, _ := newOrgFixture(t, nil)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": org,
		"summary": "Cost cycles",
		"detail":  "cost",
		"source":  "test",
	}
	result, _ := WriteHandler(res, nil)(context.Background(), req)
	if result == nil || !result.IsError {
		t.Fatal("org write without file should be an error")
	}
	content := extractTextContent(t, result)
	if !strings.Contains(content, "no markdown files") {
		t.Errorf("error should say there are no markdown files: %s", content)
	}
}

func TestWriteHandlerOrgRuleRejected(t *testing.T) {
	res, org, _ := newOrgFixture(t, map[string]string{"architecture.md": "## Build\n"})

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": org,
		"summary": "Cost cycles",
		"detail":  "cost",
		"source":  "test",
		"file":    "dsp-verification.md",
		"rule":    "NEVER exceed the deadline",
	}
	result, _ := WriteHandler(res, nil)(context.Background(), req)
	if result == nil || !result.IsError {
		t.Fatal("org write with rule should be an error")
	}
	content := extractTextContent(t, result)
	if !strings.Contains(content, "rule is not supported") {
		t.Errorf("error should reject rule: %s", content)
	}
}

func TestWriteHandlerOrgFileValidation(t *testing.T) {
	res, org, _ := newOrgFixture(t, map[string]string{"architecture.md": "## Build\n"})

	for _, name := range []string{"../evil.md", "sub/evil.md", "/tmp/evil.md", "evil.txt", ".md", "evil.md::Heading"} {
		t.Run(name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]interface{}{
				"project": org,
				"summary": "Heading",
				"detail":  "body",
				"source":  "test",
				"file":    name,
			}
			result, _ := WriteHandler(res, nil)(context.Background(), req)
			if result == nil || !result.IsError {
				t.Fatalf("org write with file %q should be an error", name)
			}
			if content := extractTextContent(t, result); !strings.Contains(content, "invalid file") {
				t.Errorf("error = %q, want it to reject the filename", content)
			}
		})
	}
}

func TestWriteHandlerOrgHeadingCollision(t *testing.T) {
	res, org, _ := newOrgFixture(t, map[string]string{
		"architecture.md": "## Build\nmake test\n",
	})

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": org,
		"summary": "Build",
		"detail":  "new build notes",
		"source":  "test",
		"file":    "architecture.md",
	}
	result, _ := WriteHandler(res, nil)(context.Background(), req)
	if result == nil || !result.IsError {
		t.Fatal("colliding org write should be an error")
	}
	content := extractTextContent(t, result)
	if !strings.Contains(content, "already exists") || !strings.Contains(content, "update_knowledge") {
		t.Errorf("error should point at update_knowledge: %s", content)
	}
}

func TestWriteHandlerProjectRequiresCategory(t *testing.T) {
	root := t.TempDir()
	if err := knowledge.EnsureDir(filepath.Join(root, "proj", ".agents")); err != nil {
		t.Fatal(err)
	}
	res, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": "proj",
		"summary": "s",
		"detail":  "d",
		"source":  "test",
	}
	result, _ := WriteHandler(res, nil)(context.Background(), req)
	if result == nil || !result.IsError {
		t.Fatal("project write without category should be an error")
	}
	if content := extractTextContent(t, result); !strings.Contains(content, "category is required") {
		t.Errorf("error should say category is required: %s", content)
	}
}

func TestUpdateHandlerOrgSection(t *testing.T) {
	res, org, dir := newOrgFixture(t, map[string]string{
		"architecture.md": "## Build\n\nold build text\n\nSource: issue #40\n",
	})

	idx := newIndexReady(t)
	defer idx.Close()
	idx.IndexAll(res)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": org,
		"id":      "org-architecture.md::Build",
		"detail":  "new build text",
	}
	result, err := UpdateHandler(res, idx)(context.Background(), req)
	if err != nil {
		t.Fatalf("UpdateHandler() error: %v", err)
	}
	if result.IsError {
		t.Fatalf("UpdateHandler() error result: %s", extractTextContent(t, result))
	}

	data, err := os.ReadFile(filepath.Join(dir, "architecture.md"))
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	content := string(data)
	for _, want := range []string{"## Build", "new build text", "Source: issue #40"} {
		if !strings.Contains(content, want) {
			t.Errorf("updated section missing %q: %s", want, content)
		}
	}
	if strings.Contains(content, "old build text") {
		t.Errorf("old body survived: %s", content)
	}

	queryReq := mcp.CallToolRequest{}
	queryReq.Params.Arguments = map[string]interface{}{
		"project": org,
		"query":   "new build text",
	}
	queryResult, err := QueryHandler(res, idx)(context.Background(), queryReq)
	if err != nil {
		t.Fatalf("QueryHandler() error: %v", err)
	}
	if queryContent := extractTextContent(t, queryResult); !strings.Contains(queryContent, "org-architecture.md::Build") {
		t.Errorf("query after org update did not find the section: %s", queryContent)
	}
}

func TestUpdateHandlerOrgRejectsAgnosticFields(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{
			name: "summary",
			args: map[string]interface{}{"summary": "Renamed"},
			want: "summary is not supported",
		},
		{
			name: "rule",
			args: map[string]interface{}{"rule": "NEVER do it"},
			want: "rule is not supported",
		},
		{
			name: "supersedes",
			args: map[string]interface{}{"supersedes": "conv-001"},
			want: "supersedes is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, org, _ := newOrgFixture(t, map[string]string{"architecture.md": "## Build\nold\n"})
			args := map[string]interface{}{
				"project": org,
				"id":      "org-architecture.md::Build",
				"detail":  "new",
			}
			for k, v := range tt.args {
				args[k] = v
			}
			req := mcp.CallToolRequest{}
			req.Params.Arguments = args
			result, _ := UpdateHandler(res, nil)(context.Background(), req)
			if result == nil || !result.IsError {
				t.Fatal("expected error result")
			}
			if content := extractTextContent(t, result); !strings.Contains(content, tt.want) {
				t.Errorf("error = %q, want it to contain %q", content, tt.want)
			}
		})
	}
}

func TestUpdateHandlerOrgInvalidID(t *testing.T) {
	res, org, _ := newOrgFixture(t, map[string]string{"architecture.md": "## Build\nold\n"})

	for _, id := range []string{"conv-001", "org-architecture.md", "org-../evil.md::Build", "org-other.md::Build"} {
		t.Run(id, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]interface{}{
				"project": org,
				"id":      id,
				"detail":  "new",
			}
			result, _ := UpdateHandler(res, nil)(context.Background(), req)
			if result == nil || !result.IsError {
				t.Fatalf("update with id %q should be an error", id)
			}
			if content := extractTextContent(t, result); !strings.Contains(content, "id") && !strings.Contains(content, "file") {
				t.Errorf("error = %q, want it to reject the id or file", content)
			}
		})
	}
}

func TestUpdateHandlerProjectRequiresCategory(t *testing.T) {
	root := t.TempDir()
	agents := filepath.Join(root, "proj", ".agents")
	if err := knowledge.EnsureDir(agents); err != nil {
		t.Fatal(err)
	}
	if err := knowledge.Save(knowledge.CategoryFilePath(agents, "conventions"),
		&knowledge.KnowledgeFile{Project: "proj", Version: 1, Entries: []knowledge.Entry{
			{ID: "conv-001", Summary: "s", Detail: "d", Source: "test", Date: "2026-01-01"},
		}}); err != nil {
		t.Fatal(err)
	}
	res, _, err := projects.Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("projects.Build() error: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": "proj",
		"id":      "conv-001",
		"detail":  "new",
	}
	result, _ := UpdateHandler(res, nil)(context.Background(), req)
	if result == nil || !result.IsError {
		t.Fatal("project update without category should be an error")
	}
	if content := extractTextContent(t, result); !strings.Contains(content, "category is required") {
		t.Errorf("error = %q, want category is required", content)
	}
}

func TestUpdateHandlerOrgMissingHeading(t *testing.T) {
	res, org, _ := newOrgFixture(t, map[string]string{"architecture.md": "## Build\nold\n"})

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]interface{}{
		"project": org,
		"id":      "org-architecture.md::Missing",
		"detail":  "new",
	}
	result, _ := UpdateHandler(res, nil)(context.Background(), req)
	if result == nil || !result.IsError {
		t.Fatal("update with unknown heading should be an error")
	}
	if content := extractTextContent(t, result); !strings.Contains(content, "not found") {
		t.Errorf("error should say not found: %s", content)
	}
}
