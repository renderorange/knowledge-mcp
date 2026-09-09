package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkdir(t *testing.T, path ...string) string {
	t.Helper()
	full := filepath.Join(path...)
	if err := os.MkdirAll(full, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	return full
}

func TestBuildSingleProject(t *testing.T) {
	dir := mkdir(t, t.TempDir(), "myproj")

	res, warnings, err := Build(nil, []string{dir}, "")
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}

	ref, err := res.Resolve("myproj")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if ref.Path != dir {
		t.Errorf("Path = %q, want %q", ref.Path, dir)
	}
	if ref.Kind != KindProject {
		t.Errorf("Kind = %v, want KindProject", ref.Kind)
	}
	if ref.Address != "myproj" {
		t.Errorf("Address = %q, want %q", ref.Address, "myproj")
	}

	names := res.KnownNames()
	if len(names) != 1 || names[0] != "myproj" {
		t.Errorf("KnownNames() = %v, want [myproj]", names)
	}
}

func TestBuildSingleRootDiscovery(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "alpha")
	mkdir(t, root, "beta")

	res, _, err := Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	for _, name := range []string{"alpha", "beta"} {
		ref, err := res.Resolve(name)
		if err != nil {
			t.Fatalf("Resolve(%q) error: %v", name, err)
		}
		if ref.Kind != KindProject {
			t.Errorf("Resolve(%q).Kind = %v, want KindProject", name, ref.Kind)
		}
	}

	_, err = res.Resolve("gamma")
	if err == nil {
		t.Fatal("Resolve(gamma) should fail")
	}
	if !strings.Contains(err.Error(), "alpha") {
		t.Errorf("error should list known names, got: %v", err)
	}
}

func TestAmbiguousBasenameQualifiedResolution(t *testing.T) {
	r1 := mkdir(t, t.TempDir(), "r1")
	r2 := mkdir(t, t.TempDir(), "r2")
	mkdir(t, r1, "api")
	mkdir(t, r2, "api")

	res, warnings, err := Build([]string{r1, r2}, nil, "")
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if len(warnings) == 0 {
		t.Error("expected ambiguity warning, got none")
	}

	_, err = res.Resolve("api")
	if err == nil {
		t.Fatal("Resolve(api) should fail as ambiguous")
	}
	if !strings.Contains(err.Error(), "r1/api") || !strings.Contains(err.Error(), "r2/api") {
		t.Errorf("error should list qualified candidates, got: %v", err)
	}

	ref, err := res.Resolve("r1/api")
	if err != nil {
		t.Fatalf("Resolve(r1/api) error: %v", err)
	}
	if want := filepath.Join(r1, "api"); ref.Path != want {
		t.Errorf("Path = %q, want %q", ref.Path, want)
	}
	if ref.Address != "r1/api" {
		t.Errorf("Address = %q, want %q", ref.Address, "r1/api")
	}
}

func TestRootsSameBasenameHardError(t *testing.T) {
	a := mkdir(t, t.TempDir(), "code")
	b := mkdir(t, t.TempDir(), "code")

	_, _, err := Build([]string{a, b}, nil, "")
	if err == nil {
		t.Fatal("two roots named \"code\" should be a hard error")
	}
	if !strings.Contains(err.Error(), "code") {
		t.Errorf("error should name the collision, got: %v", err)
	}
}

func TestBuildValidationErrors(t *testing.T) {
	tmp := t.TempDir()

	if _, _, err := Build([]string{filepath.Join(tmp, "missing")}, nil, ""); err == nil {
		t.Error("nonexistent root should error")
	}
	file := filepath.Join(tmp, "afile")
	os.WriteFile(file, []byte("x"), 0644)
	if _, _, err := Build([]string{file}, nil, ""); err == nil {
		t.Error("file root should error")
	}
	if _, _, err := Build([]string{""}, nil, ""); err == nil {
		t.Error("empty root should error")
	}
	if _, _, err := Build(nil, []string{filepath.Join(tmp, "missing")}, ""); err == nil {
		t.Error("nonexistent project should error")
	}
}

