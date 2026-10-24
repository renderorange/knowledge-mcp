package tools

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/knowledge-mcp/search"
)

// ReindexHandler handles the reindex_knowledge MCP tool. Queries already
// refresh automatically when store files change; this tool forces a full
// rebuild on demand.
func ReindexHandler(idx *search.Index) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if idx == nil {
			return mcp.NewToolResultError("search index is not available"), nil
		}
		if err := idx.Rebuild(); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("reindex failed: %v", err)), nil
		}
		count, err := idx.DocCount()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("reindexed but could not count documents: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("reindexed %d documents from the knowledge store", count)), nil
	}
}
