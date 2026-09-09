package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
	"gopkg.in/yaml.v3"
)

// InitHandler handles the init_knowledge MCP tool.
func InitHandler(res *projects.Resolver) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		projectPath, err := request.RequireString("project_path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Verify path exists
		if _, statErr := os.Stat(projectPath); os.IsNotExist(statErr) {
			return mcp.NewToolResultError(fmt.Sprintf("path does not exist: %s", projectPath)), nil
		}

		agentsDir := filepath.Join(projectPath, ".agents")

		if err := knowledge.EnsureDir(agentsDir); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("create directory: %v", err)), nil
		}

		created := []string{}

		// Create _meta.yaml if it doesn't exist
		metaPath := knowledge.MetaFilePath(agentsDir)
		if !knowledge.FileExists(metaPath) {
			meta := knowledge.Meta{
				Project:          filepath.Base(projectPath),
				KnowledgeVersion: 1,
				Created:          knowledge.Today(),
				LastUpdated:      knowledge.Today(),
				Categories:       knowledge.ValidCategories(),
			}
			data, marshalErr := yaml.Marshal(meta)
			if marshalErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("marshal _meta.yaml: %v", marshalErr)), nil
			}
			if writeErr := os.WriteFile(metaPath, data, 0644); writeErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("write _meta.yaml: %v", writeErr)), nil
			}
			created = append(created, "_meta.yaml")
		}

		// Create category files if they don't exist
		for _, cat := range knowledge.ValidCategories() {
			catPath := knowledge.CategoryFilePath(agentsDir, cat)
			if !knowledge.FileExists(catPath) {
				kf := &knowledge.KnowledgeFile{
					Project: filepath.Base(projectPath),
					Version: 1,
					Entries: []knowledge.Entry{},
				}
				if saveErr := knowledge.Save(catPath, kf); saveErr != nil {
					return mcp.NewToolResultError(fmt.Sprintf("write %s.yaml: %v", cat, saveErr)), nil
				}
				created = append(created, cat+".yaml")
			}
		}

		resolvable := res != nil && res.Covers(projectPath)

		if len(created) == 0 {
			if resolvable {
				return mcp.NewToolResultText("already initialized — .agents/ exists with all files"), nil
			}
			return mcp.NewToolResultText("already initialized — .agents/ exists with all files\nwarning: this path is not under any configured root/project; add it via --project or --root and restart to make it queryable"), nil
		}

		if resolvable {
			return mcp.NewToolResultText(fmt.Sprintf("initialized .agents/ with: %v", created)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf(
			"initialized .agents/ with: %v\nwarning: this path is not under any configured root/project; add it via --project or --root and restart to make it queryable", created)), nil
	}
}
