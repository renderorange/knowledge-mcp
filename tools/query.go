package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
			return mcp.NewToolResultError(fmt.Sprintf(
				"invalid category: %q (must be %s)", category, strings.Join(knowledge.ValidCategories(), ", "))), nil
		}

		results, err := idx.Query(ref.Address, query, category, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search error: %v", err)), nil
		}

		scanned := []string{ref.Address}

		// If querying a non-global project, also include global results.
		if ref.Address != projects.GlobalAddress {
			if globalRef, ok := res.GlobalRef(); ok {
				globalResults, globalErr := idx.Query(globalRef.Address, query, category, limit)
				if globalErr == nil {
					results = mergeResults(results, globalResults, limit)
					scanned = append(scanned, globalRef.Address)
				}
			}
		}

		failures := failuresFor(idx, scanned...)

		if len(results) == 0 {
			// Absence from a store that only partly loaded is not absence.
			// Say so as a failure, not as a caveat: a trailing sentence on a
			// success is the one shape that gets skimmed.
			if len(failures) > 0 {
				return mcp.NewToolResultError(fmt.Sprintf(
					"cannot answer: %d knowledge file(s) failed to load, so a missing entry cannot be told apart from an unreadable file. Repair or remove them and retry:\n%s",
					len(failures), failureDetails(failures))), nil
			}
			return mcp.NewToolResultText("no matching knowledge entries found"), nil
		}

		data, _ := json.MarshalIndent(results, "", "  ")
		return mcp.NewToolResultText(withWarnings(string(data), failures)), nil
	}
}

// failuresFor returns the load failures belonging to the projects a query
// actually consulted. A corrupt file elsewhere in the store is not this
// query's problem and must not produce noise here.
func failuresFor(idx *search.Index, addresses ...string) []search.LoadFailure {
	if idx == nil {
		return nil
	}
	all := idx.LoadFailures()
	if len(all) == 0 {
		return nil
	}
	want := make(map[string]bool, len(addresses))
	for _, a := range addresses {
		want[a] = true
	}
	var out []search.LoadFailure
	for _, f := range all {
		if want[f.Project] {
			out = append(out, f)
		}
	}
	return out
}

// failureDetails renders one line per failed file.
func failureDetails(failures []search.LoadFailure) string {
	var b strings.Builder
	for _, f := range failures {
		fmt.Fprintf(&b, "  - %s: %s\n", f.Path, f.Err)
	}
	return b.String()
}

// warningHeader renders the leading warning for a partially loaded store.
func warningHeader(failures []search.LoadFailure) string {
	if len(failures) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "warning: %d knowledge file(s) failed to load; results may be incomplete:\n", len(failures))
	b.WriteString(failureDetails(failures))
	b.WriteString("\n")
	return b.String()
}

// withWarnings puts the store-load warning before the payload. Absence of a
// rule in a store that only partly loaded is not an absent rule, so the warning
// leads rather than trails: a paragraph after the answer is a paragraph nobody
// reads.
func withWarnings(text string, failures []search.LoadFailure) string {
	return warningHeader(failures) + text
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
