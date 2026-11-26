package diag

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/renderorange/knowledge-mcp/install"
	"github.com/renderorange/knowledge-mcp/internal/update"
	"github.com/renderorange/knowledge-mcp/projects"
	"github.com/renderorange/knowledge-mcp/search"
)

// ErrQueryBlocked is returned by watchdog when fn exceeds the deadline —
// the slow-kick query-block detector.
var ErrQueryBlocked = errors.New("query blocked: index never became ready")

// updateHTTPClient is the network client used by --check-updates. It is a
// package variable so tests can swap in a counting stub and prove that
// without the flag no request is ever made.
var updateHTTPClient update.Client = &http.Client{Timeout: update.RequestTimeout}

// checkUpdateFn is the network path of --check-updates, swappable in tests.
var checkUpdateFn = update.Check

// osExecutable is os.Executable, swappable in tests (a fake binary inside a
// temp git repo stands in for the real install).
var osExecutable = os.Executable

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
	checkUpdates := fs.Bool("check-updates", false, "Check the installed binary against the latest GitHub release (once per 24h)")
	brief := fs.Bool("brief", false, "Print one line when --check-updates finds a newer release, nothing otherwise")
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

	// --brief is the shell-prompt/statusline contract: no report at all, one
	// line when --check-updates finds the binary behind, nothing otherwise,
	// exit 0. It runs before every other check, so a bare
	// "debug --brief --check-updates" without project flags still works.
	if *brief {
		if *checkUpdates {
			if line, ok := briefLine(version); ok {
				fmt.Fprintln(out, line)
			}
		}
		return 0
	}

	var findings []Finding
	add := func(level, name, detail string) {
		findings = append(findings, Finding{level, name, detail})
	}

	// Version findings are always on: they read local files only, never the
	// network.
	exePath, exeErr := osExecutable()
	for _, f := range versionFindings(exePath, exeErr, version) {
		add(f.Level, f.Name, f.Detail)
	}

	// --check-updates is strictly opt-in: without the flag no outbound
	// request is made, even with a warm cache.
	if *checkUpdates {
		outcome := updateFinding(version)
		add(outcome.Level, outcome.Name, outcome.Detail)
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

	// store.load surfaces the files the index could not read or parse: a
	// store that partly failed to load must not look like a store with
	// nothing in it.
	for _, failure := range idx.LoadFailures() {
		add("warn", "store.load", fmt.Sprintf("%s: %s", failure.Path, failure.Err))
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

// versionFindings reports the binary's local identity and, when it can be
// found in a git checkout, whether the embedded version matches the checkout's
// describe string. Always on, no network.
func versionFindings(exePath string, exeErr error, version string) []Finding {
	var findings []Finding

	if exeErr != nil {
		findings = append(findings, Finding{
			Level:  "warn",
			Name:   "version.local",
			Detail: fmt.Sprintf("can't locate executable: %v", exeErr),
		})
		return findings
	}

	detail := fmt.Sprintf("version=%s path=%s", version, exePath)
	if info, err := os.Stat(exePath); err == nil {
		detail += fmt.Sprintf(" mtime=%s", info.ModTime().Format(time.RFC3339))
	}
	findings = append(findings, Finding{Level: "ok", Name: "version.local", Detail: detail})

	describe, err := gitDescribe(exePath)
	if err != nil {
		return findings // no checkout; nothing to compare
	}

	embedded, describeVersion := normalizeVersion(version), normalizeVersion(describe)
	if embedded != "" && describeVersion != "" && embedded != describeVersion {
		findings = append(findings, Finding{
			Level:  "warn",
			Name:   "version.skew",
			Detail: fmt.Sprintf("installed %s, checkout %s -> stale", version, describe),
		})
	}
	return findings
}

// normalizeVersion strips a leading "v" and any -dirty/-N-gHASH suffix so
// "v1.7.1", "1.7.1-1-gabc123" and "v1.7.1-dirty" compare on the tag. A
// string that does not look like a version (the build-time default "dev", a
// raw git hash) normalizes to empty, so unknown versions are never reported
// as behind anything.
func normalizeVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v = v[:i]
	}
	if v == "" || v[0] < '0' || v[0] > '9' {
		return ""
	}
	return v
}

// gitDescribe finds the git checkout an executable lives in and returns its
// git describe (e.g. "v1.7.1-1-gabc123"). It returns an error when the
// executable is not in a checkout.
func gitDescribe(exePath string) (string, error) {
	dir := filepath.Dir(exePath)
	cmd := exec.Command("git", "-C", dir, "describe", "--tags", "--always", "--dirty")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// updateFinding consults the latest release (through the cache when fresh)
// and reports whether the installed binary is behind. Network failure or a
// non-200 is a warn, never a fail, and never blocks.
func updateFinding(version string) Finding {
	latest, err := latestRelease(version)
	if err != nil {
		return Finding{
			Level:  "warn",
			Name:   "update.check",
			Detail: fmt.Sprintf("could not check for updates: %v", err),
		}
	}
	embedded := normalizeVersion(version)
	current := normalizeVersion(latest)
	if embedded != "" && current != "" && embedded != current {
		return Finding{
			Level:  "warn",
			Name:   "update.available",
			Detail: fmt.Sprintf("installed %s, latest %s", version, latest),
		}
	}
	return Finding{Level: "ok", Name: "update.current", Detail: "installed binary is current"}
}

// latestRelease returns the newest release tag, preferring a fresh cache that
// was written for this exact binary version. A cache written by an older (or
// newer) binary is stale for this one: version skew means its "latest" was
// learned without the release that this binary is not — re-check.
func latestRelease(version string) (string, error) {
	if st, ok := update.ReadCache(); ok && st.BinaryVersion == version && st.LatestTag != "" {
		return st.LatestTag, nil
	}
	return checkUpdateFn(updateHTTPClient, version)
}

// briefLine implements the statusline contract for --brief --check-updates:
// exactly one line ("v1.7.0->v1.7.2") when a newer release exists, nothing
// when current, ahead, unknown, or network-failed, and exit 0. The statusline
// relies on the empty case to drop the segment, and supplies its own "kmcp"
// label, so the line is the bare version arrow.
func briefLine(version string) (string, bool) {
	latest, err := latestRelease(version)
	if err != nil {
		return "", false
	}
	if isBehind(version, latest) {
		return fmt.Sprintf("%s->%s", version, latest), true
	}
	return "", false
}

// isBehind reports whether the installed version is strictly older than the
// latest release. Plain string inequality is wrong here: v1.7.2 installed
// against a v1.7.1-cached latest is ahead, not behind. Only digits and dots
// are compared; a non-semver side (dev, a raw hash) is never behind.
func isBehind(installed, latest string) bool {
	iv := normalizeVersion(installed)
	lv := normalizeVersion(latest)
	if iv == "" || lv == "" || iv == lv {
		return false
	}
	return versionLess(iv, lv)
}

// versionLess compares dotted numeric versions segment by segment: 1.7.10 >
// 1.7.9 because the third segment compares numerically, which a byte compare
// would get wrong. Missing segments count as 0, so "1.7" < "1.7.1".
func versionLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var an, bn int64
		if i < len(as) {
			an, _ = strconv.ParseInt(as[i], 10, 64)
		}
		if i < len(bs) {
			bn, _ = strconv.ParseInt(bs[i], 10, 64)
		}
		if an != bn {
			return an < bn
		}
	}
	return false
}
