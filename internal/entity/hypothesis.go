package entity

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	StatusOpen         = "open"
	StatusUnreviewed   = "unreviewed" // concluded but awaiting gate review
	StatusSupported    = "supported"
	StatusRefuted      = "refuted"
	StatusInconclusive = "inconclusive"
	StatusKilled       = "killed"
)

type Hypothesis struct {
	ID                      string    `yaml:"id"                    json:"id"`
	GoalID                  string    `yaml:"goal_id,omitempty"     json:"goal_id,omitempty"`
	Parent                  string    `yaml:"parent,omitempty"      json:"parent,omitempty"`
	Claim                   string    `yaml:"claim"                 json:"claim"`
	Predicts                Predicts  `yaml:"predicts"              json:"predicts"`
	KillIf                  []string  `yaml:"kill_if"               json:"kill_if"`
	InspiredBy              []string  `yaml:"inspired_by,omitempty" json:"inspired_by,omitempty"`
	AllowInvalidatedLessons bool      `yaml:"allow_invalidated_lessons,omitempty" json:"allow_invalidated_lessons,omitempty"`
	Status                  string    `yaml:"status"                json:"status"`
	Priority                string    `yaml:"priority,omitempty"    json:"priority,omitempty"`
	Author                  string    `yaml:"author"                json:"author"`
	CreatedAt               time.Time `yaml:"created_at"            json:"created_at"`
	Tags                    []string  `yaml:"tags,omitempty"        json:"tags,omitempty"`
	Body                    string    `yaml:"-"                     json:"body,omitempty"`
}

// Predicts is what a hypothesis claims will happen to one instrument.
// A positive MinEffect commits to a magnitude: "supported" requires the
// 95% CI to be clean on the predicted side AND |delta_frac| >= MinEffect.
// A zero MinEffect marks the hypothesis as *directional*: the agent
// claims only that the instrument will move in Direction, with no
// magnitude commitment. The CI-clean-side check still runs — a neutral
// result still downgrades — but any clean-CI effect in the predicted
// direction is "supported." Use directional when no prior evidence
// (lesson, literature, back-of-envelope calc) grounds a specific
// threshold; follow up with a quantitative hypothesis once the first
// measurement gives you one.
type Predicts struct {
	Instrument string  `yaml:"instrument" json:"instrument"`
	Target     string  `yaml:"target"     json:"target"`
	Direction  string  `yaml:"direction"  json:"direction"`
	MinEffect  float64 `yaml:"min_effect" json:"min_effect"`
}

func ParseHypothesis(data []byte) (*Hypothesis, error) {
	yb, body, err := ParseFrontmatter(data)
	if err != nil {
		return nil, err
	}
	var h Hypothesis
	if err := yaml.Unmarshal(yb, &h); err != nil {
		return nil, fmt.Errorf("parse hypothesis yaml: %w", err)
	}
	h.Body = string(body)
	return &h, nil
}

func (h *Hypothesis) Marshal() ([]byte, error) {
	body := h.Body
	if body == "" {
		body = "# Notes\n\n_No notes._\n"
	}
	return WriteFrontmatter(h, body)
}
