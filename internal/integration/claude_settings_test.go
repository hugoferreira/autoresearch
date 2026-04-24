package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/bytter/autoresearch/internal/integration"
	"github.com/bytter/autoresearch/internal/testkit"
)

func readSettings(t testkit.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse %s: %v\n%s", path, err, string(b))
	}
	return m
}

func allowList(t testkit.T, doc map[string]any) []string {
	t.Helper()
	perms, ok := doc["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("no permissions key: %v", doc)
	}
	raw, ok := perms["allow"].([]any)
	if !ok {
		t.Fatalf("no allow list: %v", perms)
	}
	out := make([]string, len(raw))
	for i, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("allow[%d] not a string: %v", i, v)
		}
		out[i] = s
	}
	return out
}

var _ = testkit.Spec("TestEnsureClaudeSettings_Created", func(t testkit.T) {
	dir := t.TempDir()
	r, err := integration.EnsureClaudeSettings(dir, []string{integration.AutoresearchAllowEntry})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Created || r.Updated || r.AlreadyOK {
		t.Errorf("expected Created, got %+v", r)
	}
	if got := allowList(t, readSettings(t, r.Path)); len(got) != 1 || got[0] != integration.AutoresearchAllowEntry {
		t.Errorf("allow contents: %v", got)
	}
})

var _ = testkit.Spec("TestEnsureClaudeSettings_AddsToExistingAllow", func(t testkit.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const pre = `{
  "permissions": {
    "allow": ["Bash(git status:*)", "Bash(go test:*)"],
    "deny": ["Bash(rm -rf:*)"]
  },
  "otherKey": "preserved"
}`
	if err := os.WriteFile(settingsPath, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := integration.EnsureClaudeSettings(dir, []string{integration.AutoresearchAllowEntry})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Updated {
		t.Errorf("expected Updated, got %+v", r)
	}
	doc := readSettings(t, settingsPath)
	got := allowList(t, doc)
	// All three should be present.
	want := map[string]bool{
		"Bash(git status:*)":               true,
		"Bash(go test:*)":                  true,
		integration.AutoresearchAllowEntry: true,
	}
	if len(got) != len(want) {
		t.Errorf("allow len: got %v want %v", got, want)
	}
	for _, s := range got {
		if !want[s] {
			t.Errorf("unexpected allow entry: %s", s)
		}
	}
	// Unrelated keys must survive.
	if doc["otherKey"] != "preserved" {
		t.Errorf("otherKey not preserved: %v", doc["otherKey"])
	}
	perms := doc["permissions"].(map[string]any)
	deny, _ := perms["deny"].([]any)
	if len(deny) != 1 || deny[0] != "Bash(rm -rf:*)" {
		t.Errorf("deny not preserved: %v", deny)
	}
})

var _ = testkit.Spec("TestEnsureClaudeSettings_Idempotent", func(t testkit.T) {
	dir := t.TempDir()
	entries := []string{integration.AutoresearchAllowEntry}
	if _, err := integration.EnsureClaudeSettings(dir, entries); err != nil {
		t.Fatal(err)
	}
	r, err := integration.EnsureClaudeSettings(dir, entries)
	if err != nil {
		t.Fatal(err)
	}
	if !r.AlreadyOK {
		t.Errorf("second call expected AlreadyOK, got %+v", r)
	}
	if len(r.Added) != 0 {
		t.Errorf("second call should add nothing, got %v", r.Added)
	}
})

var _ = testkit.Spec("TestEnsureClaudeSettings_NoPermissionsKey", func(t testkit.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"env": {"FOO": "bar"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := integration.EnsureClaudeSettings(dir, []string{integration.AutoresearchAllowEntry})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Updated {
		t.Errorf("expected Updated, got %+v", r)
	}
	doc := readSettings(t, settingsPath)
	got := allowList(t, doc)
	if len(got) != 1 || got[0] != integration.AutoresearchAllowEntry {
		t.Errorf("allow contents: %v", got)
	}
	env, _ := doc["env"].(map[string]any)
	if env["FOO"] != "bar" {
		t.Errorf("env.FOO not preserved: %v", env)
	}
})

var _ = testkit.Spec("TestEnsureClaudeSettings_InvalidJSON", func(t testkit.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{ not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := integration.EnsureClaudeSettings(dir, []string{integration.AutoresearchAllowEntry}); err == nil {
		t.Error("expected parse error on invalid json")
	}
})

var _ = testkit.Spec("TestWorktreeAllowEntries_UsesDoubleSlashForAbsolute", func(t testkit.T) {
	got := integration.WorktreeAllowEntries("/Users/bob/Library/Caches/autoresearch/proj-abc/worktrees")
	want := []string{
		"Read(//Users/bob/Library/Caches/autoresearch/proj-abc/worktrees/**)",
		"Edit(//Users/bob/Library/Caches/autoresearch/proj-abc/worktrees/**)",
		"Write(//Users/bob/Library/Caches/autoresearch/proj-abc/worktrees/**)",
	}
	if len(got) != len(want) {
		t.Fatalf("len: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d: got %q want %q", i, got[i], want[i])
		}
	}
})

var _ = testkit.Spec("TestPreviewClaudeSettings", func(t testkit.T) {
	dir := t.TempDir()
	entries := []string{integration.AutoresearchAllowEntry}

	r, err := integration.PreviewClaudeSettings(dir, entries)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Created {
		t.Errorf("absent: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.json")); err == nil {
		t.Error("preview should not create file")
	}

	if _, err := integration.EnsureClaudeSettings(dir, entries); err != nil {
		t.Fatal(err)
	}
	r, err = integration.PreviewClaudeSettings(dir, entries)
	if err != nil {
		t.Fatal(err)
	}
	if !r.AlreadyOK {
		t.Errorf("already present: %+v", r)
	}
})
