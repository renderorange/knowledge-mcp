package tools

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/agents_knowledge/knowledge"
	"github.com/renderorange/agents_knowledge/projects"
)

// VerifyHandler handles the verify_knowledge MCP tool.
func VerifyHandler(res *projects.Resolver) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		project, err := request.RequireString("project")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		category, err := request.RequireString("category")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if !knowledge.IsValidCategory(category) {
			return mcp.NewToolResultError(fmt.Sprintf("invalid category: %q (must be conventions, subsystems, or decisions)", category)), nil
		}

		id, err := request.RequireString("id")
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

		agentsDir := filepath.Join(projectPath, ".agents")
		catPath := knowledge.CategoryFilePath(agentsDir, category)

		mu := fileLocks.Get(catPath)
		mu.Lock()
		defer mu.Unlock()

		kf, loadErr := knowledge.LoadOrCreate(catPath, project)
		if loadErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("load knowledge file: %v", loadErr)), nil
		}

		found := false
		for i := range kf.Entries {
			if kf.Entries[i].ID == id {
				kf.Entries[i].LastVerified = knowledge.Today()
				kf.Entries[i].ExpiresAt = knowledge.ExpiryDate()
				found = true
				break
			}
		}

		if !found {
			return mcp.NewToolResultError(fmt.Sprintf("entry not found: %q", id)), nil
		}

		if saveErr := knowledge.Save(catPath, kf); saveErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("save knowledge file: %v", saveErr)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("verified %s in %s/%s.yaml", id, project, category)), nil
	}
}
