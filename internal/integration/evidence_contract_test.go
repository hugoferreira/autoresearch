package integration_test

import (
	"github.com/bytter/autoresearch/internal/integration"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("evidence artifact contracts", func() {
	It("documents observation artifacts, evidence failures, and read issues", func() {
		for name, doc := range sharedDocs() {
			for _, needle := range []string{
				"--evidence name=cmd",
				"observation_artifacts",
				"observation_evidence_failures",
				"observation_read_issues",
				"evidence/<name>",
				"observation.evidence_failures",
			} {
				Expect(doc).To(ContainSubstring(needle), "%s doc", name)
			}
		}
	})

	It("teaches both embedded agent families the evidence artifact contract", func() {
		checks := []struct {
			label  string
			agents []integration.AgentFile
		}{
			{label: "claude", agents: mustEmbeddedAgents()},
			{label: "codex", agents: mustEmbeddedCodexAgents()},
		}

		for _, chk := range checks {
			byName := agentContentByName(chk.agents)
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
				Expect(ok).To(BeTrue(), "%s %s agent missing", chk.label, role)
				for _, needle := range needles {
					Expect(content).To(ContainSubstring(needle), "%s %s agent", chk.label, role)
				}
			}
		}
	})

	It("keeps statistics authority with autoresearch analyze in both gate-reviewer agents", func() {
		for _, chk := range []struct {
			label  string
			agents []integration.AgentFile
		}{
			{label: "claude", agents: mustEmbeddedAgents()},
			{label: "codex", agents: mustEmbeddedCodexAgents()},
		} {
			content := agentContentByName(chk.agents)["research-gate-reviewer"]
			Expect(content).NotTo(BeEmpty(), "%s research-gate-reviewer", chk.label)
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
				Expect(content).To(ContainSubstring(needle), "%s research-gate-reviewer", chk.label)
			}
			Expect(content).NotTo(ContainSubstring("Recompute the stats yourself:"))
			Expect(content).NotTo(ContainSubstring("git show autoresearch/<candidate-exp-id>"))
		}
	})
})
