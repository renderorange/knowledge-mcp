package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/renderorange/agents_knowledge/knowledge"
	"github.com/renderorange/agents_knowledge/projects"
	"github.com/renderorange/agents_knowledge/search"
	"github.com/mark3labs/mcp-go/mcp"
)

// QueryHandler handles the query_knowledge MCP tool.
func QueryHandler(res *projects.Resolver, idx *search.Index) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		project, err := request.RequireString("project")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		ref, resolveErr := res.Resolve(project)
		if resolveErr != nil {
			return mcp.NewToolResultError(resolveErr.Error()), nil
		}

		query := request.GetString("query", "")
		category := request.GetString("category", "")
		confidence := request.GetString("confidence", "")
		staleFilter := request.GetString("stale", "")

		if staleFilter != "" && staleFilter != "true" && staleFilter != "false" {
			return mcp.NewToolResultError(fmt.Sprintf("invalid stale: %q (must be true or false)", staleFilter)), nil
		}
		limit := int(request.GetFloat("limit", 10))

		if category != "" && !knowledge.IsValidCategory(category) {
			return mcp.NewToolResultError(fmt.Sprintf("invalid category: %q (must be conventions, subsystems, or decisions)", category)), nil
		}
		if confidence != "" && !knowledge.IsValidConfidence(confidence) {
			return mcp.NewToolResultError(fmt.Sprintf("invalid confidence: %q (must be high, medium, or low)", confidence)), nil
		}

		results, err := idx.Query(ref.Address, query, category, confidence, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search error: %v", err)), nil
		}

		// Apply staleness filter if requested
		if staleFilter != "" && len(results) > 0 {
			if ref.Kind == projects.KindOrg {
				return mcp.NewToolResultError("staleness filter not supported for org roots"), nil
			}
			staleMap := buildStalenessMap(ref.Path, project)
			var filtered []search.SearchResult
			for _, r := range results {
				isStale, exists := staleMap[r.ID]
				if !exists {
					continue
				}
				if staleFilter == "true" && isStale {
					filtered = append(filtered, r)
				} else if staleFilter == "false" && !isStale {
					filtered = append(filtered, r)
				}
			}
			results = filtered
		}

		if len(results) == 0 {
			return mcp.NewToolResultText("no matching knowledge entries found"), nil
		}

		data, _ := json.MarshalIndent(results, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	}
}

// buildStalenessMap loads all entries for a project and returns a map of ID -> isStale.
func buildStalenessMap(projectPath, project string) map[string]bool {
	staleMap := make(map[string]bool)
	agentsDir := filepath.Join(projectPath, ".agents")

	for _, cat := range knowledge.ValidCategories() {
		catPath := knowledge.CategoryFilePath(agentsDir, cat)
		kf, err := knowledge.Load(catPath)
		if err != nil {
			continue
		}
		for _, entry := range kf.Entries {
			staleMap[entry.ID] = entry.IsStale()
		}
	}

	return staleMap
}
