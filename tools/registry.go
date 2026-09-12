package tools

import "github.com/mark3labs/mcp-go/mcp"

// Tool name constants keep main.go, the registry, and generated
// instructions in sync.
const (
	ToolInitKnowledge   = "init_knowledge"
	ToolWriteKnowledge  = "write_knowledge"
	ToolQueryKnowledge  = "query_knowledge"
	ToolListKnowledge   = "list_knowledge"
	ToolUpdateKnowledge = "update_knowledge"
	ToolListProjects    = "list_projects"
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
		Name: ToolWriteKnowledge, Purpose: "Add a new knowledge entry to a project's store",
		Description: "Add a new knowledge entry to a project's .agents/ store",
		Params: []ParamSpec{
			{Name: "project", Description: "Project name (bare if unique, else root/project)", Type: "string", Required: true},
			{Name: "category", Description: "Category: conventions, subsystems, or decisions", Type: "string", Required: true},
			{Name: "summary", Description: "One-line description (max 100 chars)", Type: "string", Required: true},
			{Name: "detail", Description: "Concise knowledge (10-20 lines). Summarize key facts, don't copy source files. Include only query-able information.", Type: "string", Required: true},
			{Name: "source", Description: "How this was learned (provenance)", Type: "string", Required: true},
			{Name: "rule", Description: "Imperative constraint this entry enforces (e.g. \"Never create files outside ./tmp\"). Shown prominently in list output. Max 200 chars.", Type: "string"},
		},
	},
	{
		Name: ToolQueryKnowledge, Purpose: "Full-text search across knowledge entries",
		Description: "Search knowledge entries by text and category",
		Params: []ParamSpec{
			{Name: "project", Description: "Project name (bare if unique, else root/project)", Type: "string", Required: true},
			{Name: "query", Description: "Full-text search query", Type: "string"},
			{Name: "category", Description: "Filter by category: conventions, subsystems, or decisions", Type: "string"},
			{Name: "limit", Description: "Max results (default 10)", Type: "number"},
		},
	},
	{
		Name: ToolListKnowledge, Purpose: "List all entries; rules shown first as constraints",
		Description: "List all knowledge entries for a project. Entries with a rule are shown first under constraints.",
		Params: []ParamSpec{
			{Name: "project", Description: "Project name (bare if unique, else root/project)", Type: "string", Required: true},
			{Name: "category", Description: "Filter by category: conventions, subsystems, or decisions", Type: "string"},
		},
	},
	{
		Name: ToolUpdateKnowledge, Purpose: "Update an existing knowledge entry by ID",
		Description: "Update an existing knowledge entry by ID",
		Params: []ParamSpec{
			{Name: "project", Description: "Project name (bare if unique, else root/project)", Type: "string", Required: true},
			{Name: "category", Description: "Category: conventions, subsystems, or decisions", Type: "string", Required: true},
			{Name: "id", Description: "Entry ID to update (e.g., conv-001)", Type: "string", Required: true},
			{Name: "summary", Description: "New summary (optional)", Type: "string"},
			{Name: "detail", Description: "New detail (optional)", Type: "string"},
			{Name: "rule", Description: "New imperative rule (optional, max 200 chars)", Type: "string"},
			{Name: "supersedes", Description: "ID of entry this supersedes (optional)", Type: "string"},
		},
	},
	{
		Name: ToolListProjects, Purpose: "List all discovered projects (org-wide mode)",
		Description: "List all discovered projects across configured roots",
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
