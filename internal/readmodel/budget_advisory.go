package readmodel

import (
	"fmt"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

const (
	DefaultBudgetAdvisoryFrontierStallK                = 5
	DefaultBudgetAdvisoryStaleExperimentMinutes        = 60
	DefaultBudgetAdvisoryObservationsWithoutConclusion = 5
)

const (
	budgetLimitSourceConfigured  = "configured"
	budgetLimitSourceRecommended = "recommended"
	budgetLimitSourceUnlimited   = "unlimited"
)

type BudgetAdvisoryInputs struct {
	Config       *store.Config
	State        *store.State
	Goal         *entity.Goal
	Hypotheses   []*entity.Hypothesis
	Experiments  []*entity.Experiment
	Observations []*entity.Observation
	Conclusions  []*entity.Conclusion
	Events       []store.Event
	Now          time.Time
}

type BudgetAdvisory struct {
	ConfiguredLimits BudgetAdvisoryLimits       `json:"configured_limits"`
	EffectiveLimits  BudgetAdvisoryLimits       `json:"effective_limits"`
	LimitSources     BudgetAdvisoryLimitSources `json:"limit_sources"`
	Usage            BudgetAdvisoryUsage        `json:"usage"`
	Frontier         BudgetAdvisoryFrontier     `json:"frontier"`
	StaleExperiments []StaleExperimentView      `json:"stale_experiments"`
	Warnings         []BudgetWarning            `json:"warnings"`
}

type BudgetAdvisoryLimits struct {
	MaxExperiments                int `json:"max_experiments"`
	MaxWallTimeH                  int `json:"max_wall_time_h"`
	FrontierStallK                int `json:"frontier_stall_k"`
	StaleExperimentMinutes        int `json:"stale_experiment_minutes"`
	ObservationsWithoutConclusion int `json:"observations_without_conclusion"`
}

type BudgetAdvisoryLimitSources struct {
	MaxExperiments                string `json:"max_experiments"`
	MaxWallTimeH                  string `json:"max_wall_time_h"`
	FrontierStallK                string `json:"frontier_stall_k"`
	StaleExperimentMinutes        string `json:"stale_experiment_minutes"`
	ObservationsWithoutConclusion string `json:"observations_without_conclusion"`
}

type BudgetAdvisoryUsage struct {
	Experiments                   int        `json:"experiments"`
	ElapsedH                      float64    `json:"elapsed_h"`
	ResearchStartedAt             *time.Time `json:"research_started_at,omitempty"`
	LastEventAt                   *time.Time `json:"last_event_at,omitempty"`
	TimeSinceLastEventS           float64    `json:"time_since_last_event_s"`
	ObservationsWithoutConclusion int        `json:"observations_without_conclusion"`
}

type BudgetAdvisoryFrontier struct {
	Applicable   bool   `json:"applicable"`
	StalledFor   int    `json:"stalled_for"`
	Limit        int    `json:"limit"`
	LimitSource  string `json:"limit_source"`
	StallReached bool   `json:"stall_reached"`
}

type BudgetWarning struct {
	Code           string `json:"code"`
	Severity       string `json:"severity"`
	Subject        string `json:"subject,omitempty"`
	Message        string `json:"message"`
	Recommendation string `json:"recommendation,omitempty"`
}

func BuildBudgetAdvisory(in BudgetAdvisoryInputs) BudgetAdvisory {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	advisory := BudgetAdvisory{
		ConfiguredLimits: configuredBudgetAdvisoryLimits(in.Config),
		StaleExperiments: []StaleExperimentView{},
		Warnings:         []BudgetWarning{},
	}
	advisory.EffectiveLimits, advisory.LimitSources = effectiveBudgetAdvisoryLimits(advisory.ConfiguredLimits)
	advisory.Usage = budgetAdvisoryUsage(in.State, in.Events, now)

	classByID := ClassifyExperimentsForReadFromHypotheses(in.Experiments, in.Hypotheses)
	staleThreshold := time.Duration(advisory.EffectiveLimits.StaleExperimentMinutes) * time.Minute
	advisory.StaleExperiments = FindStaleExperimentsForRead(in.Experiments, classByID, in.Events, staleThreshold, now)

	advisory.Frontier = BudgetAdvisoryFrontier{
		Applicable:  in.Goal != nil,
		Limit:       advisory.EffectiveLimits.FrontierStallK,
		LimitSource: advisory.LimitSources.FrontierStallK,
	}
	if in.Goal != nil {
		frontier := BuildFrontierSnapshot(in.Goal, in.Conclusions, NewObservationIndex(in.Observations), classByID)
		advisory.Frontier.StalledFor = frontier.StalledFor
		advisory.Frontier.StallReached = advisory.EffectiveLimits.FrontierStallK > 0 &&
			frontier.StalledFor >= advisory.EffectiveLimits.FrontierStallK
	}

	advisory.Warnings = budgetAdvisoryWarnings(advisory)
	return advisory
}

func configuredBudgetAdvisoryLimits(cfg *store.Config) BudgetAdvisoryLimits {
	if cfg == nil {
		return BudgetAdvisoryLimits{}
	}
	return BudgetAdvisoryLimits{
		MaxExperiments:                cfg.Budgets.MaxExperiments,
		MaxWallTimeH:                  cfg.Budgets.MaxWallTimeH,
		FrontierStallK:                cfg.Budgets.FrontierStallK,
		StaleExperimentMinutes:        cfg.Budgets.StaleExperimentMinutes,
		ObservationsWithoutConclusion: 0,
	}
}

func effectiveBudgetAdvisoryLimits(configured BudgetAdvisoryLimits) (BudgetAdvisoryLimits, BudgetAdvisoryLimitSources) {
	effective := configured
	sources := BudgetAdvisoryLimitSources{
		MaxExperiments:                positiveLimitSource(configured.MaxExperiments),
		MaxWallTimeH:                  positiveLimitSource(configured.MaxWallTimeH),
		FrontierStallK:                budgetLimitSourceConfigured,
		StaleExperimentMinutes:        budgetLimitSourceConfigured,
		ObservationsWithoutConclusion: budgetLimitSourceRecommended,
	}
	if effective.FrontierStallK <= 0 {
		effective.FrontierStallK = DefaultBudgetAdvisoryFrontierStallK
		sources.FrontierStallK = budgetLimitSourceRecommended
	}
	if effective.StaleExperimentMinutes <= 0 {
		effective.StaleExperimentMinutes = DefaultBudgetAdvisoryStaleExperimentMinutes
		sources.StaleExperimentMinutes = budgetLimitSourceRecommended
	}
	if effective.ObservationsWithoutConclusion <= 0 {
		effective.ObservationsWithoutConclusion = DefaultBudgetAdvisoryObservationsWithoutConclusion
		sources.ObservationsWithoutConclusion = budgetLimitSourceRecommended
	}
	return effective, sources
}

func positiveLimitSource(value int) string {
	if value > 0 {
		return budgetLimitSourceConfigured
	}
	return budgetLimitSourceUnlimited
}

func budgetAdvisoryUsage(st *store.State, events []store.Event, now time.Time) BudgetAdvisoryUsage {
	usage := BudgetAdvisoryUsage{
		ObservationsWithoutConclusion: observationsWithoutConclusion(events),
	}
	if st != nil {
		usage.Experiments = st.Counters["E"]
		if st.ResearchStartedAt != nil {
			started := *st.ResearchStartedAt
			usage.ResearchStartedAt = &started
			usage.ElapsedH = now.Sub(started).Hours()
		}
	}
	if last := latestEventAt(events); last != nil {
		usage.LastEventAt = last
		usage.TimeSinceLastEventS = nonNegativeDurationSeconds(now.Sub(*last))
	} else if st != nil && st.LastEventAt != nil {
		last := *st.LastEventAt
		usage.LastEventAt = &last
		usage.TimeSinceLastEventS = nonNegativeDurationSeconds(now.Sub(last))
	}
	return usage
}

func observationsWithoutConclusion(events []store.Event) int {
	count := 0
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Kind {
		case "conclusion.write", "conclusion.downgrade":
			return count
		case "observation.record":
			count++
		}
	}
	return count
}

