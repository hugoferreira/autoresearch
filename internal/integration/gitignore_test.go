package integration_test

import (
	"os"
	"path/filepath"

	"github.com/bytter/autoresearch/internal/integration"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe(".gitignore integration", func() {
	It("creates a missing .gitignore with the managed line", func() {
		dir := GinkgoT().TempDir()
		r, err := integration.EnsureGitignoreLine(dir, ".research/")
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Created).To(BeTrue())
		Expect(r.Added).To(BeFalse())
		Expect(r.AlreadyPresent).To(BeFalse())
		Expect(readFile(filepath.Join(dir, ".gitignore"))).To(Equal(".research/\n"))
	})

	It("appends after an existing trailing newline", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, ".gitignore")
		const pre = "node_modules/\n*.log\n"
		Expect(os.WriteFile(path, []byte(pre), 0o644)).To(Succeed())

		r, err := integration.EnsureGitignoreLine(dir, ".research/")
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Added).To(BeTrue())
		Expect(readFile(path)).To(Equal(pre + ".research/\n"))
	})

	It("adds a separator newline before appending to files without one", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, ".gitignore")
		Expect(os.WriteFile(path, []byte("node_modules/"), 0o644)).To(Succeed())

		r, err := integration.EnsureGitignoreLine(dir, ".research/")
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Added).To(BeTrue())
		Expect(readFile(path)).To(Equal("node_modules/\n.research/\n"))
	})

	It("leaves existing exact and whitespace-trimmed matches untouched", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, ".gitignore")
		const pre = "node_modules/\n.research/\n*.log\n"
		Expect(os.WriteFile(path, []byte(pre), 0o644)).To(Succeed())

		r, err := integration.EnsureGitignoreLine(dir, ".research/")
		Expect(err).NotTo(HaveOccurred())
		Expect(r.AlreadyPresent).To(BeTrue())
		Expect(readFile(path)).To(Equal(pre))

		dir = GinkgoT().TempDir()
		path = filepath.Join(dir, ".gitignore")
		Expect(os.WriteFile(path, []byte("  .research/  \n"), 0o644)).To(Succeed())
		r, err = integration.EnsureGitignoreLine(dir, ".research/")
		Expect(err).NotTo(HaveOccurred())
		Expect(r.AlreadyPresent).To(BeTrue())
	})

	It("previews create, already-present, and append outcomes without writing", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, ".gitignore")

		r, err := integration.PreviewGitignoreLine(dir, ".research/")
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Created).To(BeTrue())
		_, err = os.Stat(path)
		Expect(os.IsNotExist(err)).To(BeTrue())

		Expect(os.WriteFile(path, []byte(".research/\n"), 0o644)).To(Succeed())
		r, err = integration.PreviewGitignoreLine(dir, ".research/")
		Expect(err).NotTo(HaveOccurred())
		Expect(r.AlreadyPresent).To(BeTrue())

		Expect(os.WriteFile(path, []byte("*.log\n"), 0o644)).To(Succeed())
		r, err = integration.PreviewGitignoreLine(dir, ".research/")
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Added).To(BeTrue())
	})

	It("rejects managed lines containing newlines", func() {
		_, err := integration.EnsureGitignoreLine(GinkgoT().TempDir(), "foo\nbar")
		Expect(err).To(HaveOccurred())
	})
})

func readFile(path string) string {
	GinkgoHelper()
	b, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	return string(b)
}
