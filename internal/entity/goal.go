package entity

import (
	"bytes"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	GoalStatusActive    = "active"
	GoalStatusConcluded = "concluded"
	GoalStatusAbandoned = "abandoned"
	GoalSchemaVersion   = 4

	GoalOnThresholdAskHuman               = "ask_human"
	GoalOnThresholdStop                   = "stop"
	GoalOnThresholdContinueUntilStall     = "continue_until_stall"
	GoalOnThresholdContinueUntilBudgetCap = "continue_until_budget_cap"
)

type Goal struct {
	SchemaVersion   int          `yaml:"schema_version,omitempty"    json:"schema_version,omitempty"`
	ID              string       `yaml:"id,omitempty"                json:"id,omitempty"`
	Status          string       `yaml:"status,omitempty"            json:"status,omitempty"`
	DerivedFrom     string       `yaml:"derived_from,omitempty"      json:"derived_from,omitempty"`
	Trigger         string       `yaml:"trigger,omitempty"           json:"trigger,omitempty"`
	CreatedAt       *time.Time   `yaml:"created_at,omitempty"        json:"created_at,omitempty"`
	ClosedAt        *time.Time   `yaml:"closed_at,omitempty"         json:"closed_at,omitempty"`
	ClosureReason   string       `yaml:"closure_reason,omitempty"    json:"closure_reason,omitempty"`
	Objective       Objective    `yaml:"objective"                   json:"objective"`
	Completion      *Completion  `yaml:"completion,omitempty"        json:"completion,omitempty"`
	Constraints     []Constraint `yaml:"constraints"                 json:"constraints"`
	Rescuers        []Rescuer    `yaml:"rescuers,omitempty"          json:"rescuers,omitempty"`
	NeutralBandFrac float64      `yaml:"neutral_band_frac,omitempty" json:"neutral_band_frac,omitempty"`
	Body            string       `yaml:"-"                           json:"body,omitempty"`
}

// Rescuer is a secondary-objective clause on a Goal. When the primary
// objective's strict check would fail *only* because |delta_frac| is within
// goal.NeutralBandFrac (i.e. "didn't lose" on the primary), the firewall
// consults each rescuer's own strict check on the same candidate/baseline
// pair. If any rescuer passes, the verdict is kept as "supported" with
// strict.rescued_by naming the winning rescuer.
type Rescuer struct {
	Instrument string  `yaml:"instrument"          json:"instrument"`
	Direction  string  `yaml:"direction"           json:"direction"`
	MinEffect  float64 `yaml:"min_effect"          json:"min_effect"`
}

type Objective struct {
	Instrument string `yaml:"instrument"       json:"instrument"`
	Target     string `yaml:"target,omitempty" json:"target,omitempty"`
	Direction  string `yaml:"direction"        json:"direction"`
}

type Completion struct {
	Threshold   float64 `yaml:"threshold,omitempty"    json:"threshold,omitempty"`
	OnThreshold string  `yaml:"on_threshold,omitempty" json:"on_threshold,omitempty"`
}

type Constraint struct {
	Instrument string   `yaml:"instrument"       json:"instrument"`
	Max        *float64 `yaml:"max,omitempty"    json:"max,omitempty"`
	Min        *float64 `yaml:"min,omitempty"    json:"min,omitempty"`
	Require    string   `yaml:"require,omitempty" json:"require,omitempty"`
}

