package search

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
)

// SearchDocument is the document indexed by bleve.
type SearchDocument struct {
	Summary  string `json:"summary"`
	Detail   string `json:"detail"`
	Rule     string `json:"rule"`
	Category string `json:"category"`
	Project  string `json:"project"`
}

// SearchResult is a single search result. ID is the bare entry ID;
// the scoped index key (<project>/<entryID>) is internal.
type SearchResult struct {
	ID       string  `json:"id"`
	Summary  string  `json:"summary"`
	Detail   string  `json:"detail"`
	Rule     string  `json:"rule,omitempty"`
	Category string  `json:"category"`
	Score    float64 `json:"score"`
	Project  string  `json:"project"`
}

// LoadFailure records a store file the index could not read or parse. It is
// surfaced in tool responses so that a broken store is never reported as an
// empty one.
type LoadFailure struct {
	Project string `json:"project"`
	Path    string `json:"path"`
	Err     string `json:"error"`
}

// Index wraps an in-memory bleve index for knowledge search. The index is
// built from the store files at startup and rebuilt when those files change,
// so out-of-band edits are searchable without a process restart. It lives
// only as long as the process; there is no disk state and therefore no lock
// between instances.
type Index struct {
	mu           sync.RWMutex // guards index, fingerprint, resolver, and loadFailures
	refreshMu    sync.Mutex   // serializes rebuilds
	index        bleve.Index
	ready        chan struct{} // closed when the initial indexing completes
	readyOnce    sync.Once     // guards close of ready to prevent double-close panic
	resolver     *projects.Resolver
	fingerprint  string        // store-file stamp the current index was built from
	loadFailures []LoadFailure // files that failed to load during the last build
	scopedBuilds atomic.Int64  // counts RefreshFor builds (test instrumentation)
}

// NewIndex creates a new in-memory bleve index.
func NewIndex() (*Index, error) {
	idx, err := bleve.NewMemOnly(indexMapping())
	if err != nil {
		return nil, fmt.Errorf("create in-memory index: %w", err)
	}
	return &Index{index: idx, ready: make(chan struct{})}, nil
}

// Add indexes a document with the given (scoped) key. A later Query detects
// that the store files changed and rebuilds; the caller does not have to
// signal anything.
func (i *Index) Add(id string, doc SearchDocument) error {
	i.mu.RLock()
	defer i.mu.RUnlock()
	if i.index == nil {
		return fmt.Errorf("index is closed")
	}
	return i.index.Index(id, doc)
}

// Query searches the index, scoped to one project, with full-text
// search and optional filters. When the store files changed since the last
// build (direct edits, git checkout, migration scripts), Query rebuilds the
// index first so results never silently disagree with list_knowledge.
func (i *Index) Query(project, q, category string, limit int) ([]SearchResult, error) {
	// Block until initial indexing completes.
	<-i.ready

	if _, err := i.Refresh(); err != nil {
		log.Printf("warning: refresh search index: %v", err)
	}

	i.mu.RLock()
	defer i.mu.RUnlock()

	if i.index == nil {
		return nil, fmt.Errorf("index is closed")
	}

	if limit <= 0 {
		limit = 10
	}

	var baseQuery query.Query
	if q == "" {
		baseQuery = bleve.NewMatchAllQuery()
	} else {
		baseQuery = bleve.NewQueryStringQuery(escapeQueryString(q))
	}

	var conjuncts []query.Query
	conjuncts = append(conjuncts, baseQuery)

	if project != "" {
		projQuery := bleve.NewTermQuery(project)
		projQuery.SetField("project")
		conjuncts = append(conjuncts, projQuery)
	}
	if category != "" {
		catQuery := bleve.NewTermQuery(category)
		catQuery.SetField("category")
		conjuncts = append(conjuncts, catQuery)
	}

	var finalQuery query.Query
	if len(conjuncts) > 1 {
		finalQuery = bleve.NewConjunctionQuery(conjuncts...)
	} else {
		finalQuery = conjuncts[0]
	}

	req := bleve.NewSearchRequest(finalQuery)
	req.Size = limit
	req.Fields = []string{"summary", "detail", "rule", "category", "project"}

	result, err := i.index.Search(req)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	var results []SearchResult
	for _, hit := range result.Hits {
		r := SearchResult{
			ID:    hit.ID,
			Score: hit.Score,
		}
		if v, ok := hit.Fields["project"].(string); ok {
			r.Project = v
			if v != "" {
				r.ID = strings.TrimPrefix(hit.ID, v+"/")
			}
		}
		if v, ok := hit.Fields["summary"].(string); ok {
			r.Summary = v
		}
		if v, ok := hit.Fields["detail"].(string); ok {
			r.Detail = v
		}
		if v, ok := hit.Fields["rule"].(string); ok {
			r.Rule = v
		}
		if v, ok := hit.Fields["category"].(string); ok {
			r.Category = v
		}
		results = append(results, r)
	}

	return results, nil
}

