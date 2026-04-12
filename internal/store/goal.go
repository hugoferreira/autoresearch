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

var (
	ErrGoalNotFound     = errors.New("goal not found")
	ErrNoActiveGoal     = errors.New("no active goal (run `autoresearch goal set` to bootstrap, or `autoresearch goal new` to start a new one)")
	ErrActiveGoalExists = errors.New("an active goal already exists; conclude or abandon it before starting a new one")
)

func (s *Store) goalPath(id string) string {
	return filepath.Join(s.GoalsDir(), id+".md")
}

// ReadGoal reads a goal by id from .research/goals/<id>.md.
func (s *Store) ReadGoal(id string) (*entity.Goal, error) {
	data, err := os.ReadFile(s.goalPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrGoalNotFound
	} else if err != nil {
		return nil, fmt.Errorf("read goal %s: %w", id, err)
	}
	return entity.ParseGoal(data)
}

// WriteGoal persists a goal to .research/goals/<id>.md. The goal must have
// an ID set; callers use AllocID(KindGoal) to mint one.
func (s *Store) WriteGoal(g *entity.Goal) error {
	if strings.TrimSpace(g.ID) == "" {
		return errors.New("goal.ID is required")
	}
	data, err := g.Marshal()
	if err != nil {
		return fmt.Errorf("encode goal: %w", err)
	}
	if err := os.MkdirAll(s.GoalsDir(), 0o755); err != nil {
		return fmt.Errorf("create goals dir: %w", err)
	}
	return atomicWrite(s.goalPath(g.ID), data)
}

func (s *Store) GoalExists(id string) (bool, error) {
	_, err := os.Stat(s.goalPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// ListGoals returns every goal on disk in id order.
func (s *Store) ListGoals() ([]*entity.Goal, error) {
	entries, err := os.ReadDir(s.GoalsDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
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
	out := make([]*entity.Goal, 0, len(ids))
	for _, id := range ids {
		g, err := s.ReadGoal(id)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", id, err)
		}
		out = append(out, g)
	}
	return out, nil
}

// ActiveGoal returns the goal referenced by state.current_goal_id, or
// ErrNoActiveGoal if none is set. Read-only callers (dashboard, frontier,
// tree, steering, TUI) use this to default to "the goal the loop is
// currently working against".
func (s *Store) ActiveGoal() (*entity.Goal, error) {
	st, err := s.State()
	if err != nil {
		return nil, err
	}
	if st.CurrentGoalID == "" {
		return nil, ErrNoActiveGoal
	}
	return s.ReadGoal(st.CurrentGoalID)
}
