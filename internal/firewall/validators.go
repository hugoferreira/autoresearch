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
	if err := validateGoalRescuers(g, cfg); err != nil {
		return err
	}
	return nil
}

// validateGoalRescuers checks that rescuer clauses on a goal are
// well-formed. A rescuer is optional, but when present must name a
// registered instrument, declare a direction, and carry a positive
// min_effect (rescue is still a scientific claim). Rescuers may overlap
// with constraints — declaring a max cap on size AND marking size as a
// rescuer is a valid pattern — but cannot equal the goal's objective
// instrument (that would make the objective self-rescue, which is
// nonsense). A goal with rescuers must also declare a positive
// neutral_band_frac; rescue only fires when the primary is within that
// band, so leaving it zero silently disables rescue.
func validateGoalRescuers(g *entity.Goal, cfg *store.Config) error {
	if g == nil || len(g.Rescuers) == 0 {
		return nil
	}
	if g.NeutralBandFrac <= 0 {
		return errors.New("goal declares rescuers but neutral_band_frac is unset (rescue only fires when |delta_frac| on the primary is within neutral_band_frac — set it explicitly)")
	}
	seen := map[string]struct{}{}
	for i, r := range g.Rescuers {
		inst := strings.TrimSpace(r.Instrument)
		if inst == "" {
			return fmt.Errorf("rescuer[%d] has no `instrument` field", i)
		}
		if inst == strings.TrimSpace(g.Objective.Instrument) {
			return fmt.Errorf("rescuer[%d] instrument %q equals the goal objective — the primary cannot rescue itself", i, inst)
		}
		if _, ok := cfg.Instruments[inst]; !ok {
			return fmt.Errorf("rescuer[%d] instrument %q is not registered in config.yaml", i, inst)
		}
		if _, dup := seen[inst]; dup {
			return fmt.Errorf("rescuer[%d] instrument %q is declared more than once", i, inst)
		}
		seen[inst] = struct{}{}
		switch r.Direction {
		case "increase", "decrease":
		default:
			return fmt.Errorf("rescuer[%d] direction must be 'increase' or 'decrease', got %q", i, r.Direction)
		}
		if r.MinEffect < 0 {
			return fmt.Errorf("rescuer[%d] min_effect must be >= 0 (use 0 for a directional rescuer — any clean-CI effect in the predicted direction rescues; use a positive value when you have prior evidence for a quantitative threshold)", i)
		}
	}
	return nil
}

func ValidateExperiment(e *entity.Experiment, cfg *store.Config) error {
	if strings.TrimSpace(e.GoalID) == "" {
		return errors.New("experiment must record goal_id provenance")
	}
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
	// RescuedBy names the goal rescuer instrument whose strict check saved
	// the verdict. Empty for the clean primary-wins path.
	RescuedBy string
	// ClauseChecks is the audit trail for any goal rescuers consulted. One
	// entry per rescuer evaluated, whether it passed, failed, or was skipped
	// for missing data. Empty when rescue was not consulted.
	ClauseChecks []entity.ClauseCheck
}

