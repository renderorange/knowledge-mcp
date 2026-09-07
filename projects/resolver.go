// Package projects resolves project names to filesystem paths across one or
// more org roots and explicitly listed projects.
package projects

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Kind distinguishes resolvable targets.
type Kind int

const (
	// KindProject is a regular project with .agents/ yaml knowledge.
	KindProject Kind = iota
	// KindOrg is an org root with .agents/knowledge/ files.
	KindOrg
	// KindGlobal is a global knowledge store shared across all projects.
	KindGlobal
)

// GlobalAddress is the addressing name for the global knowledge store.
const GlobalAddress = "_global"

// Ref is one addressable project or org root.
type Ref struct {
	// Name is the bare directory basename.
	Name string
	// Qualifier is the basename of the containing root (empty for org refs).
	Qualifier string
	// Path is the canonical absolute filesystem path.
	Path string
	// Kind distinguishes projects from org roots.
	Kind Kind
	// Address is the addressing name: Name when unique, else Qualifier/Name.
	// Org refs always use their bare Name.
	Address string
}

// Resolver maps project names to filesystem paths.
type Resolver struct {
	refs      []Ref
	byAddress map[string]Ref
	byBare    map[string][]Ref
	byPath    map[string]Ref
	roots     []string
	explicit  []string
	global    string // path to global knowledge store (empty if none)
}

