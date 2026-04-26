package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Lesson state", func() {
	var (
		s   *store.Store
		now time.Time
	)

	BeforeEach(func() {
		s = createCLIStore()
		now = time.Now().UTC()
	})

	writeHypothesis := func(id string, status string) *entity.Hypothesis {
		GinkgoHelper()
		h := &entity.Hypothesis{
			ID:        id,
			Claim:     "unroll the loop",
			Predicts:  entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease", MinEffect: 0.1},
			KillIf:    []string{"flash grows"},
			Status:    status,
			Author:    "agent:analyst",
			CreatedAt: now,
		}
		Expect(s.WriteHypothesis(h)).To(Succeed())
		return h
	}

	writeConclusion := func(id string, h *entity.Hypothesis, verdict string) *entity.Conclusion {
		GinkgoHelper()
		c := &entity.Conclusion{
			ID:         id,
			Hypothesis: h.ID,
			Verdict:    verdict,
			Author:     "agent:analyst",
			CreatedAt:  now,
		}
		Expect(s.WriteConclusion(c)).To(Succeed())
		return c
	}

	writeLesson := func(l *entity.Lesson) {
		GinkgoHelper()
		Expect(s.WriteLesson(l)).To(Succeed())
	}

	Describe("initialization", func() {
		It("derives status and provenance from scope and source conclusion", func() {
			h := writeHypothesis("H-0001", entity.StatusUnreviewed)
			writeConclusion("C-0001", h, entity.VerdictSupported)

			lesson := &entity.Lesson{
				ID:        "L-0001",
				Claim:     "the unrolled loop shape is promising",
				Scope:     entity.LessonScopeHypothesis,
				Subjects:  []string{"C-0001"},
				Author:    "agent:analyst",
				CreatedAt: now,
			}
			Expect(initializeLessonState(s, lesson)).To(Succeed())
			Expect(lesson.Status).To(Equal(entity.LessonStatusProvisional))
			Expect(lesson.Provenance).NotTo(BeNil())
			Expect(lesson.Provenance.SourceChain).To(Equal(entity.LessonSourceUnreviewedDecisive))

			systemLesson := &entity.Lesson{
				ID:        "L-0002",
				Claim:     "fixture cache must be cleared before observe",
				Scope:     entity.LessonScopeSystem,
				Author:    "agent:critic",
				CreatedAt: now,
			}
			Expect(initializeLessonState(s, systemLesson)).To(Succeed())
			Expect(systemLesson.Status).To(Equal(entity.LessonStatusActive))
			Expect(systemLesson.Provenance).NotTo(BeNil())
			Expect(systemLesson.Provenance.SourceChain).To(Equal(entity.LessonSourceSystem))

			inconclusive := writeHypothesis("H-0002", entity.StatusInconclusive)
			writeConclusion("C-0002", inconclusive, entity.VerdictInconclusive)
			invalidatedLesson := &entity.Lesson{
				ID:        "L-0003",
				Claim:     "tap reordering just shifts variance around",
				Scope:     entity.LessonScopeHypothesis,
				Subjects:  []string{"C-0002"},
				Author:    "agent:analyst",
				CreatedAt: now,
			}
			Expect(initializeLessonState(s, invalidatedLesson)).To(Succeed())
			Expect(invalidatedLesson.Status).To(Equal(entity.LessonStatusInvalidated))
			Expect(invalidatedLesson.Provenance).NotTo(BeNil())
			Expect(invalidatedLesson.Provenance.SourceChain).To(Equal(entity.LessonSourceInconclusive))
		})
	})

	Describe("hypothesis synchronization", func() {
		It("promotes, invalidates, and reprovisions lessons as conclusion review state changes", func() {
			h := writeHypothesis("H-0001", entity.StatusUnreviewed)
			c := writeConclusion("C-0001", h, entity.VerdictSupported)

			e := &entity.Experiment{
				ID:          "E-0001",
				Hypothesis:  h.ID,
				Status:      entity.ExpImplemented,
				Instruments: []string{"host_timing"},
				Baseline:    entity.Baseline{Ref: "HEAD"},
				Author:      "agent:designer",
				CreatedAt:   now,
			}
			Expect(s.WriteExperiment(e)).To(Succeed())

			writeLesson(&entity.Lesson{
				ID:         "L-0001",
				Claim:      "the unrolled loop shape is promising",
				Scope:      entity.LessonScopeHypothesis,
				Subjects:   []string{"H-0001", "E-0001", "C-0001"},
				Status:     entity.LessonStatusProvisional,
				Provenance: &entity.LessonProvenance{SourceChain: entity.LessonSourceUnreviewedDecisive},
				Author:     "agent:analyst",
				CreatedAt:  now,
			})

			c.ReviewedBy = "agent:gate"
			h.Status = entity.StatusSupported
			Expect(s.WriteConclusion(c)).To(Succeed())
			Expect(s.WriteHypothesis(h)).To(Succeed())

			changes, err := syncHypothesisLessons(s, h.ID, lessonSyncOnAccept)
			Expect(err).NotTo(HaveOccurred())
			Expect(changes).To(ConsistOf(HaveField("ToStatus", entity.LessonStatusActive)))
			back, err := s.ReadLesson("L-0001")
			Expect(err).NotTo(HaveOccurred())
			Expect(back.Status).To(Equal(entity.LessonStatusActive))
			Expect(back.Provenance).NotTo(BeNil())
			Expect(back.Provenance.SourceChain).To(Equal(entity.LessonSourceReviewedDecisive))

			c.Verdict = entity.VerdictInconclusive
			h.Status = entity.StatusInconclusive
			Expect(s.WriteConclusion(c)).To(Succeed())
			Expect(s.WriteHypothesis(h)).To(Succeed())

			changes, err = syncHypothesisLessons(s, h.ID, lessonSyncOnDowngrade)
			Expect(err).NotTo(HaveOccurred())
			Expect(changes).To(ConsistOf(HaveField("ToStatus", entity.LessonStatusInvalidated)))
			back, err = s.ReadLesson("L-0001")
			Expect(err).NotTo(HaveOccurred())
			Expect(back.Status).To(Equal(entity.LessonStatusInvalidated))
			Expect(back.Provenance).NotTo(BeNil())
			Expect(back.Provenance.SourceChain).To(Equal(entity.LessonSourceInconclusive))

			c.Verdict = entity.VerdictSupported
			c.ReviewedBy = ""
			h.Status = entity.StatusUnreviewed
			Expect(s.WriteConclusion(c)).To(Succeed())
			Expect(s.WriteHypothesis(h)).To(Succeed())

			changes, err = syncHypothesisLessons(s, h.ID, lessonSyncOnAppeal)
			Expect(err).NotTo(HaveOccurred())
			Expect(changes).To(ConsistOf(HaveField("ToStatus", entity.LessonStatusProvisional)))
			back, err = s.ReadLesson("L-0001")
			Expect(err).NotTo(HaveOccurred())
			Expect(back.Status).To(Equal(entity.LessonStatusProvisional))
			Expect(back.Provenance).NotTo(BeNil())
			Expect(back.Provenance.SourceChain).To(Equal(entity.LessonSourceUnreviewedDecisive))
		})

		It("reclassifies legacy system-scoped lessons that reference a downgraded conclusion", func() {
			h := writeHypothesis("H-0001", entity.StatusSupported)
			c := writeConclusion("C-0001", h, entity.VerdictSupported)
			c.ReviewedBy = "agent:gate"
			Expect(s.WriteConclusion(c)).To(Succeed())

			writeLesson(&entity.Lesson{
				ID:         "L-0009",
				Claim:      "legacy malformed system lesson",
				Scope:      entity.LessonScopeSystem,
				Subjects:   []string{"C-0001"},
				Status:     entity.LessonStatusActive,
				Provenance: &entity.LessonProvenance{SourceChain: entity.LessonSourceSystem},
				Author:     "agent:analyst",
				CreatedAt:  now,
			})

			c.Verdict = entity.VerdictInconclusive
			h.Status = entity.StatusInconclusive
			Expect(s.WriteConclusion(c)).To(Succeed())
			Expect(s.WriteHypothesis(h)).To(Succeed())

			changes, err := syncHypothesisLessons(s, h.ID, lessonSyncOnDowngrade)
			Expect(err).NotTo(HaveOccurred())
			Expect(changes).To(ConsistOf(And(
				HaveField("LessonID", "L-0009"),
				HaveField("ToStatus", entity.LessonStatusInvalidated),
				HaveField("ToSource", entity.LessonSourceInconclusive),
			)))

			back, err := s.ReadLesson("L-0009")
			Expect(err).NotTo(HaveOccurred())
			Expect(back.Status).To(Equal(entity.LessonStatusInvalidated))
			Expect(back.Provenance).NotTo(BeNil())
			Expect(back.Provenance.SourceChain).To(Equal(entity.LessonSourceInconclusive))
		})
	})

	Describe("worktree briefs", func() {
		It("includes only lessons that are safe to use for steering", func() {
			h := writeHypothesis("H-0001", entity.StatusSupported)
			h2 := writeHypothesis("H-0002", entity.StatusUnreviewed)
			h2.Claim = "vectorize the taps"
			Expect(s.WriteHypothesis(h2)).To(Succeed())
			h3 := writeHypothesis("H-0003", entity.StatusInconclusive)
			h3.Claim = "reorder the taps"
			Expect(s.WriteHypothesis(h3)).To(Succeed())

			e := &entity.Experiment{
				ID:          "E-0001",
				Hypothesis:  h.ID,
				Status:      entity.ExpImplemented,
				Instruments: []string{"host_timing"},
				Baseline:    entity.Baseline{Ref: "HEAD"},
				Worktree:    "wt",
				Branch:      "issue",
				Author:      "agent:designer",
				CreatedAt:   now,
			}
			Expect(s.WriteExperiment(e)).To(Succeed())

			for _, l := range []*entity.Lesson{
				{
					ID:         "L-0001",
					Claim:      "reviewed steering lesson",
					Scope:      entity.LessonScopeHypothesis,
					Subjects:   []string{"H-0001"},
					Status:     entity.LessonStatusActive,
					Provenance: &entity.LessonProvenance{SourceChain: entity.LessonSourceReviewedDecisive},
					Author:     "agent:analyst",
					CreatedAt:  now,
				},
				{
					ID:         "L-0002",
					Claim:      "provisional lesson",
					Scope:      entity.LessonScopeHypothesis,
					Subjects:   []string{"H-0002"},
					Status:     entity.LessonStatusProvisional,
					Provenance: &entity.LessonProvenance{SourceChain: entity.LessonSourceUnreviewedDecisive},
					Author:     "agent:analyst",
					CreatedAt:  now,
				},
				{
					ID:         "L-0003",
					Claim:      "invalidated lesson",
					Scope:      entity.LessonScopeHypothesis,
					Subjects:   []string{"H-0003"},
					Status:     entity.LessonStatusInvalidated,
					Provenance: &entity.LessonProvenance{SourceChain: entity.LessonSourceInconclusive},
					Author:     "agent:analyst",
					CreatedAt:  now,
				},
				{
					ID:         "L-0004",
					Claim:      "system lesson",
					Scope:      entity.LessonScopeSystem,
					Status:     entity.LessonStatusActive,
					Provenance: &entity.LessonProvenance{SourceChain: entity.LessonSourceSystem},
					Author:     "agent:critic",
					CreatedAt:  now,
				},
			} {
				writeLesson(l)
			}

			Expect(s.UpdateConfig(func(cfg *store.Config) error {
				cfg.Instruments = map[string]store.Instrument{
					"host_timing": {
						Cmd:        []string{"sh", "-c", "cat timing.txt"},
						Parser:     "builtin:scalar",
						Pattern:    "([0-9]+)",
						Unit:       "ns",
						MinSamples: 5,
						Requires:   []string{"host_test=pass"},
						Evidence:   []store.EvidenceSpec{{Name: "profile", Cmd: "profile --json"}},
					},
				}
				return nil
			})).To(Succeed())

			wt := GinkgoT().TempDir()
			Expect(writeWorktreeBrief(s, e, wt, "")).To(Succeed())
			data, err := os.ReadFile(filepath.Join(wt, entity.BriefFileName))
			Expect(err).NotTo(HaveOccurred())

			var brief entity.Brief
			Expect(json.Unmarshal(data, &brief)).To(Succeed())
			Expect(brief.Lessons).To(HaveLen(2))
			Expect(brief.Lessons).To(ConsistOf(
				HaveField("ID", "L-0001"),
				HaveField("ID", "L-0004"),
			))
			Expect(brief.Lessons).To(ContainElement(SatisfyAll(
				HaveField("ID", "L-0001"),
				HaveField("Status", entity.LessonStatusActive),
				HaveField("SourceChain", entity.LessonSourceReviewedDecisive),
			)))
			Expect(brief.InstrumentContracts).To(ContainElement(SatisfyAll(
				HaveField("Name", "host_timing"),
				HaveField("Parser", "builtin:scalar"),
				HaveField("Requires", ContainElement("host_test=pass")),
				HaveField("Evidence", ContainElement(HaveField("Name", "profile"))),
			)))
			Expect(brief.ForbiddenChanges).To(ContainElement(ContainSubstring("main checkout")))
		})
	})
})