func TestNestedRootsAndDuplicates(t *testing.T) {
	outer := t.TempDir()
	inner := mkdir(t, outer, "inner")
	mkdir(t, inner, "deep")

	res, warnings, err := Build([]string{outer, inner}, nil, "")
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "nested") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected nested-root warning, got %v", warnings)
	}

	// "inner" is both a root and a child of outer — one ref.
	if names := res.KnownNames(); len(names) != 2 {
		t.Errorf("KnownNames() = %v, want 2 names (deep, inner)", names)
	}

	// Duplicate root paths dedupe with exactly one warning.
	res2, warnings2, err := Build([]string{outer, outer}, nil, "")
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if len(warnings2) != 1 || !strings.Contains(warnings2[0], "duplicate") {
		t.Errorf("duplicate root should warn exactly once, got %v", warnings2)
	}
	if got, want := len(res2.KnownNames()), len(res.KnownNames())-1; got != want {
		t.Errorf("KnownNames() len = %d, want %d (deep only)", got, want)
	}
}

func TestSymlinkedProjectDiscovered(t *testing.T) {
	root := t.TempDir()
	target := mkdir(t, t.TempDir(), "real-project")
	if err := os.Symlink(target, filepath.Join(root, "link-name")); err != nil {
		t.Skip("symlinks unavailable")
	}

	res, _, err := Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	ref, err := res.Resolve("link-name")
	if err != nil {
		t.Fatalf("Resolve(link-name) error: %v", err)
	}
	if ref.Path != target {
		t.Errorf("Path = %q, want %q (canonicalized)", ref.Path, target)
	}
}

func TestOrgRefAndProjectSharingRootName(t *testing.T) {
	root := mkdir(t, t.TempDir(), "org")
	mkdir(t, root, ".agents", "knowledge")
	other := mkdir(t, t.TempDir(), "other")
	mkdir(t, other, "org") // project basename collides with root basename

	res, warnings, err := Build([]string{root, other}, nil, "")
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	orgRef, err := res.Resolve("org")
	if err != nil {
		t.Fatalf("Resolve(org) error: %v", err)
	}
	if orgRef.Kind != KindOrg {
		t.Errorf("org root should win its bare name, got kind %v", orgRef.Kind)
	}
	_ = warnings // ambiguity between org ref and project may warn; not asserted

	projRef, err := res.Resolve("other/org")
	if err != nil {
		t.Fatalf("Resolve(other/org) error: %v", err)
	}
	if projRef.Kind != KindProject {
		t.Errorf("qualified other/org kind = %v, want KindProject", projRef.Kind)
	}
}

func TestDynamicResolveNewProjectUnderRoot(t *testing.T) {
	root := t.TempDir()
	res, _, err := Build([]string{root}, nil, "")
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Project created after startup resolves immediately.
	late := mkdir(t, root, "late-project")
	ref, err := res.Resolve("late-project")
	if err != nil {
		t.Fatalf("Resolve(late-project) error: %v", err)
	}
	if ref.Path != late {
		t.Errorf("Path = %q, want %q", ref.Path, late)
	}
}

func TestCovers(t *testing.T) {
	root := t.TempDir()
	explicit := mkdir(t, t.TempDir(), "explicit")
	res, _, err := Build([]string{root}, []string{explicit}, "")
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if !res.Covers(explicit) {
		t.Error("explicit project path should be covered")
	}
	under := mkdir(t, root, "anything", "here")
	if !res.Covers(under) {
		t.Error("path under a root should be covered")
	}
	if res.Covers(mkdir(t, t.TempDir(), "elsewhere")) {
		t.Error("unrelated path should not be covered")
	}
}

func TestBuildWithGlobal(t *testing.T) {
	globalDir := mkdir(t, t.TempDir(), "global-knowledge")
	projDir := mkdir(t, t.TempDir(), "myproj")

	res, warnings, err := Build(nil, []string{projDir}, globalDir)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}

	// Global ref should be resolvable
	ref, err := res.Resolve("_global")
	if err != nil {
		t.Fatalf("Resolve(_global) error: %v", err)
	}
	if ref.Kind != KindGlobal {
		t.Errorf("Kind = %v, want KindGlobal", ref.Kind)
	}
	if ref.Address != "_global" {
		t.Errorf("Address = %q, want %q", ref.Address, "_global")
	}
	if ref.Path != globalDir {
		t.Errorf("Path = %q, want %q", ref.Path, globalDir)
	}

	// GlobalRef should return the ref
	globalRef, ok := res.GlobalRef()
	if !ok {
		t.Fatal("GlobalRef() returned false")
	}
	if globalRef.Address != "_global" {
		t.Errorf("GlobalRef().Address = %q, want %q", globalRef.Address, "_global")
	}

	// KnownNames should include _global
	names := res.KnownNames()
	found := false
	for _, n := range names {
		if n == "_global" {
			found = true
		}
	}
	if !found {
		t.Errorf("KnownNames() = %v, want _global included", names)
	}
}

