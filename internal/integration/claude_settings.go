package integration

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ClaudeSettingsRelPath is the Claude Code project settings file, relative to
// the project root. Claude Code reads permissions.allow from this file to
// decide which tool invocations run without prompting the user.
const ClaudeSettingsRelPath = ".claude/settings.json"

// AutoresearchAllowEntry is the permission grant for CLI invocations so that
// `autoresearch <verb>` calls from the main session don't prompt on every
// call. Claude Code's Bash permission matcher treats the pattern as "command
// name + args prefix"; `autoresearch:*` covers every verb and flag combo.
const AutoresearchAllowEntry = "Bash(autoresearch:*)"

// WorktreeAllowEntries returns permission entries for the worktree root
// directory so that subagents working in experiment worktrees (which live
// outside the project tree, typically under the user cache dir) don't
// trigger permission prompts on every file or shell operation.
//
// Claude Code's Read/Edit/Write permission patterns treat a leading single
// slash as project-relative, so `Read(/Users/...)` never matches a real
// absolute path. Filesystem-absolute paths require a `//` prefix: we emit
// `Read(//Users/...)` so the matcher walks from the filesystem root.
//
// Claude Code's Bash permission is command-prefix only — there's no way to
// scope it to a directory. We use Read/Edit/Write path globs for file
// operations and rely on the existing Bash(autoresearch:*) entry for CLI
// calls. Shell commands in worktrees (make, gcc, git, etc.) will still
// prompt unless the user adds broader Bash permissions themselves.
func WorktreeAllowEntries(worktreesRoot string) []string {
	abs := "//" + strings.TrimLeft(worktreesRoot, "/")
	return []string{
		"Read(" + abs + "/**)",
		"Edit(" + abs + "/**)",
		"Write(" + abs + "/**)",
	}
}

// ClaudeSettingsResult reports what EnsureClaudeSettings did. Exactly one of
// Created / Updated / AlreadyOK is true on success.
type ClaudeSettingsResult struct {
	Path      string
	Created   bool     // settings.json did not exist; we created it
	Updated   bool     // settings.json existed and we appended new entries
	AlreadyOK bool     // every requested entry was already present
	Added     []string // entries we actually added (may be empty on AlreadyOK)
}

// EnsureClaudeSettings merges the given permission allow entries into
// <projectDir>/.claude/settings.json's `permissions.allow` array. It creates
// the file if missing, adds only missing entries if the file exists, and is a
// no-op when every entry is already present. It never touches unrelated keys
// in the file.
//
// The file is parsed as generic JSON; comments or JSONC-style extensions are
// not supported and will fail parsing. That matches Claude Code's own reader.
func EnsureClaudeSettings(projectDir string, entries []string) (ClaudeSettingsResult, error) {
	if projectDir == "" {
		return ClaudeSettingsResult{}, errors.New("project dir is empty")
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return ClaudeSettingsResult{}, err
	}
	path := filepath.Join(abs, ClaudeSettingsRelPath)
	res := ClaudeSettingsResult{Path: path}

	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return res, fmt.Errorf("create .claude/: %w", err)
		}
		doc := map[string]any{
			"permissions": map[string]any{
				"allow": toAnySlice(uniqueSorted(entries)),
			},
		}
		if err := writeJSON(path, doc); err != nil {
			return res, err
		}
		res.Created = true
		res.Added = append([]string(nil), uniqueSorted(entries)...)
		return res, nil
	} else if err != nil {
		return res, fmt.Errorf("read %s: %w", path, err)
	}

	var doc map[string]any
	if len(existing) == 0 {
		doc = map[string]any{}
	} else if err := json.Unmarshal(existing, &doc); err != nil {
		return res, fmt.Errorf("parse %s: %w", path, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}

	perms, _ := doc["permissions"].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
	}
	allowAny, _ := perms["allow"].([]any)
	present := make(map[string]bool, len(allowAny))
	for _, v := range allowAny {
		if s, ok := v.(string); ok {
			present[s] = true
		}
	}

	var added []string
	for _, e := range entries {
		if !present[e] {
			allowAny = append(allowAny, e)
			present[e] = true
			added = append(added, e)
		}
	}
	if len(added) == 0 {
		res.AlreadyOK = true
		return res, nil
	}

	perms["allow"] = allowAny
	doc["permissions"] = perms
	if err := writeJSON(path, doc); err != nil {
		return res, err
	}
	sort.Strings(added)
	res.Updated = true
	res.Added = added
	return res, nil
}

// PreviewClaudeSettings reports what EnsureClaudeSettings WOULD do without
// mutating anything. Used for --dry-run.
func PreviewClaudeSettings(projectDir string, entries []string) (ClaudeSettingsResult, error) {
	if projectDir == "" {
		return ClaudeSettingsResult{}, errors.New("project dir is empty")
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return ClaudeSettingsResult{}, err
	}
	path := filepath.Join(abs, ClaudeSettingsRelPath)
	res := ClaudeSettingsResult{Path: path}

	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		res.Created = true
		res.Added = append([]string(nil), uniqueSorted(entries)...)
		return res, nil
	} else if err != nil {
		return res, err
	}

	var doc map[string]any
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &doc); err != nil {
			return res, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	present := map[string]bool{}
	if perms, ok := doc["permissions"].(map[string]any); ok {
		if allowAny, ok := perms["allow"].([]any); ok {
			for _, v := range allowAny {
				if s, ok := v.(string); ok {
					present[s] = true
				}
			}
		}
	}
	var added []string
	for _, e := range entries {
		if !present[e] {
			added = append(added, e)
			present[e] = true
		}
	}
	if len(added) == 0 {
		res.AlreadyOK = true
		return res, nil
	}
	sort.Strings(added)
	res.Updated = true
	res.Added = added
	return res, nil
}

func writeJSON(path string, doc map[string]any) error {
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	buf = append(buf, '\n')
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func toAnySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func uniqueSorted(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
