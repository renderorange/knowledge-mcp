package search

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
)

// metaFileVersion is the on-disk key-format version. A mismatch forces an index rebuild.
// Version 3: org-level knowledge docs are indexed per markdown section
// instead of as whole files.
const metaFileVersion = 3

// indexMeta is stored in a sibling file next to the bleve index directory.
// It records the key format and the project-name set the index was built with;
// a change in either means the on-disk keys no longer match, so the index is
// wiped and rebuilt from the yaml source of truth.
type indexMeta struct {
	Version int      `json:"version"`
	Names   []string `json:"names"`
}

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

// Index wraps a bleve index for knowledge search.
type Index struct {
	indexPath string
	names     []string
	index     bleve.Index
	ready     chan struct{} // closed when indexing completes or stale data exists
	readyOnce sync.Once     // guards close of ready to prevent double-close panic

	openMu      sync.Mutex
	openStarted bool          // guards against double opens
	openDone    chan struct{} // closed when the open attempt resolves
	openErr     error         // set when the open attempt fails; read after openDone closes
}

// openWaitTimeout bounds how long Query/Add wait on a pending index open.
// The only expected long-open case is another instance holding the
// single-writer bbolt lock; everything else opens in milliseconds.
const openWaitTimeout = 2 * time.Second

// ErrIndexLocked is returned when the index is not open within the bounded
// wait — most commonly because another instance holds the single-writer lock.
var ErrIndexLocked = errors.New("search index is not ready; another knowledge-mcp instance may hold the single-writer index lock, retry in a moment")

// NewIndex opens or creates a bleve index at the given path synchronously.
// indexNames is the set of project names the index will hold; a change from
// the recorded set triggers a rebuild.
func NewIndex(indexPath string, indexNames []string) (*Index, error) {
	i := NewLazyIndex(indexPath, indexNames)
	i.openStarted = true
	if err := i.open(); err != nil {
		return nil, err
	}
	close(i.openDone)
	return i, nil
}

// NewLazyIndex constructs an Index without touching disk. The open is
// deferred until OpenBackground runs it.
func NewLazyIndex(indexPath string, indexNames []string) *Index {
	return &Index{
		indexPath: indexPath,
		names:     indexNames,
		ready:     make(chan struct{}),
		openDone:  make(chan struct{}),
	}
}

// OpenBackground starts the index open on a background goroutine exactly
// once. afterOpen runs after a successful open. The open may block for a
// long time if another instance holds the single-writer lock; that must not
// delay the caller (the MCP server) from serving its protocol.
func (i *Index) OpenBackground(afterOpen func()) {
	i.openMu.Lock()
	if i.openStarted {
		i.openMu.Unlock()
		return
	}
	i.openStarted = true
	i.openMu.Unlock()

	go func() {
		err := i.open()
		i.openMu.Lock()
		i.openErr = err
		i.openMu.Unlock()
		close(i.openDone)
		if err == nil && afterOpen != nil {
			afterOpen()
		}
	}()
}

// WaitOpen waits up to d for the open attempt to resolve. It returns nil
// once the index is open, the open error if the attempt failed, or
// ErrIndexLocked if the attempt is still pending after d.
func (i *Index) WaitOpen(d time.Duration) error {
	waitCh := func() error {
		if i.openErr != nil {
			return fmt.Errorf("search index unavailable: %w", i.openErr)
		}
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-i.openDone:
		i.openMu.Lock()
		defer i.openMu.Unlock()
		return waitCh()
	case <-timer.C:
		select {
		case <-i.openDone:
			i.openMu.Lock()
			defer i.openMu.Unlock()
			return waitCh()
		default:
			return ErrIndexLocked
		}
	}
}

// open performs the actual on-disk open or create, exactly as NewIndex
// historically did.
func (i *Index) open() error {
	if err := os.MkdirAll(filepath.Dir(i.indexPath), 0755); err != nil {
		return fmt.Errorf("create index dir: %w", err)
	}

	if metaStale(i.indexPath, i.names) {
		if err := DeleteIndex(i.indexPath); err != nil {
			return fmt.Errorf("remove stale index: %w", err)
		}
	}

	mapping := indexMapping()

	var idx bleve.Index
	var err error

	if _, statErr := os.Stat(i.indexPath); os.IsNotExist(statErr) {
		idx, err = bleve.New(i.indexPath, mapping)
		if err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	} else {
		idx, err = openOrRecover(i.indexPath, mapping)
		if err != nil {
			return err
		}
	}

	meta := indexMeta{Version: metaFileVersion, Names: i.names}
	data, marshalErr := json.Marshal(meta)
	if marshalErr == nil {
		marshalErr = os.WriteFile(metaFilePath(i.indexPath), data, 0644)
	}
	if marshalErr != nil {
		idx.Close()
		return fmt.Errorf("write index meta: %w", marshalErr)
	}

	i.openMu.Lock()
	i.index = idx
	i.openMu.Unlock()

	// If index has data, close ready immediately (stale data available)
	if i.indexHasData() {
		i.CloseReady()
	}

	return nil
}

func metaFilePath(indexPath string) string {
	return indexPath + ".meta.json"
}

// metaStale reports whether the on-disk index must be rebuilt.
func metaStale(indexPath string, indexNames []string) bool {
	data, err := os.ReadFile(metaFilePath(indexPath))
	if err != nil {
		return true
	}
	var meta indexMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return true
	}
	want := slices.Clone(indexNames)
	sort.Strings(want)
	return meta.Version != metaFileVersion || !slices.Equal(meta.Names, want)
}

