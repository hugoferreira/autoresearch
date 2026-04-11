package store

import "fmt"

type EntityKind string

const (
	KindHypothesis  EntityKind = "H"
	KindExperiment  EntityKind = "E"
	KindObservation EntityKind = "O"
	KindConclusion  EntityKind = "C"
)

func (s *Store) AllocID(kind EntityKind) (string, error) {
	var id string
	err := s.UpdateState(func(st *State) error {
		st.Counters[string(kind)]++
		id = fmt.Sprintf("%s-%04d", kind, st.Counters[string(kind)])
		return nil
	})
	if err != nil {
		return "", err
	}
	return id, nil
}
