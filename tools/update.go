package tools

import (
	"context"
	"fmt"
	"log"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
	"github.com/renderorange/knowledge-mcp/search"
)

// UpdateHandler handles the update_knowledge MCP tool.
func UpdateHandler(res *projects.Resolver, idx *search.Index) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

		id, err := request.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		ref, resolveErr := res.Resolve(project)
		if resolveErr != nil {
			return mcp.NewToolResultError(resolveErr.Error()), nil
		}
		if ref.Kind == projects.KindOrg {
			return mcp.NewToolResultError(fmt.Sprintf(
				"%q is an org root; org-level knowledge is file-based — edit %s directly",
				project, res.OrgKnowledgeDir(ref))), nil
		}

		agentsDir := res.AgentsDir(ref)
		catPath := knowledge.CategoryFilePath(agentsDir, category)

		mu := fileLocks.Get(catPath)
		mu.Lock()
		defer mu.Unlock()

		kf, loadErr := knowledge.Load(catPath)
		if loadErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("load knowledge file: %v", loadErr)), nil
		}

		// Collect optional fields
		newSummary := request.GetString("summary", "")
		newDetail := request.GetString("detail", "")
		newRule := request.GetString("rule", "")
		newSupersedes := request.GetString("supersedes", "")

		// Validate optional fields before applying
		if newSummary != "" && len(newSummary) > 100 {
			return mcp.NewToolResultError(fmt.Sprintf("summary too long: %d chars (max 100)", len(newSummary))), nil
		}
		if newDetail != "" && len(newDetail) > maxDetailSize {
			return mcp.NewToolResultError(fmt.Sprintf("detail too long: %d bytes (max %d)", len(newDetail), maxDetailSize)), nil
		}
		if newRule != "" && len(newRule) > 200 {
			return mcp.NewToolResultError(fmt.Sprintf("rule too long: %d chars (max 200)", len(newRule))), nil
		}

		// Reject no-op updates
		if newSummary == "" && newDetail == "" && newRule == "" && newSupersedes == "" {
			return mcp.NewToolResultError("no fields to update (provide summary, detail, rule, or supersedes)"), nil
		}

		// Validate supersedes references an existing ID in the same category
		if newSupersedes != "" {
			found := false
			for _, entry := range kf.Entries {
				if entry.ID == newSupersedes {
					found = true
					break
				}
			}
			if !found {
				return mcp.NewToolResultError(fmt.Sprintf("supersedes references unknown entry: %q in %s", newSupersedes, category)), nil
			}
		}

		// Find and update the entry
		found := false
		for i, entry := range kf.Entries {
			if entry.ID == id {
				if newSummary != "" {
					kf.Entries[i].Summary = newSummary
				}
				if newDetail != "" {
					kf.Entries[i].Detail = newDetail
				}
				if newRule != "" {
					kf.Entries[i].Rule = newRule
				}
				if newSupersedes != "" {
					kf.Entries[i].Supersedes = newSupersedes
				}
				kf.Entries[i].Date = knowledge.Today()
				found = true
				break
			}
		}

		if !found {
			return mcp.NewToolResultError(fmt.Sprintf("entry not found: %s", id)), nil
		}

		if saveErr := knowledge.Save(catPath, kf); saveErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("save knowledge file: %v", saveErr)), nil
		}

		// Re-index in bleve
		if idx != nil {
			for _, entry := range kf.Entries {
				if entry.ID == id {
					doc := search.SearchDocument{
						Summary:  entry.Summary,
						Detail:   entry.Detail,
						Rule:     entry.Rule,
						Category: category,
						Project:  ref.Address,
					}
					if indexErr := idx.Add(ref.Address+"/"+id, doc); indexErr != nil {
						log.Printf("warning: failed to index %s/%s: %v", ref.Address, id, indexErr)
					}
					break
				}
			}
		}

		return mcp.NewToolResultText(fmt.Sprintf("updated %s in %s/%s.yaml", id, project, category)), nil
	}
}