// openOrRecover opens an existing bleve index, recovering from corruption
// (including panics from bbolt on truncated or damaged files) and verifying
// the index is actually usable with a test query.
func openOrRecover(indexPath string, mapping mapping.IndexMapping) (bleve.Index, error) {
	var idx bleve.Index
	var openErr error

	// bbolt can panic on corrupted files (e.g. truncated root.bolt).
	func() {
		defer func() {
			if r := recover(); r != nil {
				openErr = fmt.Errorf("panic opening index: %v", r)
			}
		}()
		idx, openErr = bleve.Open(indexPath)
	}()

	if openErr != nil {
		log.Printf("warning: index corrupted (%v), rebuilding", openErr)
		if rmErr := DeleteIndex(indexPath); rmErr != nil {
			return nil, fmt.Errorf("remove corrupted index: %w", rmErr)
		}
		idx, err := bleve.New(indexPath, mapping)
		if err != nil {
			return nil, fmt.Errorf("create index after corruption: %w", err)
		}
		return idx, nil
	}

	// Verify the index is actually usable — bolt pages can be
	// corrupted in ways that don't surface on Open but hang on use.
	testReq := bleve.NewSearchRequest(bleve.NewMatchAllQuery())
	testReq.Size = 1
	if _, searchErr := idx.Search(testReq); searchErr != nil {
		log.Printf("warning: index unhealthy (%v), rebuilding", searchErr)
		if rmErr := idx.Close(); rmErr != nil {
			return nil, fmt.Errorf("close unhealthy index: %w", rmErr)
		}
		if rmErr := DeleteIndex(indexPath); rmErr != nil {
			return nil, fmt.Errorf("remove unhealthy index: %w", rmErr)
		}
		idx, err := bleve.New(indexPath, mapping)
		if err != nil {
			return nil, fmt.Errorf("create index after unhealthy: %w", err)
		}
		return idx, nil
	}

	return idx, nil
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

// Add indexes a document with the given (scoped) key.
func (i *Index) Add(id string, doc SearchDocument) error {
	if err := i.WaitOpen(openWaitTimeout); err != nil {
		return err
	}
	return i.index.Index(id, doc)
}

// Query searches the index, scoped to one project, with full-text
// search and optional filters.
func (i *Index) Query(project, q, category string, limit int) ([]SearchResult, error) {
	if err := i.WaitOpen(openWaitTimeout); err != nil {
		return nil, err
	}

	// Block until indexing completes if no stale data available
	<-i.ready

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

// Close closes the bleve index, if it was ever opened.
func (i *Index) Close() error {
	i.openMu.Lock()
	idx := i.index
	i.openMu.Unlock()
	if idx == nil {
		return nil
	}
	return idx.Close()
}

// CloseReady closes the ready channel exactly once, preventing double-close panics.
// This is used by tests that add documents directly without IndexAll.
func (i *Index) CloseReady() {
	i.readyOnce.Do(func() { close(i.ready) })
}

// indexHasData reports whether the index contains any documents.
func (i *Index) indexHasData() bool {
	req := bleve.NewSearchRequest(bleve.NewMatchAllQuery())
	req.Size = 0
	result, err := i.index.Search(req)
	if err != nil {
		return false
	}
	return result.Total > 0
}

// WaitReady blocks until the index has data available for queries.
func (i *Index) WaitReady() {
	<-i.ready
}

// DocCount reports the number of documents currently in the index.
func (i *Index) DocCount() (uint64, error) {
	return i.index.DocCount()
}

// IndexAll indexes all projects known to the resolver in the background.
// It closes the ready channel when complete.
func (i *Index) IndexAll(res *projects.Resolver) {
	defer i.CloseReady()

	for _, ref := range res.Snapshot() {
		switch ref.Kind {
		case projects.KindProject:
			i.indexProjectKnowledge(res.AgentsDir(ref), ref.Address)
		case projects.KindOrg:
			i.indexOrgKnowledge(res.OrgKnowledgeDir(ref), ref.Name)
		case projects.KindGlobal:
			i.indexProjectKnowledge(res.AgentsDir(ref), ref.Address)
		}
	}
}

// indexProjectKnowledge indexes all knowledge files under an agents dir.
func (i *Index) indexProjectKnowledge(agentsDir, projectName string) {
	for _, cat := range knowledge.ValidCategories() {
		catPath := knowledge.CategoryFilePath(agentsDir, cat)
		kf, err := knowledge.Load(catPath)
		if err != nil {
			log.Printf("warning: failed to load knowledge file %s: %v", catPath, err)
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
			if addErr := i.Add(projectName+"/"+entry.ID, doc); addErr != nil {
				log.Printf("warning: failed to index %s/%s: %v", projectName, entry.ID, addErr)
			}
		}
	}
}

// indexOrgKnowledge indexes an org's knowledge files as sections.
func (i *Index) indexOrgKnowledge(knowledgeDir, orgName string) {
	for _, catFile := range []string{"architecture.md", "review.md"} {
		filePath := filepath.Join(knowledgeDir, catFile)
		data, err := os.ReadFile(filePath)
		if err != nil {
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
			if addErr := i.Add(id, doc); addErr != nil {
				log.Printf("warning: failed to index %s: %v", id, addErr)
			}
		}
	}
}

// DeleteIndex removes a bleve index directory.
func DeleteIndex(indexPath string) error {
	return os.RemoveAll(indexPath)
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
