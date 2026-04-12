package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/bytter/autoresearch/internal/entity"
)

// migrateV1ToV2 moves a v1 store (single .research/goal.md, no current_goal_id)
// into v2 shape (goals/G-NNNN.md + state.current_goal_id). It runs once, on
// the first Open after the binary upgrade, and is idempotent: if the v1 goal
// file does not exist it is a no-op.
//
// The migration:
//  1. Allocates G-0001 for the legacy goal.
//  2. Writes .research/goals/G-0001.md with status=active, created_at=mtime.
//  3. Stamps goal_id=G-0001 on every existing hypothesis (rewrites frontmatter).
//  4. Bumps state.schema_version to 2 and sets state.current_goal_id.
//  5. Removes .research/goal.md.
//  6. Appends a goal.migrated event.
func (s *Store) migrateV1ToV2() error {
	legacyPath := s.LegacyGoalPath()
	info, err := os.Stat(legacyPath)
	if errors.Is(err, os.ErrNotExist) {
		// No legacy goal. Still make sure schema_version is recorded.
		return s.UpdateState(func(st *State) error {
			if st.SchemaVersion < StateSchemaVersion {
				st.SchemaVersion = StateSchemaVersion
			}
			return nil
		})
	}
	if err != nil {
		return fmt.Errorf("stat legacy goal: %w", err)
	}

	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return fmt.Errorf("read legacy goal: %w", err)
	}
	g, err := entity.ParseGoal(data)
	if err != nil {
		return fmt.Errorf("parse legacy goal: %w", err)
	}

	const newID = "G-0001"
	createdAt := info.ModTime().UTC()
	g.ID = newID
	g.Status = entity.GoalStatusActive
	g.CreatedAt = &createdAt
	g.SchemaVersion = 2

	if err := os.MkdirAll(s.GoalsDir(), 0o755); err != nil {
		return fmt.Errorf("create goals dir: %w", err)
	}
	if err := s.WriteGoal(g); err != nil {
		return fmt.Errorf("write migrated goal: %w", err)
	}

	hyps, err := s.ListHypotheses()
	if err != nil {
		return fmt.Errorf("list hypotheses for migration: %w", err)
	}
	for _, h := range hyps {
		if h.GoalID != "" {
			continue
		}
		h.GoalID = newID
		if err := s.WriteHypothesis(h); err != nil {
			return fmt.Errorf("stamp goal_id on %s: %w", h.ID, err)
		}
	}

	if err := s.UpdateState(func(st *State) error {
		st.CurrentGoalID = newID
		st.SchemaVersion = StateSchemaVersion
		if st.Counters == nil {
			st.Counters = map[string]int{}
		}
		if st.Counters[string(KindGoal)] < 1 {
			st.Counters[string(KindGoal)] = 1
		}
		return nil
	}); err != nil {
		return fmt.Errorf("update state for migration: %w", err)
	}

	if err := os.Remove(legacyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove legacy goal.md: %w", err)
	}

	payload, _ := json.Marshal(map[string]any{
		"goal_id":           newID,
		"stamped_hypotheses": len(hyps),
	})
	return s.AppendEvent(Event{
		Kind:    "goal.migrated",
		Actor:   "system",
		Subject: newID,
		Data:    payload,
	})
}

// maybeMigrate runs schema upgrades on a freshly-opened store. Callers use
// it from Open(); Create() skips it because a new store is born at the
// current schema version.
func (s *Store) maybeMigrate() error {
	st, err := s.State()
	if err != nil {
		return err
	}
	if st.SchemaVersion >= StateSchemaVersion {
		return nil
	}
	if st.SchemaVersion < 2 {
		if err := s.migrateV1ToV2(); err != nil {
			return fmt.Errorf("migrate v1->v2: %w", err)
		}
	}
	return nil
}

