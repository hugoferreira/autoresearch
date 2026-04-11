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

var ErrObservationNotFound = errors.New("observation not found")

func (s *Store) observationPath(id string) string {
	return filepath.Join(s.ObservationsDir(), id+".json")
}

func (s *Store) ReadObservation(id string) (*entity.Observation, error) {
	data, err := os.ReadFile(s.observationPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrObservationNotFound
	} else if err != nil {
		return nil, fmt.Errorf("read observation: %w", err)
	}
	return entity.ParseObservation(data)
}

func (s *Store) WriteObservation(o *entity.Observation) error {
	data, err := o.Marshal()
	if err != nil {
		return fmt.Errorf("encode observation: %w", err)
	}
	return atomicWrite(s.observationPath(o.ID), data)
}

func (s *Store) ListObservations() ([]*entity.Observation, error) {
	entries, err := os.ReadDir(s.ObservationsDir())
	if err != nil {
		return nil, fmt.Errorf("list observations: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(ids)
	out := make([]*entity.Observation, 0, len(ids))
	for _, id := range ids {
		o, err := s.ReadObservation(id)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", id, err)
		}
		out = append(out, o)
	}
	return out, nil
}

func (s *Store) ListObservationsForExperiment(expID string) ([]*entity.Observation, error) {
	all, err := s.ListObservations()
	if err != nil {
		return nil, err
	}
	var out []*entity.Observation
	for _, o := range all {
		if o.Experiment == expID {
			out = append(out, o)
		}
	}
	return out, nil
}
