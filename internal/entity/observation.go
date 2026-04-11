package entity

import (
	"encoding/json"
	"fmt"
	"time"
)

// Artifact is a reference to a raw output file stored content-addressed under
// .research/artifacts/. An instrument may emit multiple named artifacts per
// observation (e.g. a disassembler emitting disasm, symbols, sections).
type Artifact struct {
	Name  string `json:"name"`
	SHA   string `json:"sha"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Mime  string `json:"mime,omitempty"`
}

type Observation struct {
	ID         string    `json:"id"`
	Experiment string    `json:"experiment"`
	Instrument string    `json:"instrument"`
	MeasuredAt time.Time `json:"measured_at"`
	Value      float64   `json:"value"`
	Unit       string    `json:"unit"`
	Samples    int       `json:"samples"`
	PerSample  []float64 `json:"per_sample,omitempty"`
	CILow      *float64  `json:"ci_low,omitempty"`
	CIHigh     *float64  `json:"ci_high,omitempty"`
	CIMethod   string    `json:"ci_method,omitempty"`
	Pass       *bool     `json:"pass,omitempty"`

	Artifacts []Artifact `json:"artifacts"`

	// Legacy convenience pointers to the primary (first) artifact. Kept in
	// sync by Normalize; older observations without the Artifacts list are
	// reconstructed from these fields on parse.
	RawArtifact string `json:"raw_artifact,omitempty"`
	RawSHA      string `json:"raw_sha,omitempty"`

	Command     string         `json:"command"`
	ExitCode    int            `json:"exit_code"`
	Worktree    string         `json:"worktree,omitempty"`
	BaselineSHA string         `json:"baseline_sha,omitempty"`
	Author      string         `json:"author"`
	Aux         map[string]any `json:"aux,omitempty"`
}

func ParseObservation(data []byte) (*Observation, error) {
	var o Observation
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, fmt.Errorf("parse observation: %w", err)
	}
	o.Normalize()
	return &o, nil
}

func (o *Observation) Marshal() ([]byte, error) {
	o.Normalize()
	return json.MarshalIndent(o, "", "  ")
}

// Normalize keeps the Artifacts list and the legacy raw_sha/raw_artifact
// fields consistent. If only the legacy fields are populated (pre-multi-
// artifact observations), a single-element Artifacts list is reconstructed.
// If Artifacts is populated, the legacy fields are overwritten from the
// primary (index 0) artifact.
func (o *Observation) Normalize() {
	if len(o.Artifacts) == 0 && o.RawArtifact != "" {
		o.Artifacts = []Artifact{{
			Name: "primary",
			SHA:  o.RawSHA,
			Path: o.RawArtifact,
		}}
	}
	if len(o.Artifacts) > 0 {
		o.RawArtifact = o.Artifacts[0].Path
		o.RawSHA = o.Artifacts[0].SHA
	}
}

// Primary returns a pointer to the first artifact, or nil if none.
func (o *Observation) Primary() *Artifact {
	if len(o.Artifacts) == 0 {
		return nil
	}
	return &o.Artifacts[0]
}
