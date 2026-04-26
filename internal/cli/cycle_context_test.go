package cli

import (
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type cliCycleContextAssessment struct {
	Met             bool   `json:"met"`
	MetByConclusion string `json:"met_by_conclusion,omitempty"`
}

type cliCycleContextInFlight struct {
	ID                  string                        `json:"id"`
	Hypothesis          string                        `json:"hypothesis"`
	Status              string                        `json:"status"`
	Instruments         []string                      `json:"instruments"`
	RecommendedBaseline *readmodel.BaselineResolution `json:"recommended_baseline,omitempty"`
}

type cliCycleContextGoal struct {
	GoalID          string                         `json:"goal_id"`
	Counts          map[string]int                 `json:"counts"`
	Goal            *entity.Goal                   `json:"goal,omitempty"`
	FrontierBest    *cliFrontierRow                `json:"frontier_best"`
	FrontierStallK  int                            `json:"frontier_stall_k"`
	StalledFor      int                            `json:"stalled_for"`
	StallReached    bool                           `json:"stall_reached"`
	GoalAssessment  *cliCycleContextAssessment     `json:"goal_assessment,omitempty"`
	OpenHypotheses  []entity.Hypothesis            `json:"open_hypotheses"`
	InFlight        []cliCycleContextInFlight      `json:"in_flight"`
	ActiveLessons   []readmodel.LessonSummaryView  `json:"active_lessons"`
	RelevantLessons []readmodel.RelevantLessonView `json:"relevant_lessons"`
}

type cliCycleContextResponse struct {
	Project                string                           `json:"project"`
	ScopeGoalID            string                           `json:"scope_goal_id,omitempty"`
	ScopeAll               bool                             `json:"scope_all"`
	Paused                 bool                             `json:"paused"`
	PauseReason            string                           `json:"pause_reason,omitempty"`
	Mode                   string                           `json:"mode"`
	MainCheckoutDirty      bool                             `json:"main_checkout_dirty"`
	MainCheckoutDirtyPaths []string                         `json:"main_checkout_dirty_paths"`
	Counts                 map[string]int                   `json:"counts"`
	BudgetAdvisory         readmodel.BudgetAdvisory         `json:"budget_advisory"`
	Instruments            map[string]store.Instrument      `json:"instruments"`
	ActiveScratch          []readmodel.ScratchWorkspaceView `json:"active_scratch"`
	StaleScratch           []readmodel.ScratchWorkspaceView `json:"stale_scratch,omitempty"`
	Goal                   *entity.Goal                     `json:"goal,omitempty"`
	FrontierBest           *cliFrontierRow                  `json:"frontier_best"`
	FrontierStallK         int                              `json:"frontier_stall_k"`
	StalledFor             int                              `json:"stalled_for"`
	StallReached           bool                             `json:"stall_reached"`
	GoalAssessment         *cliCycleContextAssessment       `json:"goal_assessment,omitempty"`
	OpenHypotheses         []entity.Hypothesis              `json:"open_hypotheses"`
	InFlight               []cliCycleContextInFlight        `json:"in_flight"`
	ActiveLessons          []readmodel.LessonSummaryView    `json:"active_lessons"`
	RelevantLessons        []readmodel.RelevantLessonView   `json:"relevant_lessons"`
	Goals                  []cliCycleContextGoal            `json:"goals,omitempty"`
}

var _ = Describe("cycle-context command", func() {
	It("captures an empty initialized store as a total read-only snapshot", func() {
		dir, _ := createCLIStoreDir()

		ctx := runCLIJSON[cliCycleContextResponse](dir, "cycle-context")

		Expect(ctx.Project).To(Equal(dir))
		Expect(ctx.ScopeAll).To(BeTrue())
		Expect(ctx.Paused).To(BeFalse())
		Expect(ctx.Mode).To(Equal("strict"))
		Expect(ctx.MainCheckoutDirty).To(BeFalse())
		Expect(ctx.MainCheckoutDirtyPaths).To(BeEmpty())
		Expect(ctx.Counts).To(HaveKeyWithValue("hypotheses", 0))
		Expect(ctx.Counts).To(HaveKeyWithValue("experiments", 0))
		Expect(ctx.Counts).To(HaveKeyWithValue("observations", 0))
		Expect(ctx.Counts).To(HaveKeyWithValue("conclusions", 0))
		Expect(ctx.Counts).To(HaveKeyWithValue("lessons", 0))
		Expect(ctx.BudgetAdvisory.EffectiveLimits.MaxExperiments).To(Equal(0))
		Expect(ctx.BudgetAdvisory.LimitSources.MaxExperiments).To(Equal("unlimited"))
		Expect(ctx.BudgetAdvisory.EffectiveLimits.StaleExperimentMinutes).To(Equal(readmodel.DefaultBudgetAdvisoryStaleExperimentMinutes))
		Expect(ctx.BudgetAdvisory.Warnings).To(BeEmpty())
		Expect(ctx.Instruments).To(BeEmpty())
		Expect(ctx.Goals).To(BeEmpty())
	})

	It("works while paused and reports pause fields", func() {
		dir, s := createCLIStoreDir()
		now := time.Now().UTC()
		Expect(s.UpdateState(func(st *store.State) error {
			st.Paused = true
			st.PauseReason = "waiting for reviewer"
			st.PausedAt = &now
			return nil
		})).To(Succeed())

		ctx := runCLIJSON[cliCycleContextResponse](dir, "cycle-context")

		Expect(ctx.Paused).To(BeTrue())
		Expect(ctx.PauseReason).To(Equal("waiting for reviewer"))
	})

	It("captures frontier best, open work, active lessons, and instruments for the active goal", func() {
		dir := setupObserveScenarioStore()
		registerScenarioInstruments(dir)
		fixture := setupLifecycleFixture(dir)

		win := setupLifecycleCandidate(dir, cliLifecycleCandidateSpec{
			Claim: "tighten the hot loop", MinEffect: "0.1",
			RefName: "candidate/context-win", Message: "improve timing",
			Timing: "80\n", Size: "900\n",
		})
		obs := observeLifecycleCandidate(dir, win)
		concl := runCLIJSON[cliIDResponse](dir,
			"conclude", win.HypothesisID,
			"--verdict", "supported",
			"--baseline-experiment", fixture.BaselineID,
			"--observations", observeResultID(obs, "timing"),
		)
		runCLIJSON[cliIDResponse](dir,
			"conclusion", "accept", concl.ID,
			"--reviewed-by", "human:gate",
			"--rationale", "Stats confirmed. Code matches the mechanism. No gaming or metric manipulation was detected.",
		)
		lesson := runCLIJSON[cliIDResponse](dir,
			"lesson", "add",
			"--from", concl.ID,
			"--claim", "tightening the hot loop paid off in this fixture",
			"--body", "## Evidence\nCited conclusion improved timing.\n\n## Mechanism\nThe candidate lowered timing.\n\n## Scope and counterexamples\nApplies to this fixture.\n\n## For the next generator\nPrefer similar loop changes.",
		)

		open := setupLifecycleCandidate(dir, cliLifecycleCandidateSpec{
			Claim: "try a smaller follow-up", MinEffect: "0.05",
			RefName: "candidate/context-open", Message: "follow-up timing",
			Timing: "78\n", Size: "900\n",
		})

		ctx := runCLIJSON[cliCycleContextResponse](dir, "cycle-context", "--goal", fixture.GoalID)

		Expect(ctx.ScopeGoalID).To(Equal(fixture.GoalID))
		Expect(ctx.ScopeAll).To(BeFalse())
		Expect(ctx.Goal).NotTo(BeNil())
		Expect(ctx.FrontierBest).NotTo(BeNil())
		Expect(ctx.FrontierBest.Candidate).To(Equal(win.ExperimentID))
		Expect(ctx.FrontierBest.Conclusion).To(Equal(concl.ID))
		Expect(ctx.GoalAssessment).NotTo(BeNil())
		Expect(ctx.GoalAssessment.Met).To(BeTrue())
		Expect(ctx.GoalAssessment.MetByConclusion).To(Equal(concl.ID))
		Expect(ctx.OpenHypotheses).To(ContainElement(HaveField("ID", open.HypothesisID)))
		Expect(ctx.InFlight).To(ContainElement(HaveField("ID", open.ExperimentID)))
		var openInFlight *cliCycleContextInFlight
		for i := range ctx.InFlight {
			if ctx.InFlight[i].ID == open.ExperimentID {
				openInFlight = &ctx.InFlight[i]
				break
			}
		}
		Expect(openInFlight).NotTo(BeNil())
		Expect(openInFlight.RecommendedBaseline).NotTo(BeNil())
		Expect(openInFlight.RecommendedBaseline.ExperimentID).To(Equal(fixture.BaselineID))
		Expect(openInFlight.RecommendedBaseline.Source).To(Equal(readmodel.BaselineSourceGoalBaseline))
		Expect(ctx.ActiveLessons).To(ContainElement(SatisfyAll(
			HaveField("ID", lesson.ID),
			HaveField("Status", entity.LessonStatusActive),
			HaveField("Claim", "tightening the hot loop paid off in this fixture"),
		)))
		Expect(ctx.RelevantLessons).To(ContainElement(SatisfyAll(
			HaveField("ID", lesson.ID),
			HaveField("Score", BeNumerically(">", 0)),
			HaveField("Claim", "tightening the hot loop paid off in this fixture"),
		)))
		Expect(ctx.Instruments).To(HaveKey("timing"))
		Expect(ctx.Instruments).To(HaveKey("binary_size"))
		Expect(ctx.Instruments).To(HaveKey("host_test"))
	})

	It("returns per-goal sections for --goal all", func() {
		dir := setupObserveScenarioStore()
		registerScenarioInstruments(dir)
		fixture := setupLifecycleFixture(dir)

		ctx := runCLIJSON[cliCycleContextResponse](dir, "cycle-context", "--goal", "all")

		Expect(ctx.ScopeAll).To(BeTrue())
		Expect(ctx.Goals).To(ContainElement(SatisfyAll(
			HaveField("GoalID", fixture.GoalID),
			HaveField("Goal", Not(BeNil())),
		)))
	})
})
