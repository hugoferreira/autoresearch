package integration_test

import (
	"github.com/bytter/autoresearch/internal/integration"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("shared generated agent docs", func() {
	It("uses parent-session handoff instead of same-cycle review resolution", func() {
		for name, doc := range sharedDocs() {
			Expect(doc).NotTo(ContainSubstring("autoresearch conclusion accept <C-id> ...  # or downgrade, before the next cycle"), name)
			Expect(doc).To(ContainSubstring("yield with review pending"), name)
		}
	})

	It("documents main checkout isolation", func() {
		for name, doc := range sharedDocs() {
			Expect(doc).To(ContainSubstring("main checkout"), name)
			Expect(doc).To(ContainSubstring("main_checkout_dirty"), name)
			Expect(doc).To(ContainSubstring("main_checkout_dirty_paths"), name)
			Expect(doc).To(ContainSubstring("harness changes belong in experiment"), name)
		}
	})

	It("documents conservative lesson scope selection", func() {
		for name, doc := range sharedDocs() {
			for _, needle := range []string{
				"Choosing lesson scope",
				"If unsure, choose `scope: hypothesis`",
				"target-wide invariants",
				"measurement caveats",
				"A local lesson misclassified",
				"unrelated goals",
			} {
				Expect(doc).To(ContainSubstring(needle), "%s doc", name)
			}
		}
	})
})

func sharedDocs() map[string]string {
	return map[string]string{
		"claude": integration.ClaudeDoc("vtest"),
		"codex":  integration.CodexDoc("vtest"),
	}
}
