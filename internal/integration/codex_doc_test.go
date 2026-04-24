package integration_test

import (
	"strings"

	"github.com/bytter/autoresearch/internal/integration"
	"github.com/bytter/autoresearch/internal/testkit"
)

var _ = testkit.Spec("TestCodexDoc_RewrittenForCodex", func(t testkit.T) {
	doc := integration.CodexDoc("vtest")
	if !strings.Contains(doc, ".codex/autoresearch.md") {
		t.Fatal("codex doc should reference .codex/autoresearch.md")
	}
	if strings.Contains(doc, "@.codex/autoresearch.md") {
		t.Fatal("codex doc should use plain .codex/autoresearch.md paths, not @ mention syntax")
	}
	if strings.Contains(doc, ".claude/") {
		t.Error("codex doc should not reference .claude/ anywhere after rewrite")
	}
	if !strings.Contains(doc, "AGENTS.md") {
		t.Fatal("codex doc should reference AGENTS.md")
	}
	if strings.Contains(doc, "CLAUDE.md") {
		t.Fatal("codex doc should not reference CLAUDE.md")
	}
	if !strings.Contains(doc, "autoresearch install codex") {
		t.Fatal("codex doc should reference autoresearch install codex")
	}
	if !strings.Contains(doc, "treats Codex as an agentic researcher") {
		t.Fatal("codex doc should mention Codex in the intro")
	}
	if !strings.Contains(doc, ".codex/agents/research-orchestrator.toml") {
		t.Fatal("codex doc should reference .codex/agents/research-orchestrator.toml")
	}
	if strings.Contains(doc, ".codex/agents/research-orchestrator.md") {
		t.Fatal("codex doc should not reference legacy .md codex agents")
	}

	// Notebook layer must propagate through the rewriter unchanged so Codex
	// agents see the same rationale flags and lesson verbs as Claude agents.
	// If a future change to ClaudeDoc breaks propagation (e.g. renames a
	// section or a verb in a way the rewriter mangles), this test is the
	// canary.
	notebookInvariants := []string{
		"## The notebook layer",
		"autoresearch lesson add",
		"autoresearch lesson list",
		"autoresearch lesson supersede",
		"hypothesis add --rationale",
		"experiment design --design-notes",
		"experiment implement --impl-notes",
		"What have we learned so far?",
		// Body-structure guidance must survive the rewriter and appear
		// in the Codex reference doc so agents writing lessons have a
		// top-level reminder of the required sections.
		"`--body`",
		"## Evidence",
		"## Mechanism",
		"## Scope and counterexamples",
		"## For the next generator",
		"## Core cycle cheat sheet",
		"primary contract for routine autoresearch work",
		"`<verb> --help` only when",
		"Limits are ceilings, not quotas",
		"provisional until gate review",
		"do not start another cycle while the chain is",
		"parent/main session owns the next handoff",
		"do not nest another orchestrator",
	}
	for _, s := range notebookInvariants {
		if !strings.Contains(doc, s) {
			t.Errorf("codex doc missing notebook-layer content: %q", s)
		}
	}
})