func latestEventAt(events []store.Event) *time.Time {
	if len(events) == 0 {
		return nil
	}
	last := events[0].Ts
	for _, ev := range events[1:] {
		if ev.Ts.After(last) {
			last = ev.Ts
		}
	}
	return &last
}

func nonNegativeDurationSeconds(d time.Duration) float64 {
	if d < 0 {
		return 0
	}
	return d.Seconds()
}

func budgetAdvisoryWarnings(advisory BudgetAdvisory) []BudgetWarning {
	warnings := []BudgetWarning{}
	if limit := advisory.ConfiguredLimits.MaxExperiments; limit > 0 {
		used := advisory.Usage.Experiments
		switch {
		case used >= limit:
			warnings = append(warnings, BudgetWarning{
				Code:           "max_experiments_reached",
				Severity:       "critical",
				Message:        fmt.Sprintf("max_experiments=%d reached after %d experiments", limit, used),
				Recommendation: "finish in-flight work or ask the human to raise the experiment budget before designing more experiments",
			})
		case used*100 >= limit*80:
			warnings = append(warnings, BudgetWarning{
				Code:           "max_experiments_near_limit",
				Severity:       "warning",
				Message:        fmt.Sprintf("experiment budget is %d/%d used", used, limit),
				Recommendation: "prioritize high-value hypotheses and avoid opening low-confidence experiments",
			})
		}
	}
	if limit := advisory.ConfiguredLimits.MaxWallTimeH; limit > 0 {
		elapsed := advisory.Usage.ElapsedH
		switch {
		case elapsed >= float64(limit):
			warnings = append(warnings, BudgetWarning{
				Code:           "max_wall_time_reached",
				Severity:       "critical",
				Message:        fmt.Sprintf("max_wall_time_h=%d reached after %.2f hours", limit, elapsed),
				Recommendation: "finish in-flight work or ask the human to raise the wall-time budget before designing more experiments",
			})
		case elapsed >= float64(limit)*0.8:
			warnings = append(warnings, BudgetWarning{
				Code:           "max_wall_time_near_limit",
				Severity:       "warning",
				Message:        fmt.Sprintf("wall-time budget is %.2f/%d hours used", elapsed, limit),
				Recommendation: "prefer conclusions or cleanup over starting speculative work",
			})
		}
	}
	if advisory.Frontier.Applicable && advisory.Frontier.StallReached {
		warnings = append(warnings, BudgetWarning{
			Code:           "frontier_stalled",
			Severity:       "warning",
			Message:        fmt.Sprintf("frontier has stalled for %d conclusion(s), meeting the %s threshold of %d", advisory.Frontier.StalledFor, advisory.Frontier.LimitSource, advisory.Frontier.Limit),
			Recommendation: "stop or re-steer unless there is a strong, distinct next hypothesis",
		})
	}
	for _, stale := range advisory.StaleExperiments {
		warnings = append(warnings, BudgetWarning{
			Code:           "stale_experiment",
			Severity:       "warning",
			Subject:        stale.ID,
			Message:        fmt.Sprintf("%s has had no recorded activity for %.1f minutes", stale.ID, stale.StaleMinutes),
			Recommendation: "finish, reset, or explicitly abandon this in-flight work before opening more fronts",
		})
	}
	if limit := advisory.EffectiveLimits.ObservationsWithoutConclusion; limit > 0 &&
		advisory.Usage.ObservationsWithoutConclusion >= limit {
		warnings = append(warnings, BudgetWarning{
			Code:           "observations_without_conclusion",
			Severity:       "warning",
			Message:        fmt.Sprintf("%d observation(s) have been recorded since the last conclusion", advisory.Usage.ObservationsWithoutConclusion),
			Recommendation: "write a conclusion or explain why additional measurement is still necessary",
		})
	}
	return warnings
}
