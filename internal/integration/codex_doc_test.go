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
	if strings.Contains(doc, ".claude/autoresearch.md") {
		t.Fatal("codex doc should not reference .claude/autoresearch.md")
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
}
