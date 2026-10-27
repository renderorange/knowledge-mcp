package tools

import (
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/knowledge-mcp/knowledge"
)

// categoryNames keeps tool descriptions in sync with ValidCategories.
func categoryNames() string {
	return strings.Join(knowledge.ValidCategories(), ", ")
}

// Tool name constants keep main.go, the registry, and generated
// instructions in sync.
const (
	ToolInitKnowledge    = "init_knowledge"
	ToolWriteKnowledge   = "write_knowledge"
	ToolQueryKnowledge   = "query_knowledge"
	ToolListKnowledge    = "list_knowledge"
	ToolUpdateKnowledge  = "update_knowledge"
	ToolListProjects     = "list_projects"
	ToolReindexKnowledge = "reindex_knowledge"
)

// ParamSpec describes one tool parameter.
type ParamSpec struct {
	Name        string
	Description string
	Type        string // "string" or "number"
	Required    bool
}

// ToolSpec is the metadata install templates and MCP registration share.
type ToolSpec struct {
	Name        string
	Purpose     string // one line used by generated agent instructions
	Description string // full MCP tool description
	Params      []ParamSpec
}

// Registry lists every MCP tool the server serves, in registration order.
var Registry = []ToolSpec{
	{
		Name: ToolInitKnowledge, Purpose: "Create a project's .agents/ knowledge directory structure",
		Description: "Create .agents/ knowledge directory structure for a project",
		Params: []ParamSpec{
			{Name: "project_path", Description: "Absolute path to the project root", Type: "string", Required: true},
		},
	},
	{
		Name: ToolWriteKnowledge, Purpose: "Add a knowledge entry or an org markdown section",
		Description: "Add a new knowledge entry to a project's .agents/ store, or a markdown section to an org root's .agents/knowledge/ file",
		Params: []ParamSpec{
			{Name: "project", Description: "Project name (bare if unique, else root/project)", Type: "string", Required: true},
			{Name: "category", Description: fmt.Sprintf("Category: %s (required for project stores; ignored for org roots)", categoryNames()), Type: "string"},
			{Name: "summary", Description: "One-line description (max 100 chars); the section heading for org roots", Type: "string", Required: true},
			{Name: "detail", Description: "Concise knowledge (10-20 lines). Summarize key facts, don't copy source files. The section body for org roots.", Type: "string", Required: true},
			{Name: "source", Description: "How this was learned (provenance); folded into the section body for org roots", Type: "string", Required: true},
			{Name: "rule", Description: "Imperative constraint this entry enforces (e.g. \"Never create files outside ./tmp\"). Shown prominently in list output. Max 200 chars. Not supported for org roots.", Type: "string"},
			{Name: "file", Description: "Org roots only: markdown file to append to (e.g. dsp-verification.md). Omit to list the available files.", Type: "string"},
		},
	},
	{
		Name: ToolQueryKnowledge, Purpose: "Full-text search across knowledge entries",
		Description: "Search knowledge entries by text and category",
		Params: []ParamSpec{
			{Name: "project", Description: "Project name (bare if unique, else root/project)", Type: "string", Required: true},
			{Name: "query", Description: "Full-text search query", Type: "string"},
			{Name: "category", Description: fmt.Sprintf("Filter by category: %s", categoryNames()), Type: "string"},
			{Name: "limit", Description: "Max results (default 10)", Type: "number"},
		},
	},
	{
		Name: ToolListKnowledge, Purpose: "List all entries; rules shown first as constraints",
		Description: "List all knowledge entries for a project. Entries with a rule are shown first under constraints.",
		Params: []ParamSpec{
			{Name: "project", Description: "Project name (bare if unique, else root/project)", Type: "string", Required: true},
			{Name: "category", Description: fmt.Sprintf("Filter by category: %s", categoryNames()), Type: "string"},
		},
	},
	{
		Name: ToolUpdateKnowledge, Purpose: "Update a knowledge entry or an org markdown section",
		Description: "Update an existing knowledge entry by ID, or replace an org markdown section body (id org-<file>::<heading>)",
		Params: []ParamSpec{
			{Name: "project", Description: "Project name (bare if unique, else root/project)", Type: "string", Required: true},
			{Name: "category", Description: fmt.Sprintf("Category: %s (required for project stores; ignored for org roots)", categoryNames()), Type: "string"},
			{Name: "id", Description: "Entry ID (conv-001) or org section ID (org-<file>::<heading>)", Type: "string", Required: true},
			{Name: "summary", Description: "New summary (optional; not supported for org sections)", Type: "string"},
			{Name: "detail", Description: "New detail (optional; required for org sections)", Type: "string"},
			{Name: "rule", Description: "New imperative rule (optional, max 200 chars; not supported for org sections)", Type: "string"},
			{Name: "supersedes", Description: "ID of entry this supersedes (optional; not supported for org sections)", Type: "string"},
		},
	},
	{
		Name: ToolListProjects, Purpose: "List all discovered projects (org-wide mode)",
		Description: "List all discovered projects across configured roots",
	},
	{
		Name: ToolReindexKnowledge, Purpose: "Rebuild the search index from store files",
		Description: "Rebuild the in-memory search index from the knowledge store files. Queries refresh automatically when store files change; use this to force a full rebuild.",
	},
}

// MCPTool converts the spec into an mcp.Tool for server registration.
func (t ToolSpec) MCPTool() mcp.Tool {
	opts := []mcp.ToolOption{mcp.WithDescription(t.Description)}
	for _, p := range t.Params {
		propOpts := []mcp.PropertyOption{mcp.Description(p.Description)}
		if p.Required {
			propOpts = append(propOpts, mcp.Required())
		}
		switch p.Type {
		case "string":
			opts = append(opts, mcp.WithString(p.Name, propOpts...))
		case "number":
			opts = append(opts, mcp.WithNumber(p.Name, propOpts...))
		}
	}
	return mcp.NewTool(t.Name, opts...)
}
