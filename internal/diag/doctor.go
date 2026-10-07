package diag

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/renderorange/knowledge-mcp/install"
	"github.com/renderorange/knowledge-mcp/projects"
	"github.com/renderorange/knowledge-mcp/search"
)

// ErrQueryBlocked is returned by watchdog when fn exceeds the deadline —
// the slow-kick query-block detector.
var ErrQueryBlocked = errors.New("query blocked: index never became ready")

// doctorPaths collects repeatable path flags (mirror of main's pathList).
type doctorPaths []string

func (p *doctorPaths) String() string { return strings.Join(*p, ",") }
func (p *doctorPaths) Set(v string) error {
	if v == "" {
		return fmt.Errorf("empty path")
	}
	*p = append(*p, v)
	return nil
}

// Finding is one doctor check result. Level is "ok", "warn", or "fail".
type Finding struct {
	Level  string
	Name   string
	Detail string
}

// RunDoctor executes the debug subcommand against the same flags as server
// mode, prints the report to stdout, and returns the process exit code:
// 1 when any finding is "fail", else 0.
func RunDoctor(args []string, version string) int {
	fs := flag.NewFlagSet("debug", flag.ContinueOnError)
	var roots, projs doctorPaths
	globalPath := fs.String("global", "", "Path to a global knowledge store shared across all projects")
	indexOverride := fs.String("index", "", "Deprecated: the search index is in-memory; accepted and ignored")
	storeDir := fs.String("store", "", "Central directory for all knowledge stores")
	noIndexOnStartup := fs.Bool("no-index-on-startup", false, "Deprecated: the index is always built at startup; accepted and ignored")
	timeout := fs.Duration("timeout", 5*time.Second, "Self-query watchdog deadline")
	logFile := fs.String("log-file", "", "Tee the report to this file")
	fs.Var(&roots, "root", "Org root whose immediate children are projects (repeatable)")
	fs.Var(&projs, "project", "Single project root (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	out := io.Writer(os.Stdout)
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: open --log-file: %v\n", err)
		} else {
			defer f.Close()
			out = io.MultiWriter(os.Stdout, f)
		}
	}

	var findings []Finding
	add := func(level, name, detail string) {
		findings = append(findings, Finding{level, name, detail})
	}

	fmt.Fprintf(out, "knowledge-mcp debug (version %s)\n\n", version)
	fmt.Fprintln(out, "== Resolution ==")

	if len(roots) == 0 && len(projs) == 0 && *globalPath == "" {
		add("fail", "config.empty", "at least one --project, --root, or --global is required")
		printFindings(out, findings)
		return 1
	}

	tResolve := time.Now()
	resolver, warnings, err := projects.BuildWithStore([]string(roots), []string(projs), *globalPath, *storeDir)
	resolveDur := time.Since(tResolve).Round(time.Millisecond)
	if err != nil {
		add("fail", "resolve.failed", err.Error())
		fmt.Fprintf(out, "resolve: fail: %v\n", err)
		printFindings(out, findings)
		return 1
	}
	for _, w := range warnings {
		add("warn", "resolve.warning", w)
		fmt.Fprintf(out, "warning: %s\n", w)
	}
	for _, ref := range resolver.Snapshot() {
		fmt.Fprintf(out, "- %s kind=%d path=%s\n", ref.Address, ref.Kind, ref.Path)
	}
	fmt.Fprintf(out, "entries=%d warnings=%d\n", len(resolver.Entries()), len(warnings))

	if *indexOverride != "" {
		fmt.Fprintln(out, "warning: --index is deprecated and ignored; the search index is per-process in-memory")
	}
	if *noIndexOnStartup {
		fmt.Fprintln(out, "warning: --no-index-on-startup is deprecated and ignored; the index is always built at startup")
	}

	tIndex := time.Now()
	idx, err := search.NewIndex()
	if err != nil {
		add("fail", "index.create", err.Error())
		printFindings(out, findings)
		return 1
	}
	defer idx.Close()

	fmt.Fprintln(out, "\n== Startup trail ==")
	fmt.Fprintf(out, "resolve.done dur=%s\n", resolveDur)
	fmt.Fprintf(out, "index.kick started (in-memory, dur=%s)\n", time.Since(tIndex).Round(time.Millisecond))
	go idx.IndexAll(resolver)

	tQuery := time.Now()
	qerr := watchdog(*timeout, func() error {
		_, err := idx.Query("", "", "", 1)
		return err
	})
	queryDur := time.Since(tQuery).Round(time.Millisecond)
	switch {
	case errors.Is(qerr, ErrQueryBlocked):
		add("fail", "query.blocked", fmt.Sprintf("dur>=%s", *timeout))
		fmt.Fprintf(out, "self-query fail query.blocked dur=%s\n", queryDur)
	case qerr != nil:
		add("fail", "query.error", qerr.Error())
		fmt.Fprintf(out, "self-query fail query.error err=%v\n", qerr)
	default:
		fmt.Fprintf(out, "self-query ok dur=%s\n", queryDur)
	}

	fmt.Fprintln(out, "\n== Environment ==")
	fmt.Fprintf(out, "version: %s\n", version)
	count, cerr := idx.DocCount()
	if cerr != nil {
		add("warn", "index.count", cerr.Error())
		fmt.Fprintf(out, "doc count: error: %v\n", cerr)
	} else {
		fmt.Fprintf(out, "doc count: %d\n", count)
	}

	if rec, ok := install.LoadRecord(); ok {
		fmt.Fprintf(out, "install record: %s\n", recordSummary(rec))
	} else {
		fmt.Fprintln(out, "install record: none")
	}

	printFindings(out, findings)
	for _, f := range findings {
		if f.Level == "fail" {
			return 1
		}
	}
	return 0
}

// watchdog runs fn with a deadline. On timeout it returns ErrQueryBlocked
// and abandons fn's goroutine (fn must be safe to leak, e.g. a blocked
// index query).
func watchdog(d time.Duration, fn func() error) error {
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		return ErrQueryBlocked
	}
}

func printFindings(out io.Writer, findings []Finding) {
	fmt.Fprintln(out, "\nfindings:")
	if len(findings) == 0 {
		fmt.Fprintln(out, "  ok none")
		return
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Level > findings[j].Level })
	for _, f := range findings {
		fmt.Fprintf(out, "  %s %s %s\n", f.Level, f.Name, f.Detail)
	}
}

func recordSummary(rec install.Record) string {
	parts := []string{"version=" + rec.Version}
	if len(rec.Roots) > 0 {
		parts = append(parts, "roots="+strings.Join(rec.Roots, ","))
	}
	if len(rec.Projects) > 0 {
		parts = append(parts, "projects="+strings.Join(rec.Projects, ","))
	}
	if rec.Global != "" {
		parts = append(parts, "global="+rec.Global)
	}
	if rec.Store != "" {
		parts = append(parts, "store="+rec.Store)
	}
	if rec.Index != "" {
		parts = append(parts, "index="+rec.Index)
	}
	return strings.Join(parts, " ")
}
