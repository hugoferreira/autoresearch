package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
	"github.com/onsi/ginkgo/v2"
)

func mustCreateCLIStore(t testkit.T) *store.Store {
	t.Helper()
	s, err := store.Create(t.TempDir(), store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var _ = ginkgo.Describe("TestInitializeLessonState", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		s := mustCreateCLIStore(t)
		now := time.Now().UTC()

		h := &entity.Hypothesis{
			ID:        "H-0001",
			Claim:     "unroll the loop",
			Predicts:  entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease", MinEffect: 0.1},
			KillIf:    []string{"flash grows"},
			Status:    entity.StatusUnreviewed,
			Author:    "agent:analyst",
			CreatedAt: now,
		}
		if err := s.WriteHypothesis(h); err != nil {
			t.Fatal(err)
		}
		c := &entity.Conclusion{
			ID:         "C-0001",
			Hypothesis: h.ID,
			Verdict:    entity.VerdictSupported,
			Author:     "agent:analyst",
			CreatedAt:  now,
		}
		if err := s.WriteConclusion(c); err != nil {
			t.Fatal(err)
		}

		lesson := &entity.Lesson{
			ID:        "L-0001",
			Claim:     "the unrolled loop shape is promising",
			Scope:     entity.LessonScopeHypothesis,
			Subjects:  []string{"C-0001"},
			Author:    "agent:analyst",
			CreatedAt: now,
		}
		if err := initializeLessonState(s, lesson); err != nil {
			t.Fatalf("initializeLessonState failed: %v", err)
		}
		if lesson.Status != entity.LessonStatusProvisional {
			t.Fatalf("lesson status = %q, want %q", lesson.Status, entity.LessonStatusProvisional)
		}
		if lesson.Provenance == nil || lesson.Provenance.SourceChain != entity.LessonSourceUnreviewedDecisive {
			t.Fatalf("lesson provenance = %+v, want %q", lesson.Provenance, entity.LessonSourceUnreviewedDecisive)
		}

		systemLesson := &entity.Lesson{
			ID:        "L-0002",
			Claim:     "fixture cache must be cleared before observe",
			Scope:     entity.LessonScopeSystem,
			Author:    "agent:critic",
			CreatedAt: now,
		}
		if err := initializeLessonState(s, systemLesson); err != nil {
			t.Fatalf("initializeLessonState(system) failed: %v", err)
		}
		if systemLesson.Status != entity.LessonStatusActive {
			t.Fatalf("system lesson status = %q, want %q", systemLesson.Status, entity.LessonStatusActive)
		}
		if systemLesson.Provenance == nil || systemLesson.Provenance.SourceChain != entity.LessonSourceSystem {
			t.Fatalf("system lesson provenance = %+v, want %q", systemLesson.Provenance, entity.LessonSourceSystem)
		}

		h2 := &entity.Hypothesis{
			ID:        "H-0002",
			Claim:     "reorder taps",
			Predicts:  entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease", MinEffect: 0.1},
			KillIf:    []string{"flash grows"},
			Status:    entity.StatusInconclusive,
			Author:    "agent:analyst",
			CreatedAt: now,
		}
		if err := s.WriteHypothesis(h2); err != nil {
			t.Fatal(err)
		}
		c2 := &entity.Conclusion{
			ID:         "C-0002",
			Hypothesis: h2.ID,
			Verdict:    entity.VerdictInconclusive,
			Author:     "agent:analyst",
			CreatedAt:  now,
		}
		if err := s.WriteConclusion(c2); err != nil {
			t.Fatal(err)
		}
		invalidatedLesson := &entity.Lesson{
			ID:        "L-0003",
			Claim:     "tap reordering just shifts variance around",
			Scope:     entity.LessonScopeHypothesis,
			Subjects:  []string{"C-0002"},
			Author:    "agent:analyst",
			CreatedAt: now,
		}
		if err := initializeLessonState(s, invalidatedLesson); err != nil {
			t.Fatalf("initializeLessonState(inconclusive) failed: %v", err)
		}
		if invalidatedLesson.Status != entity.LessonStatusInvalidated {
			t.Fatalf("invalidated lesson status = %q, want %q", invalidatedLesson.Status, entity.LessonStatusInvalidated)
		}
		if invalidatedLesson.Provenance == nil || invalidatedLesson.Provenance.SourceChain != entity.LessonSourceInconclusive {
			t.Fatalf("invalidated lesson provenance = %+v, want %q", invalidatedLesson.Provenance, entity.LessonSourceInconclusive)
		}
	})
})

