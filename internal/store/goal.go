package store

import (
	"errors"
	"fmt"
	"os"

	"github.com/bytter/autoresearch/internal/entity"
)

var ErrGoalNotSet = errors.New("no goal has been set (run `autoresearch goal set`)")

func (s *Store) ReadGoal() (*entity.Goal, error) {
	data, err := os.ReadFile(s.GoalPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrGoalNotSet
	} else if err != nil {
		return nil, fmt.Errorf("read goal: %w", err)
	}
	return entity.ParseGoal(data)
}

func (s *Store) WriteGoal(g *entity.Goal) error {
	data, err := g.Marshal()
	if err != nil {
		return fmt.Errorf("encode goal: %w", err)
	}
	return atomicWrite(s.GoalPath(), data)
}
