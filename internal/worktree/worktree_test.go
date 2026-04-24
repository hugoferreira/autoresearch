package worktree_test

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bytter/autoresearch/internal/worktree"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func gitInit() string {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		runGit(dir, args...)
	}
	Expect(os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644)).To(Succeed())
	runGit(dir, "add", "README.md")
	runGit(dir, "commit", "-m", "init")
	return dir
}

func gitCommit(dir, file, body, msg string) {
	GinkgoHelper()
	Expect(os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644)).To(Succeed())
	runGit(dir, "add", file)
	runGit(dir, "commit", "-m", msg)
}

func runGit(dir string, args ...string) {
	GinkgoHelper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "git %v failed:\n%s", args, out)
}

var _ = Describe("git worktree integration", func() {
	It("detects git repositories", func() {
		dir := gitInit()
		Expect(worktree.IsRepo(dir)).To(BeTrue())
		Expect(worktree.IsRepo(GinkgoT().TempDir())).To(BeFalse())
	})

	It("adds and removes experiment worktrees at a resolved baseline SHA", func() {
		dir := gitInit()
		sha, err := worktree.ResolveRef(dir, "HEAD")
		Expect(err).NotTo(HaveOccurred())
		Expect(sha).To(HaveLen(40))

		wtPath := filepath.Join(dir, ".research", "worktrees", "E-0001")
		Expect(os.MkdirAll(filepath.Dir(wtPath), 0o755)).To(Succeed())
		Expect(worktree.Add(dir, wtPath, "autoresearch/E-0001", sha)).To(Succeed())
		Expect(filepath.Join(wtPath, "README.md")).To(BeAnExistingFile())

		Expect(worktree.Remove(dir, wtPath)).To(Succeed())
		Expect(wtPath).NotTo(BeAnExistingFile())
	})

	It("resolves branch names and HEAD to full symbolic refs", func() {
		dir := gitInit()

		got, err := worktree.SymbolicFullName(dir, "main")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("refs/heads/main"))

		got, err = worktree.SymbolicFullName(dir, "HEAD")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("refs/heads/main"))
	})

	It("returns diffs between a baseline SHA and a branch", func() {
		dir := gitInit()
		baseSHA, err := worktree.ResolveRef(dir, "HEAD")
		Expect(err).NotTo(HaveOccurred())

		runGit(dir, "checkout", "-b", "feature")
		gitCommit(dir, "feature.txt", "feature\n", "add feature")

		diff, err := worktree.Diff(dir, baseSHA, "feature")
		Expect(err).NotTo(HaveOccurred())
		Expect(diff).To(ContainSubstring("feature.txt"))
		Expect(diff).To(ContainSubstring("+feature"))
	})

	It("cherry-picks commits made after a feature branch base", func() {
		dir := gitInit()

		runGit(dir, "checkout", "-b", "feature")
		featureBaseSHA, err := worktree.ResolveRef(dir, "HEAD")
		Expect(err).NotTo(HaveOccurred())
		gitCommit(dir, "feature.txt", "feature\n", "add feature")
		runGit(dir, "checkout", "main")

		_, err = worktree.CherryPick(dir, featureBaseSHA, "feature")
		Expect(err).NotTo(HaveOccurred())
		Expect(filepath.Join(dir, "feature.txt")).To(BeAnExistingFile())
	})

	It("merges feature branches into the current checkout", func() {
		dir := gitInit()

		runGit(dir, "checkout", "-b", "feature")
		gitCommit(dir, "feature.txt", "feature\n", "add feature")
		runGit(dir, "checkout", "main")

		_, err := worktree.Merge(dir, "feature")
		Expect(err).NotTo(HaveOccurred())
		Expect(filepath.Join(dir, "feature.txt")).To(BeAnExistingFile())
	})

	It("lists modified and untracked paths in status order", func() {
		dir := gitInit()
		Expect(os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\nupdated\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("draft\n"), 0o644)).To(Succeed())

		got, err := worktree.DirtyPaths(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal([]string{"README.md", "notes.txt"}))
	})
})