var _ = ginkgo.Describe("TestSyncHypothesisLessons", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		s := mustCreateCLIStore(t)
		now := time.Now().UTC()

		h := &entity.Hypothesis{
			ID:        "H-0001",
			Claim:     "unroll the loop",
			Predicts:  entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease", MinEffect: 0.1},
			KillIf:    []string{"flash grows"},
			Status:    entity.StatusUnreviewed,
			Author:    "agent:analyst",
			CreatedAt: now,
		}
		if err := s.WriteHypothesis(h); err != nil {
			t.Fatal(err)
		}
		c := &entity.Conclusion{
			ID:         "C-0001",
			Hypothesis: h.ID,
			Verdict:    entity.VerdictSupported,
			Author:     "agent:analyst",
			CreatedAt:  now,
		}
		if err := s.WriteConclusion(c); err != nil {
			t.Fatal(err)
		}
		e := &entity.Experiment{
			ID:          "E-0001",
			Hypothesis:  h.ID,
			Status:      entity.ExpImplemented,
			Instruments: []string{"host_timing"},
			Baseline:    entity.Baseline{Ref: "HEAD"},
			Author:      "agent:designer",
			CreatedAt:   now,
		}
		if err := s.WriteExperiment(e); err != nil {
			t.Fatal(err)
		}

		lesson := &entity.Lesson{
			ID:         "L-0001",
			Claim:      "the unrolled loop shape is promising",
			Scope:      entity.LessonScopeHypothesis,
			Subjects:   []string{"H-0001", "E-0001", "C-0001"},
			Status:     entity.LessonStatusProvisional,
			Provenance: &entity.LessonProvenance{SourceChain: entity.LessonSourceUnreviewedDecisive},
			Author:     "agent:analyst",
			CreatedAt:  now,
		}
		if err := s.WriteLesson(lesson); err != nil {
			t.Fatal(err)
		}

		c.ReviewedBy = "agent:gate"
		h.Status = entity.StatusSupported
		if err := s.WriteConclusion(c); err != nil {
			t.Fatal(err)
		}
		if err := s.WriteHypothesis(h); err != nil {
			t.Fatal(err)
		}
		changes, err := syncHypothesisLessons(s, h.ID, lessonSyncOnAccept)
		if err != nil {
			t.Fatalf("syncHypothesisLessons(accept) failed: %v", err)
		}
		if len(changes) != 1 || changes[0].ToStatus != entity.LessonStatusActive {
			t.Fatalf("accept changes = %+v", changes)
		}
		back, err := s.ReadLesson("L-0001")
		if err != nil {
			t.Fatal(err)
		}
		if back.Status != entity.LessonStatusActive || back.Provenance == nil || back.Provenance.SourceChain != entity.LessonSourceReviewedDecisive {
			t.Fatalf("accepted lesson = %+v", back)
		}

		c.Verdict = entity.VerdictInconclusive
		h.Status = entity.StatusInconclusive
		if err := s.WriteConclusion(c); err != nil {
			t.Fatal(err)
		}
		if err := s.WriteHypothesis(h); err != nil {
			t.Fatal(err)
		}
		changes, err = syncHypothesisLessons(s, h.ID, lessonSyncOnDowngrade)
		if err != nil {
			t.Fatalf("syncHypothesisLessons(downgrade) failed: %v", err)
		}
		if len(changes) != 1 || changes[0].ToStatus != entity.LessonStatusInvalidated {
			t.Fatalf("downgrade changes = %+v", changes)
		}
		back, err = s.ReadLesson("L-0001")
		if err != nil {
			t.Fatal(err)
		}
		if back.Status != entity.LessonStatusInvalidated || back.Provenance == nil || back.Provenance.SourceChain != entity.LessonSourceInconclusive {
			t.Fatalf("downgraded lesson = %+v", back)
		}

		c.Verdict = entity.VerdictSupported
		c.ReviewedBy = ""
		h.Status = entity.StatusUnreviewed
		if err := s.WriteConclusion(c); err != nil {
			t.Fatal(err)
		}
		if err := s.WriteHypothesis(h); err != nil {
			t.Fatal(err)
		}
		changes, err = syncHypothesisLessons(s, h.ID, lessonSyncOnAppeal)
		if err != nil {
			t.Fatalf("syncHypothesisLessons(appeal) failed: %v", err)
		}
		if len(changes) != 1 || changes[0].ToStatus != entity.LessonStatusProvisional {
			t.Fatalf("appeal changes = %+v", changes)
		}
		back, err = s.ReadLesson("L-0001")
		if err != nil {
			t.Fatal(err)
		}
		if back.Status != entity.LessonStatusProvisional || back.Provenance == nil || back.Provenance.SourceChain != entity.LessonSourceUnreviewedDecisive {
			t.Fatalf("appealed lesson = %+v", back)
		}
	})
})

