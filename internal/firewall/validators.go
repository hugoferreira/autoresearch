package firewall

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/stats"
	"github.com/bytter/autoresearch/internal/store"
)

// ScopeBoundaryHint is emitted when a goal lacks a measurable instrument —
// the hallmark of a feature-delivery request pointed at the wrong tool.
const ScopeBoundaryHint = `autoresearch optimizes measurable properties of a working system; it does not build features. If your goal is feature delivery, this is the wrong tool.`

// RequireActiveGoal asserts that the store has an active goal. It is called
// by mutating verbs that create entities bound to a goal (hypothesis.add
// today; experiment and conclusion inherit via the hypothesis chain).
// Returns store.ErrNoActiveGoal when no goal is currently active so
// orchestrators see a stable sentinel.
func RequireActiveGoal(st *store.State) error {
	if st == nil || strings.TrimSpace(st.CurrentGoalID) == "" {
		return store.ErrNoActiveGoal
	}
	return nil
}

// RequireNoActiveGoal asserts that no goal is currently active — the
// precondition for `goal new`. Returns store.ErrActiveGoalExists with the
// offending id appended to the message so the caller can surface it.
func RequireNoActiveGoal(st *store.State) error {
	if st != nil && strings.TrimSpace(st.CurrentGoalID) != "" {
		return fmt.Errorf("%w (active goal is %s — run `autoresearch goal conclude` or `autoresearch goal abandon` first)",
			store.ErrActiveGoalExists, st.CurrentGoalID)
	}
	return nil
}

func ValidateGoal(g *entity.Goal, cfg *store.Config) error {
	if strings.TrimSpace(g.Objective.Instrument) == "" {
		return fmt.Errorf("goal.md objective has no `instrument` field. %s", ScopeBoundaryHint)
	}
	if _, ok := cfg.Instruments[g.Objective.Instrument]; !ok {
		return fmt.Errorf("objective instrument %q is not registered in config.yaml — register it first with `autoresearch instrument register`", g.Objective.Instrument)
	}
	switch g.Objective.Direction {
	case "increase", "decrease":
	default:
		return fmt.Errorf("objective direction must be 'increase' or 'decrease', got %q", g.Objective.Direction)
	}
	if g.Completion != nil {
		if g.Completion.Threshold <= 0 {
			return errors.New("goal completion.threshold must be > 0")
		}
		switch g.EffectiveOnThreshold() {
		case entity.GoalOnThresholdAskHuman,
			entity.GoalOnThresholdStop,
			entity.GoalOnThresholdContinueUntilStall,
			entity.GoalOnThresholdContinueUntilBudgetCap:
		default:
			return fmt.Errorf("goal completion.on_threshold must be one of %q, %q, %q, or %q, got %q",
				entity.GoalOnThresholdAskHuman,
				entity.GoalOnThresholdStop,
				entity.GoalOnThresholdContinueUntilStall,
				entity.GoalOnThresholdContinueUntilBudgetCap,
				g.Completion.OnThreshold)
		}
	}
	if len(g.Constraints) == 0 {
		return errors.New("goal must declare at least one constraint (autoresearch needs something to bound the search)")
	}
	for i, c := range g.Constraints {
		if strings.TrimSpace(c.Instrument) == "" {
			return fmt.Errorf("constraint[%d] has no `instrument` field", i)
		}
		if _, ok := cfg.Instruments[c.Instrument]; !ok {
			return fmt.Errorf("constraint[%d] instrument %q is not registered in config.yaml", i, c.Instrument)
		}
		if c.Max == nil && c.Min == nil && strings.TrimSpace(c.Require) == "" {
			return fmt.Errorf("constraint[%d] must set at least one of max, min, or require", i)
		}
	}
	return nil
}

