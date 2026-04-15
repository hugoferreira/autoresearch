package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bytter/autoresearch/internal/integration"
)

func TestEnsureCodexInstructions_Created(t *testing.T) {
	dir := t.TempDir()
	r, err := integration.EnsureCodexInstructions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Created || r.Added || r.Updated || r.AlreadyOK {
		t.Fatalf("expected Created, got %+v", r)
	}
	body, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, ".codex/autoresearch.md") {
		t.Fatalf("missing codex doc reference: %s", text)
	}
	if !strings.Contains(text, "spawn_agent") {
		t.Fatalf("missing spawn_agent guidance: %s", text)
	}
	for _, needle := range []string{
		".codex/agents/research-orchestrator.toml",
		"Budgets are caps, not quotas",
		"review pending",
		"--inspired-by",
		"Choose lesson scope conservatively",
		"measurement caveats",
		"prefer hypothesis scope",
		"Do not spend early turns probing `--help`",
		"exact `agent_type` name",
		"Do not emulate those roles by spawning `explorer`",
		"one-cycle leaf role",
		"parent/main session owns the next handoff",
		"nested `spawn_agent` / `send_input` / `wait_agent`",
		"main checkout as read-only during research",
		"main_checkout_dirty_paths",
		"bootstrap/harness files",
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("managed block missing %q: %s", needle, text)
		}
	}
}

func TestEnsureCodexInstructions_AppendsManagedBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	const pre = "# Team Notes\n\nKeep tests deterministic.\n"
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := integration.EnsureCodexInstructions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Added {
		t.Fatalf("expected Added, got %+v", r)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.HasPrefix(text, pre) {
		t.Fatalf("user content not preserved: %q", text)
	}
}

func TestEnsureCodexInstructions_Idempotent(t *testing.T) {
	dir := t.TempDir()
	if _, err := integration.EnsureCodexInstructions(dir); err != nil {
		t.Fatal(err)
	}
	r, err := integration.EnsureCodexInstructions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !r.AlreadyOK {
		t.Fatalf("expected AlreadyOK, got %+v", r)
	}
}

func TestPreviewCodexInstructions(t *testing.T) {
	dir := t.TempDir()
	r, err := integration.PreviewCodexInstructions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Created {
		t.Fatalf("expected Created, got %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err == nil {
		t.Fatal("preview should not create AGENTS.md")
	}
}