// Close closes the bleve index.
func (i *Index) Close() error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.index == nil {
		return nil
	}
	err := i.index.Close()
	i.index = nil
	return err
}

// CloseReady closes the ready channel exactly once, preventing double-close panics.
// This is used by tests that add documents directly without IndexAll.
func (i *Index) CloseReady() {
	i.readyOnce.Do(func() { close(i.ready) })
}

// WaitReady blocks until the index is ready for queries.
func (i *Index) WaitReady() {
	<-i.ready
}

// DocCount reports the number of documents currently in the index.
func (i *Index) DocCount() (uint64, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	if i.index == nil {
		return 0, fmt.Errorf("index is closed")
	}
	return i.index.DocCount()
}

// LoadFailures returns a copy of the store files that failed to load during
// the last index build. Empty means every file parsed. Callers should surface
// a non-empty result next to any query answer it affects: absence of a rule in
// a partially loaded store is not evidence that the rule does not exist.
func (i *Index) LoadFailures() []LoadFailure {
	i.mu.RLock()
	defer i.mu.RUnlock()
	if len(i.loadFailures) == 0 {
		return nil
	}
	out := make([]LoadFailure, len(i.loadFailures))
	copy(out, i.loadFailures)
	return out
}

// ScopedBuilds reports how many RefreshFor builds have been performed. Test
// instrumentation: known projects must never pay the per-query scoped rebuild.
func (i *Index) ScopedBuilds() int64 {
	return i.scopedBuilds.Load()
}

// RefreshFor returns an index built from the resolver's stores plus the given
// refs, for queries against projects the startup snapshot does not contain.
//
// It clones rather than mutating the shared index. Only a few addresses (the
// queried project, the global store) belong in the result set, and rebuilding
// the live index on every differently-addressed query would both cost more and
// widen what every other query can see.
func (i *Index) RefreshFor(refs []projects.Ref) (*Index, error) {
	<-i.ready

	i.mu.RLock()
	res := i.resolver
	i.mu.RUnlock()
	if res == nil {
		// An index with no resolver was populated directly (tests, or a caller
		// that adds documents by hand). Nothing can be rebuilt from disk, so
		// the caller gets the index it already has.
		return i, nil
	}

	i.scopedBuilds.Add(1)

	fresh, failures, err := buildIndexFor(res, refs)
	if err != nil {
		return nil, err
	}

	clone := &Index{
		index:        fresh,
		ready:        make(chan struct{}),
		resolver:     nil, // a scoped clone has no store fingerprint to track
		loadFailures: failures,
	}
	// The clone is fully built above; Query blocks on ready until it is closed.
	clone.CloseReady()
	return clone, nil
}

// Refresh rebuilds the index when the store files changed since the last
// build. It reports whether a rebuild happened. The unchanged case takes no
// rebuild lock, so concurrent queries are not serialized by the check.
func (i *Index) Refresh() (bool, error) {
	i.mu.RLock()
	res := i.resolver
	current := i.fingerprint
	i.mu.RUnlock()
	if res == nil {
		return false, nil
	}
	if storeFingerprint(res) == current {
		return false, nil
	}

	i.refreshMu.Lock()
	defer i.refreshMu.Unlock()

	// Re-check: another goroutine may have rebuilt while we waited.
	i.mu.RLock()
	res = i.resolver
	current = i.fingerprint
	i.mu.RUnlock()
	if res == nil {
		return false, nil
	}
	fp := storeFingerprint(res)
	if fp == current {
		return false, nil
	}
	if err := i.rebuildLocked(res, fp); err != nil {
		return false, err
	}
	return true, nil
}