func ValidateExperiment(e *entity.Experiment, cfg *store.Config) error {
	if !e.IsBaseline && strings.TrimSpace(e.Hypothesis) == "" {
		return errors.New("experiment must reference a hypothesis")
	}
	if len(e.Instruments) == 0 {
		return errors.New("experiment must declare at least one instrument")
	}
	for _, name := range e.Instruments {
		if _, ok := cfg.Instruments[name]; !ok {
			return fmt.Errorf("instrument %q is not registered in config.yaml", name)
		}
	}
	if strings.TrimSpace(e.Baseline.Ref) == "" {
		return errors.New("baseline ref is required")
	}
	return nil
}

// VerdictDecision is the result of applying the strict firewall to a
// requested verdict. If Passed is false, FinalVerdict is always "inconclusive"
// and Reasons explains why.
type VerdictDecision struct {
	FinalVerdict string
	Passed       bool
	Downgraded   bool
	Reasons      []string
}

// CheckStrictVerdict applies the strict-mode firewall to a requested verdict
// against a hypothesis and a statistical comparison. Rules:
//
//   - "supported" requires (a) the entire 95% CI on the fractional effect to
//     lie on the predicted side of zero, and (b) the point-estimate magnitude
//     |delta_frac| to meet or exceed the hypothesis's min_effect.
//   - "refuted" is allowed unconditionally (kill_if clauses are free-form
//     strings the CLI cannot evaluate), but if the CI lies entirely on the
//     WRONG side of zero — a structural refutation — the decision notes it.
//   - "inconclusive" always passes.
//
// When "supported" is requested and the evidence doesn't justify it, the
// verdict is downgraded to "inconclusive" and the reasons are recorded.
func CheckStrictVerdict(requested string, h *entity.Hypothesis, c *stats.Comparison) VerdictDecision {
	d := VerdictDecision{FinalVerdict: requested, Passed: true}
	switch requested {
	case entity.VerdictSupported:
		if c == nil || c.NBaseline < 2 || c.NCandidate < 2 {
			d.Passed = false
			d.Downgraded = true
			d.FinalVerdict = entity.VerdictInconclusive
			d.Reasons = append(d.Reasons, "no comparison available (baseline or candidate has fewer than 2 samples)")
			return d
		}
		switch h.Predicts.Direction {
		case "decrease":
			if c.CIHighFrac >= 0 {
				d.Reasons = append(d.Reasons, fmt.Sprintf("95%% CI upper bound on delta_frac is %+.4f — crosses zero (not a clean decrease)", c.CIHighFrac))
			}
		case "increase":
			if c.CILowFrac <= 0 {
				d.Reasons = append(d.Reasons, fmt.Sprintf("95%% CI lower bound on delta_frac is %+.4f — crosses zero (not a clean increase)", c.CILowFrac))
			}
		}
		if math.Abs(c.DeltaFrac) < h.Predicts.MinEffect {
			d.Reasons = append(d.Reasons, fmt.Sprintf("|delta_frac| %.4f < min_effect %.4f — effect too small to call supported", math.Abs(c.DeltaFrac), h.Predicts.MinEffect))
		}
		if len(d.Reasons) > 0 {
			d.Passed = false
			d.Downgraded = true
			d.FinalVerdict = entity.VerdictInconclusive
		}

	case entity.VerdictRefuted:
		// Free-form kill_if strings can't be parsed; trust the agent but note
		// when the CI structurally refutes the prediction (wrong-side clean).
		if c != nil && c.NBaseline >= 2 && c.NCandidate >= 2 {
			switch h.Predicts.Direction {
			case "decrease":
				if c.CILowFrac > 0 {
					d.Reasons = append(d.Reasons, fmt.Sprintf("structural refutation: CI lower bound %+.4f is on the increase side (predicted decrease)", c.CILowFrac))
				}
			case "increase":
				if c.CIHighFrac < 0 {
					d.Reasons = append(d.Reasons, fmt.Sprintf("structural refutation: CI upper bound %+.4f is on the decrease side (predicted increase)", c.CIHighFrac))
				}
			}
		}

	case entity.VerdictInconclusive:
		// No gate.

	default:
		d.Passed = false
		d.Reasons = append(d.Reasons, fmt.Sprintf("unknown verdict %q (want supported|refuted|inconclusive)", requested))
	}
	return d
}

