package tools

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	"github.com/renderorange/agents_knowledge/knowledge"
	"github.com/renderorange/agents_knowledge/search"
	"github.com/mark3labs/mcp-go/mcp"
)

const maxDetailSize = 1024 * 1024 // 1MB

var fileLocks = knowledge.NewFileLocks()

// WriteHandler handles the write_knowledge MCP tool.
// It requires a projectPathFn to resolve the project path from the project name.
func WriteHandler(projectPathFn func(project string) string, idx *search.Index) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

		confidence, err := request.RequireString("confidence")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if !knowledge.IsValidConfidence(confidence) {
			return mcp.NewToolResultError(fmt.Sprintf("invalid confidence: %q (must be high, medium, or low)", confidence)), nil
		}

		source, err := request.RequireString("source")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		projectPath := projectPathFn(project)
		if projectPath == "" {
			return mcp.NewToolResultError(fmt.Sprintf("unknown project: %q", project)), nil
		}

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
			ID:           id,
			Summary:      summary,
			Detail:       detail,
			Confidence:   confidence,
			Source:       source,
			Date:         knowledge.Today(),
			ExpiresAt:    knowledge.ExpiryDate(),
			LastVerified: knowledge.Today(),
		}

		kf.Entries = append(kf.Entries, entry)

		if saveErr := knowledge.Save(catPath, kf); saveErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("save knowledge file: %v", saveErr)), nil
		}

		// Index in bleve
		if idx != nil {
			doc := search.SearchDocument{
				Summary:    summary,
				Detail:     detail,
				Category:   category,
				Confidence: confidence,
			}
			if indexErr := idx.Add(id, doc); indexErr != nil {
				log.Printf("warning: failed to index %s: %v", id, indexErr)
			}
		}

		return mcp.NewToolResultText(fmt.Sprintf("wrote %s to %s/%s.yaml", id, project, category)), nil
	}
}
