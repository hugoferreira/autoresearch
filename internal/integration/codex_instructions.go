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

This block is managed by ` + "`autoresearch init`" + ` and ` + "`autoresearch codex install`" + `.
Edits inside this block will be overwritten.

- Read ` + "`.codex/autoresearch.md`" + ` before mutating research state.
- Use ` + "`autoresearch <verb>`" + ` to record every hypothesis, experiment, observation, and conclusion.
- Never edit ` + "`.research/`" + ` directly; the CLI is the only writer.
- Humans steer through the main Codex conversation. Read-only views like ` + "`dashboard`" + `, ` + "`log`" + `, ` + "`tree`" + `, ` + "`frontier`" + `, and ` + "`report`" + ` are windows onto state, not steering surfaces.
- When delegating with ` + "`spawn_agent`" + `, read the matching brief under ` + "`.codex/agents/research-*.md`" + ` first. Use ` + "`worker`" + ` for ` + "`research-implementer`" + ` and ` + "`default`" + ` for the other five roles.
` + codexBlockEnd + `
`
}
