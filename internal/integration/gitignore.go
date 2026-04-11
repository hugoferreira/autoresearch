package integration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GitignoreResult reports what EnsureGitignoreLine did. Exactly one of
// Created / Added / AlreadyPresent will be true on success.
type GitignoreResult struct {
	Path           string
	Created        bool // .gitignore did not exist; we created it with the line
	Added          bool // .gitignore existed; we appended the line
	AlreadyPresent bool // .gitignore already contained the line; no mutation
}

// EnsureGitignoreLine makes sure `line` appears as its own entry in
// <projectDir>/.gitignore. It creates the file if missing, appends the line
// (preserving the existing trailing-newline state) if the file exists without
// that entry, and is a no-op if the entry is already present. It never
// touches any other content in the file.
//
// The comparison is whole-line and whitespace-trimmed, so `" .research/ "`
// matches `".research/"`. Any existing non-matching rules are preserved
// exactly as-is.
func EnsureGitignoreLine(projectDir, line string) (GitignoreResult, error) {
	if strings.ContainsAny(line, "\n\r") {
		return GitignoreResult{}, errors.New("gitignore line must not contain newlines")
	}
	path := filepath.Join(projectDir, ".gitignore")
	res := GitignoreResult{Path: path}

	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
			return res, fmt.Errorf("create .gitignore: %w", err)
		}
		res.Created = true
		return res, nil
	} else if err != nil {
		return res, fmt.Errorf("read .gitignore: %w", err)
	}

	for _, rawLine := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(rawLine) == line {
			res.AlreadyPresent = true
			return res, nil
		}
	}

	prefix := ""
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		prefix = "\n"
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return res, fmt.Errorf("open .gitignore: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(prefix + line + "\n"); err != nil {
		return res, fmt.Errorf("append .gitignore: %w", err)
	}
	res.Added = true
	return res, nil
}

// PreviewGitignoreLine reports what EnsureGitignoreLine WOULD do without
// mutating anything. Used for --dry-run.
func PreviewGitignoreLine(projectDir, line string) (GitignoreResult, error) {
	path := filepath.Join(projectDir, ".gitignore")
	res := GitignoreResult{Path: path}
	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		res.Created = true
		return res, nil
	} else if err != nil {
		return res, err
	}
	for _, rawLine := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(rawLine) == line {
			res.AlreadyPresent = true
			return res, nil
		}
	}
	res.Added = true
	return res, nil
}
