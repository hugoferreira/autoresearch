package cli

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/bytter/autoresearch/internal/store"
)

type concludeResolutionJSON struct {
	RequestedObservations []string `json:"requested_observations"`
	UsedObservations      []string `json:"used_observations"`
	IgnoredObservations   []struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	} `json:"ignored_observations"`
	CandidateExperiment string `json:"candidate_experiment"`
	CandidateSource     string `json:"candidate_source"`
	BaselineExperiment  string `json:"baseline_experiment,omitempty"`
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
	timingObservation   string
	sizeObservation     string
}

func setupConcludeFallbackFixture(t *testing.T) concludeFixture {
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

	writeObservation := func(id, expID, instrument string, value float64, perSample []float64) {
		t.Helper()
		o := &entity.Observation{
			ID:         id,
			Experiment: expID,
			Instrument: instrument,
			MeasuredAt: now,
			Value:      value,
			Samples:    len(perSample),
			PerSample:  perSample,
			Unit:       "ns",
			Author:     "agent:observer",
		}
		if instrument == "binary_size" {
			o.Unit = "bytes"
		}
		if err := s.WriteObservation(o); err != nil {
			t.Fatal(err)
		}
	}
	writeObservation("O-0001", goalBaseline.ID, "binary_size", 900, []float64{900, 900, 900, 900, 900})
	writeObservation("O-0002", ancestorExp.ID, "timing", 100.4, []float64{100, 101, 99, 100, 102})
	writeObservation("O-0003", candidateExp.ID, "timing", 70.4, []float64{70, 71, 69, 72, 70})
	writeObservation("O-0004", candidateExp.ID, "binary_size", 860, []float64{860, 860, 860, 860, 860})

	ancestorConcl := &entity.Conclusion{
		ID:           "C-0001",
		Hypothesis:   ancestorHyp.ID,
		Verdict:      entity.VerdictSupported,
		Observations: []string{"O-0002"},
		CandidateExp: ancestorExp.ID,
		BaselineExp:  goalBaseline.ID,
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
		timingObservation:   "O-0003",
		sizeObservation:     "O-0004",
	}
}

func TestConclude_JSONSurfacesResolutionAndEventAudit(t *testing.T) {
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
	if got, want := resp.Conclusion.Observations, []string{fx.timingObservation}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("stored observations = %v, want %v", got, want)
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
	if got := payload["baseline_source"]; got != readmodel.BaselineSourceAncestorSupported {
		t.Fatalf("payload baseline_source = %v, want %q", got, readmodel.BaselineSourceAncestorSupported)
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
}

func TestConclude_TextOutputSurfacesFallback(t *testing.T) {
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
	if !strings.Contains(out, "ignored:     "+fx.sizeObservation+" (instrument \"binary_size\" does not match predicted instrument \"timing\")") {
		t.Fatalf("output missing ignored observation audit:\n%s", out)
	}
	if !strings.Contains(out, "baseline:    "+fx.ancestorExperiment+"  (n=5, source=ancestor_supported via "+fx.ancestorHypothesis+"/"+fx.ancestorConclusion+")") {
		t.Fatalf("output missing baseline source path:\n%s", out)
	}
	if !strings.Contains(out, "baseline note: candidate recorded baseline "+fx.goalBaseline+" has no observations on instrument \"timing\"") {
		t.Fatalf("output missing baseline fallback note:\n%s", out)
	}
}

func TestConclude_ExplicitBaselineRemainsStrict(t *testing.T) {
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
}

func runCLIResult(t *testing.T, dir string, args ...string) (string, string, error) {
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
