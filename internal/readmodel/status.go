package readmodel

import (
	"sort"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

type BudgetSnapshot struct {
	Limits BudgetLimits `json:"limits"`
	Usage  BudgetUsage  `json:"usage"`
}

type BudgetLimits struct {
	MaxExperiments int `json:"max_experiments"`
	MaxWallTimeH   int `json:"max_wall_time_h"`
	FrontierStallK int `json:"frontier_stall_k"`
}

type BudgetUsage struct {
	Experiments int     `json:"experiments"`
	ElapsedH    float64 `json:"elapsed_h"`
}

func BuildBudgetSnapshot(cfg *store.Config, st *store.State, now time.Time) BudgetSnapshot {
	snap := BudgetSnapshot{}
	if cfg != nil {
		snap.Limits.MaxExperiments = cfg.Budgets.MaxExperiments
		snap.Limits.MaxWallTimeH = cfg.Budgets.MaxWallTimeH
		snap.Limits.FrontierStallK = cfg.Budgets.FrontierStallK
	}
	if st != nil {
		snap.Usage.Experiments = st.Counters["E"]
		if st.ResearchStartedAt != nil {
			snap.Usage.ElapsedH = now.Sub(*st.ResearchStartedAt).Hours()
		}
	}
	return snap
}

func BuildCounts(hypotheses, experiments, observations, conclusions int) map[string]int {
	return buildCounts(hypotheses, experiments, observations, conclusions, -1)
}

func BuildCountsWithLessons(hypotheses, experiments, observations, conclusions, lessons int) map[string]int {
	return buildCounts(hypotheses, experiments, observations, conclusions, lessons)
}

func buildCounts(hypotheses, experiments, observations, conclusions, lessons int) map[string]int {
	counts := map[string]int{
		"hypotheses":   hypotheses,
		"experiments":  experiments,
		"observations": observations,
		"conclusions":  conclusions,
	}
	if lessons >= 0 {
		counts["lessons"] = lessons
	}
	return counts
}

func FindUnobservedGoalInstruments(goal *entity.Goal, obs []*entity.Observation) []string {
	if goal == nil {
		return nil
	}
	needed := map[string]bool{goal.Objective.Instrument: true}
	for _, c := range goal.Constraints {
		needed[c.Instrument] = true
	}
	for _, o := range obs {
		delete(needed, o.Instrument)
	}
	if len(needed) == 0 {
		return nil
	}
	out := make([]string, 0, len(needed))
	for inst := range needed {
		out = append(out, inst)
	}
	sort.Strings(out)
	return out
}
