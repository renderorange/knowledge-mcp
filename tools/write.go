package tools

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
	"github.com/renderorange/knowledge-mcp/search"
)

const maxDetailSize = 1024 * 1024 // 1MB

var fileLocks = knowledge.NewFileLocks()

// WriteHandler handles the write_knowledge MCP tool.
func WriteHandler(res *projects.Resolver, idx *search.Index) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

		summary, err := request.RequireString("summary")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(summary) > 100 {
			return mcp.NewToolResultError(fmt.Sprintf("summary too long: %d chars (max 100)", len(summary))), nil
		}

		detail, err := request.RequireString("detail")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(detail) > maxDetailSize {
			return mcp.NewToolResultError(fmt.Sprintf("detail too long: %d bytes (max %d)", len(detail), maxDetailSize)), nil
		}

		source, err := request.RequireString("source")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		rule := request.GetString("rule", "")
		if len(rule) > 200 {
			return mcp.NewToolResultError(fmt.Sprintf("rule too long: %d chars (max 200)", len(rule))), nil
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

		id := knowledge.NextID(kf.Entries, knowledge.CategoryPrefix(category))

		entry := knowledge.Entry{
			ID:      id,
			Summary: summary,
			Detail:  detail,
			Rule:    rule,
			Source:  source,
			Date:    knowledge.Today(),
		}

		kf.Entries = append(kf.Entries, entry)

		if saveErr := knowledge.Save(catPath, kf); saveErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("save knowledge file: %v", saveErr)), nil
		}

		// Index in bleve
		if idx != nil {
			doc := search.SearchDocument{
				Summary:  summary,
				Detail:   detail,
				Rule:     rule,
				Category: category,
				Project:  ref.Address,
			}
			if indexErr := idx.Add(ref.Address+"/"+id, doc); indexErr != nil {
				log.Printf("warning: failed to index %s/%s: %v", ref.Address, id, indexErr)
			}
		}

		return mcp.NewToolResultText(fmt.Sprintf("wrote %s to %s/%s.yaml", id, project, category)), nil
	}
}
