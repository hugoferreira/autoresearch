package readmodel

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/store"
)

const (
	PreflightSeverityError   = "error"
	PreflightSeverityWarning = "warning"
)

type ExperimentPreflightReport struct {
	Experiment string                     `json:"experiment"`
	Hypothesis string                     `json:"hypothesis,omitempty"`
	OK         bool                       `json:"ok"`
	Errors     int                        `json:"errors"`
	Warnings   int                        `json:"warnings"`
	Baseline   *BaselineResolution        `json:"baseline,omitempty"`
	Issues     []ExperimentPreflightIssue `json:"issues,omitempty"`
}

type ExperimentPreflightIssue struct {
	Severity       string `json:"severity"`
	Code           string `json:"code"`
	Subject        string `json:"subject,omitempty"`
	Message        string `json:"message"`
	Recommendation string `json:"recommendation,omitempty"`
}

func PreflightExperiment(s *store.Store, expID string) (*ExperimentPreflightReport, error) {
	exp, err := s.ReadExperiment(expID)
	if err != nil {
		return nil, err
	}
	report := &ExperimentPreflightReport{Experiment: exp.ID, Hypothesis: exp.Hypothesis}
	cfg, err := s.Config()
	if err != nil {
		return nil, err
	}
	if exp.IsBaseline {
		report.add(PreflightSeverityError, "baseline_experiment", exp.ID,
			"preflight is for hypothesis-backed experiments; baseline experiments are measured directly",
			"run preflight on the candidate experiment instead")
		report.finish()
		return report, nil
	}
	hyp, err := s.ReadHypothesis(exp.Hypothesis)
	if err != nil {
		return nil, fmt.Errorf("read hypothesis %s: %w", exp.Hypothesis, err)
	}
	goal, err := s.ReadGoal(hyp.GoalID)
	if err != nil {
		report.add(PreflightSeverityWarning, "goal_unreadable", hyp.GoalID, err.Error(),
			"confirm the experiment still belongs to a readable active goal before implementing")
	}

	report.checkInstruments(exp, hyp, goal, cfg)
	report.checkLessons(s, hyp)
	report.checkMechanismEvidence(exp, hyp, cfg)
	report.checkBaseline(s, exp, hyp)
	report.finish()
	return report, nil
}

func (r *ExperimentPreflightReport) checkInstruments(exp *entity.Experiment, hyp *entity.Hypothesis, goal *entity.Goal, cfg *store.Config) {
	declared := map[string]struct{}{}
	for _, name := range exp.Instruments {
		declared[name] = struct{}{}
		if cfg != nil {
			if _, ok := cfg.Instruments[name]; !ok {
				r.add(PreflightSeverityError, "unknown_instrument", name,
					fmt.Sprintf("experiment declares unregistered instrument %s", name),
					"register the instrument or remove it from the experiment design")
			}
		}
	}
	if _, ok := declared[hyp.Predicts.Instrument]; !ok {
		r.add(PreflightSeverityError, "missing_predicted_instrument", hyp.Predicts.Instrument,
			fmt.Sprintf("experiment does not measure the hypothesis predicted instrument %s", hyp.Predicts.Instrument),
			"redesign the experiment with the predicted instrument included")
	}
	if goal == nil {
		return
	}
	for _, constraint := range goal.Constraints {
		if _, ok := declared[constraint.Instrument]; ok {
			continue
		}
		r.add(PreflightSeverityError, "missing_constraint_instrument", constraint.Instrument,
			fmt.Sprintf("experiment does not measure goal constraint instrument %s", constraint.Instrument),
			"redesign the experiment with every goal constraint instrument included")
	}
}

func (r *ExperimentPreflightReport) checkLessons(s *store.Store, hyp *entity.Hypothesis) {
	if len(hyp.InspiredBy) == 0 {
		return
	}
	lessons := make([]*entity.Lesson, 0, len(hyp.InspiredBy))
	for _, id := range hyp.InspiredBy {
		lesson, err := s.ReadLesson(id)
		if err != nil {
			r.add(PreflightSeverityError, "lesson_unreadable", id, err.Error(),
				"remove the stale lesson citation or restore the lesson before implementing")
			continue
		}
		lessons = append(lessons, lesson)
		if !lessonMatchesHypothesis(lesson, hyp) {
			r.add(PreflightSeverityWarning, "lesson_relevance_unclear", id,
				fmt.Sprintf("lesson %s does not obviously match predicted instrument %s or target %s", id, hyp.Predicts.Instrument, hyp.Predicts.Target),
				"confirm the design notes explain why this lesson applies")
		}
	}
	if err := firewall.CheckInspiredByLessonsReviewed(s, lessons); err != nil {
		r.add(PreflightSeverityError, "lesson_not_reviewed", "",
			err.Error(),
			"use only active lessons sourced from system facts or reviewed decisive chains")
	}
}

