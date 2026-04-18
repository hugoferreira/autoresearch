// Package instrument executes registered instruments against an experiment's
// worktree, returning a normalized result ready to become an Observation.
//
// Four built-in parsers are supported:
//
//	builtin:passfail — run cmd once; value=1 if exit==0 else 0, unit="pass".
//	                   Used for host_compile, host_test, and similar binary
//	                   outcomes.
//	builtin:timing   — run cmd N times; value=mean seconds, per_sample + BCa
//	                   95% bootstrap CI (via internal/stats).
//	builtin:size     — run cmd once; first numeric column is the "text" size
//	                   in bytes; all header columns land in aux. Tolerant of
//	                   both GNU `size` (text/data/bss) and Mach-O
//	                   `size` (__TEXT/__DATA/...) output formats.
//	builtin:scalar   — run cmd N times; extract a single integer from each
//	                   stdout using a user-declared regex (instrument.Pattern,
//	                   set via `--pattern` at `instrument register` time).
//	                   The regex MUST have exactly one capture group. Returns
//	                   per_sample + BCa 95% CI. The unit is whatever the user
//	                   declared when registering — cycles, instructions,
//	                   page_faults, bytes, whatever the command prints.
//	                   Intended for any tool whose output contains a single
//	                   scalar: qemu semihosting, perf stat, objdump line
//	                   counts, cachegrind, etc. Nothing in the parser knows
//	                   about any specific tool.
package instrument

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/stats"
	"github.com/bytter/autoresearch/internal/store"
)

type Config struct {
	ProjectDir  string
	WorktreeDir string
	Name        string
	Instrument  store.Instrument
	Samples     int // 0 => use instrument.MinSamples; falls back to parser default
}

// ArtifactContent is an in-memory artifact produced by a parser, before the
// store has hashed and written it. Instruments may emit multiple artifacts per
// run (e.g. a disassembler producing disasm + symbols + sections).
type ArtifactContent struct {
	Name     string // logical name, e.g. "disasm"
	Filename string // on-disk filename under the content-addressed bucket
	Content  []byte
	Mime     string
}

type Result struct {
	Artifacts        []ArtifactContent
	EvidenceFailures []entity.EvidenceFailure

	Command    string
	ExitCode   int
	StartedAt  time.Time
	FinishedAt time.Time

	Value     float64
	Unit      string
	SamplesN  int
	PerSample []float64
	CILow     *float64
	CIHigh    *float64
	CIMethod  string
	Pass      *bool
	Aux       map[string]any
}

func Run(ctx context.Context, cfg Config) (*Result, error) {
	if len(cfg.Instrument.Cmd) == 0 {
		return nil, errors.New("instrument has no cmd")
	}
	var (
		res *Result
		err error
	)
	switch cfg.Instrument.Parser {
	case "builtin:passfail":
		res, err = runPassFail(ctx, cfg)
	case "builtin:timing":
		res, err = runTiming(ctx, cfg)
	case "builtin:size":
		res, err = runSize(ctx, cfg)
	case "builtin:scalar":
		res, err = runScalar(ctx, cfg)
	default:
		return nil, fmt.Errorf("unknown parser %q (available: builtin:passfail, builtin:timing, builtin:size, builtin:scalar)", cfg.Instrument.Parser)
	}
	if err != nil {
		return nil, err
	}
	return runEvidence(ctx, cfg, res)
}

type execOutcome struct {
	Command  string
	Stdout   []byte
	ExitCode int
	Elapsed  time.Duration
	Start    time.Time
}

func execOnce(ctx context.Context, cfg Config, argv []string) (*execOutcome, error) {
	if len(argv) == 0 {
		return nil, errors.New("empty argv")
	}
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir = cfg.WorktreeDir
	c.Env = append(os.Environ(),
		"WORKTREE="+cfg.WorktreeDir,
		"PROJECT="+cfg.ProjectDir,
	)
	start := time.Now()
	out, runErr := c.CombinedOutput()
	elapsed := time.Since(start)
	exitCode := 0
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			exitCode = ee.ExitCode()
		} else {
			return nil, fmt.Errorf("spawn %q: %w", strings.Join(argv, " "), runErr)
		}
	}
	return &execOutcome{
		Command:  strings.Join(argv, " "),
		Stdout:   out,
		ExitCode: exitCode,
		Elapsed:  elapsed,
		Start:    start,
	}, nil
}

func execShell(ctx context.Context, cfg Config, script string) (*execOutcome, error) {
	return execOnce(ctx, cfg, []string{"sh", "-c", script})
}

