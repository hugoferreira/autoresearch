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

type Experiment struct {
	ID string `yaml:"id"                                  json:"id"`
	// GoalID is durable goal provenance for the experiment. For baseline
	// experiments it is the only ownership link; for hypothesis-backed
	// experiments it denormalizes the parent hypothesis's GoalID so read
	// surfaces can scope experiments without replaying history or joining
	// through hypotheses on every path.
	GoalID      string    `yaml:"goal_id,omitempty"                   json:"goal_id,omitempty"`
	Hypothesis  string    `yaml:"hypothesis"                          json:"hypothesis"`
	IsBaseline  bool      `yaml:"is_baseline,omitempty"               json:"is_baseline,omitempty"`
	Status      string    `yaml:"status"                              json:"status"`
	Baseline    Baseline  `yaml:"baseline"                            json:"baseline"`
	Instruments []string  `yaml:"instruments"                         json:"instruments"`
	Worktree    string    `yaml:"worktree,omitempty"                  json:"worktree,omitempty"`
	Branch      string    `yaml:"branch,omitempty"                    json:"branch,omitempty"`
	Attempt     int       `yaml:"attempt,omitempty"                   json:"attempt,omitempty"`
	Budget      Budget    `yaml:"budget,omitempty"                    json:"budget,omitempty"`
	Author      string    `yaml:"author"                              json:"author"`
	CreatedAt   time.Time `yaml:"created_at"                          json:"created_at"`
	// ReferencedAsBaselineBy lists conclusion IDs that used this experiment
	// as a baseline. Populated by `conclude` when it writes a new
	// conclusion. Non-empty → this experiment has finished its job as a
	// comparator and should drop out of the dashboard's "in flight" panel
	// regardless of its own status field. This keeps `status` honest (it
	// still means "was *this* experiment analyzed?") while letting the
	// dashboard surface only experiments the loop can still act on.
	ReferencedAsBaselineBy []string `yaml:"referenced_as_baseline_by,omitempty" json:"referenced_as_baseline_by,omitempty"`
	Body                   string   `yaml:"-"                                   json:"body,omitempty"`
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
