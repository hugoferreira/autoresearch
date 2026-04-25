package integration_test

import (
	"github.com/bytter/autoresearch/internal/integration"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Codex reference doc", func() {
	It("rewrites Claude-specific paths and install commands for Codex", func() {
		doc := integration.CodexDoc("vtest")
		Expect(doc).To(ContainSubstring(".codex/autoresearch.md"))
		Expect(doc).NotTo(ContainSubstring("@.codex/autoresearch.md"))
		Expect(doc).NotTo(ContainSubstring(".claude/"))
		Expect(doc).To(ContainSubstring("AGENTS.md"))
		Expect(doc).NotTo(ContainSubstring("CLAUDE.md"))
		Expect(doc).To(ContainSubstring("autoresearch install codex"))
		Expect(doc).To(ContainSubstring("treats Codex as an agentic researcher"))
		Expect(doc).To(ContainSubstring(".codex/agents/research-orchestrator.toml"))
		Expect(doc).NotTo(ContainSubstring(".codex/agents/research-orchestrator.md"))
	})

	It("preserves notebook-layer guidance through the rewriter", func() {
		doc := integration.CodexDoc("vtest")
		for _, needle := range []string{
			"## The notebook layer",
			"autoresearch lesson add",
			"autoresearch lesson list",
			"autoresearch lesson supersede",
			"hypothesis add --rationale",
			"experiment design --design-notes",
			"experiment implement --impl-notes",
			"What have we learned so far?",
			"`--body`",
			"## Evidence",
			"## Mechanism",
			"## Scope and counterexamples",
			"## For the next generator",
			"## Core cycle cheat sheet",
			"autoresearch cycle-context --json",
			"primary contract for routine autoresearch work",
			"`<verb> --help` only when",
			"Limits are ceilings, not quotas",
			"provisional until gate review",
			"do not start another cycle while the chain is",
			"parent/main session owns the next handoff",
			"do not nest another orchestrator",
		} {
			Expect(doc).To(ContainSubstring(needle))
		}
	})
})
