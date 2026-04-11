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

var ErrHypothesisNotFound = errors.New("hypothesis not found")

func (s *Store) hypothesisPath(id string) string {
	return filepath.Join(s.HypothesesDir(), id+".md")
}

func (s *Store) ReadHypothesis(id string) (*entity.Hypothesis, error) {
	data, err := os.ReadFile(s.hypothesisPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrHypothesisNotFound
	} else if err != nil {
		return nil, fmt.Errorf("read hypothesis: %w", err)
	}
	return entity.ParseHypothesis(data)
}

func (s *Store) WriteHypothesis(h *entity.Hypothesis) error {
	data, err := h.Marshal()
	if err != nil {
		return fmt.Errorf("encode hypothesis: %w", err)
	}
	return atomicWrite(s.hypothesisPath(h.ID), data)
}

func (s *Store) HypothesisExists(id string) (bool, error) {
	_, err := os.Stat(s.hypothesisPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) ListHypotheses() ([]*entity.Hypothesis, error) {
	entries, err := os.ReadDir(s.HypothesesDir())
	if err != nil {
		return nil, fmt.Errorf("list hypotheses: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".md"))
	}
	sort.Strings(ids)
	out := make([]*entity.Hypothesis, 0, len(ids))
	for _, id := range ids {
		h, err := s.ReadHypothesis(id)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", id, err)
		}
		out = append(out, h)
	}
	return out, nil
}
