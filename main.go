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

	"github.com/mark3labs/mcp-go/server"

	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
	"github.com/renderorange/knowledge-mcp/search"
	"github.com/renderorange/knowledge-mcp/tools"
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
	storeDir := flag.String("store", "", "Central directory for all knowledge stores; in-tree .agents/ is ignored when set")
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

	resolver, warnings, err := projects.BuildWithStore([]string(roots), []string(projs), *globalPath, *storeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	for _, w := range warnings {
		log.Printf("warning: %s", w)
	}

	indexBasePath, err := indexLocation(*indexOverride, *storeDir, roots, projs, resolver.Entries())
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
	registerTools(s, buildHandlers(resolver, idx, len(roots) > 0))

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
			indexProjectKnowledge(res.AgentsDir(ref), ref.Address, idx)
		case projects.KindOrg:
			indexOrgKnowledge(res.OrgKnowledgeDir(ref), ref.Name, idx)
		case projects.KindGlobal:
			indexProjectKnowledge(res.AgentsDir(ref), ref.Address, idx)
		}
	}
}

// indexProjectKnowledge indexes all knowledge files under an agents dir
// under the given addressing name.
func indexProjectKnowledge(agentsDir, projectName string, idx *search.Index) {
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

// indexOrgKnowledge indexes an org's knowledge files as individual markdown
// sections, so queries return only the relevant section instead of the
// entire document.
func indexOrgKnowledge(knowledgeDir, orgName string, idx *search.Index) {
	for _, catFile := range []string{"architecture.md", "review.md"} {
		filePath := filepath.Join(knowledgeDir, catFile)
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

// registerTools adds every registry tool with an available handler.
// list_projects is only served in org-wide mode (its handler is absent
// otherwise) — the loop does the gating by design.
func registerTools(s *server.MCPServer, handlerMap map[string]server.ToolHandlerFunc) {
	for _, spec := range tools.Registry {
		handler, ok := handlerMap[spec.Name]
		if !ok {
			continue // not served in this mode
		}
		s.AddTool(spec.MCPTool(), handler)
	}
}

// buildHandlers wires the tools package handlers to the resolver/index.
func buildHandlers(res *projects.Resolver, idx *search.Index, orgMode bool) map[string]server.ToolHandlerFunc {
	h := map[string]server.ToolHandlerFunc{
		tools.ToolInitKnowledge:   tools.InitHandler(res),
		tools.ToolWriteKnowledge:  tools.WriteHandler(res, idx),
		tools.ToolQueryKnowledge:  tools.QueryHandler(res, idx),
		tools.ToolListKnowledge:   tools.ListHandler(res, idx),
		tools.ToolUpdateKnowledge: tools.UpdateHandler(res, idx),
	}
	if orgMode {
		h[tools.ToolListProjects] = tools.ListProjectsHandler(res)
	}
	return h
}

// indexLocation picks the bleve index path: explicit override, a --store
// default (<store>/.index), legacy per-config locations for single-entry
// configs, or a hashed XDG state dir for multi-entry configs.
func indexLocation(override, store string, roots, projs pathList, canonicalEntries []string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	if store != "" {
		return filepath.Join(store, ".index"), nil
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
