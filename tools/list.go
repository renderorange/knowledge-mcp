package tools

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/renderorange/agents_knowledge/knowledge"
	"github.com/renderorange/agents_knowledge/projects"
	"github.com/mark3labs/mcp-go/mcp"
)

// ListHandler handles the list_knowledge MCP tool.
func ListHandler(res *projects.Resolver) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		project, err := request.RequireString("project")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		ref, resolveErr := res.Resolve(project)
		if resolveErr != nil {
			return mcp.NewToolResultError(resolveErr.Error()), nil
		}
		if ref.Kind == projects.KindOrg {
			return mcp.NewToolResultError(fmt.Sprintf(
				"%q is an org root; org-level knowledge is file-based — edit %s/.agents/knowledge/ directly",
				project, ref.Path)), nil
		}
		projectPath := ref.Path

		filterCategory := request.GetString("category", "")
		if filterCategory != "" && !knowledge.IsValidCategory(filterCategory) {
			return mcp.NewToolResultError(fmt.Sprintf("invalid category: %q (must be conventions, subsystems, or decisions)", filterCategory)), nil
		}

		agentsDir := filepath.Join(projectPath, ".agents")

		output := ""
		for _, cat := range knowledge.ValidCategories() {
			if filterCategory != "" && cat != filterCategory {
				continue
			}

			catPath := knowledge.CategoryFilePath(agentsDir, cat)

			mu := fileLocks.Get(catPath)
			mu.RLock()
			kf, loadErr := knowledge.LoadOrCreate(catPath, project)
			mu.RUnlock()

			if loadErr != nil {
				continue
			}

			if len(kf.Entries) == 0 {
				continue
			}

			output += fmt.Sprintf("## %s (%d entries)\n", cat, len(kf.Entries))
			for _, entry := range kf.Entries {
				output += fmt.Sprintf("- [%s] %s (confidence: %s, date: %s)\n",
					entry.ID, entry.Summary, entry.Confidence, entry.Date)
			}
			output += "\n"
		}

		if output == "" {
			return mcp.NewToolResultText("no knowledge entries found"), nil
		}

		return mcp.NewToolResultText(output), nil
	}
}
