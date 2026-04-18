package readmodel

import (
	"math"
	"sort"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

type FrontierRow struct {
	Conclusion string  `json:"conclusion"`
	Hypothesis string  `json:"hypothesis"`
	Candidate  string  `json:"candidate_experiment"`
	Value      float64 `json:"value"`
	DeltaFrac  float64 `json:"delta_frac"`
	// Classification is a read-time label for the candidate experiment.
	// "dead" means the experiment's parent hypothesis is no longer
	// loop-actionable for steering (terminal or decisive-but-unreviewed), so
	// the row is still historically valid but no longer actionable work.
	Classification string `json:"classification"`
	// HypothesisStatus records the hypothesis status that caused
	// Classification=dead. Kept separate from Hypothesis, which is the ID.
	HypothesisStatus string `json:"hypothesis_status,omitempty"`
	// RescuedBy is non-empty when the backing conclusion was supported via
	// a goal rescuer rather than a clean primary win. Renderers display a
	// "rescued by <instrument>" annotation so the reader cannot mistake a
	// rescued row for a clean primary-metric improvement.
	RescuedBy string `json:"rescued_by,omitempty"`
	// TiebreakValues holds one candidate value per goal.Rescuers clause, in
	// declared order. Used when two rows are within goal.NeutralBandFrac on
	// primary — the sort then picks the row that wins on the first rescuer
	// whose value differs. Absent rescuer data is represented as NaN so the
	// tiebreak skips cleanly over it.
	TiebreakValues []float64 `json:"-"`
}

type FrontierGoalAssessment struct {
	Mode              string  `json:"mode"`
	Threshold         float64 `json:"threshold,omitempty"`
	OnThreshold       string  `json:"on_threshold,omitempty"`
	Met               bool    `json:"met"`
	MetByConclusion   string  `json:"met_by_conclusion,omitempty"`
	RecommendedAction string  `json:"recommended_action"`
}

// FrontierSnapshot is the shared read projection for frontier surfaces.
type FrontierSnapshot struct {
	Rows       []FrontierRow          `json:"rows"`
	Assessment FrontierGoalAssessment `json:"assessment"`
	StalledFor int                    `json:"stalled_for"`
}

type frontierCandidate struct {
	Conclusion *entity.Conclusion
	Value      float64
}

// ComputeFrontier returns rows in best-first order for the objective and the
// count of conclusions written since the last frontier improvement.
func ComputeFrontier(s *store.Store, goal *entity.Goal, concls []*entity.Conclusion) (rows []FrontierRow, stalledFor int) {
	snap := ComputeFrontierSnapshot(s, goal, concls)
	return snap.Rows, snap.StalledFor
}

// ComputeFrontierSnapshot loads the read-side context needed to build a full
// frontier projection from store-backed entities.
func ComputeFrontierSnapshot(s *store.Store, goal *entity.Goal, concls []*entity.Conclusion) FrontierSnapshot {
	return BuildFrontierSnapshot(goal, concls, LoadObservationsByExperiment(s), LoadExperimentReadClasses(s))
}

// BuildFrontierSnapshot composes the frontier rows, goal assessment, and
// stalled-for counter from already-loaded read-side inputs.
func BuildFrontierSnapshot(goal *entity.Goal, concls []*entity.Conclusion, obsByExp map[string][]*entity.Observation, expClassByID map[string]ExperimentReadClass) FrontierSnapshot {
	rows, stalled := ComputeFrontierFromObservations(goal, concls, obsByExp, expClassByID)
	return FrontierSnapshot{
		Rows:       rows,
		Assessment: AssessGoalCompletion(goal, concls, obsByExp, expClassByID),
		StalledFor: stalled,
	}
}

func ComputeFrontierFromObservations(goal *entity.Goal, concls []*entity.Conclusion, obsByExp map[string][]*entity.Observation, expClassByID map[string]ExperimentReadClass) (rows []FrontierRow, stalledFor int) {
	rows = []FrontierRow{} // always non-nil so --json emits [] not null
	requireByInst := frontierRequireConstraints(goal)

	for _, c := range concls {
		val, ok := frontierCandidateValue(goal, c, obsByExp, requireByInst)
		if !ok {
			continue
		}
		class := ExperimentReadClassForID(expClassByID, c.CandidateExp)
		rows = append(rows, FrontierRow{
			Conclusion:       c.ID,
			Hypothesis:       c.Hypothesis,
			Candidate:        c.CandidateExp,
			Value:            val,
			DeltaFrac:        c.Effect.DeltaFrac,
			Classification:   class.Classification,
			HypothesisStatus: class.HypothesisStatus,
			RescuedBy:        c.Strict.RescuedBy,
			TiebreakValues:   rescuerTiebreakValues(goal, obsByExp[c.CandidateExp]),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return FrontierRowBetter(goal, rows[i], rows[j])
	})

	sortedByTime := make([]*entity.Conclusion, len(concls))
	copy(sortedByTime, concls)
	sort.Slice(sortedByTime, func(i, j int) bool { return sortedByTime[i].CreatedAt.Before(sortedByTime[j].CreatedAt) })

	hasBest := false
	var bestRow FrontierRow
	for _, c := range sortedByTime {
		val, ok := frontierCandidateValue(goal, c, obsByExp, requireByInst)
		if ok {
			cur := FrontierRow{
				Value:          val,
				TiebreakValues: rescuerTiebreakValues(goal, obsByExp[c.CandidateExp]),
			}
			if !hasBest {
				hasBest = true
				bestRow = cur
				stalledFor = 0
				continue
			}
			if FrontierRowBetter(goal, cur, bestRow) {
				bestRow = cur
				stalledFor = 0
				continue
			}
		}
		// stalled_for tracks conclusions written since the last frontier
		// improvement, not just supported+feasible conclusions. Once the
		// frontier exists, every later non-improving conclusion advances it.
		if hasBest {
			stalledFor++
		}
	}
	return rows, stalledFor
}

// FrontierRowBetter reports whether a is strictly better than b on the goal.
// When the primary values are within goal.NeutralBandFrac of each other, the
// comparison falls through to rescuers in goal-declared order; the first
// rescuer whose value differs decides. This is a limited Pareto-tiebreak,
// not a full multi-objective frontier.
func FrontierRowBetter(goal *entity.Goal, a, b FrontierRow) bool {
	if goal == nil {
		return false
	}
	if !withinPrimaryNeutralBand(goal, a.Value, b.Value) {
		return betterFrontierValue(goal.Objective.Direction, a.Value, b.Value)
	}
	for i, r := range goal.Rescuers {
		if i >= len(a.TiebreakValues) || i >= len(b.TiebreakValues) {
			continue
		}
		av, bv := a.TiebreakValues[i], b.TiebreakValues[i]
		if math.IsNaN(av) || math.IsNaN(bv) {
			continue
		}
		if av == bv {
			continue
		}
		return betterFrontierValue(r.Direction, av, bv)
	}
	return false
}

func AssessGoalCompletion(goal *entity.Goal, concls []*entity.Conclusion, obsByExp map[string][]*entity.Observation, expClassByID map[string]ExperimentReadClass) FrontierGoalAssessment {
	_ = expClassByID
	if goal == nil || goal.IsOpenEnded() {
		return FrontierGoalAssessment{
			Mode:              "open_ended",
			Met:               false,
			RecommendedAction: "continue",
		}
	}

	assessment := FrontierGoalAssessment{
		Mode:              "threshold",
		Threshold:         goal.Completion.Threshold,
		OnThreshold:       goal.EffectiveOnThreshold(),
		Met:               false,
		RecommendedAction: "continue",
	}

	var bestReviewed frontierCandidate
	hasReviewed := false
	for _, c := range concls {
		if c == nil || c.ReviewedBy == "" {
			continue
		}
		// goal_assessment is about accepted historical wins, not loop
		// actionability. Once a supported conclusion is accepted, its parent
		// hypothesis becomes supported and the experiment read class goes dead;
		// the assessment must still be able to report the goal as met.
		if c.Verdict != entity.VerdictSupported || c.Effect.Instrument != goal.Objective.Instrument {
			continue
		}
		obs := obsByExp[c.CandidateExp]
		if !goalConstraintsSatisfied(obs, goal.Constraints) {
			continue
		}
		val, ok := candidateObjectiveValue(obs, goal.Objective.Instrument)
		if !ok {
			continue
		}
		if !hasReviewed || betterFrontierValue(goal.Objective.Direction, val, bestReviewed.Value) {
			bestReviewed = frontierCandidate{Conclusion: c, Value: val}
			hasReviewed = true
		}
	}
	if !hasReviewed {
		return assessment
	}
	if meetsGoalThreshold(goal.Objective.Direction, bestReviewed.Conclusion.Effect.DeltaFrac, goal.Completion.Threshold) {
		assessment.Met = true
		assessment.MetByConclusion = bestReviewed.Conclusion.ID
		switch assessment.OnThreshold {
		case entity.GoalOnThresholdAskHuman:
			assessment.RecommendedAction = "ask_human"
		case entity.GoalOnThresholdStop:
			assessment.RecommendedAction = "stop"
		default:
			assessment.RecommendedAction = "continue"
		}
	}
	return assessment
}

// LoadObservationsByExperiment reads all observations once and returns them
// indexed by experiment ID. A nil map (on read error) is safe — callers get
// empty slices from the nil map.
func LoadObservationsByExperiment(s *store.Store) map[string][]*entity.Observation {
	all, err := s.ListObservations()
	if err != nil {
		return nil
	}
	return GroupObservationsByExperiment(all)
}

// GroupObservationsByExperiment buckets already-loaded observations by their
// experiment ID for frontier and status projections.
func GroupObservationsByExperiment(all []*entity.Observation) map[string][]*entity.Observation {
	m := make(map[string][]*entity.Observation, len(all))
	for _, o := range all {
		if o == nil {
			continue
		}
		m[o.Experiment] = append(m[o.Experiment], o)
	}
	return m
}

// rescuerTiebreakValues extracts one candidate value per goal rescuer, in
// declared order. Missing observations (the candidate wasn't measured on a
// rescuer instrument) are represented as NaN so the tiebreak comparator can
// skip them cleanly.
func rescuerTiebreakValues(goal *entity.Goal, obs []*entity.Observation) []float64 {
	if goal == nil || len(goal.Rescuers) == 0 {
		return nil
	}
	out := make([]float64, len(goal.Rescuers))
	for i, r := range goal.Rescuers {
		v, ok := candidateObjectiveValue(obs, r.Instrument)
		if !ok {
			out[i] = math.NaN()
			continue
		}
		out[i] = v
	}
	return out
}

// withinPrimaryNeutralBand reports whether two primary values are close
// enough (relative to the larger magnitude) that the goal considers them
// "tied" on the primary metric. Returns false whenever the goal doesn't
// declare a neutral band, preserving v3-goal behaviour exactly.
func withinPrimaryNeutralBand(goal *entity.Goal, a, b float64) bool {
	if goal == nil || goal.NeutralBandFrac <= 0 {
		return false
	}
	denom := math.Max(math.Max(math.Abs(a), math.Abs(b)), 1e-9)
	return math.Abs(a-b)/denom <= goal.NeutralBandFrac
}

func frontierRequireConstraints(goal *entity.Goal) map[string]string {
	requireByInst := map[string]string{}
	if goal == nil {
		return requireByInst
	}
	for _, c := range goal.Constraints {
		if c.Require != "" {
			requireByInst[c.Instrument] = c.Require
		}
	}
	return requireByInst
}

func frontierCandidateValue(goal *entity.Goal, c *entity.Conclusion, obsByExp map[string][]*entity.Observation, requireByInst map[string]string) (float64, bool) {
	if goal == nil || c == nil {
		return 0, false
	}
	if c.Verdict != entity.VerdictSupported || c.Effect.Instrument != goal.Objective.Instrument {
		return 0, false
	}
	if !requireSatisfied(obsByExp[c.CandidateExp], requireByInst) {
		return 0, false
	}
	return candidateObjectiveValue(obsByExp[c.CandidateExp], goal.Objective.Instrument)
}

func betterFrontierValue(direction string, candidate, incumbent float64) bool {
	if direction == "decrease" {
		return candidate < incumbent
	}
	return candidate > incumbent
}

func meetsGoalThreshold(direction string, deltaFrac, threshold float64) bool {
	if threshold <= 0 {
		return false
	}
	if direction == "decrease" {
		return deltaFrac <= -threshold
	}
	return deltaFrac >= threshold
}

func requireSatisfied(obs []*entity.Observation, req map[string]string) bool {
	if len(req) == 0 {
		return true
	}
	for inst, wanted := range req {
		ok := false
		for _, o := range obs {
			if o.Instrument != inst {
				continue
			}
			switch wanted {
			case "pass":
				if o.Pass != nil && *o.Pass {
					ok = true
				}
			default:
				// For v1, any non-"pass" require is treated as pass-through;
				// we document that require-clauses other than "pass" are
				// not evaluated yet.
				ok = true
			}
			if ok {
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func goalConstraintsSatisfied(obs []*entity.Observation, constraints []entity.Constraint) bool {
	for _, c := range constraints {
		if !goalConstraintSatisfied(obs, c) {
			return false
		}
	}
	return true
}

func goalConstraintSatisfied(obs []*entity.Observation, c entity.Constraint) bool {
	for _, o := range obs {
		if o.Instrument != c.Instrument {
			continue
		}
		switch {
		case c.Max != nil:
			if o.Value <= *c.Max {
				return true
			}
		case c.Min != nil:
			if o.Value >= *c.Min {
				return true
			}
		case c.Require != "":
			switch c.Require {
			case "pass":
				if o.Pass != nil && *o.Pass {
					return true
				}
			default:
				return true
			}
		}
	}
	return false
}

func candidateObjectiveValue(obs []*entity.Observation, instrument string) (float64, bool) {
	for _, o := range obs {
		if o.Instrument == instrument {
			return o.Value, true
		}
	}
	return 0, false
}
