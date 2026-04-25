package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/bytter/autoresearch/internal/integration"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func readSettings(path string) map[string]any {
	GinkgoHelper()
	b, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	var m map[string]any
	Expect(json.Unmarshal(b, &m)).To(Succeed(), "parse %s:\n%s", path, string(b))
	return m
}

func allowList(doc map[string]any) []string {
	GinkgoHelper()
	perms, ok := doc["permissions"].(map[string]any)
	Expect(ok).To(BeTrue(), "permissions key")
	raw, ok := perms["allow"].([]any)
	Expect(ok).To(BeTrue(), "allow list")
	out := make([]string, len(raw))
	for i, v := range raw {
		s, ok := v.(string)
		Expect(ok).To(BeTrue(), "allow[%d]", i)
		out[i] = s
	}
	return out
}

func denyList(doc map[string]any) []string {
	GinkgoHelper()
	perms, ok := doc["permissions"].(map[string]any)
	Expect(ok).To(BeTrue(), "permissions key")
	raw, ok := perms["deny"].([]any)
	Expect(ok).To(BeTrue(), "deny list")
	out := make([]string, len(raw))
	for i, v := range raw {
		s, ok := v.(string)
		Expect(ok).To(BeTrue(), "deny[%d]", i)
		out[i] = s
	}
	return out
}

