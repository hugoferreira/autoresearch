package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/worktree"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type cliScratchResponse struct {
	Status               string         `json:"status"`
	ID                   string         `json:"id"`
	Worktree             string         `json:"worktree"`
	Branch               string         `json:"branch"`
	Scratch              entity.Scratch `json:"scratch"`
	PromotionInstruction string         `json:"promotion_instruction"`
}

type cliScratchShowResponse struct {
	Scratch              entity.Scratch `json:"scratch"`
	PromotionInstruction string         `json:"promotion_instruction"`
}

type cliScratchStatusResponse struct {
	MainCheckoutDirty bool                             `json:"main_checkout_dirty"`
	Counts            map[string]int                   `json:"counts"`
	ActiveScratch     []readmodel.ScratchWorkspaceView `json:"active_scratch,omitempty"`
	StaleScratch      []readmodel.ScratchWorkspaceView `json:"stale_scratch,omitempty"`
}

var _ = Describe("scratch command", func() {
	BeforeEach(saveGlobals)

	It("creates, exposes, and cleans up a scratch worktree without changing experiment state", func() {
		dir := setupObserveScenarioStore()
		registerScenarioInstruments(dir)
		goal := runCLIJSON[cliIDResponse](dir,
			"goal", "set",
			"--objective-instrument", "timing",
			"--objective-target", "kernel",
			"--objective-direction", "decrease",
			"--constraint-max", "binary_size=1000",
		)

		created := runCLIJSON[cliScratchResponse](dir,
			"scratch", "create",
			"--from", "HEAD",
			"--name", "singleton miss probe",
			"--notes", "Check whether singleton misses dominate before designing an experiment.",
		)

		Expect(created.Status).To(Equal("ok"))
		Expect(created.ID).To(Equal("S-0001"))
		Expect(created.Scratch.Status).To(Equal(entity.ScratchStatusActive))
		Expect(created.Scratch.Name).To(Equal("singleton miss probe"))
		Expect(created.Worktree).NotTo(HavePrefix(dir + string(os.PathSeparator)))
		Expect(created.Worktree).To(ContainSubstring(filepath.Join("worktrees", "scratch", created.ID)))
		Expect(worktree.IsRepo(created.Worktree)).To(BeTrue())
		Expect(created.Branch).To(HavePrefix("autoresearch/scratch/S-0001-singleton-miss-probe"))
		Expect(created.PromotionInstruction).To(ContainSubstring("normal hypothesis and experiment"))

		Expect(strings.TrimSpace(runCLI(dir, "scratch", "path", created.ID))).To(Equal(created.Worktree))
		shown := runCLIJSON[cliScratchShowResponse](dir, "scratch", "show", created.ID)
		Expect(shown.Scratch.Worktree).To(Equal(created.Worktree))
		Expect(shown.PromotionInstruction).To(ContainSubstring("observe/artifact"))
		Expect(runCLIJSON[[]entity.Scratch](dir, "scratch", "list")).To(HaveLen(1))

		Expect(os.WriteFile(filepath.Join(created.Worktree, "probe.txt"), []byte("temporary instrumentation\n"), 0o644)).To(Succeed())
		status := runCLIJSON[cliScratchStatusResponse](dir, "status")
		Expect(status.MainCheckoutDirty).To(BeFalse())
		Expect(status.Counts).To(HaveKeyWithValue("experiments", 0))
		Expect(status.ActiveScratch).To(ContainElement(HaveField("ID", created.ID)))

		ctx := runCLIJSON[cliCycleContextResponse](dir, "cycle-context")
		Expect(ctx.ActiveScratch).To(ContainElement(SatisfyAll(
			HaveField("ID", created.ID),
			HaveField("Worktree", created.Worktree),
		)))

		frontier := runCLIJSON[cliFrontierResponse](dir, "frontier", "--goal", goal.ID)
		Expect(frontier.Frontier).To(BeEmpty())

		cleaned := runCLIJSON[cliScratchResponse](dir,
			"scratch", "cleanup", created.ID,
			"--reason", "premise checked",
		)
		Expect(cleaned.Scratch.Status).To(Equal(entity.ScratchStatusCleaned))
		Expect(cleaned.Scratch.CleanupReason).To(Equal("premise checked"))
		_, statErr := os.Stat(created.Worktree)
		Expect(os.IsNotExist(statErr)).To(BeTrue())
		Expect(runCLIJSON[[]entity.Scratch](dir, "scratch", "list")).To(BeEmpty())
		Expect(runCLIJSON[[]entity.Scratch](dir, "scratch", "list", "--status", "all")).To(HaveLen(1))

		s, err := store.Open(dir)
		Expect(err).NotTo(HaveOccurred())
		cleanupEvent := findLastEvent(s, "scratch.cleanup")
		Expect(cleanupEvent).NotTo(BeNil())
		Expect(cleanupEvent.Subject).To(Equal(created.ID))
		Expect(decodePayload(cleanupEvent)).To(SatisfyAll(
			HaveKeyWithValue("from", entity.ScratchStatusActive),
			HaveKeyWithValue("to", entity.ScratchStatusCleaned),
			HaveKeyWithValue("reason", "premise checked"),
		))
	})

	It("surfaces stale active scratch workspaces in status and cycle-context", func() {
		dir := setupObserveScenarioStore()
		created := runCLIJSON[cliScratchResponse](dir,
			"scratch", "create",
			"--name", "old probe",
		)
		s, err := store.Open(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(s.UpdateConfig(func(cfg *store.Config) error {
			cfg.Budgets.StaleExperimentMinutes = 5
			return nil
		})).To(Succeed())
		sc, err := s.ReadScratch(created.ID)
		Expect(err).NotTo(HaveOccurred())
		sc.CreatedAt = time.Now().UTC().Add(-10 * time.Minute)
		Expect(s.WriteScratch(sc)).To(Succeed())

		status := runCLIJSON[cliScratchStatusResponse](dir, "status")
		Expect(status.StaleScratch).To(ContainElement(HaveField("ID", created.ID)))

		ctx := runCLIJSON[cliCycleContextResponse](dir, "cycle-context")
		Expect(ctx.StaleScratch).To(ContainElement(HaveField("ID", created.ID)))
	})

	It("is pause-gated", func() {
		dir, s := createCLIStoreDir()
		now := time.Now().UTC()
		Expect(s.UpdateState(func(st *store.State) error {
			st.Paused = true
			st.PauseReason = "reviewing"
			st.PausedAt = &now
			return nil
		})).To(Succeed())

		_, _, err := runCLIResult(dir, "scratch", "create", "--name", "blocked probe")

		Expect(errors.Is(err, ErrPaused)).To(BeTrue(), "expected ErrPaused, got %v", err)
	})
})
