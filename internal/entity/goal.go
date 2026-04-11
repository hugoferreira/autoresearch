package entity

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type Goal struct {
	SchemaVersion int          `yaml:"schema_version,omitempty" json:"schema_version,omitempty"`
	Objective     Objective    `yaml:"objective"                json:"objective"`
	Constraints   []Constraint `yaml:"constraints"              json:"constraints"`
	Body          string       `yaml:"-"                        json:"-"`
}

type Objective struct {
	Instrument   string  `yaml:"instrument"              json:"instrument"`
	Target       string  `yaml:"target,omitempty"        json:"target,omitempty"`
	Direction    string  `yaml:"direction"               json:"direction"`
	TargetEffect float64 `yaml:"target_effect,omitempty" json:"target_effect,omitempty"`
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
	var g Goal
	if err := yaml.Unmarshal(yb, &g); err != nil {
		return nil, fmt.Errorf("parse goal yaml: %w", err)
	}
	g.Body = string(body)
	if g.SchemaVersion == 0 {
		g.SchemaVersion = 1
	}
	return &g, nil
}

func (g *Goal) Marshal() ([]byte, error) {
	if g.SchemaVersion == 0 {
		g.SchemaVersion = 1
	}
	body := g.Body
	if body == "" {
		body = "# Steering\n\n_No steering notes yet._\n"
	}
	return WriteFrontmatter(g, body)
}

func (g *Goal) Steering() string {
	return ExtractSection(g.Body, "Steering")
}
