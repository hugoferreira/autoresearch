package readmodel

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
)

const (
	LintSeverityError   = "error"
	LintSeverityWarning = "warning"
)

type ConclusionLintReport struct {
	Conclusion string                `json:"conclusion"`
	OK         bool                  `json:"ok"`
	Errors     int                   `json:"errors"`
	Warnings   int                   `json:"warnings"`
	Issues     []ConclusionLintIssue `json:"issues,omitempty"`
}

type ConclusionLintIssue struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Subject  string `json:"subject,omitempty"`
	Message  string `json:"message"`
}

func LintConclusion(s *store.Store, conclusionID string) (*ConclusionLintReport, error) {
	c, err := s.ReadConclusion(conclusionID)
	if err != nil {
		return nil, err
	}
	report := &ConclusionLintReport{Conclusion: c.ID}
	hyp, goal := lintConclusionContext(s, report, c)
	observations := lintCitedObservations(s, report, c)
	lintRelevantObservations(s, report, c, observations)
	lintRequiredConstraints(s, report, c, goal)
	lintMechanismEvidence(s, report, c, hyp, observations)
	lintBaselineRefs(s, report, c)
	report.OK = len(report.Issues) == 0
	return report, nil
}

func lintConclusionContext(s *store.Store, report *ConclusionLintReport, c *entity.Conclusion) (*entity.Hypothesis, *entity.Goal) {
	if strings.TrimSpace(c.Hypothesis) == "" {
		report.add(LintSeverityError, "missing_hypothesis", c.ID, "conclusion has no hypothesis")
		return nil, nil
	}
	hyp, err := s.ReadHypothesis(c.Hypothesis)
	if err != nil {
		report.add(LintSeverityError, "missing_hypothesis", c.Hypothesis, err.Error())
		return nil, nil
	}
	if strings.TrimSpace(hyp.GoalID) == "" {
		return hyp, nil
	}
	goal, err := s.ReadGoal(hyp.GoalID)
	if err != nil {
		report.add(LintSeverityWarning, "missing_goal", hyp.GoalID, err.Error())
		return hyp, nil
	}
	return hyp, goal
}

func lintCitedObservations(s *store.Store, report *ConclusionLintReport, c *entity.Conclusion) map[string]*entity.Observation {
	observations := make(map[string]*entity.Observation, len(c.Observations))
	for _, id := range c.Observations {
		obs, err := s.ReadObservation(id)
		if err != nil {
			report.add(LintSeverityError, "missing_observation", id, err.Error())
			continue
		}
		observations[id] = obs
		if c.CandidateExp != "" && obs.Experiment != c.CandidateExp {
			report.add(LintSeverityError, "observation_wrong_experiment", id,
				fmt.Sprintf("observation belongs to %s, conclusion candidate is %s", obs.Experiment, c.CandidateExp))
		}
		if c.Effect.Instrument != "" && obs.Instrument != c.Effect.Instrument {
			report.add(LintSeverityWarning, "observation_wrong_instrument", id,
				fmt.Sprintf("observation instrument is %s, conclusion primary instrument is %s", obs.Instrument, c.Effect.Instrument))
		}
		lintCandidateScope(report, c, obs)
	}
	return observations
}

func lintCandidateScope(report *ConclusionLintReport, c *entity.Conclusion, obs *entity.Observation) {
	if obs.Experiment != c.CandidateExp || obs.Instrument != c.Effect.Instrument {
		return
	}
	if c.CandidateAttempt > 0 && obs.Attempt > 0 && obs.Attempt != c.CandidateAttempt {
		report.add(LintSeverityError, "candidate_attempt_mismatch", obs.ID,
			fmt.Sprintf("observation attempt is %d, conclusion candidate attempt is %d", obs.Attempt, c.CandidateAttempt))
	}
	if c.CandidateRef != "" && obs.CandidateRef != "" && obs.CandidateRef != c.CandidateRef {
		report.add(LintSeverityError, "candidate_ref_mismatch", obs.ID,
			fmt.Sprintf("observation candidate ref is %s, conclusion candidate ref is %s", obs.CandidateRef, c.CandidateRef))
	}
	if c.CandidateSHA != "" && obs.CandidateSHA != "" && obs.CandidateSHA != c.CandidateSHA {
		report.add(LintSeverityError, "candidate_sha_mismatch", obs.ID,
			fmt.Sprintf("observation candidate sha is %s, conclusion candidate sha is %s", obs.CandidateSHA, c.CandidateSHA))
	}
}

