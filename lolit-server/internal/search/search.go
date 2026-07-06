package search

import (
	"fmt"
	"os"

	"github.com/blevesearch/bleve/v2"
)

// Engine wraps Bleve index.
type Engine struct {
	idx bleve.Index
}

type Doc struct {
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

func New(path string) (*Engine, error) {
	var idx bleve.Index
	if _, err := os.Stat(path); os.IsNotExist(err) {
		mapping := bleve.NewIndexMapping()
		idx, err = bleve.New(path, mapping)
		if err != nil {
			return nil, err
		}
	} else {
		idx, err = bleve.Open(path)
		if err != nil {
			return nil, err
		}
	}
	return &Engine{idx: idx}, nil
}

func (e *Engine) Index(id string, doc Doc) error {
	return e.idx.Index(id, doc)
}

func (e *Engine) Delete(id string) error {
	return e.idx.Delete(id)
}

func (e *Engine) Search(q string, limit int) (*bleve.SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	req := bleve.NewSearchRequest(bleve.NewQueryStringQuery(q))
	req.Size = limit
	return e.idx.Search(req)
}

func (e *Engine) Close() error {
	return e.idx.Close()
}

func DocID(repo, path string) string {
	return fmt.Sprintf("%s/%s", repo, path)
}
