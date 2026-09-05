package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/agents_knowledge/projects"
)

// ListProjectsHandler handles the list_projects MCP tool.
func ListProjectsHandler(res *projects.Resolver) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var projLines, orgLines []string
		for _, ref := range res.Snapshot() {
			if ref.Kind == projects.KindOrg {
				orgLines = append(orgLines, fmt.Sprintf("- %s (%s)", ref.Name, ref.Path))
				continue
			}
			line := fmt.Sprintf("- %s (%s)", ref.Address, ref.Path)
			if ref.Address != ref.Name {
				line += "  [ambiguous — use qualified name]"
			}
			projLines = append(projLines, line)
		}

		var sections []string
		if len(projLines) > 0 {
			sections = append(sections, "projects:\n"+strings.Join(projLines, "\n"))
		}
		if len(orgLines) > 0 {
			sections = append(sections, "org roots:\n"+strings.Join(orgLines, "\n"))
		}
		if len(sections) == 0 {
			return mcp.NewToolResultText("no projects found"), nil
		}
		return mcp.NewToolResultText(strings.Join(sections, "\n") + "\n"), nil
	}
}
