package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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
		if !strings.HasSuffix(a.Filename, ".toml") {
			t.Errorf("%s: expected .toml filename, got %s", a.Name, a.Filename)
		}
		if !bytes.Contains(a.Content, []byte(".codex/autoresearch.md")) {
			t.Errorf("%s: missing codex doc reference", a.Name)
		}
		if bytes.Contains(a.Content, []byte(".claude/autoresearch.md")) {
			t.Errorf("%s: still references claude doc", a.Name)
		}
		if bytes.Contains(a.Content, []byte("@.codex/autoresearch.md")) {
			t.Errorf("%s: should use plain codex doc path, not @ mention syntax", a.Name)
		}
		if !bytes.Contains(a.Content, []byte("autoresearch install codex agents")) {
			t.Errorf("%s: missing codex install marker", a.Name)
		}
		if !bytes.Contains(a.Content, []byte("name = \""+a.Name+"\"")) {
			t.Errorf("%s: missing TOML name field", a.Name)
		}
		if !bytes.Contains(a.Content, []byte("description = ")) {
			t.Errorf("%s: missing TOML description field", a.Name)
		}
		if !bytes.Contains(a.Content, []byte("developer_instructions = '''")) {
			t.Errorf("%s: missing TOML developer_instructions field", a.Name)
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
			"burst of\n`--help`",
			"### Command spine",
			"provisional",
			"review pending",
			"ceiling, not",
			"sandbox_mode = \"workspace-write\"",
			"Do not spawn another `research-orchestrator`",
			"dispatch research-gate-reviewer on C-NNNN",
			"nested child sessions expose `spawn_agent`, `send_input`, `wait_agent`",
			"main checkout stays read-only",
			"main_checkout_dirty_paths",
			"bootstrap scripts",
		}},
		{"research-gate-reviewer", []string{
			"autoresearch lesson add",
			"conclusion downgrade",
			"repetitive `--help` lookups",
			"sandbox_mode = \"read-only\"",
			"leaf autoresearch role",
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

func TestCodexDelegationHandoffContract(t *testing.T) {
	agents, err := integration.EmbeddedCodexAgents()
	if err != nil {
		t.Fatal(err)
	}
	var orchestrator string
	for _, a := range agents {
		if a.Name == "research-orchestrator" {
			orchestrator = string(a.Content)
			break
		}
	}
	if orchestrator == "" {
		t.Fatal("research-orchestrator missing from embedded codex agents")
	}
	for _, needle := range []string{
		"one full hypothesis cycle",
		"Do not spawn another `research-orchestrator`",
		"Do **not** dispatch `research-gate-reviewer` yourself from this role.",
		"return to the parent/main session with an explicit handoff",
	} {
		if !strings.Contains(orchestrator, needle) {
			t.Fatalf("orchestrator contract missing %q", needle)
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

func TestInstallCodexAgents_RemovesLegacyMarkdown(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".codex", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"research-orchestrator.md", "research-gate-reviewer.md"} {
		if err := os.WriteFile(filepath.Join(agentsDir, name), []byte("legacy"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := integration.InstallCodexAgents(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"research-orchestrator.md", "research-gate-reviewer.md"} {
		if _, err := os.Stat(filepath.Join(agentsDir, name)); !os.IsNotExist(err) {
			t.Fatalf("legacy file %s should be removed, stat err=%v", name, err)
		}
	}
}

func TestInstallCodexAgents_PreservesSiblingFiles(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".codex", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	customPath := filepath.Join(agentsDir, "my-custom-agent.toml")
	if err := os.WriteFile(customPath, []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := integration.InstallCodexAgents(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(customPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "custom" {
		t.Errorf("custom agent file was clobbered: %q", string(b))
	}
}