// Build constructs a resolver from org roots, explicit project paths, and an
// optional global knowledge path. It returns startup warnings for ambiguous
// names and path overlaps. It returns an error for unresolvable configurations:
// nonexistent or non-directory paths, empty values, and duplicate root basenames.
func Build(roots, projects []string, global string) (*Resolver, []string, error) {
	var warnings []string

	rootPaths, err := canonicalAll(roots, "root")
	if err != nil {
		return nil, nil, err
	}
	projPaths, err := canonicalAll(projects, "project")
	if err != nil {
		return nil, nil, err
	}

	rootPaths = dedupePaths(rootPaths, &warnings, "root")
	projPaths = dedupePaths(projPaths, &warnings, "project")

	// Root basenames must be unique: they qualify project names.
	seenRoots := map[string]string{}
	for _, rp := range rootPaths {
		bn := filepath.Base(rp)
		if prev, ok := seenRoots[bn]; ok {
			return nil, nil, fmt.Errorf(
				"two roots named %q (%s, %s): qualified project names would collide; rename one root or pass it as --project",
				bn, prev, rp)
		}
		seenRoots[bn] = rp
	}

	// Warn about nested roots: the inner root's children are discovered
	// by both roots' scans and deduped by path.
	for i, outer := range rootPaths {
		for j, inner := range rootPaths {
			if i != j && strings.HasPrefix(inner, outer+string(filepath.Separator)) {
				warnings = append(warnings, fmt.Sprintf(
					"root %s is nested under root %s", inner, outer))
			}
		}
	}

	var refs []Ref
	for _, pp := range projPaths {
		refs = append(refs, Ref{
			Name:      filepath.Base(pp),
			Qualifier: filepath.Base(filepath.Dir(pp)),
			Path:      pp,
			Kind:      KindProject,
		})
	}
	for _, rp := range rootPaths {
		children, childWarnings, err := ScanRoot(rp)
		if err != nil {
			return nil, nil, fmt.Errorf("scan root %q: %w", rp, err)
		}
		warnings = append(warnings, childWarnings...)
		refs = append(refs, children...)
		if hasOrgKnowledge(rp) {
			refs = append(refs, Ref{
				Name: filepath.Base(rp),
				Path: rp,
				Kind: KindOrg,
			})
		}
	}

	// Validate and add global knowledge store if provided.
	var globalRef *Ref
	if global != "" {
		abs, err := filepath.Abs(global)
		if err != nil {
			return nil, nil, fmt.Errorf("global %q: %w", global, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, nil, fmt.Errorf("global %q: does not exist", global)
		}
		if !info.IsDir() {
			return nil, nil, fmt.Errorf("global %q: not a directory", global)
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, nil, fmt.Errorf("global %q: %w", global, err)
		}
		globalRef = &Ref{
			Name:    GlobalAddress,
			Path:    resolved,
			Kind:    KindGlobal,
			Address: GlobalAddress,
		}
	}

	r := &Resolver{
		byAddress: map[string]Ref{},
		byBare:    map[string][]Ref{},
		byPath:    map[string]Ref{},
		roots:     rootPaths,
		explicit:  projPaths,
		global:    global,
	}

	// Dedupe by path; project kind wins over org kind.
	for _, ref := range refs {
		if existing, ok := r.byPath[ref.Path]; ok {
			if existing.Kind == KindProject && ref.Kind == KindOrg {
				warnings = append(warnings, fmt.Sprintf(
					"path %s is both a project and an org root; treating as project", ref.Path))
			} else {
				warnings = append(warnings, fmt.Sprintf(
					"duplicate project path %s", ref.Path))
			}
			continue
		}
		r.byPath[ref.Path] = ref
		r.refs = append(r.refs, ref)
	}

	// Add global ref if provided and not already covered by a project.
	if globalRef != nil {
		if _, exists := r.byPath[globalRef.Path]; !exists {
			r.refs = append(r.refs, *globalRef)
			r.byPath[globalRef.Path] = *globalRef
		} else {
			warnings = append(warnings, fmt.Sprintf(
				"global path %s is already a known project; skipping as global", globalRef.Path))
		}
	}

	// Compute addressing names. Org roots always keep their bare name
	// (root basenames are unique); projects qualify when their bare name
	// is claimed by more than one ref.
	nameCount := map[string]int{}
	for _, ref := range r.refs {
		nameCount[ref.Name]++
	}
	for i := range r.refs {
		ref := &r.refs[i]
		switch {
		case ref.Kind == KindOrg:
			ref.Address = ref.Name
		case ref.Kind == KindGlobal:
			// Global ref keeps its fixed address.
		case nameCount[ref.Name] == 1:
			ref.Address = ref.Name
		default:
			ref.Address = ref.Qualifier + "/" + ref.Name
		}
		r.byBare[ref.Name] = append(r.byBare[ref.Name], *ref)
	}

	// Address collisions should be impossible after the checks above;
	// fail loudly rather than silently corrupt name resolution.
	for _, ref := range r.refs {
		if _, ok := r.byAddress[ref.Address]; ok {
			return nil, nil, fmt.Errorf("addressing name collision: %q", ref.Address)
		}
		r.byAddress[ref.Address] = ref
	}

	// Warn about ambiguous bare names.
	for name, refs := range r.byBare {
		if len(refs) > 1 && !hasOrgRef(refs) {
			warnings = append(warnings, fmt.Sprintf(
				"ambiguous project name %q: use qualified names (%s)",
				name, joinAddresses(refs)))
		}
	}

	return r, warnings, nil
}

// Resolve maps a bare or qualified name to a Ref. Unknown names and
// ambiguous bare names return errors listing known names or candidates.
func (r *Resolver) Resolve(name string) (Ref, error) {
	if name == "" {
		return Ref{}, fmt.Errorf("empty project name")
	}

	if strings.Contains(name, "/") {
		if ref, ok := r.byAddress[name]; ok {
			return ref, nil
		}
		// Dynamic fallback for projects created after startup.
		if ref, ok := r.resolveDynamicQualified(name); ok {
			return ref, nil
		}
		return Ref{}, fmt.Errorf("unknown project: %q (known: %s)",
			name, strings.Join(r.KnownNames(), ", "))
	}

	refs := r.byBare[name]

	// Org roots always win their bare name.
	for _, ref := range refs {
		if ref.Kind == KindOrg {
			return ref, nil
		}
	}

	switch len(refs) {
	case 1:
		return refs[0], nil
	case 0:
		// Dynamic fallback for projects created after startup.
		if ref, ok := r.resolveDynamicBare(name); ok {
			return ref, nil
		}
		return Ref{}, fmt.Errorf("unknown project: %q (known: %s)",
			name, strings.Join(r.KnownNames(), ", "))
	default:
		return Ref{}, fmt.Errorf("ambiguous project %q: use a qualified name (%s)",
			name, joinAddresses(refs))
	}
}

// KnownNames returns the sorted addressing names of all refs.
func (r *Resolver) KnownNames() []string {
	names := make([]string, 0, len(r.refs))
	for _, ref := range r.refs {
		names = append(names, ref.Address)
	}
	sort.Strings(names)
	return names
}

// GlobalRef returns the global knowledge ref, or false if none is configured.
func (r *Resolver) GlobalRef() (Ref, bool) {
	if r.global == "" {
		return Ref{}, false
	}
	ref, ok := r.byAddress[GlobalAddress]
	return ref, ok
}

// Snapshot returns all refs sorted by addressing name.
func (r *Resolver) Snapshot() []Ref {
	out := make([]Ref, len(r.refs))
	copy(out, r.refs)
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

// Entries returns the canonicalized root and explicit-project paths the
// resolver was built with, sorted. Used to key the shared index location.
func (r *Resolver) Entries() []string {
	out := slices.Concat(r.roots, r.explicit)
	sort.Strings(out)
	return out
}

// Covers reports whether path is a known ref or lies under any root.
// Paths created after startup under a root are covered.
func (r *Resolver) Covers(path string) bool {
	resolved, ok := canonicalize(path)
	if !ok {
		return false
	}
	if _, ok := r.byPath[resolved]; ok {
		return true
	}
	for _, root := range r.roots {
		if strings.HasPrefix(resolved, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (r *Resolver) resolveDynamicBare(name string) (Ref, bool) {
	for _, root := range r.roots {
		path := filepath.Join(root, name)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return Ref{
				Name: name, Qualifier: filepath.Base(root),
				Path: path, Kind: KindProject, Address: name,
			}, true
		}
	}
	return Ref{}, false
}

func (r *Resolver) resolveDynamicQualified(name string) (Ref, bool) {
	i := strings.LastIndex(name, "/")
	qualifier, base := name[:i], name[i+1:]
	for _, root := range r.roots {
		if filepath.Base(root) != qualifier {
			continue
		}
		path := filepath.Join(root, base)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return Ref{
				Name: base, Qualifier: qualifier,
				Path: path, Kind: KindProject, Address: name,
			}, true
		}
	}
	return Ref{}, false
}

// ScanRoot returns refs for the immediate child directories of root,
// skipping .agents and .git and following symlinks. It returns warnings
// for unreadable entries (e.g. permission errors, dangling symlinks).
func ScanRoot(root string) ([]Ref, []string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, err
	}
	qualifier := filepath.Base(root)
	var refs []Ref
	var warnings []string
	for _, entry := range entries {
		name := entry.Name()
		if name == ".agents" || name == ".git" {
			continue
		}
		path := filepath.Join(root, name)
		// os.Stat follows symlinks; DirEntry.IsDir does not.
		info, statErr := os.Stat(path)
		if statErr != nil {
			warnings = append(warnings, fmt.Sprintf(
				"unreadable entry %s under %s: %v", name, root, statErr))
			continue
		}
		if !info.IsDir() {
			continue
		}
		refs = append(refs, Ref{
			Name: name, Qualifier: qualifier,
			Path: canonicalOrAbs(path), Kind: KindProject,
		})
	}
	return refs, warnings, nil
}

func hasOrgKnowledge(root string) bool {
	info, err := os.Stat(filepath.Join(root, ".agents", "knowledge"))
	return err == nil && info.IsDir()
}

func canonicalize(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", false
	}
	return resolved, true
}

func canonicalAll(paths []string, what string) ([]string, error) {
	var out []string
	for _, p := range paths {
		if p == "" {
			return nil, fmt.Errorf("empty %s path", what)
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", what, p, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("%s %q: does not exist", what, p)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s %q: not a directory", what, p)
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", what, p, err)
		}
		out = append(out, resolved)
	}
	return out, nil
}

func canonicalOrAbs(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func dedupePaths(paths []string, warnings *[]string, what string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		if seen[p] {
			*warnings = append(*warnings, fmt.Sprintf("duplicate %s path %s (ignored)", what, p))
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func hasOrgRef(refs []Ref) bool {
	for _, ref := range refs {
		if ref.Kind == KindOrg {
			return true
		}
	}
	return false
}

func joinAddresses(refs []Ref) string {
	parts := make([]string, len(refs))
	for i, ref := range refs {
		parts[i] = ref.Address
	}
	return strings.Join(parts, ", ")
}
