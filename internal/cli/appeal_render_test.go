package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

const appealedDowngradeReason = "benchmark setup was invalid"
const appealedRebuttal = "benchmark setup was valid because warm-up and CPU pinning were already controlled"

func setupAppealedConclusionScenario(t *testing.T) (string, *store.Store) {
	t.Helper()
	saveGlobals(t)

	dir, s := setupGoalStore(t)
	now := time.Now().UTC()

	h := &entity.Hypothesis{
		ID:        "H-0001",
		GoalID:    "G-0001",
		Claim:     "tighten loop",
		Predicts:  entity.Predicts{Instrument: "timing", Target: "fir", Direction: "decrease", MinEffect: 0.1},
		KillIf:    []string{"tests fail"},
		Status:    entity.StatusSupported,
		Author:    "agent:analyst",
		CreatedAt: now,
	}
	if err := s.WriteHypothesis(h); err != nil {
		t.Fatal(err)
	}

	e := &entity.Experiment{
		ID:          "E-0001",
		Hypothesis:  h.ID,
		Status:      entity.ExpMeasured,
		Baseline:    entity.Baseline{Ref: "HEAD"},
		Instruments: []string{"timing"},
		Author:      "agent:designer",
		CreatedAt:   now,
	}
	if err := s.WriteExperiment(e); err != nil {
		t.Fatal(err)
	}

	body := entity.AppendMarkdownSection("", "Interpretation", "The timing win matches the intended mechanism.")
	c := &entity.Conclusion{
		ID:           "C-0001",
		Hypothesis:   h.ID,
		Verdict:      entity.VerdictSupported,
		CandidateExp: e.ID,
		Effect: entity.Effect{
			Instrument: "timing",
			DeltaFrac:  -0.2,
			CILowFrac:  -0.25,
			CIHighFrac: -0.15,
			PValue:     0.01,
			CIMethod:   "bootstrap_bca_95",
			NCandidate: 5,
			NBaseline:  5,
		},
		StatTest:   "welch",
		ReviewedBy: "human:gate",
		Author:     "agent:analyst",
		CreatedAt:  now,
		Body:       body,
	}
	if err := s.WriteConclusion(c); err != nil {
		t.Fatal(err)
	}

	runCLI(t, dir,
		"conclusion", "downgrade", c.ID,
		"--reason", appealedDowngradeReason,
		"--reviewed-by", "human:alice",
	)
	runCLI(t, dir,
		"conclusion", "appeal", c.ID,
		"--rebuttal", appealedRebuttal,
		"--author", "agent:orchestrator",
	)

	reopened, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, reopened
}

func TestAppealedConclusionCLIReadSurfacesPreserveHistoricalDowngradeContext(t *testing.T) {
	dir, _ := setupAppealedConclusionScenario(t)

	listOut := runCLI(t, dir, "conclusion", "list", "--hypothesis", "H-0001")
	for _, want := range []string{"C-0001", "supported", "downgraded from supported"} {
		if !strings.Contains(listOut, want) {
			t.Fatalf("conclusion list missing %q:\n%s", want, listOut)
		}
	}

	showOut := runCLI(t, dir, "conclusion", "show", "C-0001")
	for _, want := range []string{
		"verdict:      supported",
		"downgraded:   from \"supported\" with reasons:",
		"critic downgrade: " + appealedDowngradeReason,
		"# Appeal",
		appealedRebuttal,
	} {
		if !strings.Contains(showOut, want) {
			t.Fatalf("conclusion show missing %q:\n%s", want, showOut)
		}
	}
	if strings.Contains(showOut, "reviewed_by:") {
		t.Fatalf("conclusion show unexpectedly reports reviewed_by after appeal:\n%s", showOut)
	}

	reportOut := runCLI(t, dir, "report", "H-0001")
	for _, want := range []string{
		"### C-0001 — supported",
		"> **Downgraded** from `supported`",
		"critic downgrade: " + appealedDowngradeReason,
		"# Appeal",
		appealedRebuttal,
	} {
		if !strings.Contains(reportOut, want) {
			t.Fatalf("report missing %q:\n%s", want, reportOut)
		}
	}
}

func TestAppealedConclusionTUIReadSurfacesPreserveHistoricalDowngradeContext(t *testing.T) {
	_, s := setupAppealedConclusionScenario(t)

	c, err := s.ReadConclusion("C-0001")
	if err != nil {
		t.Fatal(err)
	}

	listView := newConclusionListView(goalScope{All: true})
	nv, _ := listView.update(concListLoadedMsg{list: []*entity.Conclusion{c}}, nil)
	listOut := stripANSI(nv.view(120, 20))
	for _, want := range []string{"C-0001", "supported", "downgraded from supported"} {
		if !strings.Contains(listOut, want) {
			t.Fatalf("TUI conclusion list missing %q:\n%s", want, listOut)
		}
	}

	detailView := newConclusionDetailView("C-0001")
	nv, _ = detailView.update(concDetailLoadedMsg{c: c}, nil)
	detailOut := stripANSI(nv.view(120, 30))
	for _, want := range []string{"C-0001", "supported", "downgraded from supported", appealedDowngradeReason} {
		if !strings.Contains(detailOut, want) {
			t.Fatalf("TUI conclusion detail missing %q:\n%s", want, detailOut)
		}
	}
	if strings.Contains(detailOut, "reviewed_by=") {
		t.Fatalf("TUI conclusion detail unexpectedly reports reviewed_by after appeal:\n%s", detailOut)
	}

	h, err := s.ReadHypothesis("H-0001")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := buildReport(s, h)
	if err != nil {
		t.Fatal(err)
	}
	reportMD := renderReportMarkdown(rep)
	view := newReportView("H-0001")
	nv, _ = view.update(reportLoadedMsg{md: reportMD}, nil)
	rv := nv.(*reportView)
	reportOut := stripANSI(rv.ensureRendered(140))
	for _, want := range []string{"C-0001", "supported", "Downgraded", "supported", appealedDowngradeReason, appealedRebuttal} {
		if !strings.Contains(reportOut, want) {
			t.Fatalf("TUI report missing %q:\n%s", want, reportOut)
		}
	}
}
