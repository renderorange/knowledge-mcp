package search

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// SearchDocument is the document indexed by bleve.
type SearchDocument struct {
	Summary    string `json:"summary"`
	Detail     string `json:"detail"`
	Category   string `json:"category"`
	Confidence string `json:"confidence"`
}

// SearchResult is a single search result.
type SearchResult struct {
	ID         string  `json:"id"`
	Summary    string  `json:"summary"`
	Detail     string  `json:"detail"`
	Category   string  `json:"category"`
	Confidence string  `json:"confidence"`
	Score      float64 `json:"score"`
}

// Index wraps a bleve index for knowledge search.
type Index struct {
	indexPath string
	index     bleve.Index
}

// NewIndex opens or creates a bleve index at the given path.
func NewIndex(indexPath string) (*Index, error) {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(indexPath), 0755); err != nil {
		return nil, fmt.Errorf("create index dir: %w", err)
	}

	var idx bleve.Index
	var err error

	if _, statErr := os.Stat(indexPath); os.IsNotExist(statErr) {
		mapping := bleve.NewIndexMapping()
		idx, err = bleve.New(indexPath, mapping)
		if err != nil {
			return nil, fmt.Errorf("create index: %w", err)
		}
	} else {
		idx, err = bleve.Open(indexPath)
		if err != nil {
			return nil, fmt.Errorf("open index: %w", err)
		}
	}

	return &Index{indexPath: indexPath, index: idx}, nil
}

// Add indexes a document with the given ID.
func (i *Index) Add(id string, doc SearchDocument) error {
	return i.index.Index(id, doc)
}

// Query searches the index with full-text search and optional filters.
func (i *Index) Query(q string, category string, confidence string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 10
	}

	var baseQuery query.Query
	if q == "" {
		baseQuery = bleve.NewMatchAllQuery()
	} else {
		baseQuery = bleve.NewQueryStringQuery(q)
	}

	// Build conjunction with filters
	var conjuncts []query.Query
	conjuncts = append(conjuncts, baseQuery)

	if category != "" {
		catQuery := bleve.NewTermQuery(category)
		catQuery.SetField("category")
		conjuncts = append(conjuncts, catQuery)
	}
	if confidence != "" {
		confQuery := bleve.NewTermQuery(confidence)
		confQuery.SetField("confidence")
		conjuncts = append(conjuncts, confQuery)
	}

	var finalQuery query.Query
	if len(conjuncts) > 1 {
		finalQuery = bleve.NewConjunctionQuery(conjuncts...)
	} else {
		finalQuery = conjuncts[0]
	}

	req := bleve.NewSearchRequest(finalQuery)
	req.Size = limit
	req.Fields = []string{"summary", "detail", "category", "confidence"}

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
		if v, ok := hit.Fields["summary"].(string); ok {
			r.Summary = v
		}
		if v, ok := hit.Fields["detail"].(string); ok {
			r.Detail = v
		}
		if v, ok := hit.Fields["category"].(string); ok {
			r.Category = v
		}
		if v, ok := hit.Fields["confidence"].(string); ok {
			r.Confidence = v
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
