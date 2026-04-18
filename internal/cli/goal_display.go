package cli

import "github.com/bytter/autoresearch/internal/entity"

func formatGoalObjective(g *entity.Goal) string {
	if g == nil {
		return ""
	}
	obj := g.Objective.Direction + " " + g.Objective.Instrument
	if g.Objective.Target != "" {
		obj += " on " + g.Objective.Target
	}
	return obj
}

func formatGoalCompletion(g *entity.Goal) string {
	if g == nil {
		return ""
	}
	if g.IsOpenEnded() {
		return "open-ended -> continue_until_stall"
	}
	return formatGoalThresholdDecision(g.Completion.Threshold, g.EffectiveOnThreshold())
}
