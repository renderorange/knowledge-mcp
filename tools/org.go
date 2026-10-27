package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
)

// writeOrgSection appends a markdown section to an org knowledge file. It
// never guesses the file: a missing name returns the available files.
func writeOrgSection(res *projects.Resolver, ref projects.Ref, project, file, summary, detail, source, rule string) *mcp.CallToolResult {
	heading := strings.TrimSpace(summary)
	if heading == "" {
		return mcp.NewToolResultError("summary must not be blank for org sections")
	}
	if rule != "" {
		return mcp.NewToolResultError("rule is not supported for org sections; include the constraint in detail")
	}

	dir := res.OrgKnowledgeDir(ref)
	if file == "" {
		available, err := knowledge.OrgKnowledgeFiles(dir)
		if err != nil || len(available) == 0 {
			return mcp.NewToolResultError(fmt.Sprintf(
				"%q is an org root; file is required and there are no markdown files in %s (pass file=<name>.md to create one)",
				project, dir))
		}
		return mcp.NewToolResultError(fmt.Sprintf(
			"%q is an org root; file is required (available: %s)", project, strings.Join(available, ", ")))
	}
	if err := validateOrgFile(file); err != nil {
		return mcp.NewToolResultError(err.Error())
	}

	path := filepath.Join(dir, file)
	mu := fileLocks.Get(path)
	mu.Lock()
	defer mu.Unlock()

	existing, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return mcp.NewToolResultError(fmt.Sprintf("read org knowledge file: %v", readErr))
	}
	if readErr == nil {
		_, lookErr := knowledge.OrgSectionBody(string(existing), heading)
		switch {
		case lookErr == nil:
			return mcp.NewToolResultError(fmt.Sprintf(
				"section %q already exists in %s; use update_knowledge with id %q",
				heading, path, orgSectionID(file, heading)))
		case errors.Is(lookErr, knowledge.ErrOrgDuplicateSection):
			return mcp.NewToolResultError(fmt.Sprintf(
				"section %q appears more than once in %s; fix the file before writing", heading, path))
		}
	}

	if err := knowledge.EnsureDir(dir); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create org knowledge directory: %v", err))
	}
	data, err := knowledge.AppendOrgSection(string(existing), heading, orgSectionBody(detail, source))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("append org section: %v", err))
	}
	if err := knowledge.SaveText(path, data); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("save org knowledge file: %v", err))
	}
	return mcp.NewToolResultText(fmt.Sprintf("wrote section %q to %s", heading, path))
}

// updateOrgSection replaces the body of an org markdown section. The id is
// org-<file>::<heading>; headings are immutable because they are part of the
// id, and provenance lines are carried over from the old body.
func updateOrgSection(res *projects.Resolver, ref projects.Ref, id, summary, detail, rule, supersedes string) *mcp.CallToolResult {
	if summary != "" {
		return mcp.NewToolResultError("summary is not supported for org sections; headings are immutable")
	}
	if rule != "" {
		return mcp.NewToolResultError("rule is not supported for org sections; include the constraint in detail")
	}
	if supersedes != "" {
		return mcp.NewToolResultError("supersedes is not supported for org sections")
	}
	if detail == "" {
		return mcp.NewToolResultError("detail is required for org section updates")
	}
	if len(detail) > maxDetailSize {
		return mcp.NewToolResultError(fmt.Sprintf("detail too long: %d bytes (max %d)", len(detail), maxDetailSize))
	}

	file, heading, ok := parseOrgSectionID(id)
	if !ok {
		return mcp.NewToolResultError(fmt.Sprintf("invalid org section id %q: want org-<file>::<heading>", id))
	}
	if err := validateOrgFile(file); err != nil {
		return mcp.NewToolResultError(err.Error())
	}

	dir := res.OrgKnowledgeDir(ref)
	available, err := knowledge.OrgKnowledgeFiles(dir)
	if err != nil || !containsString(available, file) {
		names := strings.Join(available, ", ")
		if names == "" {
			names = "none"
		}
		return mcp.NewToolResultError(fmt.Sprintf("org root has no knowledge file %q (available: %s)", file, names))
	}

	path := filepath.Join(dir, file)
	mu := fileLocks.Get(path)
	mu.Lock()
	defer mu.Unlock()

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("read org knowledge file: %v", readErr))
	}

	body, bodyErr := knowledge.OrgSectionBody(string(data), heading)
	if bodyErr != nil {
		if errors.Is(bodyErr, knowledge.ErrOrgDuplicateSection) {
			return mcp.NewToolResultError(fmt.Sprintf(
				"section %q appears more than once in %s; fix the file before updating", heading, path))
		}
		return mcp.NewToolResultError(fmt.Sprintf("section %q not found in %s", heading, path))
	}

	newBody := orgSectionBody(detail, "")
	if src := trailingSource(body); src != "" && trailingSource(newBody) == "" {
		newBody += "\n\n" + src
	}

	updated, err := knowledge.ReplaceOrgSection(string(data), heading, newBody)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("replace org section: %v", err))
	}
	if err := knowledge.SaveText(path, updated); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("save org knowledge file: %v", err))
	}
	return mcp.NewToolResultText(fmt.Sprintf("updated section %q in %s", heading, path))
}

// validateOrgFile accepts a bare markdown filename only. It rejects path
// separators, traversal, "::" (the id delimiter) and empty stems.
func validateOrgFile(name string) error {
	if name == "" {
		return errors.New("file is required")
	}
	if name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid file %q: must be a bare .md filename (no directories)", name)
	}
	if strings.Contains(name, "::") {
		return fmt.Errorf("invalid file %q: must not contain \"::\"", name)
	}
	ext := filepath.Ext(name)
	if !strings.EqualFold(ext, ".md") || strings.TrimSpace(strings.TrimSuffix(name, ext)) == "" {
		return fmt.Errorf("invalid file %q: must be a .md filename", name)
	}
	return nil
}

func parseOrgSectionID(id string) (file, heading string, ok bool) {
	rest, found := strings.CutPrefix(id, "org-")
	if !found {
		return "", "", false
	}
	file, heading, found = strings.Cut(rest, "::")
	heading = strings.TrimSpace(heading)
	if !found || file == "" || heading == "" {
		return "", "", false
	}
	return file, heading, true
}

func orgSectionID(file, heading string) string {
	return "org-" + file + "::" + heading
}

func orgSectionBody(detail, source string) string {
	body := strings.TrimSpace(detail)
	if source == "" {
		return body
	}
	return body + "\n\nSource: " + strings.TrimSpace(source)
}

// trailingSource returns a final "Source: ..." line from a section body.
func trailingSource(body string) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	last := strings.TrimSpace(lines[len(lines)-1])
	if strings.HasPrefix(last, "Source: ") {
		return last
	}
	return ""
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