// BudgetBreach describes which budget rule a would-be mutation violates.
// A zero value means no breach; Ok() reports that.
type BudgetBreach struct {
	Rule     string // "max_experiments" | "max_wall_time_h" | ""
	Limit    any
	Observed any
	Message  string
}

func (b BudgetBreach) Ok() bool { return b.Rule == "" }

// CheckBudgetForNewExperiment enforces the "dry-up" policy: when any budget
// rule would be violated by creating a new experiment, refuse. In-flight
// experiments can still be implemented, observed, and concluded — the hard
// stop only applies at design time, so the orchestrator can finish what it
// started before shutting down.
func CheckBudgetForNewExperiment(st *store.State, cfg *store.Config, now time.Time) BudgetBreach {
	b := cfg.Budgets
	if b.MaxExperiments > 0 {
		current := st.Counters["E"]
		if current >= b.MaxExperiments {
			return BudgetBreach{
				Rule:     "max_experiments",
				Limit:    b.MaxExperiments,
				Observed: current,
				Message: fmt.Sprintf("max_experiments=%d reached (already designed %d); no new experiments until `autoresearch budget set` raises the cap",
					b.MaxExperiments, current),
			}
		}
	}
	if b.MaxWallTimeH > 0 && st.ResearchStartedAt != nil {
		elapsed := now.Sub(*st.ResearchStartedAt)
		limit := time.Duration(b.MaxWallTimeH) * time.Hour
		if elapsed >= limit {
			return BudgetBreach{
				Rule:     "max_wall_time_h",
				Limit:    b.MaxWallTimeH,
				Observed: elapsed.Round(time.Minute).String(),
				Message: fmt.Sprintf("max_wall_time_h=%d reached (elapsed %s since init); no new experiments until the budget is raised",
					b.MaxWallTimeH, elapsed.Round(time.Minute)),
			}
		}
	}
	return BudgetBreach{}
}

// CheckObservationRequest validates an observe request before any command runs.
// It enforces that the instrument is registered, the experiment is in a state
// that accepts observations (implemented, measured, or analyzed), and — in
// strict mode — that the requested sample count meets the instrument's
// min_samples gate.
func CheckObservationRequest(instName string, requestedSamples int, exp *entity.Experiment, cfg *store.Config, strict bool) error {
	inst, ok := cfg.Instruments[instName]
	if !ok {
		return fmt.Errorf("instrument %q is not registered in config.yaml", instName)
	}
	switch exp.Status {
	case entity.ExpImplemented, entity.ExpMeasured, entity.ExpAnalyzed:
	default:
		return fmt.Errorf("experiment %s is in status %q; observations require the experiment to be implemented first", exp.ID, exp.Status)
	}
	if strict && inst.MinSamples > 0 {
		// requestedSamples==0 means "use the default" which will be clamped to
		// MinSamples inside the runner, so that's fine. Only reject an
		// explicit request below the minimum.
		if requestedSamples > 0 && requestedSamples < inst.MinSamples {
			return fmt.Errorf("instrument %q requires at least %d samples in strict mode, got %d", instName, inst.MinSamples, requestedSamples)
		}
	}
	return nil
}

