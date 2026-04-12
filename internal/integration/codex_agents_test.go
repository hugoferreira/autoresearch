package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/bytter/autoresearch/internal/integration"
)

func TestEmbeddedCodexAgents_RewrittenForCodex(t *testing.T) {
	agents, err := integration.EmbeddedCodexAgents()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 6 {
		t.Fatalf("agent count: got %d want 6", len(agents))
	}
	for _, a := range agents {
		if !bytes.Contains(a.Content, []byte(".codex/autoresearch.md")) {
			t.Errorf("%s: missing codex doc reference", a.Name)
		}
		if bytes.Contains(a.Content, []byte(".claude/autoresearch.md")) {
			t.Errorf("%s: still references claude doc", a.Name)
		}
		if !bytes.Contains(a.Content, []byte("autoresearch codex agents install")) {
			t.Errorf("%s: missing codex install marker", a.Name)
		}
	}
}

func TestInstallCodexAgents(t *testing.T) {
	dir := t.TempDir()
	r, err := integration.InstallCodexAgents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Count != 6 {
		t.Fatalf("count: got %d want 6", r.Count)
	}
	for _, fn := range r.Written {
		path := filepath.Join(dir, ".codex", "agents", fn)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s: %v", path, err)
		}
	}
}
