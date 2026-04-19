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
			if st.SchemaVersion < 2 {
				st.SchemaVersion = 2
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
	g.SchemaVersion = entity.GoalSchemaVersion

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
		st.SchemaVersion = 2
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
		"goal_id":            newID,
		"stamped_hypotheses": len(hyps),
	})
	return s.AppendEvent(Event{
		Kind:    "goal.migrated",
		Actor:   "system",
		Subject: newID,
		Data:    payload,
	})
}

// migrateV2ToV3 backfills durable experiment.goal_id provenance:
// hypothesis-backed experiments inherit it from their hypothesis, and legacy
// baseline experiments get it from their historical experiment.baseline event.
func (s *Store) migrateV2ToV3() error {
	exps, err := s.ListExperiments()
	if err != nil {
		return fmt.Errorf("list experiments for migration: %w", err)
	}
	hyps, err := s.ListHypotheses()
	if err != nil {
		return fmt.Errorf("list hypotheses for migration: %w", err)
	}

	hypGoal := make(map[string]string, len(hyps))
	for _, h := range hyps {
		if h == nil {
			continue
		}
		hypGoal[h.ID] = h.GoalID
	}

	needBaselineBackfill := map[string]struct{}{}
	type goalBackfill struct {
		ID     string
		GoalID string
		Source string
	}
	var backfilled []goalBackfill
	var (
		stampedHypothesis int
		stampedBaseline   int
	)
	for _, e := range exps {
		if e == nil || e.GoalID != "" {
			continue
		}
		switch {
		case e.Hypothesis != "":
			gid := hypGoal[e.Hypothesis]
			if gid == "" {
				return fmt.Errorf("backfill experiment %s goal_id: hypothesis %s has no goal_id", e.ID, e.Hypothesis)
			}
			e.GoalID = gid
			if err := s.WriteExperiment(e); err != nil {
				return fmt.Errorf("write experiment %s during migration: %w", e.ID, err)
			}
			backfilled = append(backfilled, goalBackfill{ID: e.ID, GoalID: gid, Source: "hypothesis"})
			stampedHypothesis++
		case e.IsBaseline:
			needBaselineBackfill[e.ID] = struct{}{}
		}
	}

	if len(needBaselineBackfill) > 0 {
		legacyGoals, err := s.legacyBaselineGoalMap(needBaselineBackfill)
		if err != nil {
			return fmt.Errorf("backfill baseline goal ownership: %w", err)
		}
		for _, e := range exps {
			if e == nil {
				continue
			}
			if _, ok := needBaselineBackfill[e.ID]; !ok {
				continue
			}
			gid := legacyGoals[e.ID]
			if gid == "" {
				return fmt.Errorf("backfill experiment %s goal_id: no experiment.baseline ownership event found", e.ID)
			}
			e.GoalID = gid
			if err := s.WriteExperiment(e); err != nil {
				return fmt.Errorf("write baseline experiment %s during migration: %w", e.ID, err)
			}
			backfilled = append(backfilled, goalBackfill{ID: e.ID, GoalID: gid, Source: "legacy_baseline_event"})
			stampedBaseline++
		}
	}

	if err := s.UpdateState(func(st *State) error {
		if st.SchemaVersion < 3 {
			st.SchemaVersion = 3
		}
		return nil
	}); err != nil {
		return fmt.Errorf("update state for experiment goal_id migration: %w", err)
	}

	if stampedHypothesis == 0 && stampedBaseline == 0 {
		return nil
	}
	for _, bf := range backfilled {
		payload, _ := json.Marshal(map[string]any{
			"goal_id": bf.GoalID,
			"source":  bf.Source,
		})
		if err := s.AppendEvent(Event{
			Kind:    "experiment.goal_backfilled",
			Actor:   "system",
			Subject: bf.ID,
			Data:    payload,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) legacyBaselineGoalMap(needed map[string]struct{}) (map[string]string, error) {
	out := map[string]string{}
	if len(needed) == 0 {
		return out, nil
	}
	events, err := s.Events(0)
	if err != nil {
		return nil, err
	}
	type baselinePayload struct {
		Goal string `json:"goal"`
	}
	for _, ev := range events {
		if ev.Kind != "experiment.baseline" {
			continue
		}
		if _, ok := needed[ev.Subject]; !ok {
			continue
		}
		var payload baselinePayload
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			return nil, fmt.Errorf("decode experiment.baseline payload for %s: %w", ev.Subject, err)
		}
		if payload.Goal == "" {
			return nil, fmt.Errorf("experiment.baseline event for %s is missing goal ownership", ev.Subject)
		}
		if prev, dup := out[ev.Subject]; dup && prev != payload.Goal {
			return nil, fmt.Errorf("conflicting experiment.baseline goal ownership for %s: %s vs %s", ev.Subject, prev, payload.Goal)
		}
		out[ev.Subject] = payload.Goal
	}
	return out, nil
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
		st.SchemaVersion = 2
	}
	if st.SchemaVersion < 3 {
		if err := s.migrateV2ToV3(); err != nil {
			return fmt.Errorf("migrate v2->v3: %w", err)
		}
	}
	return nil
}
