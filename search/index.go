package search

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

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

// Index wraps an in-memory bleve index for knowledge search. The index is
// rebuilt from the store files on every startup and lives only as long as
// the process; there is no disk state and therefore no lock between
// instances.
type Index struct {
	index     bleve.Index
	ready     chan struct{} // closed when indexing completes
	readyOnce sync.Once     // guards close of ready to prevent double-close panic
}

// NewIndex creates a new in-memory bleve index.
func NewIndex() (*Index, error) {
	idx, err := bleve.NewMemOnly(indexMapping())
	if err != nil {
		return nil, fmt.Errorf("create in-memory index: %w", err)
	}
	return &Index{index: idx, ready: make(chan struct{})}, nil
}

// Add indexes a document with the given (scoped) key.
func (i *Index) Add(id string, doc SearchDocument) error {
	return i.index.Index(id, doc)
}

// Query searches the index, scoped to one project, with full-text
// search and optional filters.
func (i *Index) Query(project, q, category string, limit int) ([]SearchResult, error) {
	// Block until indexing completes.
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

// Close closes the bleve index.
func (i *Index) Close() error {
	if i == nil || i.index == nil {
		return nil
	}
	return i.index.Close()
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
	return i.index.DocCount()
}

// IndexAll indexes all projects known to the resolver. It closes the ready
// channel when complete.
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
			if !os.IsNotExist(err) {
				log.Printf("warning: failed to load knowledge file %s: %v", catPath, err)
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
