package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/bytter/autoresearch/internal/integration"
	"github.com/bytter/autoresearch/internal/testkit"
	"gopkg.in/yaml.v3"
)

var _ = testkit.Spec("TestEmbeddedAgents_AllPresent", func(t testkit.T) {
	agents, err := integration.EmbeddedAgents()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"research-orchestrator":  true,
		"research-gate-reviewer": true,
	}
	got := map[string]bool{}
	for _, a := range agents {
		got[a.Name] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("missing embedded agent: %s", name)
		}
	}
	if len(agents) != len(want) {
		t.Errorf("agent count: got %d want %d", len(agents), len(want))
	}
})

var _ = testkit.Spec("TestEmbeddedAgents_FrontmatterValid", func(t testkit.T) {
	agents, err := integration.EmbeddedAgents()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		if len(a.Content) == 0 {
			t.Errorf("%s: empty", a.Name)
			continue
		}
		if !bytes.HasPrefix(a.Content, []byte("---\n")) {
			t.Errorf("%s: missing YAML frontmatter", a.Name)
			continue
		}
		// Extract frontmatter.
		rest := a.Content[4:]
		end := bytes.Index(rest, []byte("\n---\n"))
		if end < 0 {
			t.Errorf("%s: unterminated frontmatter", a.Name)
			continue
		}
		var fm struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
			Tools       string `yaml:"tools"`
		}
		if err := yaml.Unmarshal(rest[:end], &fm); err != nil {
			t.Errorf("%s: frontmatter YAML parse: %v", a.Name, err)
			continue
		}
		if fm.Name != a.Name {
			t.Errorf("%s: frontmatter name mismatch: %q", a.Name, fm.Name)
		}
		if !strings.HasPrefix(strings.ToLower(fm.Description), "use when") &&
			!strings.HasPrefix(strings.ToLower(fm.Description), "use after") &&
			!strings.HasPrefix(strings.ToLower(fm.Description), "use to") {
			t.Errorf("%s: description should start with 'Use when/after/to ...': %q", a.Name, fm.Description)
		}
		if fm.Tools == "" {
			t.Errorf("%s: empty tools list", a.Name)
		}
		// The orchestrator needs Agent + Edit/Write (for spawning helpers).
		// The gate reviewer is read-only (no Edit/Write).
		hasEdit := strings.Contains(fm.Tools, "Edit") || strings.Contains(fm.Tools, "Write")
		if a.Name == "research-orchestrator" && !hasEdit {
			t.Errorf("%s: orchestrator must have Edit/Write", a.Name)
		}
		if a.Name == "research-gate-reviewer" && hasEdit {
			t.Errorf("%s: gate reviewer must not have Edit/Write; got %q", a.Name, fm.Tools)
		}
		// Body must reference the firewall or the reference doc.
		body := rest[end+5:]
		if !bytes.Contains(body, []byte(".claude/autoresearch.md")) {
			t.Errorf("%s: body should reference .claude/autoresearch.md", a.Name)
		}
	}
})

var _ = testkit.Spec("TestInstallAgents", func(t testkit.T) {
	dir := t.TempDir()
	r, err := integration.InstallAgents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Count != 2 {
		t.Errorf("count: got %d want 2", r.Count)
	}
	for _, fn := range r.Written {
		path := filepath.Join(dir, ".claude", "agents", fn)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s: %v", path, err)
		}
	}
})

var _ = testkit.Spec("TestInstallAgents_Idempotent", func(t testkit.T) {
	dir := t.TempDir()
	_, err := integration.InstallAgents(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Drop a user-owned file that isn't one of ours; make sure it survives.
	customPath := filepath.Join(dir, ".claude", "agents", "my-custom-agent.md")
	if err := os.WriteFile(customPath, []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = integration.InstallAgents(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(customPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "custom" {
		t.Errorf("custom agent file was clobbered: %q", string(b))
	}
})