// StrictContext carries the extra inputs the firewall needs to consider
// goal-level rescuers on a failed primary check. It is optional: a
// zero-value StrictContext makes CheckStrictVerdictWithContext behave
// exactly like CheckStrictVerdict.
type StrictContext struct {
	Goal *entity.Goal
	// RescuerComparison returns a stats.Comparison for the given rescuer
	// instrument, computed against the same candidate/baseline pair used
	// for the primary check. Returns (nil, "<reason>") when no comparison
	// is possible (e.g. no observations on that instrument on either side).
	// The reason is recorded in the rescuer's ClauseCheck.
	RescuerComparison func(instrument string) (*stats.Comparison, string)
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
//
// This form takes no goal-level context and therefore never rescues. Callers
// that want goal-rescuer support should use CheckStrictVerdictWithContext.
func CheckStrictVerdict(requested string, h *entity.Hypothesis, c *stats.Comparison) VerdictDecision {
	return CheckStrictVerdictWithContext(requested, h, c, StrictContext{})
}

// CheckStrictVerdictWithContext is the rescuer-aware form of the strict
// firewall. If the primary "supported" check fails AND |delta_frac| is within
// goal.NeutralBandFrac AND ctx.Goal has rescuers AND ctx.RescuerComparison is
// non-nil, each rescuer runs its own strict check on the same sample pair.
// The first rescuer to pass rescues the verdict; d.RescuedBy names the
// winner and d.ClauseChecks audits every rescuer considered.
func CheckStrictVerdictWithContext(requested string, h *entity.Hypothesis, c *stats.Comparison, ctx StrictContext) VerdictDecision {
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
			// Primary failed. Try rescue before finalizing the downgrade.
			if rescued := tryRescue(h, c, ctx, d.Reasons); rescued != nil {
				return *rescued
			}
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

// tryRescue attempts to salvage a failed "supported" primary check by
// consulting goal-level rescuers. Returns a decision pointer when a rescuer
// passes; nil otherwise (caller should finalize the downgrade). The primary
// Reasons are preserved and annotated with the rescue citation so the audit
// trail tells the full story.
func tryRescue(_ *entity.Hypothesis, c *stats.Comparison, ctx StrictContext, primaryReasons []string) *VerdictDecision {
	if ctx.Goal == nil || len(ctx.Goal.Rescuers) == 0 || ctx.RescuerComparison == nil {
		return nil
	}
	if ctx.Goal.NeutralBandFrac <= 0 {
		return nil
	}
	if c == nil {
		return nil
	}
	if math.Abs(c.DeltaFrac) > ctx.Goal.NeutralBandFrac {
		// Primary moved outside the user-declared neutral band — this is a
		// real regression (or a real win that already failed some other gate).
		// Rescue only saves neutrals, not losses.
		return nil
	}

	var clauseChecks []entity.ClauseCheck
	var winner *entity.ClauseCheck
	for i := range ctx.Goal.Rescuers {
		r := ctx.Goal.Rescuers[i]
		clauseChecks = append(clauseChecks, evaluateRescuer(r, ctx))
		if winner == nil && clauseChecks[len(clauseChecks)-1].Passed {
			winner = &clauseChecks[len(clauseChecks)-1]
		}
	}
	if winner == nil {
		return nil
	}

	d := VerdictDecision{
		FinalVerdict: entity.VerdictSupported,
		Passed:       true,
		Downgraded:   false,
		RescuedBy:    winner.Instrument,
		ClauseChecks: clauseChecks,
	}
	// Preserve the primary reasons so readers see why rescue was needed.
	d.Reasons = append(d.Reasons, primaryReasons...)
	d.Reasons = append(d.Reasons, fmt.Sprintf("rescued by %s: |delta_frac| %.4f <= neutral_band_frac %.4f on primary, and rescuer met its own strict check",
		winner.Instrument, math.Abs(c.DeltaFrac), ctx.Goal.NeutralBandFrac))
	return &d
}

// evaluateRescuer runs the same strict-check logic as the primary path on
// a single rescuer clause, returning a ClauseCheck. Missing data or an
// unrecognised direction yields a non-passing entry with an explanatory
// reason so the audit trail is self-describing.
func evaluateRescuer(r entity.Rescuer, ctx StrictContext) entity.ClauseCheck {
	cc := entity.ClauseCheck{
		Role:       "rescuer",
		Instrument: r.Instrument,
		Direction:  r.Direction,
		MinEffect:  r.MinEffect,
	}
	cmp, missingReason := ctx.RescuerComparison(r.Instrument)
	if cmp == nil {
		if strings.TrimSpace(missingReason) == "" {
			missingReason = "no comparison available for rescuer"
		}
		cc.Reasons = []string{missingReason}
		return cc
	}

	effect := buildEffectFromComparison(r.Instrument, cmp)
	cc.Effect = &effect

	if cmp.NBaseline < 2 || cmp.NCandidate < 2 {
		cc.Reasons = append(cc.Reasons, "no comparison available (baseline or candidate has fewer than 2 samples)")
		return cc
	}
	switch r.Direction {
	case "decrease":
		if cmp.CIHighFrac >= 0 {
			cc.Reasons = append(cc.Reasons, fmt.Sprintf("95%% CI upper bound on delta_frac is %+.4f — crosses zero (not a clean decrease)", cmp.CIHighFrac))
		}
	case "increase":
		if cmp.CILowFrac <= 0 {
			cc.Reasons = append(cc.Reasons, fmt.Sprintf("95%% CI lower bound on delta_frac is %+.4f — crosses zero (not a clean increase)", cmp.CILowFrac))
		}
	default:
		cc.Reasons = append(cc.Reasons, fmt.Sprintf("unknown rescuer direction %q", r.Direction))
	}
	if math.Abs(cmp.DeltaFrac) < r.MinEffect {
		cc.Reasons = append(cc.Reasons, fmt.Sprintf("|delta_frac| %.4f < min_effect %.4f — rescuer effect too small", math.Abs(cmp.DeltaFrac), r.MinEffect))
	}
	cc.Passed = len(cc.Reasons) == 0
	return cc
}

// buildEffectFromComparison mirrors cli.buildEffect but stays inside the
// firewall package so the firewall doesn't take a dependency on cli.
func buildEffectFromComparison(instrument string, cmp *stats.Comparison) entity.Effect {
	return entity.Effect{
		Instrument: instrument,
		DeltaAbs:   cmp.DeltaAbs,
		DeltaFrac:  cmp.DeltaFrac,
		CILowAbs:   cmp.CILowAbs,
		CIHighAbs:  cmp.CIHighAbs,
		CILowFrac:  cmp.CILowFrac,
		CIHighFrac: cmp.CIHighFrac,
		PValue:     cmp.PValue,
		CIMethod:   cmp.CIMethod,
		NCandidate: cmp.NCandidate,
		NBaseline:  cmp.NBaseline,
	}
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

// InstrumentUsage enumerates where a named instrument is still referenced
// by goals, hypotheses, or observations. An empty usage means the
// instrument can be removed without orphaning anything.
type InstrumentUsage struct {
	// Goals whose objective.instrument == name. Deleting one of these
	// would leave the goal unmeasurable; this is never bypassable.
	GoalObjectives []string
	// Goals whose constraints[].instrument == name. Deleting is a
	// weakening of the goal's definition but does not strand it.
	GoalConstraints []string
	// Hypothesis IDs whose predicts.instrument == name. Deleting
	// invalidates their predictions.
	Hypotheses []string
	// Observation IDs recorded with instrument == name. Deleting
	// orphans the recorded data from the instrument definition.
	Observations []string
}

func (u InstrumentUsage) Ok() bool {
	return len(u.GoalObjectives) == 0 &&
		len(u.GoalConstraints) == 0 &&
		len(u.Hypotheses) == 0 &&
		len(u.Observations) == 0
}

// BlocksEvenWithForce is true when usage contains a reference that must
// never be bypassed — currently, a goal objective pointing at the
// instrument. Losing the objective instrument leaves the goal
// structurally broken, so `instrument delete --force` refuses too.
func (u InstrumentUsage) BlocksEvenWithForce() bool {
	return len(u.GoalObjectives) > 0
}

// Summary formats the usage as a human-readable, multi-line explanation
// suitable for an error message or event payload. Empty usage returns "".
func (u InstrumentUsage) Summary() string {
	if u.Ok() {
		return ""
	}
	var parts []string
	if len(u.GoalObjectives) > 0 {
		parts = append(parts, "goal objective: "+strings.Join(u.GoalObjectives, ", "))
	}
	if len(u.GoalConstraints) > 0 {
		parts = append(parts, "goal constraint: "+strings.Join(u.GoalConstraints, ", "))
	}
	if len(u.Hypotheses) > 0 {
		parts = append(parts, "hypothesis predictions: "+strings.Join(u.Hypotheses, ", "))
	}
	if len(u.Observations) > 0 {
		parts = append(parts, "observations: "+strings.Join(u.Observations, ", "))
	}
	return strings.Join(parts, "; ")
}

// CheckInstrumentSafeToDelete scans the supplied entities for references
// to the named instrument and returns the usage. Callers interpret the
// result: a non-empty Ok()==false usage should block deletion by default,
// but `--force` may accept usage.BlocksEvenWithForce()==false. Existence
// of the instrument itself is checked by the store (ErrInstrumentNotFound).
func CheckInstrumentSafeToDelete(name string, goals []*entity.Goal, hyps []*entity.Hypothesis, obs []*entity.Observation) InstrumentUsage {
	var u InstrumentUsage
	for _, g := range goals {
		if g == nil || g.Status != entity.GoalStatusActive {
			continue
		}
		if g.Objective.Instrument == name {
			u.GoalObjectives = append(u.GoalObjectives, g.ID)
		}
		for _, c := range g.Constraints {
			if c.Instrument == name {
				u.GoalConstraints = append(u.GoalConstraints, g.ID)
				break
			}
		}
	}
	for _, h := range hyps {
		if h == nil {
			continue
		}
		if h.Predicts.Instrument == name {
			u.Hypotheses = append(u.Hypotheses, h.ID)
		}
	}
	for _, o := range obs {
		if o == nil {
			continue
		}
		if o.Instrument == name {
			u.Observations = append(u.Observations, o.ID)
		}
	}
	return u
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
	if l.Scope == entity.LessonScopeSystem && len(l.Subjects) > 0 {
		return errors.New("lesson with scope=system cannot cite --from subjects; omit --scope to infer hypothesis, or drop --from for a free-floating system note")
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

// InspiredByLessonOptions controls exceptional --inspired-by validation paths.
type InspiredByLessonOptions struct {
	AllowInvalidated bool
}

func AssessLessonSourceChain(r inspiredByReviewReader, lesson *entity.Lesson) (string, error) {
	if lesson == nil {
		return "", errors.New("cannot assess source chain for a nil lesson")
	}
	if len(lesson.Subjects) == 0 {
		if lesson.Scope == entity.LessonScopeSystem {
			return entity.LessonSourceSystem, nil
		}
		return entity.LessonSourceInconclusive, nil
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

// CheckInspiredByLessonsReviewed blocks lessons that are not currently safe
// steering inputs. This closes the loophole where an orchestrator cites an
// unreviewed or invalidated lesson via --inspired-by instead of using the
// active lesson surface.
func CheckInspiredByLessonsReviewed(r inspiredByReviewReader, lessons []*entity.Lesson) error {
	return CheckInspiredByLessonsReviewedWithOptions(r, lessons, InspiredByLessonOptions{})
}

func CheckInspiredByLessonsReviewedWithOptions(r inspiredByReviewReader, lessons []*entity.Lesson, opts InspiredByLessonOptions) error {
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
		status := lesson.EffectiveStatus()
		if status == entity.LessonStatusInvalidated {
			if opts.AllowInvalidated {
				continue
			}
			return invalidatedInspiredByError(lesson.ID)
		}
		if status != entity.LessonStatusActive {
			return fmt.Errorf("lesson %s is %s — only active lessons may be used in --inspired-by", lesson.ID, status)
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
		case entity.LessonSourceInconclusive:
			if opts.AllowInvalidated {
				continue
			}
			return invalidatedInspiredByError(lesson.ID)
		default:
			return fmt.Errorf("lesson %s resolves to a non-decisive chain (%s) — only reviewed decisive lessons may be used in --inspired-by", lesson.ID, source)
		}
	}
	return nil
}

func invalidatedInspiredByError(id string) error {
	return fmt.Errorf("lesson %s is invalidated; use lesson list --status active for usable recommendations, or pass --allow-invalidated for retrospective use", id)
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

func CheckHypothesisInstrumentWithinGoal(goal *entity.Goal, h *entity.Hypothesis) error {
	if goal == nil {
		return errors.New("cannot validate hypothesis instrument without an active goal")
	}
	if h == nil {
		return errors.New("cannot validate a nil hypothesis")
	}

	predicted := strings.TrimSpace(h.Predicts.Instrument)
	if predicted == "" {
		return nil
	}

	allowed := allowedHypothesisInstruments(goal)
	for _, inst := range allowed {
		if inst == predicted {
			return nil
		}
	}

	allowedText := strings.Join(allowed, ", ")
	if allowedText == "" {
		allowedText = "(none)"
	}
	return fmt.Errorf("hypothesis predicts instrument %q, but the active goal only authorizes hypothesis instruments on %s; supporting instruments may still be observed on experiments, but new hypotheses must target the goal objective or an explicit constraint instrument. Start a new goal if you want to optimize %q instead",
		predicted, allowedText, predicted)
}

func allowedHypothesisInstruments(goal *entity.Goal) []string {
	if goal == nil {
		return nil
	}

	var out []string
	seen := map[string]struct{}{}
	add := func(inst string) {
		inst = strings.TrimSpace(inst)
		if inst == "" {
			return
		}
		if _, ok := seen[inst]; ok {
			return
		}
		seen[inst] = struct{}{}
		out = append(out, inst)
	}

	add(goal.Objective.Instrument)
	for _, c := range goal.Constraints {
		add(c.Instrument)
	}
	for _, r := range goal.Rescuers {
		add(r.Instrument)
	}
	return out
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
	if h.Predicts.MinEffect < 0 {
		return errors.New("predicts min_effect must be >= 0 (use 0 for a directional hypothesis — predicts direction only, with no minimum magnitude; use a positive value when you have prior evidence to ground a quantitative threshold)")
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
