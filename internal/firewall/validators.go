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
	if strings.TrimSpace(e.Hypothesis) == "" {
		return errors.New("experiment must reference a hypothesis")
	}
	switch e.Tier {
	case entity.TierHost, entity.TierQemu, entity.TierHardware:
	default:
		return fmt.Errorf("tier must be host, qemu, or hardware; got %q", e.Tier)
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
	Rule      string // "max_experiments" | "max_wall_time_h" | ""
	Limit     any
	Observed  any
	Message   string
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

// CheckTierGate enforces the host-first rule: qemu-tier experiments require at
// least one prior host-tier experiment on the same hypothesis, and hardware
// experiments are always human-gated. Callers pass priorHostExperiments=true
// when such a predecessor exists.
func CheckTierGate(tier string, priorHostExperiments bool, force bool) error {
	switch tier {
	case entity.TierHost:
		return nil
	case entity.TierQemu:
		if priorHostExperiments || force {
			return nil
		}
		return errors.New("qemu-tier experiments require a prior host-tier experiment on the same hypothesis (pass --force to override)")
	case entity.TierHardware:
		if !force {
			return errors.New("hardware-tier experiments require explicit --force and a human approver")
		}
		return nil
	default:
		return fmt.Errorf("unknown tier %q", tier)
	}
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
