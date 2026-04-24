package cli

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
)

type concludeResolutionJSON struct {
	RequestedObservations []string `json:"requested_observations"`
	UsedObservations      []string `json:"used_observations"`
	IgnoredObservations   []struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	} `json:"ignored_observations"`
	CandidateExperiment string `json:"candidate_experiment"`
	CandidateAttempt    int    `json:"candidate_attempt,omitempty"`
	CandidateRef        string `json:"candidate_ref,omitempty"`
	CandidateSHA        string `json:"candidate_sha,omitempty"`
	CandidateSource     string `json:"candidate_source"`
	BaselineExperiment  string `json:"baseline_experiment,omitempty"`
	BaselineAttempt     int    `json:"baseline_attempt,omitempty"`
	BaselineRef         string `json:"baseline_ref,omitempty"`
	BaselineSHA         string `json:"baseline_sha,omitempty"`
	BaselineSource      string `json:"baseline_source"`
	BaselineNote        string `json:"baseline_note,omitempty"`
	AncestorHypothesis  string `json:"ancestor_hypothesis,omitempty"`
	AncestorConclusion  string `json:"ancestor_conclusion,omitempty"`
}

type concludeJSONResponse struct {
	ID         string                 `json:"id"`
	Conclusion entity.Conclusion      `json:"conclusion"`
	Resolution concludeResolutionJSON `json:"resolution"`
}

type concludeFixture struct {
	dir                 string
	goalID              string
	goalBaseline        string
	ancestorHypothesis  string
	ancestorExperiment  string
	ancestorConclusion  string
	currentHypothesis   string
	candidateExperiment string
	candidateRef        string
	candidateSHA        string
	timingObservation   string
	sizeObservation     string
}

