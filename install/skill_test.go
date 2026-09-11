package install

import (
	"strings"
	"testing"

	"github.com/renderorange/knowledge-mcp/tools"
)

func TestRenderSkillMDContent(t *testing.T) {
	md := string(RenderSkillMD(tools.Registry))
	for _, want := range []string{
		"---\n", "name: knowledge-mcp", "description:",
		"list_knowledge", "query_knowledge", "write_knowledge",
		"## Gotchas", "summaries are not rules",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("skill missing %q", want)
		}
	}
	for _, spec := range tools.Registry {
		if !strings.Contains(md, spec.Name) {
			t.Errorf("skill missing tool %q", spec.Name)
		}
	}
}