// Rebuild re-indexes every known store from disk and swaps the new index in
// atomically. Unlike Refresh it always rebuilds. Queries in flight finish
// against the previous index before it is closed.
func (i *Index) Rebuild() error {
	i.refreshMu.Lock()
	defer i.refreshMu.Unlock()

	i.mu.RLock()
	res := i.resolver
	i.mu.RUnlock()
	if res == nil {
		return fmt.Errorf("index has no resolver; the server has not finished startup indexing")
	}
	return i.rebuildLocked(res, storeFingerprint(res))
}

// rebuildLocked builds a fresh index and swaps it in. Callers must hold
// refreshMu; the old index is closed after the swap.
func (i *Index) rebuildLocked(res *projects.Resolver, fp string) error {
	fresh, failures, err := buildIndex(res)
	if err != nil {
		return err
	}

	i.mu.Lock()
	old := i.index
	i.index = fresh
	i.fingerprint = fp
	i.loadFailures = failures
	i.mu.Unlock()

	if old != nil {
		if closeErr := old.Close(); closeErr != nil {
			log.Printf("warning: close replaced search index: %v", closeErr)
		}
	}
	return nil
}

// IndexAll indexes all projects known to the resolver. It closes the ready
// channel when complete.
func (i *Index) IndexAll(res *projects.Resolver) {
	i.mu.Lock()
	i.resolver = res
	i.mu.Unlock()

	i.refreshMu.Lock()
	err := i.rebuildLocked(res, storeFingerprint(res))
	i.refreshMu.Unlock()
	if err != nil {
		log.Printf("warning: build search index: %v", err)
	}

	i.CloseReady()
}

// buildIndex creates a fresh in-memory index from the resolver's stores. It
// also returns every source file it could not read or parse, because a store
// that fails to load must not look like a store with nothing in it.
func buildIndex(res *projects.Resolver) (bleve.Index, []LoadFailure, error) {
	return buildIndexFor(res, nil)
}

// buildIndexFor builds the index from the resolver's snapshot plus any extra
// refs that resolved dynamically.
func buildIndexFor(res *projects.Resolver, extra []projects.Ref) (bleve.Index, []LoadFailure, error) {
	idx, err := bleve.NewMemOnly(indexMapping())
	if err != nil {
		return nil, nil, fmt.Errorf("create in-memory index: %w", err)
	}

	var failures []LoadFailure
	for _, ref := range res.Snapshot() {
		switch ref.Kind {
		case projects.KindOrg:
			failures = append(failures, indexOrgKnowledge(idx, res.OrgKnowledgeDir(ref), ref.Name)...)
		default:
			failures = append(failures, indexProjectKnowledge(idx, res.AgentsDir(ref), ref.Address)...)
		}
	}
	for _, ref := range extra {
		switch ref.Kind {
		case projects.KindOrg:
			failures = append(failures, indexOrgKnowledge(idx, res.OrgKnowledgeDir(ref), ref.Name)...)
		default:
			failures = append(failures, indexProjectKnowledge(idx, res.AgentsDir(ref), ref.Address)...)
		}
	}
	return idx, failures, nil
}

// storeFingerprint hashes the identity of every knowledge source file the
// index is built from: path, size, and modification time. Missing files are
// part of the stamp so creation and deletion both trigger a rebuild.
func storeFingerprint(res *projects.Resolver) string {
	sum := sha256.New()
	for _, ref := range res.Snapshot() {
		fmt.Fprintf(sum, "%s:%d\n", ref.Address, ref.Kind)
		if ref.Kind == projects.KindOrg {
			dir := res.OrgKnowledgeDir(ref)
			files, err := knowledge.OrgKnowledgeFiles(dir)
			if err != nil {
				fmt.Fprintf(sum, "error:%v\n", err)
				continue
			}
			for _, name := range files {
				hashFileStamp(sum, filepath.Join(dir, name))
			}
			continue
		}
		for _, cat := range knowledge.ValidCategories() {
			hashFileStamp(sum, knowledge.CategoryFilePath(res.AgentsDir(ref), cat))
		}
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// hashFileStamp writes path/size/mtime (or a missing marker) into h.
func hashFileStamp(h hash.Hash, path string) {
	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(h, "%s:missing\n", path)
		return
	}
	fmt.Fprintf(h, "%s:%d:%d\n", path, info.Size(), info.ModTime().UnixNano())
}

// indexProjectKnowledge indexes all knowledge files under an agents dir. Any
// file that cannot be loaded is returned as a LoadFailure rather than only
// logged: an agent that gets "no such entry" cannot tell whether the rule is
// absent or the file that holds it is unreadable.
func indexProjectKnowledge(idx bleve.Index, agentsDir, projectName string) []LoadFailure {
	var failures []LoadFailure
	for _, cat := range knowledge.ValidCategories() {
		catPath := knowledge.CategoryFilePath(agentsDir, cat)
		kf, err := knowledge.Load(catPath)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("warning: failed to load knowledge file %s: %v", catPath, err)
				failures = append(failures, LoadFailure{
					Project: projectName,
					Path:    catPath,
					Err:     err.Error(),
				})
			}
			continue
		}
		for _, entry := range kf.Entries {
			doc := SearchDocument{
				Summary:  entry.Summary,
				Detail:   entry.Detail,
				Rule:     entry.Rule,
				Category: cat,
				Project:  projectName,
			}
			if addErr := idx.Index(projectName+"/"+entry.ID, doc); addErr != nil {
				log.Printf("warning: failed to index %s/%s: %v", projectName, entry.ID, addErr)
			}
		}
	}
	return failures
}

