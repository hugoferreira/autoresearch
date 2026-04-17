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

var ErrExperimentNotFound = errors.New("experiment not found")

func (s *Store) experimentPath(id string) string {
	return filepath.Join(s.ExperimentsDir(), id+".md")
}

func (s *Store) ReadExperiment(id string) (*entity.Experiment, error) {
	path := s.experimentPath(id)
	e, err := s.expCache.getOrLoad(path, func(p string) (*entity.Experiment, error) {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read experiment: %w", err)
		}
		return entity.ParseExperiment(data)
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrExperimentNotFound
	}
	return e, err
}

func (s *Store) WriteExperiment(e *entity.Experiment) error {
	data, err := e.Marshal()
	if err != nil {
		return fmt.Errorf("encode experiment: %w", err)
	}
	path := s.experimentPath(e.ID)
	if err := atomicWrite(path, data); err != nil {
		return err
	}
	s.expCache.drop(path)
	return nil
}

func (s *Store) ExperimentExists(id string) (bool, error) {
	_, err := os.Stat(s.experimentPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) ListExperiments() ([]*entity.Experiment, error) {
	entries, err := os.ReadDir(s.ExperimentsDir())
	if err != nil {
		return nil, fmt.Errorf("list experiments: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(ids)
	out := make([]*entity.Experiment, 0, len(ids))
	for _, id := range ids {
		e, err := s.ReadExperiment(id)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", id, err)
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *Store) ListExperimentsForHypothesis(hypID string) ([]*entity.Experiment, error) {
	all, err := s.ListExperiments()
	if err != nil {
		return nil, err
	}
	var out []*entity.Experiment
	for _, e := range all {
		if e.Hypothesis == hypID {
			out = append(out, e)
		}
	}
	return out, nil
}