func runEvidence(ctx context.Context, cfg Config, res *Result) (*Result, error) {
	if len(cfg.Instrument.Evidence) == 0 {
		return res, nil
	}
	for _, spec := range cfg.Instrument.Evidence {
		o, err := execShell(ctx, cfg, spec.Cmd)
		if err != nil {
			res.EvidenceFailures = append(res.EvidenceFailures, entity.EvidenceFailure{
				Name:  spec.Name,
				Error: err.Error(),
			})
			continue
		}
		if o.ExitCode != 0 {
			res.EvidenceFailures = append(res.EvidenceFailures, entity.EvidenceFailure{
				Name:     spec.Name,
				ExitCode: o.ExitCode,
			})
			continue
		}
		res.Artifacts = append(res.Artifacts, ArtifactContent{
			Name:     "evidence/" + spec.Name,
			Filename: spec.Name + ".txt",
			Content:  o.Stdout,
			Mime:     "text/plain",
		})
	}
	res.FinishedAt = time.Now()
	return res, nil
}

func runPassFail(ctx context.Context, cfg Config) (*Result, error) {
	o, err := execOnce(ctx, cfg, cfg.Instrument.Cmd)
	if err != nil {
		return nil, err
	}
	pass := o.ExitCode == 0
	val := 0.0
	if pass {
		val = 1.0
	}
	return &Result{
		Artifacts: []ArtifactContent{
			{Name: "stdout", Filename: "stdout.txt", Content: o.Stdout, Mime: "text/plain"},
		},
		EvidenceFailures: nil,
		Command:          o.Command,
		ExitCode:         o.ExitCode,
		StartedAt:        o.Start,
		FinishedAt:       o.Start.Add(o.Elapsed),
		Value:            val,
		Unit:             "pass",
		SamplesN:         1,
		Pass:             &pass,
		Aux:              map[string]any{"elapsed_s": o.Elapsed.Seconds()},
	}, nil
}

func runTiming(ctx context.Context, cfg Config) (*Result, error) {
	samples := cfg.Samples
	if samples <= 0 {
		samples = cfg.Instrument.MinSamples
	}
	if samples <= 0 {
		samples = 5
	}

	type sampleRec struct {
		Run      int     `json:"run"`
		ExitCode int     `json:"exit_code"`
		ElapsedS float64 `json:"elapsed_s"`
	}

	per := make([]float64, 0, samples)
	recs := make([]sampleRec, 0, samples)

	started := time.Now()
	for i := 1; i <= samples; i++ {
		o, err := execOnce(ctx, cfg, cfg.Instrument.Cmd)
		if err != nil {
			return nil, fmt.Errorf("sample %d: %w", i, err)
		}
		if o.ExitCode != 0 {
			return nil, fmt.Errorf("sample %d: command %q exited %d\n%s", i, o.Command, o.ExitCode, strings.TrimSpace(string(o.Stdout)))
		}
		per = append(per, o.Elapsed.Seconds())
		recs = append(recs, sampleRec{Run: i, ExitCode: o.ExitCode, ElapsedS: o.Elapsed.Seconds()})
	}
	finished := time.Now()

	summary := stats.Summarize(per, stats.DefaultIterations, 0)
	mean, low, high := summary.Mean, summary.CILow, summary.CIHigh

	rawDoc := map[string]any{
		"command":  strings.Join(cfg.Instrument.Cmd, " "),
		"worktree": cfg.WorktreeDir,
		"samples":  recs,
	}
	raw, _ := json.MarshalIndent(rawDoc, "", "  ")

	return &Result{
		Artifacts: []ArtifactContent{
			{Name: "timing", Filename: "timing.json", Content: raw, Mime: "application/json"},
		},
		EvidenceFailures: nil,
		Command:          strings.Join(cfg.Instrument.Cmd, " "),
		ExitCode:         0,
		StartedAt:        started,
		FinishedAt:       finished,
		Value:            mean,
		Unit:             "seconds",
		SamplesN:         samples,
		PerSample:        per,
		CILow:            &low,
		CIHigh:           &high,
		CIMethod:         "bootstrap_bca_95",
	}, nil
}

func runSize(ctx context.Context, cfg Config) (*Result, error) {
	o, err := execOnce(ctx, cfg, cfg.Instrument.Cmd)
	if err != nil {
		return nil, err
	}
	if o.ExitCode != 0 {
		return nil, fmt.Errorf("size command %q exited %d\n%s", o.Command, o.ExitCode, strings.TrimSpace(string(o.Stdout)))
	}
	text, aux, err := parseSize(o.Stdout)
	if err != nil {
		return nil, err
	}
	return &Result{
		Artifacts: []ArtifactContent{
			{Name: "size", Filename: "size.txt", Content: o.Stdout, Mime: "text/plain"},
		},
		EvidenceFailures: nil,
		Command:          o.Command,
		ExitCode:         o.ExitCode,
		StartedAt:        o.Start,
		FinishedAt:       o.Start.Add(o.Elapsed),
		Value:            float64(text),
		Unit:             "bytes",
		SamplesN:         1,
		Aux:              aux,
	}, nil
}

