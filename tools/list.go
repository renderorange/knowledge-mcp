package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
	"github.com/renderorange/knowledge-mcp/search"
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

		filterCategory := request.GetString("category", "")
		if filterCategory != "" && !knowledge.IsValidCategory(filterCategory) {
			return mcp.NewToolResultError(fmt.Sprintf("invalid category: %q (must be conventions, subsystems, or decisions)", filterCategory)), nil
		}

		output := ""

		// For non-global lookups, prepend the shared global store with an
		// explicit "(global)" label so merged entries are not mistaken for
		// the project's own.
		if ref.Address != projects.GlobalAddress {
			if globalRef, ok := res.GlobalRef(); ok {
				output += listProjectEntries(res.AgentsDir(globalRef), projects.GlobalAddress, filterCategory, " (global)")
			}
		}

		if ref.Kind == projects.KindOrg {
			output += listOrgEntries(idx, ref.Address, filterCategory)
			if strings.TrimSpace(output) == "" {
				return mcp.NewToolResultText("no knowledge entries found"), nil
			}
			return mcp.NewToolResultText(output), nil
		}

		local := listProjectEntries(res.AgentsDir(ref), project, filterCategory, "")

		if local == "" {
			if strings.TrimSpace(output) == "" {
				return mcp.NewToolResultText("no knowledge entries found"), nil
			}
			output += "no project-specific knowledge entries found\n"
		} else {
			output += local
		}

		return mcp.NewToolResultText(output), nil
	}
}

// listOrgEntries lists an org root's knowledge sections, grouped by source
// file.
func listOrgEntries(idx *search.Index, orgAddress, filterCategory string) string {
	if idx == nil {
		return ""
	}

	results, err := idx.Query(orgAddress, "", filterCategory, 200)
	if err != nil {
		return ""
	}
	if len(results) == 0 {
		return ""
	}

	type fileGroup struct {
		file    string
		results []search.SearchResult
	}
	var groups []fileGroup
	for _, r := range results {
		file, _, hasSection := strings.Cut(strings.TrimPrefix(r.ID, "org-"), "::")
		if !hasSection {
			continue
		}
		if len(groups) == 0 || groups[len(groups)-1].file != file {
			groups = append(groups, fileGroup{file: file})
		}
		groups[len(groups)-1].results = append(groups[len(groups)-1].results, r)
	}

	output := "## org-level knowledge (by section)\n\n"
	for _, g := range groups {
		output += fmt.Sprintf("### %s (%d sections)\n", g.file, len(g.results))
		for _, r := range g.results {
			_, heading, _ := strings.Cut(strings.TrimPrefix(r.ID, "org-"), "::")
			output += fmt.Sprintf("- [%s :: %s]\n", g.file, heading)
		}
		output += "\n"
	}
	return output
}

func listProjectEntries(agentsDir, projectName, filterCategory, headerSuffix string) string {

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
		output += fmt.Sprintf("## constraints%s (%d)\n", headerSuffix, len(constraints))
		for _, entry := range constraints {
			output += fmt.Sprintf("[%s] %s (date: %s)\n", entry.ID, entry.Rule, entry.Date)
		}
		output += "\n"
	}

	for _, ce := range categories {
		if len(ce.entries) == 0 {
			continue
		}
		output += fmt.Sprintf("## %s%s (%d entries)\n", ce.category, headerSuffix, len(ce.entries))
		for _, entry := range ce.entries {
			output += fmt.Sprintf("- [%s] %s (date: %s)\n",
				entry.ID, entry.Summary, entry.Date)
		}
		output += "\n"
	}

	return output
}
