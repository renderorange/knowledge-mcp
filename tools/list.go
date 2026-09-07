package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/agents_knowledge/knowledge"
	"github.com/renderorange/agents_knowledge/projects"
	"github.com/renderorange/agents_knowledge/search"
)

// ListHandler handles the list_knowledge MCP tool.
func ListHandler(res *projects.Resolver, idx *search.Index) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		project, err := request.RequireString("project")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		ref, resolveErr := res.Resolve(project)
		if resolveErr != nil {
			return mcp.NewToolResultError(resolveErr.Error()), nil
		}

		// For org roots, use the index to list entries
		if ref.Kind == projects.KindOrg {
			results, queryErr := idx.Query(ref.Address, "", "", 100)
			if queryErr != nil {
				return mcp.NewToolResultError(fmt.Sprintf("search error: %v", queryErr)), nil
			}
			if len(results) == 0 {
				return mcp.NewToolResultText("no knowledge entries found"), nil
			}

			var orgConstraints []search.SearchResult
			var orgRegular []search.SearchResult
			for _, r := range results {
				if r.Rule != "" {
					orgConstraints = append(orgConstraints, r)
				} else {
					orgRegular = append(orgRegular, r)
				}
			}

			output := ""
			if len(orgConstraints) > 0 {
				output += fmt.Sprintf("## constraints (%d)\n", len(orgConstraints))
				for _, r := range orgConstraints {
					id := strings.TrimPrefix(r.ID, "org-")
					output += fmt.Sprintf("[%s] %s\n", id, r.Rule)
				}
				output += "\n"
			}
			if len(orgRegular) > 0 {
				output += fmt.Sprintf("## org-level knowledge (%d entries)\n\n", len(orgRegular))
				for _, r := range orgRegular {
					id := strings.TrimPrefix(r.ID, "org-")
					output += fmt.Sprintf("- [%s] %s\n", id, r.Summary)
				}
			}
			return mcp.NewToolResultText(output), nil
		}

		projectPath := ref.Path

		filterCategory := request.GetString("category", "")
		if filterCategory != "" && !knowledge.IsValidCategory(filterCategory) {
			return mcp.NewToolResultError(fmt.Sprintf("invalid category: %q (must be conventions, subsystems, or decisions)", filterCategory)), nil
		}

		output := ""

		// If querying a non-global project, also include global entries.
		if ref.Address != projects.GlobalAddress {
			if globalRef, ok := res.GlobalRef(); ok {
				output += listProjectEntries(globalRef.Path, projects.GlobalAddress, filterCategory)
			}
		}

		output += listProjectEntries(projectPath, project, filterCategory)

		if output == "" {
			return mcp.NewToolResultText("no knowledge entries found"), nil
		}

		return mcp.NewToolResultText(output), nil
	}
}

func listProjectEntries(projectPath, projectName, filterCategory string) string {
	agentsDir := filepath.Join(projectPath, ".agents")

	var constraints []knowledge.Entry
	type catEntries struct {
		category string
		entries  []knowledge.Entry
	}
	var categories []catEntries

	for _, cat := range knowledge.ValidCategories() {
		if filterCategory != "" && cat != filterCategory {
			continue
		}

		catPath := knowledge.CategoryFilePath(agentsDir, cat)

		mu := fileLocks.Get(catPath)
		mu.RLock()
		kf, loadErr := knowledge.LoadOrCreate(catPath, projectName)
		mu.RUnlock()

		if loadErr != nil {
			continue
		}

		if len(kf.Entries) == 0 {
			continue
		}

		var regular []knowledge.Entry
		for _, entry := range kf.Entries {
			if entry.Rule != "" {
				constraints = append(constraints, entry)
			} else {
				regular = append(regular, entry)
			}
		}
		categories = append(categories, catEntries{category: cat, entries: regular})
	}

	output := ""

	if len(constraints) > 0 {
		output += fmt.Sprintf("## constraints (%d)\n", len(constraints))
		for _, entry := range constraints {
			output += fmt.Sprintf("[%s] %s (date: %s)\n", entry.ID, entry.Rule, entry.Date)
		}
		output += "\n"
	}

	for _, ce := range categories {
		if len(ce.entries) == 0 {
			continue
		}
		output += fmt.Sprintf("## %s (%d entries)\n", ce.category, len(ce.entries))
		for _, entry := range ce.entries {
			output += fmt.Sprintf("- [%s] %s (date: %s)\n",
				entry.ID, entry.Summary, entry.Date)
		}
		output += "\n"
	}

	return output
}
