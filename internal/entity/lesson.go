package entity

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Lesson is a distilled, supersedable claim the research loop has learned.
// Unlike Hypothesis (a proposal to test), Experiment (one attempt), or
// Conclusion (a verdict on one attempt), a Lesson sits *above* the per-cycle
// artifacts. It captures what should inform the next cycle.
//
// Two scopes:
//
//   - "hypothesis" — tied to one or more H-/E-/C- ids the lesson was
//     extracted from. Example: "unroll past 8x shows no win on FIR_NTAPS=32
//     — cache line pressure dominates."
//   - "system" — free-floating incidental findings about the target codebase
//     or the research apparatus itself. Example: "the test harness caches
//     stale fixtures across runs; always make clean before observing."
type Lesson struct {
	ID             string    `yaml:"id"                      json:"id"`
	Claim          string    `yaml:"claim"                   json:"claim"`
	Scope          string    `yaml:"scope"                   json:"scope"`
	Subjects       []string  `yaml:"subjects,omitempty"      json:"subjects,omitempty"`
	Tags           []string  `yaml:"tags,omitempty"          json:"tags,omitempty"`
	Status         string    `yaml:"status"                  json:"status"`
	SupersedesID   string    `yaml:"supersedes,omitempty"    json:"supersedes,omitempty"`
	SupersededByID string    `yaml:"superseded_by,omitempty" json:"superseded_by,omitempty"`
	Author         string    `yaml:"author"                  json:"author"`
	CreatedAt      time.Time `yaml:"created_at"              json:"created_at"`
	Body           string    `yaml:"-"                       json:"body,omitempty"`
}

const (
	LessonScopeHypothesis  = "hypothesis"
	LessonScopeSystem      = "system"
	LessonStatusActive     = "active"
	LessonStatusSuperseded = "superseded"
)

func ParseLesson(data []byte) (*Lesson, error) {
	yb, body, err := ParseFrontmatter(data)
	if err != nil {
		return nil, err
	}
	var l Lesson
	if err := yaml.Unmarshal(yb, &l); err != nil {
		return nil, fmt.Errorf("parse lesson yaml: %w", err)
	}
	l.Body = string(body)
	return &l, nil
}

func (l *Lesson) Marshal() ([]byte, error) {
	body := l.Body
	if body == "" {
		// Honest placeholder: do NOT silently echo the claim as if the
		// body were populated. A human reviewing a lesson file with this
		// placeholder can see immediately that the agent skipped the
		// --body flag and the lesson is underspecified.
		body = "_No body provided — pass `--body` with Evidence, Mechanism, " +
			"Scope, and For-the-next-generator sections. See " +
			"`.claude/agents/research-orchestrator.md`._\n"
	}
	return WriteFrontmatter(l, body)
}
