package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/renderorange/agents_knowledge/projects"
	"github.com/renderorange/agents_knowledge/search"
	"github.com/renderorange/agents_knowledge/tools"
)

var version = "dev"

type pathList []string

func (p *pathList) String() string {
	return strings.Join(*p, ",")
}

func (p *pathList) Set(v string) error {
	if v == "" {
		return fmt.Errorf("empty path")
	}
	*p = append(*p, v)
	return nil
}

func main() {
	var roots pathList
	var projs pathList
	showVersion := flag.Bool("version", false, "Print version and exit")
	globalPath := flag.String("global", "", "Path to a global knowledge store shared across all projects")
	indexOverride := flag.String("index", "", "Override the search index location")
	flag.Var(&roots, "root", "Org root whose immediate children are projects (repeatable)")
	flag.Var(&projs, "project", "Single project root (repeatable)")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	if len(roots) == 0 && len(projs) == 0 && *globalPath == "" {
		fmt.Fprintln(os.Stderr, "error: at least one --project, --root, or --global is required")
		os.Exit(1)
	}

	resolver, warnings, err := projects.Build([]string(roots), []string(projs), *globalPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	for _, w := range warnings {
		log.Printf("warning: %s", w)
	}

	indexBasePath, err := indexLocation(*indexOverride, roots, projs, resolver.Entries())
	if err != nil {
		log.Fatalf("determine index location: %v", err)
	}

	idx, err := search.NewIndex(indexBasePath, resolver.KnownNames())
	if err != nil {
		log.Fatalf("init search index: %v", err)
	}
	defer idx.Close()

	indexAll(resolver, idx)

	// Create MCP server
	s := server.NewMCPServer(
		"knowledge-mcp",
		version,
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
		tools.InitHandler(resolver),
	)

	s.AddTool(
		mcp.NewTool("write_knowledge",
			mcp.WithDescription("Add a new knowledge entry to a project's .agents/ store"),
			mcp.WithString("project",
				mcp.Required(),
				mcp.Description("Project name (bare if unique, else root/project)"),
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
				mcp.Description("Concise knowledge (10-20 lines). Summarize key facts, don't copy source files. Include only query-able information."),
			),
			mcp.WithString("source",
				mcp.Required(),
				mcp.Description("How this was learned (provenance)"),
			),
			mcp.WithString("rule",
				mcp.Description("Imperative constraint this entry enforces (e.g. \"Never create files outside ./tmp\"). Shown prominently in list output. Max 200 chars."),
			),
		),
		tools.WriteHandler(resolver, idx),
	)

	s.AddTool(
		mcp.NewTool("query_knowledge",
			mcp.WithDescription("Search knowledge entries by text and category"),
			mcp.WithString("project",
				mcp.Required(),
				mcp.Description("Project name (bare if unique, else root/project)"),
			),
			mcp.WithString("query",
				mcp.Description("Full-text search query"),
			),
			mcp.WithString("category",
				mcp.Description("Filter by category: conventions, subsystems, or decisions"),
			),
			mcp.WithNumber("limit",
				mcp.Description("Max results (default 10)"),
			),
		),
		tools.QueryHandler(resolver, idx),
	)

	s.AddTool(
		mcp.NewTool("list_knowledge",
			mcp.WithDescription("List all knowledge entries for a project. Entries with a rule are shown first under constraints."),
			mcp.WithString("project",
				mcp.Required(),
				mcp.Description("Project name (bare if unique, else root/project)"),
			),
			mcp.WithString("category",
				mcp.Description("Filter by category: conventions, subsystems, or decisions"),
			),
		),
		tools.ListHandler(resolver, idx),
	)

	s.AddTool(
		mcp.NewTool("update_knowledge",
			mcp.WithDescription("Update an existing knowledge entry by ID"),
			mcp.WithString("project",
				mcp.Required(),
				mcp.Description("Project name (bare if unique, else root/project)"),
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
			mcp.WithString("rule",
				mcp.Description("New imperative rule (optional, max 200 chars)"),
			),
			mcp.WithString("supersedes",
				mcp.Description("ID of entry this supersedes (optional)"),
			),
		),
		tools.UpdateHandler(resolver, idx),
	)

	// Register list_projects tool (org-wide mode only)
	if len(roots) > 0 {
		s.AddTool(
			mcp.NewTool("list_projects",
				mcp.WithDescription("List all discovered projects across configured roots"),
			),
			tools.ListProjectsHandler(resolver),
		)
	}

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

// indexAll indexes every ref known to the resolver.
func indexAll(res *projects.Resolver, idx *search.Index) {
	for _, ref := range res.Snapshot() {
		switch ref.Kind {
		case projects.KindProject:
			indexProjectKnowledge(ref.Path, ref.Address, idx)
		case projects.KindOrg:
			indexOrgKnowledge(ref.Path, ref.Name, idx)
		case projects.KindGlobal:
			indexProjectKnowledge(ref.Path, ref.Address, idx)
		}
	}
}

// indexProjectKnowledge indexes all knowledge files in a single project
// under the given addressing name.
func indexProjectKnowledge(projectPath, projectName string, idx *search.Index) {
	agentsDir := filepath.Join(projectPath, ".agents")
	for _, cat := range knowledge.ValidCategories() {
		catPath := knowledge.CategoryFilePath(agentsDir, cat)
		kf, err := knowledge.Load(catPath)
		if err != nil {
			continue
		}
		for _, entry := range kf.Entries {
			doc := search.SearchDocument{
				Summary:  entry.Summary,
				Detail:   entry.Detail,
				Rule:     entry.Rule,
				Category: cat,
				Project:  projectName,
			}
			if addErr := idx.Add(projectName+"/"+entry.ID, doc); addErr != nil {
				log.Printf("warning: failed to index %s/%s: %v", projectName, entry.ID, addErr)
			}
		}
	}
}

// indexOrgKnowledge indexes an org root's .agents/knowledge/ files as
// individual markdown sections, so queries return only the relevant
// section instead of the entire document.
func indexOrgKnowledge(root, orgName string, idx *search.Index) {
	orgAgentsDir := filepath.Join(root, ".agents", "knowledge")
	for _, catFile := range []string{"architecture.md", "review.md"} {
		filePath := filepath.Join(orgAgentsDir, catFile)
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		for _, sec := range splitSections(string(data)) {
			heading, body := sec[0], sec[1]
			doc := search.SearchDocument{
				Summary:  fmt.Sprintf("%s: %s", catFile, heading),
				Detail:   body,
				Category: "conventions",
				Project:  orgName,
			}
			id := fmt.Sprintf("%s/org-%s::%s", orgName, catFile, heading)
			if addErr := idx.Add(id, doc); addErr != nil {
				log.Printf("warning: failed to index %s: %v", id, addErr)
			}
		}
	}
}

// splitSections splits a knowledge markdown document on lines starting
// with "## ". Content before the first section heading is kept under
// "Overview". Subsection headings (### ...) stay part of their parent
// section body.
func splitSections(data string) [][2]string {
	var sections [][2]string
	var currentTitle string
	var current strings.Builder

	flush := func() {
		title := currentTitle
		if title == "" {
			title = "Overview"
		}
		body := strings.TrimSpace(current.String())
		if title == "Overview" && body == "" {
			current.Reset()
			return
		}
		sections = append(sections, [2]string{title, body})
		current.Reset()
	}

	for _, line := range strings.Split(data, "\n") {
		if rest, ok := strings.CutPrefix(line, "## "); ok {
			flush()
			currentTitle = strings.TrimSpace(rest)
			current.WriteString(line)
			current.WriteString("\n")
			continue
		}
		current.WriteString(line)
		current.WriteString("\n")
	}
	flush()
	return sections
}

// indexLocation picks the bleve index path: explicit override, legacy
// per-config locations for single-entry configs, or a hashed XDG state
// dir for multi-entry configs.
func indexLocation(override string, roots, projs pathList, canonicalEntries []string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	if len(roots)+len(projs) == 1 {
		single := roots
		if len(single) == 0 {
			single = projs
		}
		return filepath.Join(single[0], ".agents", ".index"), nil
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve state dir: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	sum := sha256.Sum256([]byte(strings.Join(canonicalEntries, "\x00")))
	return filepath.Join(base, "knowledge-mcp", hex.EncodeToString(sum[:8])+".index"), nil
}
