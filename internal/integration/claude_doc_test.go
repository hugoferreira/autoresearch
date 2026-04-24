package integration_test

import (
	"strings"

	"github.com/bytter/autoresearch/internal/integration"
	"github.com/bytter/autoresearch/internal/testkit"
)

var _ = testkit.Spec("TestSharedDocs_CommandSpineUsesParentReviewHandoff", func(t testkit.T) {
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
})

var _ = testkit.Spec("TestSharedDocs_MainCheckoutIsolationContract", func(t testkit.T) {
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
			"harness changes belong in experiment",
		} {
			if !strings.Contains(doc, needle) {
				t.Fatalf("%s doc missing main-checkout isolation guidance %q", name, needle)
			}
		}
	}
})

var _ = testkit.Spec("TestSharedDocs_LessonScopeGuidance", func(t testkit.T) {
	claudeDoc := integration.ClaudeDoc("vtest")
	codexDoc := integration.CodexDoc("vtest")

	for name, doc := range map[string]string{
		"claude": claudeDoc,
		"codex":  codexDoc,
	} {
		for _, needle := range []string{
			"Choosing lesson scope",
			"If unsure, choose `scope: hypothesis`",
			"target-wide invariants",
			"measurement caveats",
			"A local lesson misclassified",
			"unrelated goals",
		} {
			if !strings.Contains(doc, needle) {
				t.Fatalf("%s doc missing lesson-scope guidance %q", name, needle)
			}
		}
	}
})
