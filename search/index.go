package search

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
)

// metaFileVersion is the on-disk key-format version. A mismatch forces an index rebuild.
const metaFileVersion = 2

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
	index     bleve.Index
}

// NewIndex opens or creates a bleve index at the given path. indexNames is the
// set of project names the index will hold; a change from the recorded set
// triggers a rebuild.
func NewIndex(indexPath string, indexNames []string) (*Index, error) {
	if err := os.MkdirAll(filepath.Dir(indexPath), 0755); err != nil {
		return nil, fmt.Errorf("create index dir: %w", err)
	}

	if metaStale(indexPath, indexNames) {
		if err := DeleteIndex(indexPath); err != nil {
			return nil, fmt.Errorf("remove stale index: %w", err)
		}
	}

	mapping := indexMapping()

	var idx bleve.Index
	var err error

	if _, statErr := os.Stat(indexPath); os.IsNotExist(statErr) {
		idx, err = bleve.New(indexPath, mapping)
		if err != nil {
			return nil, fmt.Errorf("create index: %w", err)
		}
	} else {
		idx, err = bleve.Open(indexPath)
		if err != nil {
			// Index corrupted — rebuild from scratch
			log.Printf("warning: index corrupted (%v), rebuilding", err)
			if rmErr := DeleteIndex(indexPath); rmErr != nil {
				return nil, fmt.Errorf("remove corrupted index: %w", rmErr)
			}
			idx, err = bleve.New(indexPath, mapping)
			if err != nil {
				return nil, fmt.Errorf("create index after corruption: %w", err)
			}
		}
	}

	meta := indexMeta{Version: metaFileVersion, Names: indexNames}
	data, marshalErr := json.Marshal(meta)
	if marshalErr == nil {
		marshalErr = os.WriteFile(metaFilePath(indexPath), data, 0644)
	}
	if marshalErr != nil {
		idx.Close()
		return nil, fmt.Errorf("write index meta: %w", marshalErr)
	}

	return &Index{indexPath: indexPath, index: idx}, nil
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
	return i.index.Index(id, doc)
}

// Query searches the index, scoped to one project, with full-text
// search and optional filters.
func (i *Index) Query(project, q, category string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 10
	}

	var baseQuery query.Query
	if q == "" {
		baseQuery = bleve.NewMatchAllQuery()
	} else {
		baseQuery = bleve.NewQueryStringQuery(q)
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
	return i.index.Close()
}

// DeleteIndex removes a bleve index directory.
func DeleteIndex(indexPath string) error {
	return os.RemoveAll(indexPath)
}
