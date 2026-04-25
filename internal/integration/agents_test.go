package integration_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/bytter/autoresearch/internal/integration"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

var _ = Describe("Claude embedded agents", func() {
	It("embeds the orchestrator and gate reviewer", func() {
		agents := mustEmbeddedAgents()
		Expect(agentContentByName(agents)).To(HaveKey("research-orchestrator"))
		Expect(agentContentByName(agents)).To(HaveKey("research-gate-reviewer"))
		Expect(agents).To(HaveLen(2))
	})

	It("documents runtime shell discipline", func() {
		for _, a := range mustEmbeddedAgents() {
			content := string(a.Content)
			for _, needle := range shellDisciplineNeedles() {
				Expect(content).To(ContainSubstring(needle), "claude %s brief", a.Name)
			}
		}
	})

	It("has valid frontmatter and role-appropriate tool permissions", func() {
		for _, a := range mustEmbeddedAgents() {
			Expect(a.Content).NotTo(BeEmpty(), a.Name)
			Expect(string(a.Content)).To(HavePrefix("---\n"), a.Name)

			rest := a.Content[4:]
			end := bytes.Index(rest, []byte("\n---\n"))
			Expect(end).To(BeNumerically(">=", 0), a.Name)
			var fm struct {
				Name        string `yaml:"name"`
				Description string `yaml:"description"`
				Tools       string `yaml:"tools"`
			}
			Expect(yaml.Unmarshal(rest[:end], &fm)).To(Succeed(), a.Name)
			Expect(fm.Name).To(Equal(a.Name))
			desc := strings.ToLower(fm.Description)
			Expect(desc).To(Or(HavePrefix("use when"), HavePrefix("use after"), HavePrefix("use to")), a.Name)
			Expect(fm.Tools).NotTo(BeEmpty())

			hasEdit := strings.Contains(fm.Tools, "Edit") || strings.Contains(fm.Tools, "Write")
			switch a.Name {
			case "research-orchestrator":
				Expect(hasEdit).To(BeTrue())
			case "research-gate-reviewer":
				Expect(hasEdit).To(BeFalse())
			}
			body := rest[end+5:]
			Expect(string(body)).To(ContainSubstring(".claude/autoresearch.md"))
		}
	})
})

var _ = Describe("Claude agent installation", func() {
	It("writes all managed agent files", func() {
		dir := GinkgoT().TempDir()
		r, err := integration.InstallAgents(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Count).To(Equal(2))
		for _, fn := range r.Written {
			Expect(filepath.Join(dir, ".claude", "agents", fn)).To(BeAnExistingFile())
		}
	})

	It("is idempotent and preserves sibling user files", func() {
		dir := GinkgoT().TempDir()
		_, err := integration.InstallAgents(dir)
		Expect(err).NotTo(HaveOccurred())

		customPath := filepath.Join(dir, ".claude", "agents", "my-custom-agent.md")
		Expect(os.WriteFile(customPath, []byte("custom"), 0o644)).To(Succeed())
		_, err = integration.InstallAgents(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(readFile(customPath)).To(Equal("custom"))
	})
})

func mustEmbeddedAgents() []integration.AgentFile {
	GinkgoHelper()
	agents, err := integration.EmbeddedAgents()
	Expect(err).NotTo(HaveOccurred())
	return agents
}

func mustEmbeddedCodexAgents() []integration.AgentFile {
	GinkgoHelper()
	agents, err := integration.EmbeddedCodexAgents()
	Expect(err).NotTo(HaveOccurred())
	return agents
}

func shellDisciplineNeedles() []string {
	return []string{
		"not a shell contract",
		"Do not assume bash",
		`[ "$x" = y ]`,
		`[ $x == y ]`,
		`printf 'SHELL=%s argv0=%s\n'`,
		`ps -p $$ -o comm=`,
		`[[ "$x" == y ]]`,
	}
}

func agentContentByName(agents []integration.AgentFile) map[string]string {
	out := map[string]string{}
	for _, a := range agents {
		out[a.Name] = string(a.Content)
	}
	return out
}
