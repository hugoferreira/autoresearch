package store_test

import (
	"os"
	"path/filepath"

	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func mustCreate() (*store.Store, string) {
	GinkgoHelper()
	dir := GinkgoT().TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	Expect(err).NotTo(HaveOccurred())
	return s, dir
}

var _ = Describe("store initialization", func() {
	It("creates the durable .research layout without creating local worktrees", func() {
		s, dir := mustCreate()

		for _, f := range []string{
			filepath.Join(dir, ".research", "config.yaml"),
			filepath.Join(dir, ".research", "state.json"),
			filepath.Join(dir, ".research", "events.jsonl"),
		} {
			Expect(f).To(BeAnExistingFile())
		}
		for _, d := range []string{
			s.HypothesesDir(),
			s.ExperimentsDir(),
			s.ObservationsDir(),
			s.ConclusionsDir(),
			s.ArtifactsDir(),
			s.LessonsDir(),
			s.ScratchDir(),
		} {
			Expect(d).To(BeADirectory())
		}
		Expect(filepath.Join(dir, ".research", "worktrees")).NotTo(BeADirectory())

		cfg, err := s.Config()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Worktrees.Root).NotTo(BeEmpty())
		Expect(filepath.IsAbs(cfg.Worktrees.Root)).To(BeTrue())
		absDir, err := filepath.Abs(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Worktrees.Root).NotTo(HavePrefix(absDir))

		gotRoot, err := s.WorktreesRoot()
		Expect(err).NotTo(HaveOccurred())
		Expect(gotRoot).To(Equal(cfg.Worktrees.Root))
	})

	It("derives a stable default worktree root from the project path", func() {
		r1, err := store.DefaultWorktreesRoot("/tmp/firmware")
		Expect(err).NotTo(HaveOccurred())
		r2, err := store.DefaultWorktreesRoot("/tmp/firmware")
		Expect(err).NotTo(HaveOccurred())
		r3, err := store.DefaultWorktreesRoot("/other/firmware")
		Expect(err).NotTo(HaveOccurred())

		Expect(r2).To(Equal(r1))
		Expect(r3).NotTo(Equal(r1))
	})

	It("rejects double creation", func() {
		_, dir := mustCreate()
		_, err := store.Create(dir, store.Config{
			Build: store.CommandSpec{Command: "true"},
			Test:  store.CommandSpec{Command: "true"},
		})
		Expect(err).To(MatchError(store.ErrAlreadyInitialized))
	})

	It("rejects opening uninitialized directories", func() {
		_, err := store.Open(GinkgoT().TempDir())
		Expect(err).To(MatchError(store.ErrNotInitialized))
	})

	It("walks up from nested project directories to find the store", func() {
		root, _ := mustCreate()
		sub := filepath.Join(root.Root(), "src", "inner", "deep")
		Expect(os.MkdirAll(sub, 0o755)).To(Succeed())

		s, err := store.Open(sub)
		Expect(err).NotTo(HaveOccurred())
		Expect(s.Root()).To(Equal(root.Root()))
	})

	It("stops upward search at the filesystem root", func() {
		dir := GinkgoT().TempDir()
		sub := filepath.Join(dir, "a", "b", "c")
		Expect(os.MkdirAll(sub, 0o755)).To(Succeed())
		_, err := store.Open(sub)
		Expect(err).To(MatchError(store.ErrNotInitialized))
	})

	It("round-trips config defaults through disk", func() {
		_, dir := mustCreate()
		s, err := store.Open(dir)
		Expect(err).NotTo(HaveOccurred())
		cfg, err := s.Config()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Build.Command).To(Equal("true"))
		Expect(cfg.Test.Command).To(Equal("true"))
		Expect(cfg.Mode).To(Equal("strict"))
		Expect(cfg.SchemaVersion).To(Equal(1))
	})
})

var _ = Describe("store state", func() {
	It("allocates monotonically increasing IDs per entity kind across reopen", func() {
		s, _ := mustCreate()

		id1, err := s.AllocID(store.KindHypothesis)
		Expect(err).NotTo(HaveOccurred())
		id2, err := s.AllocID(store.KindHypothesis)
		Expect(err).NotTo(HaveOccurred())
		eid, err := s.AllocID(store.KindExperiment)
		Expect(err).NotTo(HaveOccurred())
		s2, err := store.Open(s.Root())
		Expect(err).NotTo(HaveOccurred())
		id3, err := s2.AllocID(store.KindHypothesis)
		Expect(err).NotTo(HaveOccurred())

		Expect([]string{id1, id2, eid, id3}).To(Equal([]string{"H-0001", "H-0002", "E-0001", "H-0003"}))
	})

	It("appends events, reads them in order, and updates LastEventAt", func() {
		s, _ := mustCreate()
		Expect(s.AppendEvent(store.Event{Kind: "init", Actor: "system"})).To(Succeed())
		Expect(s.AppendEvent(store.Event{Kind: "hypothesis.add", Actor: "human:alice", Subject: "H-0001"})).To(Succeed())

		events, err := s.Events(0)
		Expect(err).NotTo(HaveOccurred())
		Expect(events).To(HaveLen(2))
		Expect([]string{events[0].Kind, events[1].Kind}).To(Equal([]string{"init", "hypothesis.add"}))
		Expect(events[1].Subject).To(Equal("H-0001"))

		st, err := s.State()
		Expect(err).NotTo(HaveOccurred())
		Expect(st.LastEventAt).NotTo(BeNil())
	})

	It("limits event reads to the newest entries", func() {
		s, _ := mustCreate()
		for i := 0; i < 5; i++ {
			Expect(s.AppendEvent(store.Event{Kind: "noop"})).To(Succeed())
		}
		got, err := s.Events(3)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveLen(3))
	})

	It("counts entity files while tolerating empty directories", func() {
		s, _ := mustCreate()
		counts, err := s.Counts()
		Expect(err).NotTo(HaveOccurred())
		Expect(counts).To(HaveKeyWithValue("hypotheses", 0))
		Expect(counts).To(HaveKeyWithValue("experiments", 0))
		Expect(counts).To(HaveKeyWithValue("observations", 0))
		Expect(counts).To(HaveKeyWithValue("conclusions", 0))
		Expect(counts).To(HaveKeyWithValue("lessons", 0))

		Expect(os.WriteFile(filepath.Join(s.HypothesesDir(), "H-0001.md"), []byte("stub"), 0o644)).To(Succeed())
		counts, err = s.Counts()
		Expect(err).NotTo(HaveOccurred())
		Expect(counts).To(HaveKeyWithValue("hypotheses", 1))
	})
})