func setupConcludeFallbackFixture(t testkit.T) concludeFixture {
	t.Helper()

	dir := t.TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	goal := &entity.Goal{
		ID:        "G-0001",
		Status:    entity.GoalStatusActive,
		CreatedAt: &now,
		Objective: entity.Objective{Instrument: "timing", Direction: "decrease"},
	}
	otherGoal := &entity.Goal{
		ID:        "G-0002",
		Status:    entity.GoalStatusActive,
		CreatedAt: &now,
		Objective: entity.Objective{Instrument: "timing", Direction: "decrease"},
	}
	for _, g := range []*entity.Goal{goal, otherGoal} {
		if err := s.WriteGoal(g); err != nil {
			t.Fatal(err)
		}
	}

	goalBaseline := &entity.Experiment{
		ID:         "E-0001",
		GoalID:     goal.ID,
		IsBaseline: true,
		Status:     entity.ExpMeasured,
		Baseline:   entity.Baseline{Ref: "HEAD", SHA: "abc123"},
		Instruments: []string{
			"binary_size",
		},
		Author:    "system",
		CreatedAt: now,
	}
	ancestorHyp := &entity.Hypothesis{
		ID:        "H-0001",
		GoalID:    goal.ID,
		Claim:     "ancestor",
		Status:    entity.StatusSupported,
		Author:    "agent:analyst",
		CreatedAt: now,
		Predicts: entity.Predicts{
			Instrument: "timing",
			Target:     "kernel",
			Direction:  "decrease",
			MinEffect:  0.05,
		},
	}
	currentHyp := &entity.Hypothesis{
		ID:        "H-0002",
		GoalID:    goal.ID,
		Parent:    ancestorHyp.ID,
		Claim:     "descendant",
		Status:    entity.StatusOpen,
		Author:    "agent:analyst",
		CreatedAt: now,
		Predicts: entity.Predicts{
			Instrument: "timing",
			Target:     "kernel",
			Direction:  "decrease",
			MinEffect:  0.05,
		},
	}
	ancestorExp := &entity.Experiment{
		ID:         "E-0002",
		GoalID:     goal.ID,
		Hypothesis: ancestorHyp.ID,
		Status:     entity.ExpMeasured,
		Baseline:   entity.Baseline{Ref: "HEAD", SHA: "abc123"},
		Instruments: []string{
			"timing",
		},
		Author:    "agent:observer",
		CreatedAt: now,
	}
	candidateExp := &entity.Experiment{
		ID:         "E-0003",
		GoalID:     goal.ID,
		Hypothesis: currentHyp.ID,
		Status:     entity.ExpMeasured,
		Baseline: entity.Baseline{
			Ref:        "HEAD",
			SHA:        "def456",
			Experiment: goalBaseline.ID,
		},
		Instruments: []string{"timing", "binary_size"},
		Author:      "agent:observer",
		CreatedAt:   now,
	}
	for _, h := range []*entity.Hypothesis{ancestorHyp, currentHyp} {
		if err := s.WriteHypothesis(h); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range []*entity.Experiment{goalBaseline, ancestorExp, candidateExp} {
		if err := s.WriteExperiment(e); err != nil {
			t.Fatal(err)
		}
	}

	writeObservation := func(id, expID, instrument string, value float64, perSample []float64, candidateRef, candidateSHA string) {
		t.Helper()
		o := &entity.Observation{
			ID:           id,
			Experiment:   expID,
			Instrument:   instrument,
			MeasuredAt:   now,
			Value:        value,
			Samples:      len(perSample),
			PerSample:    perSample,
			Unit:         "ns",
			CandidateRef: candidateRef,
			CandidateSHA: candidateSHA,
			Author:       "agent:observer",
		}
		if candidateRef != "" || candidateSHA != "" {
			o.Attempt = 1
		}
		if instrument == "binary_size" {
			o.Unit = "bytes"
		}
		if err := s.WriteObservation(o); err != nil {
			t.Fatal(err)
		}
	}
	ancestorRef := "refs/heads/candidate/E-0002-a1"
	ancestorSHA := "1111111111111111111111111111111111111111"
	candidateRef := "refs/heads/candidate/E-0003-a1"
	candidateSHA := "2222222222222222222222222222222222222222"
	writeObservation("O-0001", goalBaseline.ID, "binary_size", 900, []float64{900, 900, 900, 900, 900}, "", "")
	writeObservation("O-0002", ancestorExp.ID, "timing", 100.4, []float64{100, 101, 99, 100, 102}, ancestorRef, ancestorSHA)
	writeObservation("O-0003", candidateExp.ID, "timing", 70.4, []float64{70, 71, 69, 72, 70}, candidateRef, candidateSHA)
	writeObservation("O-0004", candidateExp.ID, "binary_size", 860, []float64{860, 860, 860, 860, 860}, candidateRef, candidateSHA)

	ancestorConcl := &entity.Conclusion{
		ID:               "C-0001",
		Hypothesis:       ancestorHyp.ID,
		Verdict:          entity.VerdictSupported,
		Observations:     []string{"O-0002"},
		CandidateExp:     ancestorExp.ID,
		CandidateAttempt: 1,
		CandidateRef:     ancestorRef,
		CandidateSHA:     ancestorSHA,
		BaselineExp:      goalBaseline.ID,
		Effect: entity.Effect{
			Instrument: "timing",
			DeltaFrac:  -0.15,
			NCandidate: 5,
			NBaseline:  5,
		},
		StatTest:   "mann_whitney_u",
		Strict:     entity.Strict{Passed: true},
		Author:     "agent:analyst",
		ReviewedBy: "human:gate",
		CreatedAt:  now,
	}
	if err := s.WriteConclusion(ancestorConcl); err != nil {
		t.Fatal(err)
	}

	if err := s.AppendEvent(store.Event{
		Ts:      now,
		Kind:    "experiment.baseline",
		Actor:   "system",
		Subject: goalBaseline.ID,
		Data:    jsonRaw(map[string]any{"goal": goal.ID}),
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateState(func(st *store.State) error {
		st.CurrentGoalID = otherGoal.ID
		st.Counters["G"] = 2
		st.Counters["H"] = 2
		st.Counters["E"] = 3
		st.Counters["O"] = 4
		st.Counters["C"] = 1
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	return concludeFixture{
		dir:                 dir,
		goalID:              goal.ID,
		goalBaseline:        goalBaseline.ID,
		ancestorHypothesis:  ancestorHyp.ID,
		ancestorExperiment:  ancestorExp.ID,
		ancestorConclusion:  ancestorConcl.ID,
		currentHypothesis:   currentHyp.ID,
		candidateExperiment: candidateExp.ID,
		candidateRef:        candidateRef,
		candidateSHA:        candidateSHA,
		timingObservation:   "O-0003",
		sizeObservation:     "O-0004",
	}
}

var _ = testkit.Spec("TestConclude_JSONSurfacesResolutionAndEventAudit", func(t testkit.T) {
	saveGlobals(t)
	fx := setupConcludeFallbackFixture(t)

	resp := runCLIJSON[concludeJSONResponse](t, fx.dir,
		"conclude", fx.currentHypothesis,
		"--verdict", "supported",
		"--observations", strings.Join([]string{fx.timingObservation, fx.sizeObservation}, ","),
	)

	if resp.Conclusion.CandidateExp != fx.candidateExperiment {
		t.Fatalf("candidate_experiment = %q, want %q", resp.Conclusion.CandidateExp, fx.candidateExperiment)
	}
	if resp.Conclusion.CandidateRef != fx.candidateRef {
		t.Fatalf("candidate_ref = %q, want %q", resp.Conclusion.CandidateRef, fx.candidateRef)
	}
	if resp.Conclusion.CandidateSHA != fx.candidateSHA {
		t.Fatalf("candidate_sha = %q, want %q", resp.Conclusion.CandidateSHA, fx.candidateSHA)
	}
	if resp.Conclusion.CandidateAttempt != 1 {
		t.Fatalf("candidate_attempt = %d, want 1", resp.Conclusion.CandidateAttempt)
	}
	if got, want := resp.Conclusion.Observations, []string{fx.timingObservation}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("stored observations = %v, want %v", got, want)
	}
	if resp.Resolution.CandidateRef != fx.candidateRef {
		t.Fatalf("resolution candidate_ref = %q, want %q", resp.Resolution.CandidateRef, fx.candidateRef)
	}
	if resp.Resolution.CandidateSHA != fx.candidateSHA {
		t.Fatalf("resolution candidate_sha = %q, want %q", resp.Resolution.CandidateSHA, fx.candidateSHA)
	}
	if resp.Resolution.CandidateAttempt != 1 {
		t.Fatalf("resolution candidate_attempt = %d, want 1", resp.Resolution.CandidateAttempt)
	}
	if resp.Resolution.CandidateSource != concludeCandidateSourceObservations {
		t.Fatalf("candidate_source = %q, want %q", resp.Resolution.CandidateSource, concludeCandidateSourceObservations)
	}
	if got, want := resp.Resolution.UsedObservations, []string{fx.timingObservation}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("used_observations = %v, want %v", got, want)
	}
	if got, want := resp.Resolution.RequestedObservations, []string{fx.timingObservation, fx.sizeObservation}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("requested_observations = %v, want %v", got, want)
	}
	if got, want := len(resp.Resolution.IgnoredObservations), 1; got != want {
		t.Fatalf("ignored_observations len = %d, want %d", got, want)
	}
	if resp.Resolution.IgnoredObservations[0].ID != fx.sizeObservation {
		t.Fatalf("ignored observation id = %q, want %q", resp.Resolution.IgnoredObservations[0].ID, fx.sizeObservation)
	}
	if resp.Resolution.BaselineExperiment != fx.ancestorExperiment {
		t.Fatalf("baseline_experiment = %q, want %q", resp.Resolution.BaselineExperiment, fx.ancestorExperiment)
	}
	if resp.Resolution.BaselineAttempt != 1 {
		t.Fatalf("baseline_attempt = %d, want 1", resp.Resolution.BaselineAttempt)
	}
	if resp.Resolution.BaselineSource != readmodel.BaselineSourceAncestorSupported {
		t.Fatalf("baseline_source = %q, want %q", resp.Resolution.BaselineSource, readmodel.BaselineSourceAncestorSupported)
	}
	if resp.Resolution.AncestorHypothesis != fx.ancestorHypothesis {
		t.Fatalf("ancestor_hypothesis = %q, want %q", resp.Resolution.AncestorHypothesis, fx.ancestorHypothesis)
	}
	if resp.Resolution.AncestorConclusion != fx.ancestorConclusion {
		t.Fatalf("ancestor_conclusion = %q, want %q", resp.Resolution.AncestorConclusion, fx.ancestorConclusion)
	}
	if !strings.Contains(resp.Resolution.BaselineNote, fx.goalBaseline) {
		t.Fatalf("baseline_note = %q, want to mention %s", resp.Resolution.BaselineNote, fx.goalBaseline)
	}

	s, err := store.Open(fx.dir)
	if err != nil {
		t.Fatal(err)
	}
	written, err := s.ReadConclusion(resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := written.Observations, []string{fx.timingObservation}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("written conclusion observations = %v, want %v", got, want)
	}

	e := findLastEvent(t, s, "conclusion.write")
	if e == nil {
		t.Fatal("conclusion.write event not found")
	}
	payload := decodePayload(t, e)
	if got := payload["candidate_source"]; got != concludeCandidateSourceObservations {
		t.Fatalf("payload candidate_source = %v, want %q", got, concludeCandidateSourceObservations)
	}
	if got := payload["candidate_ref"]; got != fx.candidateRef {
		t.Fatalf("payload candidate_ref = %v, want %q", got, fx.candidateRef)
	}
	if got := payload["candidate_sha"]; got != fx.candidateSHA {
		t.Fatalf("payload candidate_sha = %v, want %q", got, fx.candidateSHA)
	}
	if got := payload["candidate_attempt"]; got != float64(1) {
		t.Fatalf("payload candidate_attempt = %v, want 1", got)
	}
	if got := payload["baseline_source"]; got != readmodel.BaselineSourceAncestorSupported {
		t.Fatalf("payload baseline_source = %v, want %q", got, readmodel.BaselineSourceAncestorSupported)
	}
	if got := payload["baseline_attempt"]; got != float64(1) {
		t.Fatalf("payload baseline_attempt = %v, want 1", got)
	}
	if got := payload["ancestor_hypothesis"]; got != fx.ancestorHypothesis {
		t.Fatalf("payload ancestor_hypothesis = %v, want %q", got, fx.ancestorHypothesis)
	}
	if got := payload["ancestor_conclusion"]; got != fx.ancestorConclusion {
		t.Fatalf("payload ancestor_conclusion = %v, want %q", got, fx.ancestorConclusion)
	}
	if got, ok := payload["observations"].([]any); !ok || len(got) != 1 || got[0] != fx.timingObservation {
		t.Fatalf("payload observations = %#v, want [%s]", payload["observations"], fx.timingObservation)
	}
	if got, ok := payload["requested_observations"].([]any); !ok || len(got) != 2 || got[0] != fx.timingObservation || got[1] != fx.sizeObservation {
		t.Fatalf("payload requested_observations = %#v, want [%s %s]", payload["requested_observations"], fx.timingObservation, fx.sizeObservation)
	}
	ignored, ok := payload["ignored_observations"].([]any)
	if !ok || len(ignored) != 1 {
		t.Fatalf("payload ignored_observations = %#v", payload["ignored_observations"])
	}
	ignoredMap, ok := ignored[0].(map[string]any)
	if !ok {
		t.Fatalf("ignored_observations[0] type = %T", ignored[0])
	}
	if ignoredMap["id"] != fx.sizeObservation {
		t.Fatalf("ignored observation id = %v, want %q", ignoredMap["id"], fx.sizeObservation)
	}
})

var _ = testkit.Spec("TestConclude_TextOutputSurfacesFallback", func(t testkit.T) {
	saveGlobals(t)
	fx := setupConcludeFallbackFixture(t)

	out := runCLI(t, fx.dir,
		"conclude", fx.currentHypothesis,
		"--verdict", "supported",
		"--observations", strings.Join([]string{fx.timingObservation, fx.sizeObservation}, ","),
	)

	if !strings.Contains(out, "candidate:   "+fx.candidateExperiment+"  (source=observations") {
		t.Fatalf("output missing candidate source:\n%s", out)
	}
	if !strings.Contains(out, "candidate ref: "+fx.candidateRef) {
		t.Fatalf("output missing candidate ref:\n%s", out)
	}
	if !strings.Contains(out, "candidate sha: "+fx.candidateSHA) {
		t.Fatalf("output missing candidate sha:\n%s", out)
	}
	if !strings.Contains(out, "ignored:     "+fx.sizeObservation+" (instrument \"binary_size\" does not match predicted instrument \"timing\")") {
		t.Fatalf("output missing ignored observation audit:\n%s", out)
	}
	if !strings.Contains(out, "baseline:    "+fx.ancestorExperiment+"  (n=5, source=ancestor_supported via "+fx.ancestorHypothesis+"/"+fx.ancestorConclusion+")") {
		t.Fatalf("output missing baseline source path:\n%s", out)
	}
	if !strings.Contains(out, "baseline note: candidate recorded baseline "+fx.goalBaseline+" has no observations on instrument \"timing\"") {
		t.Fatalf("output missing baseline fallback note:\n%s", out)
	}
})

var _ = testkit.Spec("TestConclude_ExplicitBaselineRemainsStrict", func(t testkit.T) {
	saveGlobals(t)
	fx := setupConcludeFallbackFixture(t)

	_, _, err := runCLIResult(t, fx.dir,
		"conclude", fx.currentHypothesis,
		"--verdict", "supported",
		"--baseline-experiment", fx.goalBaseline,
		"--observations", strings.Join([]string{fx.timingObservation, fx.sizeObservation}, ","),
	)
	if err == nil {
		t.Fatal("conclude unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "baseline experiment "+fx.goalBaseline+" has no observations on instrument \"timing\"") {
		t.Fatalf("error = %q, want missing instrument failure", err)
	}
})

var _ = testkit.Spec("TestConclude_RefusesMixedCandidateProvenance", func(t testkit.T) {
	saveGlobals(t)
	fx := setupConcludeFallbackFixture(t)

	s, err := store.Open(fx.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteObservation(&entity.Observation{
		ID:           "O-0005",
		Experiment:   fx.candidateExperiment,
		Instrument:   "timing",
		MeasuredAt:   time.Date(2026, 4, 19, 12, 1, 0, 0, time.UTC),
		Value:        69.8,
		Samples:      5,
		PerSample:    []float64{70, 70, 69, 70, 70},
		Unit:         "ns",
		CandidateRef: "refs/heads/candidate/E-0003-a2",
		CandidateSHA: "3333333333333333333333333333333333333333",
		Author:       "agent:observer",
	}); err != nil {
		t.Fatal(err)
	}

	_, _, err = runCLIResult(t, fx.dir,
		"conclude", fx.currentHypothesis,
		"--verdict", "supported",
		"--observations", strings.Join([]string{fx.timingObservation, "O-0005"}, ","),
	)
	if err == nil {
		t.Fatal("conclude unexpectedly succeeded with mixed candidate provenance")
	}
	if !strings.Contains(err.Error(), "mix candidate scope") {
		t.Fatalf("unexpected mixed provenance error: %v", err)
	}
})

var _ = testkit.Spec("TestConclude_RefusesMixedCandidateScopeAcrossAttempts", func(t testkit.T) {
	saveGlobals(t)
	fx := setupConcludeFallbackFixture(t)

	s, err := store.Open(fx.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteObservation(&entity.Observation{
		ID:           "O-0006",
		Experiment:   fx.candidateExperiment,
		Instrument:   "timing",
		MeasuredAt:   time.Date(2026, 4, 19, 12, 2, 0, 0, time.UTC),
		Value:        69.9,
		Samples:      5,
		PerSample:    []float64{70, 70, 69, 70, 70},
		Unit:         "ns",
		Attempt:      2,
		CandidateRef: fx.candidateRef,
		CandidateSHA: fx.candidateSHA,
		Author:       "agent:observer",
	}); err != nil {
		t.Fatal(err)
	}

	_, _, err = runCLIResult(t, fx.dir,
		"conclude", fx.currentHypothesis,
		"--verdict", "supported",
		"--observations", strings.Join([]string{fx.timingObservation, "O-0006"}, ","),
	)
	if err == nil {
		t.Fatal("conclude unexpectedly succeeded with mixed candidate attempts")
	}
	if !strings.Contains(err.Error(), "mix candidate scope") {
		t.Fatalf("unexpected mixed scope error: %v", err)
	}
})

var _ = testkit.Spec("TestConclude_UsesScopedRescuerComparison", func(t testkit.T) {
	saveGlobals(t)

	dir := t.TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	goal := &entity.Goal{
		ID:              "G-0001",
		Status:          entity.GoalStatusActive,
		CreatedAt:       &now,
		Objective:       entity.Objective{Instrument: "timing", Direction: "decrease"},
		NeutralBandFrac: 0.05,
		Rescuers: []entity.Rescuer{
			{Instrument: "binary_size", Direction: "decrease", MinEffect: 0.05},
		},
	}
	parent := &entity.Hypothesis{
		ID:        "H-0001",
		GoalID:    goal.ID,
		Claim:     "ancestor",
		Status:    entity.StatusSupported,
		Author:    "agent:analyst",
		CreatedAt: now,
		Predicts: entity.Predicts{
			Instrument: "timing",
			Target:     "kernel",
			Direction:  "decrease",
			MinEffect:  0.05,
		},
	}
	current := &entity.Hypothesis{
		ID:        "H-0002",
		GoalID:    goal.ID,
		Parent:    parent.ID,
		Claim:     "candidate",
		Status:    entity.StatusOpen,
		Author:    "agent:analyst",
		CreatedAt: now,
		Predicts: entity.Predicts{
			Instrument: "timing",
			Target:     "kernel",
			Direction:  "decrease",
			MinEffect:  0.05,
		},
	}
	ancestorExp := &entity.Experiment{
		ID:          "E-0001",
		GoalID:      goal.ID,
		Hypothesis:  parent.ID,
		Status:      entity.ExpMeasured,
		Attempt:     2,
		Baseline:    entity.Baseline{Ref: "HEAD", SHA: "abc123"},
		Instruments: []string{"timing", "binary_size"},
		Author:      "agent:observer",
		CreatedAt:   now,
	}
	candidateExp := &entity.Experiment{
		ID:          "E-0002",
		GoalID:      goal.ID,
		Hypothesis:  current.ID,
		Status:      entity.ExpMeasured,
		Attempt:     2,
		Baseline:    entity.Baseline{Ref: "HEAD", SHA: "abc123"},
		Instruments: []string{"timing", "binary_size"},
		Author:      "agent:observer",
		CreatedAt:   now,
	}
	for _, g := range []*entity.Goal{goal} {
		if err := s.WriteGoal(g); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range []*entity.Hypothesis{parent, current} {
		if err := s.WriteHypothesis(h); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range []*entity.Experiment{ancestorExp, candidateExp} {
		if err := s.WriteExperiment(e); err != nil {
			t.Fatal(err)
		}
	}

	writeObs := func(id, expID, instrument string, value float64, attempt int, ref, sha string) {
		t.Helper()
		perSample := []float64{value, value, value, value, value}
		unit := "ns"
		if instrument == "binary_size" {
			unit = "bytes"
		}
		if err := s.WriteObservation(&entity.Observation{
			ID:           id,
			Experiment:   expID,
			Instrument:   instrument,
			MeasuredAt:   now,
			Value:        value,
			Samples:      len(perSample),
			PerSample:    perSample,
			Unit:         unit,
			Attempt:      attempt,
			CandidateRef: ref,
			CandidateSHA: sha,
			Author:       "agent:observer",
		}); err != nil {
			t.Fatal(err)
		}
	}

	ancestorScopeA := readmodel.ObservationScope{
		Experiment: ancestorExp.ID,
		Attempt:    1,
		Ref:        "refs/heads/candidate/E-0001-a1",
		SHA:        "1111111111111111111111111111111111111111",
	}
	ancestorScopeB := readmodel.ObservationScope{
		Experiment: ancestorExp.ID,
		Attempt:    2,
		Ref:        "refs/heads/candidate/E-0001-a2",
		SHA:        "2222222222222222222222222222222222222222",
	}
	currentScopeA := readmodel.ObservationScope{
		Experiment: candidateExp.ID,
		Attempt:    1,
		Ref:        "refs/heads/candidate/E-0002-a1",
		SHA:        "3333333333333333333333333333333333333333",
	}
	currentScopeB := readmodel.ObservationScope{
		Experiment: candidateExp.ID,
		Attempt:    2,
		Ref:        "refs/heads/candidate/E-0002-a2",
		SHA:        "4444444444444444444444444444444444444444",
	}

	writeObs("O-0001", ancestorExp.ID, "timing", 100, ancestorScopeA.Attempt, ancestorScopeA.Ref, ancestorScopeA.SHA)
	writeObs("O-0002", ancestorExp.ID, "binary_size", 1000, ancestorScopeA.Attempt, ancestorScopeA.Ref, ancestorScopeA.SHA)
	writeObs("O-0003", ancestorExp.ID, "timing", 95, ancestorScopeB.Attempt, ancestorScopeB.Ref, ancestorScopeB.SHA)
	writeObs("O-0004", ancestorExp.ID, "binary_size", 800, ancestorScopeB.Attempt, ancestorScopeB.Ref, ancestorScopeB.SHA)
	writeObs("O-0005", candidateExp.ID, "timing", 100, currentScopeA.Attempt, currentScopeA.Ref, currentScopeA.SHA)
	writeObs("O-0006", candidateExp.ID, "binary_size", 900, currentScopeA.Attempt, currentScopeA.Ref, currentScopeA.SHA)
	writeObs("O-0007", candidateExp.ID, "timing", 70, currentScopeB.Attempt, currentScopeB.Ref, currentScopeB.SHA)
	writeObs("O-0008", candidateExp.ID, "binary_size", 1200, currentScopeB.Attempt, currentScopeB.Ref, currentScopeB.SHA)

	if err := s.WriteConclusion(&entity.Conclusion{
		ID:               "C-0001",
		Hypothesis:       parent.ID,
		Verdict:          entity.VerdictSupported,
		ReviewedBy:       "human:gate",
		Observations:     []string{"O-0001"},
		CandidateExp:     ancestorExp.ID,
		CandidateAttempt: ancestorScopeA.Attempt,
		CandidateRef:     ancestorScopeA.Ref,
		CandidateSHA:     ancestorScopeA.SHA,
		Effect: entity.Effect{
			Instrument: "timing",
			DeltaFrac:  -0.10,
			NCandidate: 5,
			NBaseline:  5,
		},
		StatTest:  "mann_whitney_u",
		Strict:    entity.Strict{Passed: true},
		Author:    "agent:analyst",
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	resp := runCLIJSON[concludeJSONResponse](t, dir,
		"conclude", current.ID,
		"--verdict", "supported",
		"--observations", "O-0005",
	)
	if got, want := resp.Conclusion.Verdict, entity.VerdictSupported; got != want {
		t.Fatalf("verdict = %q, want %q", got, want)
	}
	if got, want := resp.Conclusion.Strict.RescuedBy, "binary_size"; got != want {
		t.Fatalf("rescued_by = %q, want %q", got, want)
	}
	if got, want := resp.Resolution.BaselineExperiment, ancestorExp.ID; got != want {
		t.Fatalf("baseline_experiment = %q, want %q", got, want)
	}
	if got, want := resp.Resolution.BaselineAttempt, ancestorScopeA.Attempt; got != want {
		t.Fatalf("baseline_attempt = %d, want %d", got, want)
	}
	if got, want := resp.Resolution.BaselineRef, ancestorScopeA.Ref; got != want {
		t.Fatalf("baseline_ref = %q, want %q", got, want)
	}
	if got, want := resp.Resolution.BaselineSHA, ancestorScopeA.SHA; got != want {
		t.Fatalf("baseline_sha = %q, want %q", got, want)
	}
})

func runCLIResult(t testkit.T, dir string, args ...string) (string, string, error) {
	t.Helper()

	oldStdout, oldStderr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	outCh := make(chan string, 1)
	errCh := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(rOut)
		outCh <- string(data)
	}()
	go func() {
		data, _ := io.ReadAll(rErr)
		errCh <- string(data)
	}()

	os.Stdout = wOut
	os.Stderr = wErr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	root := Root()
	root.SetArgs(append([]string{"-C", dir}, args...))
	execErr := root.Execute()

	_ = wOut.Close()
	_ = wErr.Close()
	stdout := <-outCh
	stderr := <-errCh
	return stdout, stderr, execErr
}
