package integration_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bytter/autoresearch/internal/integration"
)

func TestSharedDocs_EvidenceArtifactGuidance(t *testing.T) {
	claudeDoc := integration.ClaudeDoc("vtest")
	codexDoc := integration.CodexDoc("vtest")

	for name, doc := range map[string]string{
		"claude": claudeDoc,
		"codex":  codexDoc,
	} {
		for _, needle := range []string{
			"--evidence name=cmd",
			"observation_artifacts",
			"evidence/<name>",
			"observation.evidence_failures",
		} {
			if !strings.Contains(doc, needle) {
				t.Fatalf("%s doc missing evidence-artifact guidance %q", name, needle)
			}
		}
	}
}

func TestEmbeddedAgents_EvidenceArtifactContract(t *testing.T) {
	agents, err := integration.EmbeddedAgents()
	if err != nil {
		t.Fatal(err)
	}
	codexAgents, err := integration.EmbeddedCodexAgents()
	if err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		label  string
		agents []integration.AgentFile
	}{
		{label: "claude", agents: agents},
		{label: "codex", agents: codexAgents},
	}
	for _, chk := range checks {
		byName := map[string][]byte{}
		for _, a := range chk.agents {
			byName[a.Name] = a.Content
		}
		for role, needles := range map[string][]string{
			"research-orchestrator": {
				"--evidence mechanism='profile-expr --json'",
				"evidence/<name>",
				"measurement-contract gap",
			},
			"research-gate-reviewer": {
				"observation_artifacts",
				"supported by neither the diff nor",
				"an evidence artifact.",
				"Unsupported mechanism",
			},
		} {
			content, ok := byName[role]
			if !ok {
				t.Fatalf("%s %s agent missing", chk.label, role)
			}
			for _, needle := range needles {
				if !bytes.Contains(content, []byte(needle)) {
					t.Fatalf("%s %s agent missing %q", chk.label, role, needle)
				}
			}
		}
	}
}
