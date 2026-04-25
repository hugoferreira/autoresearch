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
// the project root. Claude Code reads permissions.allow / permissions.deny
// from this file to decide which tool invocations run without prompting the
// user, and which should be refused outright.
const ClaudeSettingsRelPath = ".claude/settings.json"

// AutoresearchAllowEntry is the permission grant for CLI invocations so that
// `autoresearch <verb>` calls from the main session don't prompt on every
// call. Claude Code's Bash permission matcher treats the pattern as "command
// name + args prefix"; `autoresearch:*` covers every verb and flag combo.
const AutoresearchAllowEntry = "Bash(autoresearch:*)"

// ClaudeHarnessToolResultsDenyEntry blocks Claude Code's file reader from
// reaching into stale externalized tool-result cache files. These files are
// not authoritative autoresearch state and can be arbitrarily large.
const ClaudeHarnessToolResultsDenyEntry = "Read(~/.claude/projects/**/tool-results/**)"

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
	Path       string
	Created    bool     // settings.json did not exist; we created it
	Updated    bool     // settings.json existed and we appended new entries
	AlreadyOK  bool     // every requested entry was already present
	Added      []string // entries we actually added (may be empty on AlreadyOK)
	AddedAllow []string // allow entries we actually added
	AddedDeny  []string // deny entries we actually added
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
	return EnsureClaudeSettingsPermissions(projectDir, entries, nil)
}

// EnsureClaudeSettingsPermissions merges the given permission entries into
// <projectDir>/.claude/settings.json's `permissions.allow` and
// `permissions.deny` arrays. It preserves unrelated settings and user-owned
// entries in either permission list.
func EnsureClaudeSettingsPermissions(projectDir string, allowEntries, denyEntries []string) (ClaudeSettingsResult, error) {
	return ensureClaudeSettings(projectDir, allowEntries, denyEntries, false)
}

func ensureClaudeSettings(projectDir string, allowEntries, denyEntries []string, preview bool) (ClaudeSettingsResult, error) {
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
		res.AddedAllow = uniqueSorted(allowEntries)
		res.AddedDeny = uniqueSorted(denyEntries)
		res.Added = combineAdded(res.AddedAllow, res.AddedDeny)
		if preview {
			return res, nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return res, fmt.Errorf("create .claude/: %w", err)
		}
		perms := map[string]any{
			"allow": toAnySlice(res.AddedAllow),
		}
		if len(res.AddedDeny) > 0 {
			perms["deny"] = toAnySlice(res.AddedDeny)
		}
		doc := map[string]any{
			"permissions": perms,
		}
		if err := writeJSON(path, doc); err != nil {
			return res, err
		}
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

	allowAny, addedAllow := appendPermissionEntries(perms, "allow", allowEntries)
	denyAny, addedDeny := appendPermissionEntries(perms, "deny", denyEntries)
	if len(addedAllow) == 0 && len(addedDeny) == 0 {
		res.AlreadyOK = true
		return res, nil
	}

	if len(addedAllow) > 0 {
		perms["allow"] = allowAny
	}
	if len(addedDeny) > 0 {
		perms["deny"] = denyAny
	}
	doc["permissions"] = perms
	res.AddedAllow = addedAllow
	res.AddedDeny = addedDeny
	res.Added = combineAdded(addedAllow, addedDeny)
	if preview {
		res.Updated = true
		return res, nil
	}
	if err := writeJSON(path, doc); err != nil {
		return res, err
	}
	res.Updated = true
	return res, nil
}

// PreviewClaudeSettings reports what EnsureClaudeSettings WOULD do without
// mutating anything. Used for --dry-run.
func PreviewClaudeSettings(projectDir string, entries []string) (ClaudeSettingsResult, error) {
	return PreviewClaudeSettingsPermissions(projectDir, entries, nil)
}

// PreviewClaudeSettingsPermissions reports what
// EnsureClaudeSettingsPermissions WOULD do without mutating anything. Used for
// --dry-run.
func PreviewClaudeSettingsPermissions(projectDir string, allowEntries, denyEntries []string) (ClaudeSettingsResult, error) {
	return ensureClaudeSettings(projectDir, allowEntries, denyEntries, true)
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

func appendPermissionEntries(perms map[string]any, key string, entries []string) ([]any, []string) {
	raw, _ := perms[key].([]any)
	present := make(map[string]bool, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			present[s] = true
		}
	}

	var added []string
	for _, e := range entries {
		if !present[e] {
			raw = append(raw, e)
			present[e] = true
			added = append(added, e)
		}
	}
	sort.Strings(added)
	return raw, added
}

func combineAdded(groups ...[]string) []string {
	var out []string
	for _, group := range groups {
		out = append(out, group...)
	}
	sort.Strings(out)
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
