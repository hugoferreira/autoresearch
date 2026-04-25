package integration_test

import (
	"os"
	"path/filepath"

	"github.com/bytter/autoresearch/internal/integration"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Codex AGENTS.md instructions", func() {
	It("creates the managed block with the current research guidance", func() {
		dir := GinkgoT().TempDir()
		r, err := integration.EnsureCodexInstructions(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Created).To(BeTrue())
		Expect(r.Added).To(BeFalse())
		Expect(r.Updated).To(BeFalse())
		Expect(r.AlreadyOK).To(BeFalse())

		body, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
		Expect(err).NotTo(HaveOccurred())
		text := string(body)
		for _, needle := range []string{
			".codex/autoresearch.md",
			"spawn_agent",
			".codex/agents/research-orchestrator.toml",
			"Budgets are caps, not quotas",
			"review pending",
			"--inspired-by",
			"Choose lesson scope conservatively",
			"measurement caveats",
			"prefer hypothesis scope",
			"Do not spend early turns probing `--help`",
			"autoresearch cycle-context --json",
			"active_lessons",
			"exact `agent_type` name",
			"Do not emulate those roles by spawning `explorer`",
			"one-cycle leaf role",
			"parent/main session owns the next handoff",
			"nested `spawn_agent` / `send_input` / `wait_agent`",
			"main checkout as read-only during research",
			"main_checkout_dirty_paths",
			"bootstrap/harness files",
		} {
			Expect(text).To(ContainSubstring(needle))
		}
	})

	It("appends the managed block without rewriting user content", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "AGENTS.md")
		const pre = "# Team Notes\n\nKeep tests deterministic.\n"
		Expect(os.WriteFile(path, []byte(pre), 0o644)).To(Succeed())

		r, err := integration.EnsureCodexInstructions(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Added).To(BeTrue())
		body, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(body)).To(HavePrefix(pre))
	})

	It("is idempotent once the managed block is current", func() {
		dir := GinkgoT().TempDir()
		_, err := integration.EnsureCodexInstructions(dir)
		Expect(err).NotTo(HaveOccurred())
		r, err := integration.EnsureCodexInstructions(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.AlreadyOK).To(BeTrue())
	})

	It("previews creation without writing AGENTS.md", func() {
		dir := GinkgoT().TempDir()
		r, err := integration.PreviewCodexInstructions(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Created).To(BeTrue())
		_, err = os.Stat(filepath.Join(dir, "AGENTS.md"))
		Expect(os.IsNotExist(err)).To(BeTrue())
	})
})
