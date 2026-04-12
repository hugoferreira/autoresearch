package integration_test

import (
	"strings"
	"testing"

	"github.com/bytter/autoresearch/internal/integration"
)

func TestCodexDoc_RewrittenForCodex(t *testing.T) {
	doc := integration.CodexDoc("vtest")
	if !strings.Contains(doc, ".codex/autoresearch.md") {
		t.Fatal("codex doc should reference .codex/autoresearch.md")
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
	if !strings.Contains(doc, "autoresearch codex install") {
		t.Fatal("codex doc should reference autoresearch codex install")
	}
	if !strings.Contains(doc, "treats Codex as an agentic researcher") {
		t.Fatal("codex doc should mention Codex in the intro")
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
	}
	for _, s := range notebookInvariants {
		if !strings.Contains(doc, s) {
			t.Errorf("codex doc missing notebook-layer content: %q", s)
		}
	}
}