func TestBuildWithGlobalNonexistent(t *testing.T) {
	_, _, err := Build(nil, nil, "/nonexistent/path")
	if err == nil {
		t.Fatal("nonexistent global path should error")
	}
}

func TestBuildWithGlobalAndNoProjects(t *testing.T) {
	globalDir := mkdir(t, t.TempDir(), "global")

	res, _, err := Build(nil, nil, globalDir)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	ref, err := res.Resolve("_global")
	if err != nil {
		t.Fatalf("Resolve(_global) error: %v", err)
	}
	if ref.Kind != KindGlobal {
		t.Errorf("Kind = %v, want KindGlobal", ref.Kind)
	}
}

func TestBuildWithGlobalDuplicatePath(t *testing.T) {
	// If the global path is also a project, warn and skip global
	projDir := mkdir(t, t.TempDir(), "myproj")

	res, warnings, err := Build(nil, []string{projDir}, projDir)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	found := false
	for _, w := range warnings {
		if strings.Contains(w, "already a known project") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning about duplicate global path, got %v", warnings)
	}

	// The project should still be resolvable
	_, err = res.Resolve("myproj")
	if err != nil {
		t.Fatalf("Resolve(myproj) error: %v", err)
	}
}

func TestGlobalRefNone(t *testing.T) {
	projDir := mkdir(t, t.TempDir(), "myproj")

	res, _, err := Build(nil, []string{projDir}, "")
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	_, ok := res.GlobalRef()
	if ok {
		t.Error("GlobalRef() should return false when no global is configured")
	}
}

func TestBuildWithStoreInvalid(t *testing.T) {
	tmp := t.TempDir()

	if _, _, err := BuildWithStore(nil, nil, "", filepath.Join(tmp, "missing")); err == nil {
		t.Error("nonexistent store path should error")
	}
	file := filepath.Join(tmp, "afile")
	os.WriteFile(file, []byte("x"), 0644)
	if _, _, err := BuildWithStore(nil, nil, "", file); err == nil {
		t.Error("file store path should error")
	}
}

func TestAgentsDirWithoutStore(t *testing.T) {
	dir := mkdir(t, t.TempDir(), "myproj")

	res, _, err := Build(nil, []string{dir}, "")
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	ref, err := res.Resolve("myproj")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if want := filepath.Join(dir, ".agents"); res.AgentsDir(ref) != want {
		t.Errorf("AgentsDir = %q, want %q", res.AgentsDir(ref), want)
	}
	if want := filepath.Join(dir, ".agents", "knowledge"); res.OrgKnowledgeDir(ref) != want {
		t.Errorf("OrgKnowledgeDir = %q, want %q", res.OrgKnowledgeDir(ref), want)
	}
}

func TestAgentsDirWithStore(t *testing.T) {
	r1 := mkdir(t, t.TempDir(), "r1")
	r2 := mkdir(t, t.TempDir(), "r2")
	mkdir(t, r1, "api")
	mkdir(t, r2, "api")
	globalDir := mkdir(t, t.TempDir(), "global")
	store := mkdir(t, t.TempDir(), "store")

	res, _, err := BuildWithStore([]string{r1, r2}, nil, globalDir, store)
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	qualified, err := res.Resolve("r1/api")
	if err != nil {
		t.Fatalf("Resolve(r1/api) error: %v", err)
	}
	if want := filepath.Join(store, "r1", "api", ".agents"); res.AgentsDir(qualified) != want {
		t.Errorf("AgentsDir(qualified) = %q, want %q", res.AgentsDir(qualified), want)
	}

	gref, err := res.Resolve("_global")
	if err != nil {
		t.Fatalf("Resolve(_global) error: %v", err)
	}
	if want := filepath.Join(globalDir, ".agents"); res.AgentsDir(gref) != want {
		t.Errorf("AgentsDir(global) = %q, want %q (must never re-root)", res.AgentsDir(gref), want)
	}
}

