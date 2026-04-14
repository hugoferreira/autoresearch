package integration_test

import (
	"strings"
	"testing"

	"github.com/bytter/autoresearch/internal/integration"
)

func TestSharedDocs_CommandSpineUsesParentReviewHandoff(t *testing.T) {
	claudeDoc := integration.ClaudeDoc("vtest")
	codexDoc := integration.CodexDoc("vtest")

	for name, doc := range map[string]string{
		"claude": claudeDoc,
		"codex":  codexDoc,
	} {
		if strings.Contains(doc, "autoresearch conclusion accept <C-id> ...  # or downgrade, before the next cycle") {
			t.Fatalf("%s doc still advertises same-cycle review resolution in the canonical command spine", name)
		}
		if !strings.Contains(doc, "yield with review pending") {
			t.Fatalf("%s doc missing review-pending handoff in the canonical command spine", name)
		}
	}
}

func TestSharedDocs_MainCheckoutIsolationContract(t *testing.T) {
	claudeDoc := integration.ClaudeDoc("vtest")
	codexDoc := integration.CodexDoc("vtest")

	for name, doc := range map[string]string{
		"claude": claudeDoc,
		"codex":  codexDoc,
	} {
		for _, needle := range []string{
			"main checkout",
			"main_checkout_dirty",
			"main_checkout_dirty_paths",
			"experiment and harness changes belong in experiment worktrees",
		} {
			if !strings.Contains(doc, needle) {
				t.Fatalf("%s doc missing main-checkout isolation guidance %q", name, needle)
			}
		}
	}
}
