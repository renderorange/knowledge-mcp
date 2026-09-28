package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/renderorange/knowledge-mcp/hook"
	"github.com/renderorange/knowledge-mcp/install"
	"github.com/renderorange/knowledge-mcp/internal/diag"
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
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			if err := install.Run(os.Args[2:], version); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		case "uninstall":
			if err := install.RunUninstall(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		case "hook-augment":
			runHook()
			return
		}
	}

	var roots pathList
	var projs pathList
	showVersion := flag.Bool("version", false, "Print version and exit")
	globalPath := flag.String("global", "", "Path to a global knowledge store shared across all projects")
	indexOverride := flag.String("index", "", "Override the search index location")
	storeDir := flag.String("store", "", "Central directory for all knowledge stores; in-tree .agents/ is ignored when set")
	noIndexOnStartup := flag.Bool("no-index-on-startup", false, "Skip indexing on startup; index may be stale or empty")
	debugMode := flag.Bool("debug", false, "Log diagnostics to stderr (and --log-file if set); lands in the client's log")
	logFile := flag.String("log-file", "", "Tee debug lines to this file (implies --debug)")
	flag.Var(&roots, "root", "Org root whose immediate children are projects (repeatable)")
	flag.Var(&projs, "project", "Single project root (repeatable)")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	var dbg *diag.Logger
	if *debugMode || *logFile != "" {
		out := io.Writer(os.Stderr)
		if *logFile != "" {
			f, lerr := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if lerr != nil {
				log.Printf("warning: open --log-file %s: %v (continuing with stderr only)", *logFile, lerr)
			} else {
				defer f.Close()
				out = io.MultiWriter(os.Stderr, f)
			}
		}
		dbg = diag.New(out)
	}

	if len(roots) == 0 && len(projs) == 0 && *globalPath == "" {
		fmt.Fprintln(os.Stderr, "error: at least one --project, --root, or --global is required")
		os.Exit(1)
	}

	tResolve := time.Now()
	resolver, warnings, err := projects.BuildWithStore([]string(roots), []string(projs), *globalPath, *storeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	for _, w := range warnings {
		log.Printf("warning: %s", w)
	}
	dbg.Debugf("startup", "resolve.done", "entries", len(resolver.Entries()), "warnings", len(warnings), "dur", time.Since(tResolve).Round(time.Millisecond))

	tIndex := time.Now()
	indexBasePath, err := indexLocation(*indexOverride, *storeDir, roots, projs, resolver.Entries())
	if err != nil {
		log.Fatalf("determine index location: %v", err)
	}

	idx := search.NewLazyIndex(indexBasePath, resolver.KnownNames())
	defer idx.Close()
	dbg.Debugf("startup", "index.open", "path", indexBasePath, "dur", time.Since(tIndex).Round(time.Millisecond))

	// Open the index in the background: a second instance whose index is
	// locked by another knowledge-mcp (single-writer) must not delay the
	// MCP protocol. Tool calls degrade to a clear error until the lock
	// frees, after which the pending open completes on its own.
	var afterOpen func()
	if !*noIndexOnStartup {
		afterOpen = func() {
			dbg.Debugf("startup", "index.kick", "names", len(resolver.KnownNames()))
			idx.IndexAll(resolver)
		}
	} else {
		dbg.Debugf("startup", "index.kick", "skipped", "no-index-on-startup")
	}
	idx.OpenBackground(afterOpen)

	// Create MCP server
	s := server.NewMCPServer(
		"knowledge-mcp",
		version,
		server.WithToolCapabilities(false),
	)

	// Register tools
	registerTools(s, diag.WrapHandlers(buildHandlers(resolver, idx, len(roots) > 0), dbg))

	// Graceful shutdown on SIGTERM/SIGINT
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Println("shutting down...")
		dbg.Debugf("startup", "shutdown", "signal", "sigterm|sigint")
		idx.Close()
		os.Exit(0)
	}()

	// Start stdio server
	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// runHook executes hook-augment against the recorded install config.
func runHook() {
	if err := hook.Run(os.Stdin, os.Stdout, diag.FromEnv(os.Stderr)); err != nil {
		if os.Getenv("KNM_LOG_LEVEL") != "" {
			fmt.Fprintf(os.Stderr, "hook-augment: %v\n", err)
		}
	}
	os.Exit(0)
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
