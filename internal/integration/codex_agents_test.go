package integration_test

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bytter/autoresearch/internal/integration"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Codex embedded agents", func() {
	It("rewrites Claude agent briefs into Codex TOML agents", func() {
		agents := mustEmbeddedCodexAgents()
		Expect(agents).To(HaveLen(2))
		for _, a := range agents {
			content := string(a.Content)
			Expect(a.Filename).To(HaveSuffix(".toml"), a.Name)
			Expect(content).To(ContainSubstring(".codex/autoresearch.md"), a.Name)
			Expect(content).NotTo(ContainSubstring(".claude/autoresearch.md"), a.Name)
			Expect(content).NotTo(ContainSubstring("@.codex/autoresearch.md"), a.Name)
			Expect(content).To(ContainSubstring("autoresearch install codex agents"), a.Name)
			Expect(content).To(ContainSubstring("name = \""+a.Name+"\""), a.Name)
			Expect(content).To(ContainSubstring("description = "), a.Name)
			Expect(content).To(ContainSubstring("developer_instructions = '''"), a.Name)
		}
	})

	It("propagates notebook and role-boundary guidance", func() {
		byName := agentContentByName(mustEmbeddedCodexAgents())
		for role, needles := range map[string][]string{
			"research-orchestrator": {
				"--rationale",
				"--design-notes",
				"--impl-notes",
				"--interpretation",
				"autoresearch cycle-context --json",
				"active_lessons",
				"autoresearch lesson add",
				"## Evidence",
				"## Mechanism",
				"## Scope and counterexamples",
				"## For the next generator",
				"burst of\n`--help`",
				"### Command spine",
				"provisional",
				"review pending",
				"ceiling, not",
				"sandbox_mode = \"workspace-write\"",
				"Do not spawn another `research-orchestrator`",
				"dispatch research-gate-reviewer on C-NNNN",
				"nested child sessions expose `spawn_agent`, `send_input`, `wait_agent`",
				"main checkout cleanliness",
				"main_checkout_dirty_paths",
				"bootstrap scripts",
			},
			"research-gate-reviewer": {
				"autoresearch lesson add",
				"conclusion downgrade",
				"repetitive `--help` lookups",
				"sandbox_mode = \"read-only\"",
				"leaf autoresearch role",
			},
		} {
			content, ok := byName[role]
			Expect(ok).To(BeTrue(), "missing %s", role)
			for _, needle := range needles {
				Expect(content).To(ContainSubstring(needle), "codex %s brief", role)
			}
		}
	})

	It("propagates runtime shell discipline", func() {
		for _, a := range mustEmbeddedCodexAgents() {
			content := string(a.Content)
			for _, needle := range shellDisciplineNeedles() {
				Expect(content).To(ContainSubstring(needle), "codex %s brief", a.Name)
			}
		}
	})

	It("propagates runtime harness-cache discipline", func() {
		for _, a := range mustEmbeddedCodexAgents() {
			content := string(a.Content)
			for _, needle := range harnessCacheNeedles() {
				Expect(content).To(ContainSubstring(needle), "codex %s brief", a.Name)
			}
		}
	})

	It("keeps delegation handoff responsibility with the parent session", func() {
		orchestrator := agentContentByName(mustEmbeddedCodexAgents())["research-orchestrator"]
		Expect(orchestrator).NotTo(BeEmpty())
		for _, needle := range []string{
			"one full hypothesis cycle",
			"Do not spawn another `research-orchestrator`",
			"Do **not** dispatch `research-gate-reviewer` yourself from this role.",
			"return to the parent/main session with an explicit handoff",
		} {
			Expect(orchestrator).To(ContainSubstring(needle))
		}
	})
})

var _ = Describe("Codex agent installation", func() {
	It("writes all managed TOML agent files", func() {
		dir := GinkgoT().TempDir()
		r, err := integration.InstallCodexAgents(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Count).To(Equal(2))
		for _, fn := range r.Written {
			Expect(filepath.Join(dir, ".codex", "agents", fn)).To(BeAnExistingFile())
		}
	})

	It("removes legacy markdown agents", func() {
		dir := GinkgoT().TempDir()
		agentsDir := filepath.Join(dir, ".codex", "agents")
		Expect(os.MkdirAll(agentsDir, 0o755)).To(Succeed())
		for _, name := range []string{"research-orchestrator.md", "research-gate-reviewer.md"} {
			Expect(os.WriteFile(filepath.Join(agentsDir, name), []byte("legacy"), 0o644)).To(Succeed())
		}

		_, err := integration.InstallCodexAgents(dir)
		Expect(err).NotTo(HaveOccurred())
		for _, name := range []string{"research-orchestrator.md", "research-gate-reviewer.md"} {
			_, err := os.Stat(filepath.Join(agentsDir, name))
			Expect(os.IsNotExist(err)).To(BeTrue(), name)
		}
	})

	It("preserves sibling user TOML files", func() {
		dir := GinkgoT().TempDir()
		agentsDir := filepath.Join(dir, ".codex", "agents")
		Expect(os.MkdirAll(agentsDir, 0o755)).To(Succeed())
		customPath := filepath.Join(agentsDir, "my-custom-agent.toml")
		Expect(os.WriteFile(customPath, []byte("custom"), 0o644)).To(Succeed())

		_, err := integration.InstallCodexAgents(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(readFile(customPath))).To(Equal("custom"))
	})
})