var _ = ginkgo.Describe("TestSyncHypothesisLessons_ReclassifiesMalformedSystemLessons", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		s := mustCreateCLIStore(t)
		now := time.Now().UTC()

		h := &entity.Hypothesis{
			ID:        "H-0001",
			Claim:     "unroll the loop",
			Predicts:  entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease", MinEffect: 0.1},
			KillIf:    []string{"flash grows"},
			Status:    entity.StatusSupported,
			Author:    "agent:analyst",
			CreatedAt: now,
		}
		if err := s.WriteHypothesis(h); err != nil {
			t.Fatal(err)
		}
		c := &entity.Conclusion{
			ID:         "C-0001",
			Hypothesis: h.ID,
			Verdict:    entity.VerdictSupported,
			ReviewedBy: "agent:gate",
			Author:     "agent:analyst",
			CreatedAt:  now,
		}
		if err := s.WriteConclusion(c); err != nil {
			t.Fatal(err)
		}

		legacy := &entity.Lesson{
			ID:         "L-0009",
			Claim:      "legacy malformed system lesson",
			Scope:      entity.LessonScopeSystem,
			Subjects:   []string{"C-0001"},
			Status:     entity.LessonStatusActive,
			Provenance: &entity.LessonProvenance{SourceChain: entity.LessonSourceSystem},
			Author:     "agent:analyst",
			CreatedAt:  now,
		}
		if err := s.WriteLesson(legacy); err != nil {
			t.Fatal(err)
		}

		c.Verdict = entity.VerdictInconclusive
		h.Status = entity.StatusInconclusive
		if err := s.WriteConclusion(c); err != nil {
			t.Fatal(err)
		}
		if err := s.WriteHypothesis(h); err != nil {
			t.Fatal(err)
		}

		changes, err := syncHypothesisLessons(s, h.ID, lessonSyncOnDowngrade)
		if err != nil {
			t.Fatalf("syncHypothesisLessons(downgrade malformed system) failed: %v", err)
		}
		if len(changes) != 1 {
			t.Fatalf("changes = %+v, want 1", changes)
		}
		if changes[0].LessonID != "L-0009" || changes[0].ToStatus != entity.LessonStatusInvalidated || changes[0].ToSource != entity.LessonSourceInconclusive {
			t.Fatalf("change = %+v", changes[0])
		}

		back, err := s.ReadLesson("L-0009")
		if err != nil {
			t.Fatal(err)
		}
		if back.Status != entity.LessonStatusInvalidated || back.Provenance == nil || back.Provenance.SourceChain != entity.LessonSourceInconclusive {
			t.Fatalf("reclassified malformed system lesson = %+v", back)
		}
	})
})

var _ = ginkgo.Describe("TestWriteWorktreeBrief_ExcludesNonSteeringLessons", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		s := mustCreateCLIStore(t)
		now := time.Now().UTC()

		h := &entity.Hypothesis{
			ID:        "H-0001",
			Claim:     "unroll the loop",
			Predicts:  entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease", MinEffect: 0.1},
			KillIf:    []string{"flash grows"},
			Status:    entity.StatusSupported,
			Author:    "agent:analyst",
			CreatedAt: now,
		}
		if err := s.WriteHypothesis(h); err != nil {
			t.Fatal(err)
		}
		h2 := &entity.Hypothesis{
			ID:        "H-0002",
			Claim:     "vectorize the taps",
			Predicts:  entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease", MinEffect: 0.1},
			KillIf:    []string{"flash grows"},
			Status:    entity.StatusUnreviewed,
			Author:    "agent:analyst",
			CreatedAt: now,
		}
		if err := s.WriteHypothesis(h2); err != nil {
			t.Fatal(err)
		}
		h3 := &entity.Hypothesis{
			ID:        "H-0003",
			Claim:     "reorder the taps",
			Predicts:  entity.Predicts{Instrument: "host_timing", Target: "fir", Direction: "decrease", MinEffect: 0.1},
			KillIf:    []string{"flash grows"},
			Status:    entity.StatusInconclusive,
			Author:    "agent:analyst",
			CreatedAt: now,
		}
		if err := s.WriteHypothesis(h3); err != nil {
			t.Fatal(err)
		}
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
		if err := s.WriteExperiment(e); err != nil {
			t.Fatal(err)
		}
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
			if err := s.WriteLesson(l); err != nil {
				t.Fatal(err)
			}
		}

		wt := t.TempDir()
		if err := writeWorktreeBrief(s, e, wt, ""); err != nil {
			t.Fatalf("writeWorktreeBrief failed: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(wt, entity.BriefFileName))
		if err != nil {
			t.Fatal(err)
		}
		var brief entity.Brief
		if err := json.Unmarshal(data, &brief); err != nil {
			t.Fatal(err)
		}
		if len(brief.Lessons) != 2 {
			t.Fatalf("brief lessons = %+v, want 2 steering lessons", brief.Lessons)
		}
		got := map[string]bool{}
		for _, l := range brief.Lessons {
			got[l.ID] = true
		}
		if !got["L-0001"] || !got["L-0004"] || got["L-0002"] || got["L-0003"] {
			t.Fatalf("brief lessons ids = %+v", got)
		}
	})
})