var _ = Describe("Claude settings permissions", func() {
	It("creates settings.json with the autoresearch allow entry", func() {
		dir := GinkgoT().TempDir()
		r, err := integration.EnsureClaudeSettings(dir, []string{integration.AutoresearchAllowEntry})
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Created).To(BeTrue())
		Expect(r.Updated).To(BeFalse())
		Expect(r.AlreadyOK).To(BeFalse())
		Expect(allowList(readSettings(r.Path))).To(Equal([]string{integration.AutoresearchAllowEntry}))
	})

	It("creates settings.json with managed allow and deny entries", func() {
		dir := GinkgoT().TempDir()
		r, err := integration.EnsureClaudeSettingsPermissions(
			dir,
			[]string{integration.AutoresearchAllowEntry},
			[]string{integration.ClaudeHarnessToolResultsDenyEntry},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Created).To(BeTrue())
		Expect(r.AddedAllow).To(Equal([]string{integration.AutoresearchAllowEntry}))
		Expect(r.AddedDeny).To(Equal([]string{integration.ClaudeHarnessToolResultsDenyEntry}))
		doc := readSettings(r.Path)
		Expect(allowList(doc)).To(Equal([]string{integration.AutoresearchAllowEntry}))
		Expect(denyList(doc)).To(Equal([]string{integration.ClaudeHarnessToolResultsDenyEntry}))
	})

	It("adds missing allow entries while preserving unrelated settings", func() {
		dir := GinkgoT().TempDir()
		settingsPath := filepath.Join(dir, ".claude", "settings.json")
		Expect(os.MkdirAll(filepath.Dir(settingsPath), 0o755)).To(Succeed())
		const pre = `{
  "permissions": {
    "allow": ["Bash(git status:*)", "Bash(go test:*)"],
    "deny": ["Bash(rm -rf:*)"]
  },
  "otherKey": "preserved"
}`
		Expect(os.WriteFile(settingsPath, []byte(pre), 0o644)).To(Succeed())

		r, err := integration.EnsureClaudeSettings(dir, []string{integration.AutoresearchAllowEntry})
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Updated).To(BeTrue())
		doc := readSettings(settingsPath)
		Expect(allowList(doc)).To(ConsistOf(
			"Bash(git status:*)",
			"Bash(go test:*)",
			integration.AutoresearchAllowEntry,
		))
		Expect(doc["otherKey"]).To(Equal("preserved"))
		perms := doc["permissions"].(map[string]any)
		Expect(perms["deny"]).To(Equal([]any{"Bash(rm -rf:*)"}))
	})

	It("adds missing deny entries while preserving unrelated settings", func() {
		dir := GinkgoT().TempDir()
		settingsPath := filepath.Join(dir, ".claude", "settings.json")
		Expect(os.MkdirAll(filepath.Dir(settingsPath), 0o755)).To(Succeed())
		const pre = `{
  "permissions": {
    "allow": ["Bash(git status:*)"],
    "deny": ["Bash(rm -rf:*)"]
  },
  "otherKey": "preserved"
}`
		Expect(os.WriteFile(settingsPath, []byte(pre), 0o644)).To(Succeed())

		r, err := integration.EnsureClaudeSettingsPermissions(
			dir,
			[]string{integration.AutoresearchAllowEntry},
			[]string{integration.ClaudeHarnessToolResultsDenyEntry},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Updated).To(BeTrue())
		Expect(r.AddedAllow).To(Equal([]string{integration.AutoresearchAllowEntry}))
		Expect(r.AddedDeny).To(Equal([]string{integration.ClaudeHarnessToolResultsDenyEntry}))
		doc := readSettings(settingsPath)
		Expect(allowList(doc)).To(ConsistOf("Bash(git status:*)", integration.AutoresearchAllowEntry))
		Expect(denyList(doc)).To(ConsistOf("Bash(rm -rf:*)", integration.ClaudeHarnessToolResultsDenyEntry))
		Expect(doc["otherKey"]).To(Equal("preserved"))
	})

	It("is idempotent once all entries are present", func() {
		dir := GinkgoT().TempDir()
		entries := []string{integration.AutoresearchAllowEntry}
		_, err := integration.EnsureClaudeSettings(dir, entries)
		Expect(err).NotTo(HaveOccurred())
		r, err := integration.EnsureClaudeSettings(dir, entries)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.AlreadyOK).To(BeTrue())
		Expect(r.Added).To(BeEmpty())
	})

	It("is idempotent once all managed allow and deny entries are present", func() {
		dir := GinkgoT().TempDir()
		allowEntries := []string{integration.AutoresearchAllowEntry}
		denyEntries := []string{integration.ClaudeHarnessToolResultsDenyEntry}
		_, err := integration.EnsureClaudeSettingsPermissions(dir, allowEntries, denyEntries)
		Expect(err).NotTo(HaveOccurred())
		r, err := integration.EnsureClaudeSettingsPermissions(dir, allowEntries, denyEntries)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.AlreadyOK).To(BeTrue())
		Expect(r.Added).To(BeEmpty())
		Expect(r.AddedAllow).To(BeEmpty())
		Expect(r.AddedDeny).To(BeEmpty())
	})

	It("creates permissions under settings files without a permissions key", func() {
		dir := GinkgoT().TempDir()
		settingsPath := filepath.Join(dir, ".claude", "settings.json")
		Expect(os.MkdirAll(filepath.Dir(settingsPath), 0o755)).To(Succeed())
		Expect(os.WriteFile(settingsPath, []byte(`{"env": {"FOO": "bar"}}`), 0o644)).To(Succeed())

		r, err := integration.EnsureClaudeSettings(dir, []string{integration.AutoresearchAllowEntry})
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Updated).To(BeTrue())
		doc := readSettings(settingsPath)
		Expect(allowList(doc)).To(Equal([]string{integration.AutoresearchAllowEntry}))
		env, ok := doc["env"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(env["FOO"]).To(Equal("bar"))
	})

	It("rejects invalid settings JSON", func() {
		dir := GinkgoT().TempDir()
		settingsPath := filepath.Join(dir, ".claude", "settings.json")
		Expect(os.MkdirAll(filepath.Dir(settingsPath), 0o755)).To(Succeed())
		Expect(os.WriteFile(settingsPath, []byte(`{ not json`), 0o644)).To(Succeed())

		_, err := integration.EnsureClaudeSettings(dir, []string{integration.AutoresearchAllowEntry})
		Expect(err).To(HaveOccurred())
	})

	It("formats absolute worktree allow entries with double-slash paths", func() {
		got := integration.WorktreeAllowEntries("/Users/bob/Library/Caches/autoresearch/proj-abc/worktrees")
		Expect(got).To(Equal([]string{
			"Read(//Users/bob/Library/Caches/autoresearch/proj-abc/worktrees/**)",
			"Edit(//Users/bob/Library/Caches/autoresearch/proj-abc/worktrees/**)",
			"Write(//Users/bob/Library/Caches/autoresearch/proj-abc/worktrees/**)",
		}))
	})

	It("previews creation and already-ok states without writing missing settings", func() {
		dir := GinkgoT().TempDir()
		entries := []string{integration.AutoresearchAllowEntry}

		r, err := integration.PreviewClaudeSettings(dir, entries)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Created).To(BeTrue())
		_, err = os.Stat(filepath.Join(dir, ".claude", "settings.json"))
		Expect(os.IsNotExist(err)).To(BeTrue())

		_, err = integration.EnsureClaudeSettings(dir, entries)
		Expect(err).NotTo(HaveOccurred())
		r, err = integration.PreviewClaudeSettings(dir, entries)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.AlreadyOK).To(BeTrue())
	})

	It("previews managed deny entries without writing settings", func() {
		dir := GinkgoT().TempDir()
		r, err := integration.PreviewClaudeSettingsPermissions(
			dir,
			[]string{integration.AutoresearchAllowEntry},
			[]string{integration.ClaudeHarnessToolResultsDenyEntry},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Created).To(BeTrue())
		Expect(r.AddedAllow).To(Equal([]string{integration.AutoresearchAllowEntry}))
		Expect(r.AddedDeny).To(Equal([]string{integration.ClaudeHarnessToolResultsDenyEntry}))
		_, err = os.Stat(filepath.Join(dir, ".claude", "settings.json"))
		Expect(os.IsNotExist(err)).To(BeTrue())
	})
})