func ParseGoal(data []byte) (*Goal, error) {
	yb, body, err := ParseFrontmatter(data)
	if err != nil {
		return nil, err
	}

	type rawObjective struct {
		Instrument             string   `yaml:"instrument"`
		Target                 string   `yaml:"target,omitempty"`
		Direction              string   `yaml:"direction"`
		DeprecatedTargetEffect *float64 `yaml:"target_effect,omitempty"`
	}
	type rawGoal struct {
		SchemaVersion   int          `yaml:"schema_version,omitempty"`
		ID              string       `yaml:"id,omitempty"`
		Status          string       `yaml:"status,omitempty"`
		DerivedFrom     string       `yaml:"derived_from,omitempty"`
		Trigger         string       `yaml:"trigger,omitempty"`
		CreatedAt       *time.Time   `yaml:"created_at,omitempty"`
		ClosedAt        *time.Time   `yaml:"closed_at,omitempty"`
		ClosureReason   string       `yaml:"closure_reason,omitempty"`
		Objective       rawObjective `yaml:"objective"`
		Completion      *Completion  `yaml:"completion,omitempty"`
		Constraints     []Constraint `yaml:"constraints"`
		Rescuers        []Rescuer    `yaml:"rescuers,omitempty"`
		NeutralBandFrac float64      `yaml:"neutral_band_frac,omitempty"`
	}

	var raw rawGoal
	dec := yaml.NewDecoder(bytes.NewReader(yb))
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse goal yaml: %w", err)
	}
	if raw.Objective.DeprecatedTargetEffect != nil {
		if raw.Completion != nil {
			return nil, fmt.Errorf("goal mixes deprecated objective.target_effect with completion; use completion.threshold and completion.on_threshold only")
		}
		raw.Completion = &Completion{
			Threshold:   *raw.Objective.DeprecatedTargetEffect,
			OnThreshold: GoalOnThresholdAskHuman,
		}
	}
	if raw.Completion != nil && raw.Completion.Threshold > 0 && raw.Completion.OnThreshold == "" {
		raw.Completion.OnThreshold = GoalOnThresholdAskHuman
	}
	g := Goal{
		SchemaVersion: raw.SchemaVersion,
		ID:            raw.ID,
		Status:        raw.Status,
		DerivedFrom:   raw.DerivedFrom,
		Trigger:       raw.Trigger,
		CreatedAt:     raw.CreatedAt,
		ClosedAt:      raw.ClosedAt,
		ClosureReason: raw.ClosureReason,
		Objective: Objective{
			Instrument: raw.Objective.Instrument,
			Target:     raw.Objective.Target,
			Direction:  raw.Objective.Direction,
		},
		Completion:      raw.Completion,
		Constraints:     raw.Constraints,
		Rescuers:        raw.Rescuers,
		NeutralBandFrac: raw.NeutralBandFrac,
	}
	g.Body = string(body)
	if g.SchemaVersion == 0 {
		g.SchemaVersion = GoalSchemaVersion
	}
	return &g, nil
}

func (g *Goal) Marshal() ([]byte, error) {
	if g.SchemaVersion == 0 {
		g.SchemaVersion = GoalSchemaVersion
	}
	body := g.Body
	if body == "" {
		body = "# Steering\n\n_No steering notes yet._\n"
	}
	return WriteFrontmatter(g, body)
}

// FormatConstraint returns a compact human-readable string for a constraint:
// "size_flash ≤ 131072", "size_ram ≥ 1024", or "host_test require=pass".
func FormatConstraint(c Constraint) string {
	switch {
	case c.Max != nil:
		return fmt.Sprintf("%s ≤ %g", c.Instrument, *c.Max)
	case c.Min != nil:
		return fmt.Sprintf("%s ≥ %g", c.Instrument, *c.Min)
	case c.Require != "":
		return fmt.Sprintf("%s require=%s", c.Instrument, c.Require)
	default:
		return c.Instrument
	}
}

func (g *Goal) Steering() string {
	return ExtractSection(g.Body, "Steering")
}

func (g *Goal) HasCompletionThreshold() bool {
	return g != nil && g.Completion != nil && g.Completion.Threshold > 0
}

func (g *Goal) IsOpenEnded() bool {
	return !g.HasCompletionThreshold()
}

func (g *Goal) EffectiveOnThreshold() string {
	if g == nil {
		return GoalOnThresholdContinueUntilStall
	}
	if g.HasCompletionThreshold() {
		if g.Completion.OnThreshold != "" {
			return g.Completion.OnThreshold
		}
		return GoalOnThresholdAskHuman
	}
	return GoalOnThresholdContinueUntilStall
}
