package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
	"github.com/renderorange/knowledge-mcp/search"
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
		limit := int(request.GetFloat("limit", 10))

		if category != "" && !knowledge.IsValidCategory(category) {
			return mcp.NewToolResultError(fmt.Sprintf("invalid category: %q (must be conventions, subsystems, or decisions)", category)), nil
		}

		results, err := idx.Query(ref.Address, query, category, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search error: %v", err)), nil
		}

		// If querying a non-global project, also include global results.
		if ref.Address != projects.GlobalAddress {
			if globalRef, ok := res.GlobalRef(); ok {
				globalResults, globalErr := idx.Query(globalRef.Address, query, category, limit)
				if globalErr == nil {
					results = mergeResults(results, globalResults, limit)
				}
			}
		}

		if len(results) == 0 {
			return mcp.NewToolResultText("no matching knowledge entries found"), nil
		}

		data, _ := json.MarshalIndent(results, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	}
}

// mergeResults combines project and global results, deduplicating by ID
// and respecting the limit. Project results come first.
func mergeResults(project, global []search.SearchResult, limit int) []search.SearchResult {
	seen := make(map[string]bool)
	var merged []search.SearchResult
	for _, r := range project {
		key := r.Project + "/" + r.ID
		if !seen[key] {
			seen[key] = true
			merged = append(merged, r)
		}
	}
	for _, r := range global {
		key := r.Project + "/" + r.ID
		if !seen[key] {
			seen[key] = true
			merged = append(merged, r)
		}
	}
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged
}
