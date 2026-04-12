package store

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	SchemaVersion int                   `yaml:"schema_version"`
	Build         CommandSpec           `yaml:"build"`
	Test          CommandSpec           `yaml:"test"`
	Worktrees     WorktreesConfig       `yaml:"worktrees"`
	Instruments   map[string]Instrument `yaml:"instruments,omitempty"`
	Budgets       Budgets               `yaml:"budgets,omitempty"`
	Mode          string                `yaml:"mode,omitempty"`
}

type WorktreesConfig struct {
	// Root is the absolute directory under which experiment worktrees live.
	// By default this is placed under the user cache directory so that naive
	// grep/find from within the project cannot accidentally descend into the
	// duplicate source trees of in-flight experiments. Humans can edit this
	// field in config.yaml to relocate (e.g. to a fast SSD).
	Root string `yaml:"root"`
}

type CommandSpec struct {
	Command string `yaml:"command"`
	WorkDir string `yaml:"workdir,omitempty"`
}

type Instrument struct {
	Cmd        []string `yaml:"cmd"`
	Parser     string   `yaml:"parser"`
	// Pattern is the extraction regex used by parsers that pull a scalar out
	// of command stdout (currently builtin:scalar). It MUST contain exactly
	// one capture group producing a base-10 integer. Ignored by other parsers.
	Pattern    string   `yaml:"pattern,omitempty"`
	Unit       string   `yaml:"unit"`
	MinSamples int      `yaml:"min_samples,omitempty"`
	// Requires lists instrument prerequisites as "instrument=condition" pairs.
	// Before running this instrument, the observe command checks that each
	// prerequisite has a passing observation on the same experiment.
	// v1 condition: "pass" — the prerequisite must have pass=true.
	Requires   []string `yaml:"requires,omitempty"`
}

type Budgets struct {
	MaxExperiments int `yaml:"max_experiments,omitempty"`
	MaxWallTimeH   int `yaml:"max_wall_time_h,omitempty"`
	FrontierStallK int `yaml:"frontier_stall_k,omitempty"`
}

func (s *Store) Config() (*Config, error) {
	data, err := os.ReadFile(s.ConfigPath())
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// UpdateConfig reads, mutates via fn, and writes config.yaml back.
// Single-writer semantics same as state.
func (s *Store) UpdateConfig(fn func(*Config) error) error {
	cfg, err := s.Config()
	if err != nil {
		return err
	}
	if err := fn(cfg); err != nil {
		return err
	}
	return s.writeConfig(*cfg)
}

func (s *Store) writeConfig(cfg Config) error {
	if cfg.SchemaVersion == 0 {
		cfg.SchemaVersion = 1
	}
	if cfg.Mode == "" {
		cfg.Mode = "strict"
	}
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return atomicWrite(s.ConfigPath(), data)
}
