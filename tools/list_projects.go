package tools

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/mark3labs/mcp-go/mcp"
)

// ListProjectsHandler handles the list_projects MCP tool.
func ListProjectsHandler(orgRoot string, projectPathFn func(project string) string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		entries, err := os.ReadDir(orgRoot)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("read org root: %v", err)), nil
		}

		var projects []string
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if name == ".agents" || name == ".git" || name == ".index" {
				continue
			}
			if projectPathFn(name) != "" {
				projects = append(projects, name)
			}
		}

		sort.Strings(projects)

		if len(projects) == 0 {
			return mcp.NewToolResultText("no projects found"), nil
		}

		output := "discovered projects:\n"
		for _, p := range projects {
			output += fmt.Sprintf("- %s\n", p)
		}
		return mcp.NewToolResultText(output), nil
	}
}
