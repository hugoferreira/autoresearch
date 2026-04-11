package entity

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	ExpDesigned    = "designed"
	ExpImplemented = "implemented"
	ExpMeasured    = "measured"
	ExpAnalyzed    = "analyzed"
	ExpFailed      = "failed"
)

const (
	TierHost     = "host"
	TierQemu     = "qemu"
	TierHardware = "hardware"
)

type Experiment struct {
	ID          string    `yaml:"id"                    json:"id"`
	Hypothesis  string    `yaml:"hypothesis"            json:"hypothesis"`
	Status      string    `yaml:"status"                json:"status"`
	Tier        string    `yaml:"tier"                  json:"tier"`
	Baseline    Baseline  `yaml:"baseline"              json:"baseline"`
	Instruments []string  `yaml:"instruments"           json:"instruments"`
	Worktree    string    `yaml:"worktree,omitempty"    json:"worktree,omitempty"`
	Branch      string    `yaml:"branch,omitempty"      json:"branch,omitempty"`
	Budget      Budget    `yaml:"budget,omitempty"      json:"budget,omitempty"`
	Author      string    `yaml:"author"                json:"author"`
	CreatedAt   time.Time `yaml:"created_at"            json:"created_at"`
	Body        string    `yaml:"-"                     json:"-"`
}

type Baseline struct {
	Ref        string `yaml:"ref"                  json:"ref"`
	SHA        string `yaml:"sha,omitempty"        json:"sha,omitempty"`
	Experiment string `yaml:"experiment,omitempty" json:"experiment,omitempty"`
}

type Budget struct {
	WallTimeS  int `yaml:"wall_time_s,omitempty" json:"wall_time_s,omitempty"`
	MaxSamples int `yaml:"max_samples,omitempty" json:"max_samples,omitempty"`
}

func ParseExperiment(data []byte) (*Experiment, error) {
	yb, body, err := ParseFrontmatter(data)
	if err != nil {
		return nil, err
	}
	var e Experiment
	if err := yaml.Unmarshal(yb, &e); err != nil {
		return nil, fmt.Errorf("parse experiment yaml: %w", err)
	}
	e.Body = string(body)
	return &e, nil
}

func (e *Experiment) Marshal() ([]byte, error) {
	body := e.Body
	if body == "" {
		body = "# Plan\n\n_No plan yet._\n"
	}
	return WriteFrontmatter(e, body)
}