// indexOrgKnowledge indexes every markdown document in an org's knowledge
// dir as sections.
func indexOrgKnowledge(idx bleve.Index, knowledgeDir, orgName string) []LoadFailure {
	var failures []LoadFailure
	files, err := knowledge.OrgKnowledgeFiles(knowledgeDir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("warning: failed to list org knowledge %s: %v", knowledgeDir, err)
			failures = append(failures, LoadFailure{
				Project: orgName,
				Path:    knowledgeDir,
				Err:     err.Error(),
			})
		}
		return failures
	}
	for _, catFile := range files {
		orgFile := filepath.Join(knowledgeDir, catFile)
		data, readErr := os.ReadFile(orgFile)
		if readErr != nil {
			log.Printf("warning: failed to read org knowledge file %s: %v", orgFile, readErr)
			failures = append(failures, LoadFailure{
				Project: orgName,
				Path:    orgFile,
				Err:     readErr.Error(),
			})
			continue
		}
		for _, sec := range knowledge.SplitSections(string(data)) {
			heading, body := sec[0], sec[1]
			doc := SearchDocument{
				Summary:  fmt.Sprintf("%s: %s", catFile, heading),
				Detail:   body,
				Category: "conventions",
				Project:  orgName,
			}
			id := fmt.Sprintf("%s/org-%s::%s", orgName, catFile, heading)
			if addErr := idx.Index(id, doc); addErr != nil {
				log.Printf("warning: failed to index %s: %v", id, addErr)
			}
		}
	}
	return failures
}

// indexMapping builds an explicit mapping: text analysis for content fields,
// exact keyword matching for filter fields.
func indexMapping() mapping.IndexMapping {
	im := bleve.NewIndexMapping()
	doc := bleve.NewDocumentMapping()
	for _, f := range []string{"summary", "detail", "rule"} {
		fm := bleve.NewTextFieldMapping()
		doc.AddFieldMappingsAt(f, fm)
	}
	for _, f := range []string{"category", "project"} {
		fm := bleve.NewTextFieldMapping()
		fm.Analyzer = "keyword"
		doc.AddFieldMappingsAt(f, fm)
	}
	im.DefaultMapping = doc
	return im
}

// escapeQueryString escapes lucene/bleve query-string syntax characters so
// user input is always treated as plain text and can never be parsed as
// query operators (e.g. field:value syntax, boolean operators, or an
// unterminated quoted phrase).
func escapeQueryString(q string) string {
	runes := []rune(q)
	var b strings.Builder
	for i, r := range runes {
		var prev, next rune
		if i > 0 {
			prev = runes[i-1]
		} else {
			prev = ' '
		}
		if i+1 < len(runes) {
			next = runes[i+1]
		} else {
			next = ' '
		}

		needEscape := false
		switch r {
		case '+', '-':
			// Only syntax-active as a unary operator at a token boundary;
			// hyphens inside words (test-stability) stay untouched.
			needEscape = prev == ' ' || prev == '\t' || prev == '\n' || prev == '('
		case ':', '\\', '"', '{', '}', '[', ']', '^', '~', '(', ')', '!', '/':
			needEscape = true
		case '&', '|':
			// Only syntax-active when doubled (&& / ||).
			needEscape = next == r
		}
		if needEscape {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
