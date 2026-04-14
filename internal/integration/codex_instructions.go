package integration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	CodexInstructionsRelPath = "AGENTS.md"
	codexBlockStart          = "<!-- autoresearch:codex:start -->"
	codexBlockEnd            = "<!-- autoresearch:codex:end -->"
)

type CodexInstructionsResult struct {
	Path      string
	Created   bool
	Added     bool
	Updated   bool
	AlreadyOK bool
}

func EnsureCodexInstructions(projectDir string) (CodexInstructionsResult, error) {
	return ensureCodexInstructions(projectDir, false)
}

func PreviewCodexInstructions(projectDir string) (CodexInstructionsResult, error) {
	return ensureCodexInstructions(projectDir, true)
}

func ensureCodexInstructions(projectDir string, preview bool) (CodexInstructionsResult, error) {
	if projectDir == "" {
		return CodexInstructionsResult{}, errors.New("project dir is empty")
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return CodexInstructionsResult{}, err
	}
	path := filepath.Join(abs, CodexInstructionsRelPath)
	res := CodexInstructionsResult{Path: path}
	block := codexInstructionsBlock()

	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		res.Created = true
		if preview {
			return res, nil
		}
		return res, writeText(path, block)
	}
	if err != nil {
		return res, fmt.Errorf("read %s: %w", path, err)
	}

	text := string(existing)
	start := strings.Index(text, codexBlockStart)
	end := strings.Index(text, codexBlockEnd)
	switch {
	case start == -1 && end == -1:
		res.Added = true
		next := appendManagedBlock(text, block)
		if preview {
			return res, nil
		}
		return res, writeText(path, next)
	case start >= 0 && end > start:
		end += len(codexBlockEnd)
		if end < len(text) && text[end] == '\n' {
			end++
		}
		next := text[:start] + block + text[end:]
		if next == text {
			res.AlreadyOK = true
			return res, nil
		}
		res.Updated = true
		if preview {
			return res, nil
		}
		return res, writeText(path, next)
	default:
		return res, fmt.Errorf("malformed %s: found only one autoresearch codex marker", path)
	}
}

func appendManagedBlock(text, block string) string {
	trimmed := strings.TrimRight(text, "\n")
	if strings.TrimSpace(trimmed) == "" {
		return block
	}
	return trimmed + "\n\n" + block
}

func writeText(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func codexInstructionsBlock() string {
	return codexBlockStart + `
## autoresearch

This block is managed by ` + "`autoresearch init`" + ` and ` + "`autoresearch install codex`" + `.
Edits inside this block will be overwritten.

- Read ` + "`.codex/autoresearch.md`" + ` before mutating research state.
- Before any autonomous research loop, read ` + "`.codex/agents/research-orchestrator.toml`" + ` and follow its ` + "`developer_instructions`" + ` so your control flow matches the orchestrator contract even if you stay in the main session.
- Treat ` + "`.codex/autoresearch.md`" + ` as the canonical syntax reference for routine verbs. Do not spend early turns probing ` + "`--help`" + ` for the baseline/design/implement/observe/analyze/conclude/lesson/review spine; use help only when the installed reference leaves a real flag question unanswered.
- Treat the target project's main checkout as read-only during research except for autoresearch-managed setup files (` + "`AGENTS.md`" + `, ` + "`.claude/`" + `, ` + "`.codex/`" + `, ` + "`.gitignore`" + `, ` + "`.research/`" + `). Experiment and harness changes belong in experiment worktrees. If ` + "`autoresearch status`" + ` or ` + "`dashboard`" + ` reports ` + "`main_checkout_dirty_paths`" + `, stop and surface an explicit maintenance task instead of patching bootstrap/harness files in place.
- Use ` + "`autoresearch <verb>`" + ` to record every hypothesis, experiment, observation, and conclusion.
- Never edit ` + "`.research/`" + ` directly; the CLI is the only writer.
- Capture reasoning, not just measurements: pass ` + "`--rationale`" + ` on ` + "`hypothesis add`" + `, ` + "`--design-notes`" + ` on ` + "`experiment design`" + `, ` + "`--impl-notes`" + ` on ` + "`experiment implement`" + `, and ` + "`--interpretation`" + ` on ` + "`conclude`" + `. Record cumulative lessons with ` + "`autoresearch lesson add`" + ` on decisive conclusions, and read ` + "`autoresearch lesson list --status active`" + ` before proposing new hypotheses — the loop should not re-derive what it already knows.
- Do not let ` + "`hypothesis add --predicts-instrument`" + ` drift away from the active goal. The predicted instrument must be the goal objective or an explicit goal-constraint instrument; extra instruments belong on experiments as supporting measurements, not as ad hoc new objectives.
- Lessons sourced from a decisive conclusion are provisional until gate review. Do not use ` + "`--inspired-by`" + ` to cite lessons whose source chain is not currently ` + "`reviewed_decisive`" + `.
- Budgets are caps, not quotas. ` + "`max-experiments`" + ` is a ceiling, not a target; do not create or kill filler hypotheses just to hit a number.
- Humans steer through the main Codex conversation. Read-only views like ` + "`dashboard`" + `, ` + "`log`" + `, ` + "`tree`" + `, ` + "`frontier`" + `, ` + "`lesson list`" + `, and ` + "`report`" + ` are windows onto state, not steering surfaces.
- ` + "`research-orchestrator`" + ` is a one-cycle leaf role. Do not ask it to spawn another ` + "`research-orchestrator`" + `, and do not assume child sessions expose nested ` + "`spawn_agent`" + ` / ` + "`send_input`" + ` / ` + "`wait_agent`" + ` or the same custom ` + "`agent_type`" + ` names.
- If a delegated ` + "`research-orchestrator`" + ` returns a decisive conclusion, the parent/main session owns the next handoff: dispatch ` + "`research-gate-reviewer`" + ` yourself before starting another cycle. If review delegation is unavailable, stop and yield with review pending rather than continuing unreviewed.
- When delegating with ` + "`spawn_agent`" + `, read the matching installed custom agent under ` + "`.codex/agents/research-*.toml`" + ` first, then spawn it by exact ` + "`agent_type`" + ` name. Use ` + "`research-orchestrator`" + ` to run a full hypothesis cycle, and ` + "`research-gate-reviewer`" + ` to independently review decisive conclusions.
- Do not emulate those roles by spawning ` + "`explorer`" + `, ` + "`worker`" + `, or ` + "`default`" + ` with pasted instructions. If the named custom agent is unavailable, stop and report that ` + "`autoresearch install codex agents`" + ` must be rerun.
` + codexBlockEnd + `
`
}