func lessonMatchesHypothesis(lesson *entity.Lesson, hyp *entity.Hypothesis) bool {
	if lesson == nil || hyp == nil {
		return false
	}
	if lesson.PredictedEffect != nil && lesson.PredictedEffect.Instrument == hyp.Predicts.Instrument {
		return true
	}
	for _, tag := range lesson.Tags {
		if tag == hyp.Predicts.Instrument || tag == hyp.Predicts.Target {
			return true
		}
	}
	text := strings.ToLower(lesson.Claim + "\n" + lesson.Body)
	for _, needle := range []string{hyp.Predicts.Instrument, hyp.Predicts.Target} {
		needle = strings.ToLower(strings.TrimSpace(needle))
		if needle != "" && strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func (r *ExperimentPreflightReport) checkMechanismEvidence(exp *entity.Experiment, hyp *entity.Hypothesis, cfg *store.Config) {
	c := &entity.Conclusion{
		Effect: entity.Effect{Instrument: hyp.Predicts.Instrument},
		Body:   entity.ExtractSection(exp.Body, "Design notes"),
	}
	if !containsMechanismLanguage(c, hyp) {
		return
	}
	if cfg != nil {
		for _, name := range exp.Instruments {
			inst, ok := cfg.Instruments[name]
			if ok && len(inst.Evidence) > 0 {
				return
			}
		}
	}
	r.add(PreflightSeverityWarning, "mechanism_evidence_unconfigured", exp.ID,
		"mechanism/counter language appears in the hypothesis or design notes, but no experiment instrument has evidence artifacts configured",
		"configure instrument evidence or ensure the eventual diff visibly proves the mechanism")
}

func (r *ExperimentPreflightReport) checkBaseline(s *store.Store, exp *entity.Experiment, hyp *entity.Hypothesis) {
	obsIdx, err := LoadObservationIndexStrict(s)
	if err != nil {
		r.add(PreflightSeverityWarning, "baseline_unreadable", exp.ID, err.Error(),
			"fix observation reads before relying on automatic baseline resolution")
		return
	}
	res, err := ResolveInferredBaselineWithIndex(s, obsIdx, hyp, exp, hyp.Predicts.Instrument)
	if err != nil {
		var amb *BaselineScopeAmbiguityError
		if errors.As(err, &amb) {
			r.add(PreflightSeverityWarning, "baseline_ambiguous", exp.ID, amb.Error(),
				"record the intended baseline in design notes and pass the explicit baseline/ref at analyze or conclude time")
			return
		}
		r.add(PreflightSeverityWarning, "baseline_resolution_failed", exp.ID, err.Error(),
			"resolve the baseline manually before implementing")
		return
	}
	r.Baseline = res
	if res == nil || strings.TrimSpace(res.ExperimentID) == "" {
		note := "no usable baseline could be inferred"
		if res != nil && strings.TrimSpace(res.Note) != "" {
			note = res.Note
		}
		r.add(PreflightSeverityWarning, "baseline_unresolved", exp.ID, note,
			"measure a baseline or plan an explicit baseline override before concluding")
	}
}

func (r *ExperimentPreflightReport) add(severity, code, subject, message, recommendation string) {
	if r == nil {
		return
	}
	if slices.ContainsFunc(r.Issues, func(issue ExperimentPreflightIssue) bool {
		return issue.Severity == severity && issue.Code == code && issue.Subject == subject && issue.Message == message
	}) {
		return
	}
	r.Issues = append(r.Issues, ExperimentPreflightIssue{
		Severity:       severity,
		Code:           code,
		Subject:        subject,
		Message:        message,
		Recommendation: recommendation,
	})
	switch severity {
	case PreflightSeverityError:
		r.Errors++
	case PreflightSeverityWarning:
		r.Warnings++
	}
}

func (r *ExperimentPreflightReport) finish() {
	r.OK = r.Errors == 0
}
