package install

import (
	"strings"
	"testing"
)

func TestRenderPluginContent(t *testing.T) {
	ts := string(RenderPlugin("/usr/local/bin/knowledge-mcp"))
	for _, want := range []string{
		"// knowledge-mcp:start", "// knowledge-mcp:end",
		"'hook-augment'", "/usr/local/bin/knowledge-mcp",
		"tool.execute.after", "KNM_LOG_LEVEL",
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("plugin missing %q", want)
		}
	}
}

func TestRenderPluginIncludesSessionHook(t *testing.T) {
	src := string(RenderPlugin("/bin/k"))
	for _, want := range []string{
		"session.created",
		"showToast",
		"knowledge-mcp",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("plugin missing %q", want)
		}
	}
}