// CheckInstrumentDependencies verifies that every prerequisite declared in the
// instrument's `requires` list is satisfied by a passing observation on the
// same experiment. Called from the observe command before running an instrument.
//
// Requires entries are "instrument=condition" pairs. v1 supports one condition:
//   - "pass" — the prerequisite instrument must have at least one observation
//     with pass=true on this experiment.
func CheckInstrumentDependencies(instName string, cfg *store.Config, observations []*entity.Observation) error {
	inst, ok := cfg.Instruments[instName]
	if !ok {
		return fmt.Errorf("instrument %q is not registered", instName)
	}
	if len(inst.Requires) == 0 {
		return nil
	}
	for _, req := range inst.Requires {
		parts := strings.SplitN(req, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("malformed requires entry %q (expected instrument=condition)", req)
		}
		reqInst, reqCond := parts[0], parts[1]
		switch reqCond {
		case "pass":
			found := false
			for _, o := range observations {
				if o.Instrument == reqInst && o.Pass != nil && *o.Pass {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("instrument %q requires %q to have a passing observation on this experiment first (run `autoresearch observe <exp> --instrument %s` before observing with %s)",
					instName, reqInst, reqInst, instName)
			}
		default:
			return fmt.Errorf("unsupported requires condition %q in %q (only 'pass' is supported)", reqCond, req)
		}
	}
	return nil
}

// ValidateLesson checks the structural rules of a lesson: non-empty claim,
// valid scope, required Subjects for scope=hypothesis, and well-formed ID
// prefixes on Subjects and SupersedesID. Existence of referenced entities is
// NOT checked here — that's the CLI handler's job, matching the pattern used
// for Hypothesis parent references.
func ValidatePredictedEffect(pe *entity.PredictedEffect, scope string) error {
	if strings.TrimSpace(pe.Instrument) == "" {
		return errors.New("predicted_effect.instrument is required")
	}
	switch pe.Direction {
	case "increase", "decrease":
	default:
		return fmt.Errorf("predicted_effect.direction must be 'increase' or 'decrease', got %q", pe.Direction)
	}
	if pe.MinEffect <= 0 {
		return errors.New("predicted_effect.min_effect must be > 0")
	}
	if pe.MaxEffect > 0 && pe.MaxEffect < pe.MinEffect {
		return errors.New("predicted_effect.max_effect must be >= min_effect when set")
	}
	if scope == entity.LessonScopeSystem {
		return errors.New("predicted_effect is only valid for scope=hypothesis lessons")
	}
	return nil
}

func ValidateLesson(l *entity.Lesson) error {
	if strings.TrimSpace(l.Claim) == "" {
		return errors.New("lesson claim is required")
	}
	switch l.Scope {
	case entity.LessonScopeHypothesis, entity.LessonScopeSystem:
	default:
		return fmt.Errorf("lesson scope must be %q or %q, got %q",
			entity.LessonScopeHypothesis, entity.LessonScopeSystem, l.Scope)
	}
	if l.Scope == entity.LessonScopeHypothesis && len(l.Subjects) == 0 {
		return errors.New("lesson with scope=hypothesis requires at least one subject (--from H-NNNN,C-NNNN,...)")
	}
	for i, sub := range l.Subjects {
		if !isValidSubjectID(sub) {
			return fmt.Errorf("subject[%d] %q must be an H-/E-/C- id", i, sub)
		}
	}
	if l.SupersedesID != "" && !strings.HasPrefix(l.SupersedesID, "L-") {
		return fmt.Errorf("supersedes target %q must be an L- id", l.SupersedesID)
	}
	switch l.Status {
	case "",
		entity.LessonStatusActive,
		entity.LessonStatusProvisional,
		entity.LessonStatusInvalidated,
		entity.LessonStatusSuperseded:
	default:
		return fmt.Errorf("lesson status must be %q, %q, %q, or %q, got %q",
			entity.LessonStatusActive,
			entity.LessonStatusProvisional,
			entity.LessonStatusInvalidated,
			entity.LessonStatusSuperseded,
			l.Status)
	}
	if l.Provenance != nil && l.Provenance.SourceChain != "" {
		switch l.Provenance.SourceChain {
		case entity.LessonSourceSystem,
			entity.LessonSourceReviewedDecisive,
			entity.LessonSourceUnreviewedDecisive,
			entity.LessonSourceInconclusive:
		default:
			return fmt.Errorf("lesson provenance.source_chain must be %q, %q, %q, or %q, got %q",
				entity.LessonSourceSystem,
				entity.LessonSourceReviewedDecisive,
				entity.LessonSourceUnreviewedDecisive,
				entity.LessonSourceInconclusive,
				l.Provenance.SourceChain)
		}
	}
	if l.PredictedEffect != nil {
		if err := ValidatePredictedEffect(l.PredictedEffect, l.Scope); err != nil {
			return err
		}
	}
	return nil
}

func isValidSubjectID(id string) bool {
	return strings.HasPrefix(id, "H-") ||
		strings.HasPrefix(id, "E-") ||
		strings.HasPrefix(id, "C-")
}

type inspiredByReviewReader interface {
	ReadHypothesis(id string) (*entity.Hypothesis, error)
	ReadExperiment(id string) (*entity.Experiment, error)
	ReadConclusion(id string) (*entity.Conclusion, error)
}

func AssessLessonSourceChain(r inspiredByReviewReader, lesson *entity.Lesson) (string, error) {
	if lesson == nil {
		return "", errors.New("cannot assess source chain for a nil lesson")
	}
	if lesson.Scope == entity.LessonScopeSystem || len(lesson.Subjects) == 0 {
		return entity.LessonSourceSystem, nil
	}
	if r == nil {
		return "", errors.New("cannot assess lesson source chain without a store reader")
	}

	source := entity.LessonSourceReviewedDecisive
	for _, subject := range lesson.Subjects {
		subjectSource, err := assessLessonSubjectSourceChain(r, lesson, subject)
		if err != nil {
			return "", err
		}
		if lessonSourceChainRank(subjectSource) > lessonSourceChainRank(source) {
			source = subjectSource
		}
	}
	return source, nil
}

func lessonSourceChainRank(source string) int {
	switch source {
	case entity.LessonSourceInconclusive:
		return 3
	case entity.LessonSourceUnreviewedDecisive:
		return 2
	case entity.LessonSourceReviewedDecisive:
		return 1
	case entity.LessonSourceSystem:
		return 0
	default:
		return 0
	}
}

func assessLessonSubjectSourceChain(r inspiredByReviewReader, lesson *entity.Lesson, subject string) (string, error) {
	switch {
	case strings.HasPrefix(subject, "H-"):
		h, err := r.ReadHypothesis(subject)
		if err != nil {
			return "", fmt.Errorf("lesson %s subject %s: %w", lesson.ID, subject, err)
		}
		return sourceChainForHypothesisStatus(h.Status), nil
	case strings.HasPrefix(subject, "E-"):
		e, err := r.ReadExperiment(subject)
		if err != nil {
			return "", fmt.Errorf("lesson %s subject %s: %w", lesson.ID, subject, err)
		}
		if strings.TrimSpace(e.Hypothesis) == "" {
			return entity.LessonSourceInconclusive, nil
		}
		h, err := r.ReadHypothesis(e.Hypothesis)
		if err != nil {
			return "", fmt.Errorf("lesson %s subject %s hypothesis %s: %w", lesson.ID, subject, e.Hypothesis, err)
		}
		return sourceChainForHypothesisStatus(h.Status), nil
	case strings.HasPrefix(subject, "C-"):
		c, err := r.ReadConclusion(subject)
		if err != nil {
			return "", fmt.Errorf("lesson %s subject %s: %w", lesson.ID, subject, err)
		}
		if c.Verdict != entity.VerdictSupported && c.Verdict != entity.VerdictRefuted {
			return entity.LessonSourceInconclusive, nil
		}
		if strings.TrimSpace(c.ReviewedBy) != "" {
			return entity.LessonSourceReviewedDecisive, nil
		}
		h, err := r.ReadHypothesis(c.Hypothesis)
		if err != nil {
			return "", fmt.Errorf("lesson %s subject %s hypothesis %s: %w", lesson.ID, subject, c.Hypothesis, err)
		}
		return sourceChainForHypothesisStatus(h.Status), nil
	default:
		return entity.LessonSourceInconclusive, nil
	}
}

func sourceChainForHypothesisStatus(status string) string {
	switch status {
	case entity.StatusSupported, entity.StatusRefuted:
		return entity.LessonSourceReviewedDecisive
	case entity.StatusUnreviewed:
		return entity.LessonSourceUnreviewedDecisive
	default:
		return entity.LessonSourceInconclusive
	}
}

// CheckInspiredByLessonsReviewed blocks lessons whose supporting chain still
// depends on an unreviewed decisive conclusion. This closes the loophole where
// an orchestrator cites a lesson from an unreviewed chain via --inspired-by
// instead of using --parent directly.
func CheckInspiredByLessonsReviewed(r inspiredByReviewReader, lessons []*entity.Lesson) error {
	if len(lessons) == 0 {
		return nil
	}
	if r == nil {
		return errors.New("cannot validate inspired-by lessons without a store reader")
	}
	for _, lesson := range lessons {
		if lesson == nil {
			continue
		}
		if lesson.EffectiveStatus() != entity.LessonStatusActive {
			return fmt.Errorf("lesson %s is %s — only active lessons may be used in --inspired-by", lesson.ID, lesson.EffectiveStatus())
		}
		source, err := AssessLessonSourceChain(r, lesson)
		if err != nil {
			return err
		}
		switch source {
		case entity.LessonSourceSystem, entity.LessonSourceReviewedDecisive:
			continue
		case entity.LessonSourceUnreviewedDecisive:
			return fmt.Errorf("lesson %s resolves to an unreviewed decisive chain — dispatch the gate reviewer before using that lesson in --inspired-by", lesson.ID)
		default:
			return fmt.Errorf("lesson %s resolves to a non-decisive chain (%s) — only reviewed decisive lessons may be used in --inspired-by", lesson.ID, source)
		}
	}
	return nil
}

// CheckParentReviewed ensures that a hypothesis's parent (if any) has been
// through gate review before new sub-hypotheses are derived from it. A parent
// in "unreviewed" status means its conclusion hasn't been independently
// validated — building on it would bypass the two-agent safety model.
func CheckParentReviewed(parent *entity.Hypothesis) error {
	if parent.Status == entity.StatusUnreviewed {
		return fmt.Errorf("parent hypothesis %s is unreviewed — dispatch the gate reviewer before deriving sub-hypotheses from it", parent.ID)
	}
	return nil
}

func ValidateHypothesis(h *entity.Hypothesis, cfg *store.Config) error {
	if strings.TrimSpace(h.Claim) == "" {
		return errors.New("hypothesis claim is required")
	}
	if strings.TrimSpace(h.Predicts.Instrument) == "" {
		return errors.New("hypothesis predicts.instrument is required")
	}
	if _, ok := cfg.Instruments[h.Predicts.Instrument]; !ok {
		return fmt.Errorf("predicts instrument %q is not registered in config.yaml", h.Predicts.Instrument)
	}
	if strings.TrimSpace(h.Predicts.Target) == "" {
		return errors.New("hypothesis predicts.target is required")
	}
	switch h.Predicts.Direction {
	case "increase", "decrease":
	default:
		return fmt.Errorf("predicts direction must be 'increase' or 'decrease', got %q", h.Predicts.Direction)
	}
	if h.Predicts.MinEffect <= 0 {
		return errors.New("predicts min_effect must be > 0 (a falsifiable hypothesis needs a quantitative threshold)")
	}
	for i, lid := range h.InspiredBy {
		if !strings.HasPrefix(lid, "L-") {
			return fmt.Errorf("inspired_by[%d] %q must be an L- id", i, lid)
		}
	}
	if len(h.KillIf) == 0 {
		return errors.New("hypothesis requires at least one kill_if clause (what would refute this?)")
	}
	for i, k := range h.KillIf {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("kill_if[%d] is empty", i)
		}
	}
	return nil
}
