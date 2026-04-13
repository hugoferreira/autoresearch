package entity

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	VerdictSupported    = "supported"
	VerdictRefuted      = "refuted"
	VerdictInconclusive = "inconclusive"
)

type Conclusion struct {
	ID           string    `yaml:"id"                            json:"id"`
	Hypothesis   string    `yaml:"hypothesis"                    json:"hypothesis"`
	Verdict      string    `yaml:"verdict"                       json:"verdict"`
	Observations []string  `yaml:"observations"                  json:"observations"`
	CandidateExp string    `yaml:"candidate_experiment,omitempty" json:"candidate_experiment,omitempty"`
	BaselineExp  string    `yaml:"baseline_experiment,omitempty"  json:"baseline_experiment,omitempty"`
	Effect       Effect    `yaml:"effect"                        json:"effect"`
	// IncrementalExp is the frontier-best experiment at the time this
	// conclusion was written. Together with IncrementalEffect it answers
	// "how much did this improve over the current best?" as opposed to
	// the absolute baseline which answers "how much did this improve
	// over the original unoptimized code?"
	IncrementalExp    string  `yaml:"incremental_experiment,omitempty" json:"incremental_experiment,omitempty"`
	IncrementalEffect *Effect `yaml:"incremental_effect,omitempty"     json:"incremental_effect,omitempty"`
	StatTest          string  `yaml:"stat_test"                     json:"stat_test"`
	Strict            Strict  `yaml:"strict_check"                  json:"strict_check"`
	Author            string  `yaml:"author"                        json:"author"`
	ReviewedBy        string  `yaml:"reviewed_by,omitempty"         json:"reviewed_by,omitempty"`
	CreatedAt         time.Time `yaml:"created_at"                    json:"created_at"`
	Body              string    `yaml:"-"                             json:"body,omitempty"`
}

type Effect struct {
	Instrument string  `yaml:"instrument"    json:"instrument"`
	DeltaAbs   float64 `yaml:"delta_abs"     json:"delta_abs"`
	DeltaFrac  float64 `yaml:"delta_frac"    json:"delta_frac"`
	CILowAbs   float64 `yaml:"ci_low_abs"    json:"ci_low_abs"`
	CIHighAbs  float64 `yaml:"ci_high_abs"   json:"ci_high_abs"`
	CILowFrac  float64 `yaml:"ci_low_frac"   json:"ci_low_frac"`
	CIHighFrac float64 `yaml:"ci_high_frac"  json:"ci_high_frac"`
	PValue     float64 `yaml:"p_value"       json:"p_value"`
	CIMethod   string  `yaml:"ci_method"     json:"ci_method"`
	NCandidate int     `yaml:"n_candidate"   json:"n_candidate"`
	NBaseline  int     `yaml:"n_baseline"    json:"n_baseline"`
}

type Strict struct {
	Passed        bool     `yaml:"passed"                    json:"passed"`
	RequestedFrom string   `yaml:"downgraded_from,omitempty" json:"downgraded_from,omitempty"`
	Reasons       []string `yaml:"reasons,omitempty"         json:"reasons,omitempty"`
}

func ParseConclusion(data []byte) (*Conclusion, error) {
	yb, body, err := ParseFrontmatter(data)
	if err != nil {
		return nil, err
	}
	var c Conclusion
	if err := yaml.Unmarshal(yb, &c); err != nil {
		return nil, fmt.Errorf("parse conclusion yaml: %w", err)
	}
	c.Body = string(body)
	return &c, nil
}

func (c *Conclusion) Marshal() ([]byte, error) {
	body := c.Body
	if body == "" {
		body = "# Interpretation\n\n_No interpretation provided._\n"
	}
	return WriteFrontmatter(c, body)
}