func TestBuildWithStoreExcludesStoreUnderRoot(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "alpha")
	store := mkdir(t, root, ".knowledge")
	mkdir(t, store, "alpha") // store child mirrors a project name

	res, warnings, err := BuildWithStore([]string{root}, nil, "", store)
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	if _, err := res.Resolve(".knowledge"); err == nil {
		t.Error("store dir itself must not be discovered as a project")
	}
	warned := false
	for _, w := range warnings {
		if strings.Contains(w, "inside the --store directory") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("expected exclusion warning, got %v", warnings)
	}

	// Dynamic resolution must not resurrect the store dir either.
	if _, err := res.Resolve(".knowledge"); err == nil {
		t.Error("dynamic resolution must exclude the store dir")
	}
}

func TestBuildWithStoreInTreeWarnings(t *testing.T) {
	root := t.TempDir()
	proj := mkdir(t, root, "proj")
	mkdir(t, proj, ".agents")
	org := mkdir(t, t.TempDir(), "org")
	mkdir(t, org, ".agents", "knowledge")
	store := mkdir(t, t.TempDir(), "store")

	_, warnings, err := BuildWithStore([]string{root, org}, nil, "", store)
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	var projWarn, orgWarn int
	for _, w := range warnings {
		if strings.Contains(w, "ignoring in-tree .agents at") {
			projWarn++
		}
		if strings.Contains(w, "ignoring in-tree org knowledge at") {
			orgWarn++
		}
	}
	if projWarn != 1 {
		t.Errorf("want 1 in-tree project warning, got %d (%v)", projWarn, warnings)
	}
	if orgWarn != 1 {
		t.Errorf("want 1 in-tree org warning, got %d (%v)", orgWarn, warnings)
	}
}

func TestBuildWithStoreOrgKnowledgeInStore(t *testing.T) {
	root := mkdir(t, t.TempDir(), "org")
	store := mkdir(t, t.TempDir(), "store")
	mkdir(t, store, "org", ".agents", "knowledge")

	res, _, err := BuildWithStore([]string{root}, nil, "", store)
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	ref, err := res.Resolve("org")
	if err != nil {
		t.Fatalf("Resolve(org) error: %v", err)
	}
	if ref.Kind != KindOrg {
		t.Errorf("store-hosted org knowledge should create an org ref, got kind %v", ref.Kind)
	}
	if want := filepath.Join(store, "org", ".agents", "knowledge"); res.OrgKnowledgeDir(ref) != want {
		t.Errorf("OrgKnowledgeDir = %q, want %q", res.OrgKnowledgeDir(ref), want)
	}
}

func TestBuildWithStoreNoOrgKnowledgeInTree(t *testing.T) {
	root := mkdir(t, t.TempDir(), "org")
	mkdir(t, root, ".agents", "knowledge") // in-tree only
	store := mkdir(t, t.TempDir(), "store")

	res, _, err := BuildWithStore([]string{root}, nil, "", store)
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}

	if _, err := res.Resolve("org"); err == nil {
		t.Error("in-tree org knowledge must not create an org ref when --store is set")
	}
}

func TestRefForPath(t *testing.T) {
	dir := mkdir(t, t.TempDir(), "myproj")

	res, _, err := Build(nil, []string{dir}, "")
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	ref, ok := res.RefForPath(dir)
	if !ok || ref.Address != "myproj" {
		t.Errorf("RefForPath(%q) = %#v, %v; want myproj", dir, ref, ok)
	}
	if _, ok := res.RefForPath(filepath.Join(dir, "nonexistent")); ok {
		t.Error("RefForPath should return false for unknown paths")
	}
}

func TestExplicitProjectInsideStoreKept(t *testing.T) {
	store := mkdir(t, t.TempDir(), "store")
	inner := mkdir(t, store, "inner")

	res, warnings, err := BuildWithStore(nil, []string{inner}, "", store)
	if err != nil {
		t.Fatalf("BuildWithStore() error: %v", err)
	}
	if _, err := res.Resolve("inner"); err != nil {
		t.Errorf("explicit project inside store should stay resolvable: %v", err)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "explicit project") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected explicit-project warning, got %v", warnings)
	}
}
