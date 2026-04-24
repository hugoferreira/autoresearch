package integration_test

import (
	"bytes"
	"strings"

	"github.com/bytter/autoresearch/internal/integration"
	"github.com/bytter/autoresearch/internal/testkit"
)

var _ = testkit.Spec("TestSharedDocs_EvidenceArtifactGuidance", func(t testkit.T) {
	claudeDoc := integration.ClaudeDoc("vtest")
	codexDoc := integration.CodexDoc("vtest")

	for name, doc := range map[string]string{
		"claude": claudeDoc,
		"codex":  codexDoc,
	} {
		for _, needle := range []string{
			"--evidence name=cmd",
			"observation_artifacts",
			"observation_evidence_failures",
			"observation_read_issues",
			"evidence/<name>",
			"observation.evidence_failures",
		} {
			if !strings.Contains(doc, needle) {
				t.Fatalf("%s doc missing evidence-artifact guidance %q", name, needle)
			}
		}
	}
})

var _ = testkit.Spec("TestEmbeddedAgents_EvidenceArtifactContract", func(t testkit.T) {
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
				"observation_evidence_failures",
				"observation_read_issues",
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
})

var _ = testkit.Spec("TestEmbeddedAgents_GateReviewerStatsAuthorityContract", func(t testkit.T) {
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
		var content []byte
		for _, a := range chk.agents {
			if a.Name == "research-gate-reviewer" {
				content = a.Content
				break
			}
		}
		if len(content) == 0 {
			t.Fatalf("%s research-gate-reviewer agent missing", chk.label)
		}
		for _, needle := range []string{
			"Treat `autoresearch analyze` as the authoritative stats source.",
			"--candidate-ref <candidate-ref>",
			"Do not spend tokens re-coding",
			"bootstrap CI or Mann-Whitney U",
			"Inspect the raw samples for sanity",
			"measured candidate provenance (`candidate_ref`, `candidate_sha`)",
			"git show <candidate-ref>",
			"git rev-parse <candidate-ref>",
		} {
			if !bytes.Contains(content, []byte(needle)) {
				t.Fatalf("%s research-gate-reviewer missing %q", chk.label, needle)
			}
		}
		if bytes.Contains(content, []byte("Recompute the stats yourself:")) {
			t.Fatalf("%s research-gate-reviewer still tells reviewers to recompute stats", chk.label)
		}
		if bytes.Contains(content, []byte("git show autoresearch/<candidate-exp-id>")) {
			t.Fatalf("%s research-gate-reviewer still inspects mutable experiment branches", chk.label)
		}
	}
})
