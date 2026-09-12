package install

import (
	"strings"

	"github.com/renderorange/knowledge-mcp/tools"
)

// RenderSkillMD renders the knowledge-mcp skill.
func RenderSkillMD(registry []tools.ToolSpec) []byte {
	var rows strings.Builder
	for _, spec := range registry {
		rows.WriteString("| " + spec.Name + " | " + spec.Purpose + " |\n")
	}
	md := "---\n" +
		"name: knowledge-mcp\n" +
		"description: \"Use the persistent knowledge store for project conventions, subsystems, and decisions. Triggers on: what are our conventions, does this project have rules about, how does this subsystem work, why was this decision made, capture this decision, write that down as knowledge, update that entry, knowledge store, .agents, list_knowledge, query_knowledge, write_knowledge, update_knowledge, init_knowledge.\"\n" +
		"---\n" +
		"\n" +
		"# Knowledge Store (knowledge-mcp)\n" +
		"\n" +
		"Distilled project knowledge lives in .agents/<category>.yaml files and is served\n" +
		"by the knowledge-mcp MCP server. Query the store before re-deriving facts from\n" +
		"source files.\n" +
		"\n" +
		"## Tool decision matrix\n" +
		"\n" +
		"| Tool | Purpose |\n" +
		"|---|---|\n" +
		rows.String() +
		"## Session start\n" +
		"\n" +
		"1. list_knowledge(project=\"<name>\") — constraints are shown first; read them.\n" +
		"2. For the task's domain, query_knowledge(project, query) and read the FULL\n" +
		"   detail of matching entries before acting.\n" +
		"\n" +
		"## Capture workflow\n" +
		"\n" +
		"- write_knowledge when a convention, subsystem fact, or decision is established.\n" +
		"- detail: 10-20 lines of query-able facts; never copy source files.\n" +
		"- source: record how the knowledge was learned.\n" +
		"- rule field: an imperative prohibition/requirement (surfaced first in lists).\n" +
		"- update_knowledge to change or supersede existing entries.\n" +
		"\n" +
		"## Gotchas\n" +
		"\n" +
		"- summaries are not rules: list summaries omit enforcement details — query\n" +
		"  before acting.\n" +
		"- Never duplicate always-loaded AGENTS.md instructions into the store.\n" +
		"- Org-level queries return markdown SECTIONS, not whole documents.\n" +
		"- Merged global entries are labeled (global) in listings.\n" +
		"- Under --store (central store mode), the store is single-writer: only one\n" +
		"  server may run against it.\n"
	return []byte(md)
}
