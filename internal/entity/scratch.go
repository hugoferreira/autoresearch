package entity

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	ScratchStatusActive  = "active"
	ScratchStatusCleaned = "cleaned"
)

type Scratch struct {
	ID            string     `yaml:"id"                       json:"id"`
	Name          string     `yaml:"name"                     json:"name"`
	Status        string     `yaml:"status"                   json:"status"`
	FromRef       string     `yaml:"from_ref"                 json:"from_ref"`
	FromSHA       string     `yaml:"from_sha"                 json:"from_sha"`
	Worktree      string     `yaml:"worktree"                 json:"worktree"`
	Branch        string     `yaml:"branch"                   json:"branch"`
	Author        string     `yaml:"author"                   json:"author"`
	CreatedAt     time.Time  `yaml:"created_at"               json:"created_at"`
	CleanedAt     *time.Time `yaml:"cleaned_at,omitempty"     json:"cleaned_at,omitempty"`
	CleanupReason string     `yaml:"cleanup_reason,omitempty" json:"cleanup_reason,omitempty"`
	Body          string     `yaml:"-"                        json:"body,omitempty"`
}

func ParseScratch(data []byte) (*Scratch, error) {
	yb, body, err := ParseFrontmatter(data)
	if err != nil {
		return nil, err
	}
	var s Scratch
	if err := yaml.Unmarshal(yb, &s); err != nil {
		return nil, fmt.Errorf("parse scratch yaml: %w", err)
	}
	s.Body = string(body)
	return &s, nil
}

func (s *Scratch) Marshal() ([]byte, error) {
	body := s.Body
	if body == "" {
		body = "# Scratch notes\n\nTemporary premise-check workspace. Do not cite this as conclusion evidence unless results are captured through normal observations or artifacts.\n"
	}
	return WriteFrontmatter(s, body)
}

func (s *Scratch) EffectiveStatus() string {
	if s == nil || s.Status == "" {
		return ScratchStatusActive
	}
	return s.Status
}
