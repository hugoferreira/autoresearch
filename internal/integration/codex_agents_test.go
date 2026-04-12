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
	if len(agents) != 2 {
		t.Fatalf("agent count: got %d want 2", len(agents))
	}
	for _, a := range agents {
		if !bytes.Contains(a.Content, []byte(".codex/autoresearch.md")) {
			t.Errorf("%s: missing codex doc reference", a.Name)
		}
		if bytes.Contains(a.Content, []byte(".claude/autoresearch.md")) {
			t.Errorf("%s: still references claude doc", a.Name)
		}
		if !bytes.Contains(a.Content, []byte("autoresearch install codex agents")) {
			t.Errorf("%s: missing codex install marker", a.Name)
		}
	}
}

func TestEmbeddedCodexAgents_NotebookPropagation(t *testing.T) {
	agents, err := integration.EmbeddedCodexAgents()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string][]byte{}
	for _, a := range agents {
		byName[a.Name] = a.Content
	}

	cases := []struct {
		role    string
		needles []string
	}{
		{"research-orchestrator", []string{
			"--rationale",
			"--design-notes",
			"--impl-notes",
			"--interpretation",
			"autoresearch lesson list --status active",
			"autoresearch lesson add",
			"## Evidence",
			"## Mechanism",
			"## Scope and counterexamples",
			"## For the next generator",
		}},
		{"research-gate-reviewer", []string{
			"autoresearch lesson add",
			"conclusion downgrade",
		}},
	}
	for _, c := range cases {
		content, ok := byName[c.role]
		if !ok {
			t.Errorf("role %s missing from embedded codex agents", c.role)
			continue
		}
		for _, n := range c.needles {
			if !bytes.Contains(content, []byte(n)) {
				t.Errorf("codex %s brief missing %q", c.role, n)
			}
		}
	}
}

func TestInstallCodexAgents(t *testing.T) {
	dir := t.TempDir()
	r, err := integration.InstallCodexAgents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Count != 2 {
		t.Fatalf("count: got %d want 2", r.Count)
	}
	for _, fn := range r.Written {
		path := filepath.Join(dir, ".codex", "agents", fn)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s: %v", path, err)
		}
	}
}