func lintRelevantObservations(s *store.Store, report *ConclusionLintReport, c *entity.Conclusion, cited map[string]*entity.Observation) {
	if c.CandidateExp == "" || c.Effect.Instrument == "" || conclusionDeclaresAuthoritativeObservation(c) {
		return
	}
	all, err := s.ListObservationsForExperiment(c.CandidateExp)
	if err != nil {
		report.add(LintSeverityWarning, "candidate_observations_unreadable", c.CandidateExp, err.Error())
		return
	}
	for _, obs := range all {
		if obs.Instrument != c.Effect.Instrument {
			continue
		}
		if !observationMatchesConclusionScope(obs, c) {
			continue
		}
		if _, ok := cited[obs.ID]; ok {
			continue
		}
		report.add(LintSeverityWarning, "relevant_observation_not_cited", obs.ID,
			"same candidate experiment/instrument/scope observation is not cited; cite all appended observations or state which one is authoritative")
	}
}

func lintRequiredConstraints(s *store.Store, report *ConclusionLintReport, c *entity.Conclusion, goal *entity.Goal) {
	if goal == nil || c.CandidateExp == "" {
		return
	}
	all, err := s.ListObservationsForExperiment(c.CandidateExp)
	if err != nil {
		report.add(LintSeverityWarning, "constraint_observations_unreadable", c.CandidateExp, err.Error())
		return
	}
	for _, constraint := range goal.Constraints {
		if strings.TrimSpace(constraint.Require) == "" {
			continue
		}
		matching := observationsForInstrumentAndScope(all, constraint.Instrument, c)
		if len(matching) == 0 {
			report.add(LintSeverityError, "required_instrument_missing", constraint.Instrument,
				fmt.Sprintf("required instrument %s=%s has no candidate observation", constraint.Instrument, constraint.Require))
			continue
		}
		if !anyObservationSatisfiesRequire(matching, constraint.Require) {
			report.add(LintSeverityError, "required_instrument_not_passing", constraint.Instrument,
				fmt.Sprintf("required instrument %s=%s is not satisfied by candidate observations", constraint.Instrument, constraint.Require))
		}
	}
}

func lintMechanismEvidence(s *store.Store, report *ConclusionLintReport, c *entity.Conclusion, hyp *entity.Hypothesis, cited map[string]*entity.Observation) {
	if !containsMechanismLanguage(c, hyp) {
		return
	}
	evidenceArtifacts := 0
	evidenceFailures := 0
	for _, obs := range cited {
		evidenceFailures += len(obs.EvidenceFailures)
		for _, failure := range obs.EvidenceFailures {
			report.add(LintSeverityWarning, "evidence_capture_failed", obs.ID+"/"+failure.Name, formatEvidenceFailure(failure))
		}
		for _, artifact := range obs.Artifacts {
			if !isEvidenceArtifact(artifact) {
				continue
			}
			evidenceArtifacts++
			if issue := artifactReadIssue(s, artifact); issue != "" {
				report.add(LintSeverityError, "evidence_artifact_unreadable", obs.ID+"/"+artifact.Name, issue)
			}
		}
	}
	if evidenceArtifacts == 0 && evidenceFailures == 0 {
		report.add(LintSeverityWarning, "mechanism_evidence_not_configured", c.ID,
			"mechanism/counter language appears in the conclusion or hypothesis, but no cited evidence artifact or evidence failure is recorded")
	}
}

func lintBaselineRefs(s *store.Store, report *ConclusionLintReport, c *entity.Conclusion) {
	if c.BaselineExp == "" || c.Effect.Instrument == "" {
		return
	}
	all, err := s.ListObservationsForExperiment(c.BaselineExp)
	if err != nil {
		report.add(LintSeverityWarning, "baseline_observations_unreadable", c.BaselineExp, err.Error())
		return
	}
	var baseObs []*entity.Observation
	for _, obs := range all {
		if obs.Instrument == c.Effect.Instrument {
			baseObs = append(baseObs, obs)
		}
	}
	if len(baseObs) == 0 {
		report.add(LintSeverityWarning, "baseline_observation_missing", c.BaselineExp,
			"no baseline observation matches the conclusion primary instrument")
		return
	}
	for _, obs := range baseObs {
		if c.BaselineAttempt > 0 && obs.Attempt > 0 && obs.Attempt != c.BaselineAttempt {
			report.add(LintSeverityError, "baseline_attempt_mismatch", obs.ID,
				fmt.Sprintf("baseline observation attempt is %d, conclusion baseline attempt is %d", obs.Attempt, c.BaselineAttempt))
		}
		if c.BaselineRef != "" && obs.CandidateRef != "" && obs.CandidateRef != c.BaselineRef {
			report.add(LintSeverityError, "baseline_ref_mismatch", obs.ID,
				fmt.Sprintf("baseline observation ref is %s, conclusion baseline ref is %s", obs.CandidateRef, c.BaselineRef))
		}
		if c.BaselineSHA != "" && obs.CandidateSHA != "" && obs.CandidateSHA != c.BaselineSHA {
			report.add(LintSeverityError, "baseline_sha_mismatch", obs.ID,
				fmt.Sprintf("baseline observation sha is %s, conclusion baseline sha is %s", obs.CandidateSHA, c.BaselineSHA))
		}
	}
}

