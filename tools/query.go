package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/renderorange/agents_knowledge/knowledge"
	"github.com/renderorange/agents_knowledge/search"
	"github.com/mark3labs/mcp-go/mcp"
)

// QueryHandler handles the query_knowledge MCP tool.
func QueryHandler(projectPathFn func(project string) string, idx *search.Index) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		project, err := request.RequireString("project")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if projectPathFn(project) == "" {
			return mcp.NewToolResultError(fmt.Sprintf("unknown project: %q", project)), nil
		}

		query := request.GetString("query", "")
		category := request.GetString("category", "")
		confidence := request.GetString("confidence", "")
		limit := int(request.GetFloat("limit", 10))

		if category != "" && !knowledge.IsValidCategory(category) {
			return mcp.NewToolResultError(fmt.Sprintf("invalid category: %q (must be conventions, subsystems, or decisions)", category)), nil
		}
		if confidence != "" && !knowledge.IsValidConfidence(confidence) {
			return mcp.NewToolResultError(fmt.Sprintf("invalid confidence: %q (must be high, medium, or low)", confidence)), nil
		}

		results, err := idx.Query(query, category, confidence, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search error: %v", err)), nil
		}

		if len(results) == 0 {
			return mcp.NewToolResultText("no matching knowledge entries found"), nil
		}

		data, _ := json.MarshalIndent(results, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	}
}
