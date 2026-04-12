package entity

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	StatusOpen         = "open"
	StatusSupported    = "supported"
	StatusRefuted      = "refuted"
	StatusInconclusive = "inconclusive"
	StatusKilled       = "killed"
)

type Hypothesis struct {
	ID        string    `yaml:"id"                 json:"id"`
	Parent    string    `yaml:"parent,omitempty"   json:"parent,omitempty"`
	Claim     string    `yaml:"claim"              json:"claim"`
	Predicts  Predicts  `yaml:"predicts"           json:"predicts"`
	KillIf    []string  `yaml:"kill_if"            json:"kill_if"`
	Status    string    `yaml:"status"             json:"status"`
	Priority  string    `yaml:"priority,omitempty" json:"priority,omitempty"`
	Author    string    `yaml:"author"             json:"author"`
	CreatedAt time.Time `yaml:"created_at"         json:"created_at"`
	Tags      []string  `yaml:"tags,omitempty"     json:"tags,omitempty"`
	Body      string    `yaml:"-"                  json:"body,omitempty"`
}

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
