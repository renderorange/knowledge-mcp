package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/renderorange/agents_knowledge/knowledge"
	"github.com/renderorange/agents_knowledge/search"
	"github.com/renderorange/agents_knowledge/tools"
)

func main() {
	projectPath := flag.String("project", "", "Path to a single project root")
	orgRoot := flag.String("root", "", "Path to org root (discovers projects)")
	flag.Parse()

	if *projectPath == "" && *orgRoot == "" {
		fmt.Fprintln(os.Stderr, "error: either --project or --root is required")
		os.Exit(1)
	}

	if *projectPath != "" && *orgRoot != "" {
		fmt.Fprintln(os.Stderr, "error: --project and --root are mutually exclusive")
		os.Exit(1)
	}

	// Determine project resolver and index path
	var projectPathFn func(string) string
	var indexBasePath string

	if *orgRoot != "" {
		absOrgRoot, err := filepath.Abs(*orgRoot)
		if err != nil {
			log.Fatalf("resolve org root: %v", err)
		}
		*orgRoot = absOrgRoot

		// Org-wide mode: discover projects with path traversal protection
		projectPathFn = func(project string) string {
			// Reject path separators and traversal sequences
			if strings.ContainsAny(project, `/\`) || project == "." || project == ".." {
				return ""
			}

			path := filepath.Join(*orgRoot, project)

			// Resolve to absolute and verify it's under orgRoot
			absPath, err := filepath.Abs(path)
			if err != nil {
				return ""
			}
			if !strings.HasPrefix(absPath, *orgRoot+string(filepath.Separator)) {
				return ""
			}

			info, err := os.Stat(absPath)
			if err == nil && info.IsDir() {
				return absPath
			}
			return ""
		}
		indexBasePath = filepath.Join(*orgRoot, ".agents", ".index")
	} else {
		// Single-project mode
		absProjectPath, err := filepath.Abs(*projectPath)
		if err != nil {
			log.Fatalf("resolve project path: %v", err)
		}
		*projectPath = absProjectPath

		projectName := filepath.Base(*projectPath)
		projectPathFn = func(project string) string {
			if project == projectName {
				return *projectPath
			}
			return ""
		}
		indexBasePath = filepath.Join(*projectPath, ".agents", ".index")
	}

	// Initialize bleve index
	idx, err := search.NewIndex(indexBasePath)
	if err != nil {
		log.Fatalf("init search index: %v", err)
	}
	defer idx.Close()

	// Index existing knowledge files
	if *orgRoot != "" {
		indexExistingKnowledge(*orgRoot, idx)
	} else {
		indexProjectKnowledge(*projectPath, idx)
	}

	// Create MCP server
	s := server.NewMCPServer(
		"knowledge-mcp",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	// Register tools
	s.AddTool(
		mcp.NewTool("init_knowledge",
			mcp.WithDescription("Create .agents/ knowledge directory structure for a project"),
			mcp.WithString("project_path",
				mcp.Required(),
				mcp.Description("Absolute path to the project root"),
			),
		),
		tools.InitHandler,
	)

	s.AddTool(
		mcp.NewTool("write_knowledge",
			mcp.WithDescription("Add a new knowledge entry to a project's .agents/ store"),
			mcp.WithString("project",
				mcp.Required(),
				mcp.Description("Project name"),
			),
			mcp.WithString("category",
				mcp.Required(),
				mcp.Description("Category: conventions, subsystems, or decisions"),
			),
			mcp.WithString("summary",
				mcp.Required(),
				mcp.Description("One-line description (max 100 chars)"),
			),
			mcp.WithString("detail",
				mcp.Required(),
				mcp.Description("Full knowledge content (markdown supported)"),
			),
			mcp.WithString("confidence",
				mcp.Required(),
				mcp.Description("Confidence level: high, medium, or low"),
			),
			mcp.WithString("source",
				mcp.Required(),
				mcp.Description("How this was learned (provenance)"),
			),
		),
		tools.WriteHandler(projectPathFn, idx),
	)

	s.AddTool(
		mcp.NewTool("query_knowledge",
			mcp.WithDescription("Search knowledge entries by text, category, and confidence"),
			mcp.WithString("project",
				mcp.Required(),
				mcp.Description("Project name"),
			),
			mcp.WithString("query",
				mcp.Description("Full-text search query"),
			),
			mcp.WithString("category",
				mcp.Description("Filter by category: conventions, subsystems, or decisions"),
			),
			mcp.WithString("confidence",
				mcp.Description("Filter by confidence: high, medium, or low"),
			),
			mcp.WithString("stale",
				mcp.Description("Filter by staleness: true for stale only, false for fresh only"),
			),
			mcp.WithNumber("limit",
				mcp.Description("Max results (default 10)"),
			),
		),
		tools.QueryHandler(projectPathFn, idx),
	)

	s.AddTool(
		mcp.NewTool("list_knowledge",
			mcp.WithDescription("List all knowledge entries for a project (summaries only)"),
			mcp.WithString("project",
				mcp.Required(),
				mcp.Description("Project name"),
			),
			mcp.WithString("category",
				mcp.Description("Filter by category: conventions, subsystems, or decisions"),
			),
		),
		tools.ListHandler(projectPathFn),
	)

	s.AddTool(
		mcp.NewTool("update_knowledge",
			mcp.WithDescription("Update an existing knowledge entry by ID"),
			mcp.WithString("project",
				mcp.Required(),
				mcp.Description("Project name"),
			),
			mcp.WithString("category",
				mcp.Required(),
				mcp.Description("Category: conventions, subsystems, or decisions"),
			),
			mcp.WithString("id",
				mcp.Required(),
				mcp.Description("Entry ID to update (e.g., conv-001)"),
			),
			mcp.WithString("summary",
				mcp.Description("New summary (optional)"),
			),
			mcp.WithString("detail",
				mcp.Description("New detail (optional)"),
			),
			mcp.WithString("confidence",
				mcp.Description("New confidence level (optional)"),
			),
			mcp.WithString("supersedes",
				mcp.Description("ID of entry this supersedes (optional)"),
			),
		),
		tools.UpdateHandler(projectPathFn, idx),
	)

	// Register list_projects tool (org-wide mode only)
	if *orgRoot != "" {
		s.AddTool(
			mcp.NewTool("list_projects",
				mcp.WithDescription("List all discovered projects under the org root"),
			),
			tools.ListProjectsHandler(*orgRoot, projectPathFn),
		)
	}

	s.AddTool(
		mcp.NewTool("verify_knowledge",
			mcp.WithDescription("Mark a knowledge entry as verified, extending its expiry"),
			mcp.WithString("project",
				mcp.Required(),
				mcp.Description("Project name"),
			),
			mcp.WithString("category",
				mcp.Required(),
				mcp.Description("Category: conventions, subsystems, or decisions"),
			),
			mcp.WithString("id",
				mcp.Required(),
				mcp.Description("Entry ID to verify (e.g., conv-001)"),
			),
		),
		tools.VerifyHandler(projectPathFn),
	)

	// Graceful shutdown on SIGTERM/SIGINT
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Println("shutting down...")
		idx.Close()
		os.Exit(0)
	}()

	// Start stdio server
	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// indexProjectKnowledge indexes all knowledge files in a single project.
func indexProjectKnowledge(projectPath string, idx *search.Index) {
	agentsDir := filepath.Join(projectPath, ".agents")
	for _, cat := range knowledge.ValidCategories() {
		catPath := knowledge.CategoryFilePath(agentsDir, cat)
		kf, err := knowledge.Load(catPath)
		if err != nil {
			continue
		}
		for _, entry := range kf.Entries {
			doc := search.SearchDocument{
				Summary:    entry.Summary,
				Detail:     entry.Detail,
				Category:   cat,
				Confidence: entry.Confidence,
			}
			idx.Add(entry.ID, doc)
		}
	}
}

// indexExistingKnowledge indexes knowledge from all projects under an org root.
func indexExistingKnowledge(orgRoot string, idx *search.Index) {
	// Index org-level knowledge
	orgAgentsDir := filepath.Join(orgRoot, ".agents", "knowledge")
	for _, catFile := range []string{"architecture.md", "review.md"} {
		filePath := filepath.Join(orgAgentsDir, catFile)
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		doc := search.SearchDocument{
			Summary:  fmt.Sprintf("org-level knowledge: %s", catFile),
			Detail:   string(data),
			Category: "conventions",
		}
		idx.Add("org-"+catFile, doc)
	}

	// Discover and index project knowledge
	entries, err := os.ReadDir(orgRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == ".agents" || entry.Name() == ".git" {
			continue
		}
		projectPath := filepath.Join(orgRoot, entry.Name())
		indexProjectKnowledge(projectPath, idx)
	}
}
