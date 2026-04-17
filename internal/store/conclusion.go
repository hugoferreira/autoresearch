package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
)

var ErrConclusionNotFound = errors.New("conclusion not found")

func (s *Store) conclusionPath(id string) string {
	return filepath.Join(s.ConclusionsDir(), id+".md")
}

func (s *Store) ReadConclusion(id string) (*entity.Conclusion, error) {
	path := s.conclusionPath(id)
	c, err := s.conclCache.getOrLoad(path, func(p string) (*entity.Conclusion, error) {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read conclusion: %w", err)
		}
		return entity.ParseConclusion(data)
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrConclusionNotFound
	}
	return c, err
}

func (s *Store) WriteConclusion(c *entity.Conclusion) error {
	data, err := c.Marshal()
	if err != nil {
		return fmt.Errorf("encode conclusion: %w", err)
	}
	path := s.conclusionPath(c.ID)
	if err := atomicWrite(path, data); err != nil {
		return err
	}
	s.conclCache.drop(path)
	return nil
}

func (s *Store) ListConclusions() ([]*entity.Conclusion, error) {
	entries, err := os.ReadDir(s.ConclusionsDir())
	if err != nil {
		return nil, fmt.Errorf("list conclusions: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(ids)
	out := make([]*entity.Conclusion, 0, len(ids))
	for _, id := range ids {
		c, err := s.ReadConclusion(id)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", id, err)
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) ListConclusionsForHypothesis(hypID string) ([]*entity.Conclusion, error) {
	all, err := s.ListConclusions()
	if err != nil {
		return nil, err
	}
	var out []*entity.Conclusion
	for _, c := range all {
		if c.Hypothesis == hypID {
			out = append(out, c)
		}
	}
	return out, nil
}