func observationsForInstrumentAndScope(all []*entity.Observation, instrument string, c *entity.Conclusion) []*entity.Observation {
	var out []*entity.Observation
	for _, obs := range all {
		if obs.Instrument != instrument {
			continue
		}
		if !observationMatchesConclusionScope(obs, c) {
			continue
		}
		out = append(out, obs)
	}
	return out
}

func observationMatchesConclusionScope(obs *entity.Observation, c *entity.Conclusion) bool {
	if c.CandidateAttempt > 0 && obs.Attempt > 0 && obs.Attempt != c.CandidateAttempt {
		return false
	}
	if c.CandidateRef != "" && obs.CandidateRef != "" && obs.CandidateRef != c.CandidateRef {
		return false
	}
	if c.CandidateSHA != "" && obs.CandidateSHA != "" && obs.CandidateSHA != c.CandidateSHA {
		return false
	}
	return true
}

func anyObservationSatisfiesRequire(observations []*entity.Observation, require string) bool {
	require = strings.ToLower(strings.TrimSpace(require))
	for _, obs := range observations {
		switch require {
		case "pass", "passed", "true":
			if obs.Pass != nil && *obs.Pass {
				return true
			}
			if obs.Pass == nil && obs.Value != 0 {
				return true
			}
		case "fail", "failed", "false":
			if obs.Pass != nil && !*obs.Pass {
				return true
			}
			if obs.Pass == nil && obs.Value == 0 {
				return true
			}
		default:
			return true
		}
	}
	return false
}

func containsMechanismLanguage(c *entity.Conclusion, hyp *entity.Hypothesis) bool {
	text := strings.ToLower(strings.Join([]string{
		c.Body,
		c.Effect.Instrument,
		func() string {
			if hyp == nil {
				return ""
			}
			return hyp.Claim + "\n" + hyp.Body
		}(),
	}, "\n"))
	for _, keyword := range []string{
		"attempts", "hits", "noop", "bytes/eval", "allocs", "simplify_trace",
		"profile", "counter", "counters", "trace", "subpass",
	} {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func isEvidenceArtifact(a entity.Artifact) bool {
	name := strings.ToLower(a.Name)
	return strings.HasPrefix(name, "evidence/") ||
		strings.Contains(name, "profile") ||
		strings.Contains(name, "trace") ||
		strings.Contains(name, "counter") ||
		strings.Contains(name, "subpass")
}

func artifactReadIssue(s *store.Store, a entity.Artifact) string {
	switch {
	case strings.TrimSpace(a.Path) != "":
		if _, err := os.Stat(filepath.Join(s.DirPath(), a.Path)); err != nil {
			return err.Error()
		}
	case strings.TrimSpace(a.SHA) != "":
		if _, _, _, err := s.ArtifactLocation(a.SHA); err != nil {
			return err.Error()
		}
	default:
		return "artifact has no path or sha"
	}
	return ""
}

func formatEvidenceFailure(f entity.EvidenceFailure) string {
	label := f.Name
	if f.ExitCode != 0 {
		label = fmt.Sprintf("%s (exit %d)", f.Name, f.ExitCode)
	}
	if detail := strings.TrimSpace(f.Error); detail != "" {
		return label + ": " + detail
	}
	return label
}

func conclusionDeclaresAuthoritativeObservation(c *entity.Conclusion) bool {
	body := strings.ToLower(c.Body)
	return strings.Contains(body, "authoritative observation") ||
		strings.Contains(body, "authoritative observations") ||
		strings.Contains(body, "authoritative sample")
}

func (r *ConclusionLintReport) add(severity, code, subject, message string) {
	r.Issues = append(r.Issues, ConclusionLintIssue{
		Severity: severity,
		Code:     code,
		Subject:  subject,
		Message:  message,
	})
	switch severity {
	case LintSeverityError:
		r.Errors++
	case LintSeverityWarning:
		r.Warnings++
	}
}

func ConclusionLintCodes(issues []ConclusionLintIssue) []string {
	out := make([]string, 0, len(issues))
	for _, issue := range issues {
		if slices.Contains(out, issue.Code) {
			continue
		}
		out = append(out, issue.Code)
	}
	return out
}
