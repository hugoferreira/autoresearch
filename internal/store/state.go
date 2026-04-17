package store

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// StateSchemaVersion is the current state.json schema version.
// v1: single-goal store (.research/goal.md).
// v2: multi-goal store (.research/goals/G-NNNN.md) + current_goal_id pointer.
const StateSchemaVersion = 2

type State struct {
	SchemaVersion     int            `json:"schema_version"`
	CurrentGoalID     string         `json:"current_goal_id,omitempty"`
	Paused            bool           `json:"paused"`
	PauseReason       string         `json:"pause_reason,omitempty"`
	PausedAt          *time.Time     `json:"paused_at,omitempty"`
	ResearchStartedAt *time.Time     `json:"research_started_at,omitempty"`
	Counters          map[string]int `json:"counters"`
	LastEventAt       *time.Time     `json:"last_event_at,omitempty"`
}

func (s *Store) State() (*State, error) {
	path := s.StatePath()
	cached, err := s.stateCache.getOrLoad(path, func(p string) (*State, error) {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read state: %w", err)
		}
		var st State
		if err := json.Unmarshal(data, &st); err != nil {
			return nil, fmt.Errorf("parse state: %w", err)
		}
		if st.Counters == nil {
			st.Counters = map[string]int{}
		}
		return &st, nil
	})
	if err != nil {
		return nil, err
	}
	// Return a copy so callers can mutate freely without poisoning the
	// cache. State is tiny (a handful of fields + a small map), so the
	// copy is cheap.
	out := *cached
	out.Counters = make(map[string]int, len(cached.Counters))
	for k, v := range cached.Counters {
		out.Counters[k] = v
	}
	return &out, nil
}

func (s *Store) writeState(st State) error {
	if st.SchemaVersion == 0 {
		st.SchemaVersion = StateSchemaVersion
	}
	if st.Counters == nil {
		st.Counters = map[string]int{}
	}
	data, err := json.MarshalIndent(&st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	data = append(data, '\n')
	path := s.StatePath()
	if err := atomicWrite(path, data); err != nil {
		return err
	}
	s.stateCache.drop(path)
	return nil
}

// UpdateState reads, mutates via fn, and writes state back.
// M2 is single-writer; file locking arrives with concurrent subagents in M9.
func (s *Store) UpdateState(fn func(*State) error) error {
	st, err := s.State()
	if err != nil {
		return err
	}
	if err := fn(st); err != nil {
		return err
	}
	return s.writeState(*st)
}
