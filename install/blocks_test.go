package install

import (
	"strings"
	"testing"

	"github.com/renderorange/knowledge-mcp/tools"
)

const agentsStart = "<!-- knowledge-mcp:start -->"
const agentsEnd = "<!-- knowledge-mcp:end -->"

func TestUpsertBlockAppends(t *testing.T) {
	existing := []byte("# My Instructions\n\nSome user content.\n")
	block := []byte(agentsStart + "\nNEW BLOCK\n" + agentsEnd)
	out := UpsertBlock(existing, block, agentsStart, agentsEnd)
	s := string(out)
	if !strings.HasPrefix(s, "# My Instructions\n") {
		t.Fatalf("user content lost:\n%s", s)
	}
	if !strings.HasSuffix(strings.TrimSpace(s), agentsEnd) {
		t.Fatalf("block not appended:\n%s", s)
	}
}

func TestUpsertBlockReplaces(t *testing.T) {
	existing := []byte("start\n" + agentsStart + "\nOLD\n" + agentsEnd + "\nend\n")
	block := []byte(agentsStart + "\nNEW\n" + agentsEnd)
	out := UpsertBlock(existing, block, agentsStart, agentsEnd)
	s := string(out)
	if !strings.Contains(s, "NEW") || strings.Contains(s, "OLD") {
		t.Fatalf("replace failed:\n%s", s)
	}
	if !strings.Contains(s, "start\n") || !strings.Contains(s, "end\n") {
		t.Fatalf("surrounding content lost:\n%s", s)
	}
	if strings.Count(s, agentsStart) != 1 {
		t.Fatalf("duplicate start marker:\n%s", s)
	}
}

func TestRenderAgentsBlockListsEveryTool(t *testing.T) {
	block := string(RenderAgentsBlock(tools.Registry))
	for _, spec := range tools.Registry {
		if !strings.Contains(block, "`"+spec.Name+"`") {
			t.Errorf("block missing tool %q", spec.Name)
		}
	}
	if !strings.Contains(block, "Summaries are not rules") {
		t.Errorf("block missing the summaries-are-not-rules rule")
	}
	if !strings.Contains(block, agentsStart) || !strings.Contains(block, agentsEnd) {
		t.Error("block missing markers")
	}
}