// runScalar executes the configured command N times and extracts a single
// integer from each stdout using the user-declared regex in
// cfg.Instrument.Pattern. Aggregation is a BCa 95% bootstrap over the
// per-sample values. The unit is whatever the user chose at instrument
// registration — the parser is oblivious.
func runScalar(ctx context.Context, cfg Config) (*Result, error) {
	if strings.TrimSpace(cfg.Instrument.Pattern) == "" {
		return nil, errors.New("builtin:scalar requires instrument.pattern (a regex with one capture group); set it via `instrument register --pattern`")
	}
	re, err := regexp.Compile(cfg.Instrument.Pattern)
	if err != nil {
		return nil, fmt.Errorf("compile pattern %q: %w", cfg.Instrument.Pattern, err)
	}
	if re.NumSubexp() != 1 {
		return nil, fmt.Errorf("pattern %q must have exactly one capture group, got %d", cfg.Instrument.Pattern, re.NumSubexp())
	}

	samples := cfg.Samples
	if samples <= 0 {
		samples = cfg.Instrument.MinSamples
	}
	if samples <= 0 {
		samples = 3
	}

	type sampleRec struct {
		Run      int    `json:"run"`
		ExitCode int    `json:"exit_code"`
		Value    int64  `json:"value"`
		Stdout   string `json:"stdout"`
	}
	per := make([]float64, 0, samples)
	recs := make([]sampleRec, 0, samples)

	started := time.Now()
	for i := 1; i <= samples; i++ {
		o, err := execOnce(ctx, cfg, cfg.Instrument.Cmd)
		if err != nil {
			return nil, fmt.Errorf("scalar sample %d: %w", i, err)
		}
		if o.ExitCode != 0 {
			return nil, fmt.Errorf("scalar sample %d: command %q exited %d\n%s", i, o.Command, o.ExitCode, strings.TrimSpace(string(o.Stdout)))
		}
		v, err := extractScalar(re, o.Stdout)
		if err != nil {
			return nil, fmt.Errorf("scalar sample %d: %w", i, err)
		}
		per = append(per, float64(v))
		recs = append(recs, sampleRec{
			Run: i, ExitCode: o.ExitCode, Value: v,
			Stdout: strings.TrimSpace(string(o.Stdout)),
		})
	}
	finished := time.Now()

	summary := stats.Summarize(per, stats.DefaultIterations, 0)

	rawDoc := map[string]any{
		"command":  strings.Join(cfg.Instrument.Cmd, " "),
		"pattern":  cfg.Instrument.Pattern,
		"worktree": cfg.WorktreeDir,
		"samples":  recs,
	}
	raw, _ := json.MarshalIndent(rawDoc, "", "  ")

	unit := cfg.Instrument.Unit
	if unit == "" {
		unit = "scalar"
	}
	return &Result{
		Artifacts: []ArtifactContent{
			{Name: "scalar", Filename: "scalar.json", Content: raw, Mime: "application/json"},
		},
		EvidenceFailures: nil,
		Command:          strings.Join(cfg.Instrument.Cmd, " "),
		ExitCode:         0,
		StartedAt:        started,
		FinishedAt:       finished,
		Value:            summary.Mean,
		Unit:             unit,
		SamplesN:         samples,
		PerSample:        per,
		CILow:            &summary.CILow,
		CIHigh:           &summary.CIHigh,
		CIMethod:         "bootstrap_bca_95",
	}, nil
}

func extractScalar(re *regexp.Regexp, out []byte) (int64, error) {
	m := re.FindSubmatch(out)
	if len(m) != 2 {
		return 0, fmt.Errorf("pattern /%s/ did not match output:\n%s", re.String(), strings.TrimSpace(string(out)))
	}
	v, err := strconv.ParseInt(string(m[1]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("capture %q is not a base-10 integer: %w", string(m[1]), err)
	}
	return v, nil
}

// parseSize reads the output of either GNU `size` (Berkeley format) or macOS
// Mach-O `size`. It takes the first numeric column of the first non-header
// data row as the "text" section size and returns all header-keyed values in
// aux. Unknown column names are preserved lowercased.
func parseSize(out []byte) (int64, map[string]any, error) {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		return 0, nil, errors.New("empty size output")
	}
	var header, data []string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if _, err := strconv.ParseInt(fields[0], 0, 64); err != nil {
			header = fields
			continue
		}
		data = fields
		break
	}
	if len(data) == 0 {
		return 0, nil, fmt.Errorf("no numeric row in size output:\n%s", string(out))
	}
	text, err := strconv.ParseInt(data[0], 0, 64)
	if err != nil {
		return 0, nil, fmt.Errorf("parse first column: %w", err)
	}
	aux := map[string]any{}
	for i, col := range data {
		name := fmt.Sprintf("col_%d", i)
		if i < len(header) {
			name = strings.ToLower(strings.TrimLeft(header[i], "_"))
		}
		if v, err := strconv.ParseInt(col, 0, 64); err == nil {
			aux[name] = v
		}
	}
	return text, aux, nil
}
