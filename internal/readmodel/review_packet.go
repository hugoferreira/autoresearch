package readmodel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/worktree"
)

type ReviewPacket struct {
	Conclusion          *entity.Conclusion        `json:"conclusion"`
	Goal                *entity.Goal              `json:"goal,omitempty"`
	Hypothesis          *entity.Hypothesis        `json:"hypothesis,omitempty"`
	CandidateExperiment *ExperimentReadView       `json:"candidate_experiment,omitempty"`
	BaselineExperiment  *ExperimentReadView       `json:"baseline_experiment,omitempty"`
	Observations        []ReviewPacketObservation `json:"observations"`
	ConstraintChecks    []entity.ClauseCheck      `json:"constraint_checks,omitempty"`
	Analysis            ReviewPacketAnalysis      `json:"analysis"`
	Diff                ReviewPacketDiff          `json:"diff"`
	ReadIssues          []ReviewPacketReadIssue   `json:"read_issues,omitempty"`
}

type ReviewPacketObservation struct {
	ID               string                   `json:"id"`
	Observation      *entity.Observation      `json:"observation,omitempty"`
	Artifacts        []ReviewPacketArtifact   `json:"artifacts,omitempty"`
	EvidenceFailures []entity.EvidenceFailure `json:"evidence_failures,omitempty"`
	ReadIssue        string                   `json:"read_issue,omitempty"`
}

type ReviewPacketArtifact struct {
	Artifact entity.Artifact `json:"artifact"`
	Readable bool            `json:"readable"`
	Issue    string          `json:"issue,omitempty"`
}

type ReviewPacketAnalysis struct {
	Instrument          string        `json:"instrument"`
	Effect              entity.Effect `json:"effect"`
	CandidateExperiment string        `json:"candidate_experiment,omitempty"`
	CandidateRef        string        `json:"candidate_ref,omitempty"`
	CandidateSHA        string        `json:"candidate_sha,omitempty"`
	BaselineExperiment  string        `json:"baseline_experiment,omitempty"`
	BaselineRef         string        `json:"baseline_ref,omitempty"`
	BaselineSHA         string        `json:"baseline_sha,omitempty"`
	Command             []string      `json:"command,omitempty"`
}

type ReviewPacketDiff struct {
	Base      string   `json:"base,omitempty"`
	Target    string   `json:"target,omitempty"`
	Command   []string `json:"command,omitempty"`
	Files     []string `json:"files,omitempty"`
	ShortStat string   `json:"short_stat,omitempty"`
}

type ReviewPacketReadIssue struct {
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	Message string `json:"message"`
}

func BuildReviewPacket(s *store.Store, conclusionID, projectDir string) (*ReviewPacket, error) {
	c, err := s.ReadConclusion(conclusionID)
	if err != nil {
		return nil, err
	}
	p := &ReviewPacket{
		Conclusion:       c,
		ConstraintChecks: cloneClauseChecks(c.SecondaryChecks),
		Analysis: ReviewPacketAnalysis{
			Instrument:          c.Effect.Instrument,
			Effect:              c.Effect,
			CandidateExperiment: c.CandidateExp,
			CandidateRef:        c.CandidateRef,
			CandidateSHA:        c.CandidateSHA,
			BaselineExperiment:  c.BaselineExp,
			BaselineRef:         c.BaselineRef,
			BaselineSHA:         c.BaselineSHA,
		},
	}
	p.Analysis.Command = reviewPacketAnalyzeCommand(c)
	readPacketHypothesis(s, p, c)
	readPacketExperiment(s, p, c.CandidateExp, true)
	readPacketExperiment(s, p, c.BaselineExp, false)
	readPacketObservations(s, p, c.Observations)
	buildReviewPacketDiff(p, projectDir)
	return p, nil
}

func readPacketHypothesis(s *store.Store, p *ReviewPacket, c *entity.Conclusion) {
	if strings.TrimSpace(c.Hypothesis) == "" {
		appendPacketIssue(p, "hypothesis", "", "conclusion has no hypothesis")
		return
	}
	h, err := s.ReadHypothesis(c.Hypothesis)
	if err != nil {
		appendPacketIssue(p, "hypothesis", c.Hypothesis, err.Error())
		return
	}
	p.Hypothesis = h
	if strings.TrimSpace(h.GoalID) == "" {
		return
	}
	g, err := s.ReadGoal(h.GoalID)
	if err != nil {
		appendPacketIssue(p, "goal", h.GoalID, err.Error())
		return
	}
	p.Goal = g
}

func readPacketExperiment(s *store.Store, p *ReviewPacket, id string, candidate bool) {
	if strings.TrimSpace(id) == "" {
		return
	}
	view, err := ReadExperimentForRead(s, id)
	if err != nil {
		kind := "baseline_experiment"
		if candidate {
			kind = "candidate_experiment"
		}
		appendPacketIssue(p, kind, id, err.Error())
		return
	}
	if candidate {
		p.CandidateExperiment = view
		return
	}
	p.BaselineExperiment = view
}

func readPacketObservations(s *store.Store, p *ReviewPacket, ids []string) {
	for _, id := range ids {
		row := ReviewPacketObservation{ID: id}
		obs, err := s.ReadObservation(id)
		if err != nil {
			row.ReadIssue = err.Error()
			p.Observations = append(p.Observations, row)
			appendPacketIssue(p, "observation", id, err.Error())
			continue
		}
		row.Observation = obs
		row.EvidenceFailures = cloneEvidenceFailures(obs.EvidenceFailures)
		for _, artifact := range obs.Artifacts {
			checked := checkPacketArtifact(s, artifact)
			row.Artifacts = append(row.Artifacts, checked)
			if !checked.Readable {
				appendPacketIssue(p, "artifact", fmt.Sprintf("%s/%s", obs.ID, artifact.Name), checked.Issue)
			}
		}
		p.Observations = append(p.Observations, row)
	}
}

func checkPacketArtifact(s *store.Store, artifact entity.Artifact) ReviewPacketArtifact {
	row := ReviewPacketArtifact{Artifact: artifact, Readable: true}
	switch {
	case strings.TrimSpace(artifact.Path) != "":
		if _, err := os.Stat(filepath.Join(s.DirPath(), artifact.Path)); err != nil {
			row.Readable = false
			row.Issue = err.Error()
		}
	case strings.TrimSpace(artifact.SHA) != "":
		sha, rel, _, err := s.ArtifactLocation(artifact.SHA)
		if err != nil {
			row.Readable = false
			row.Issue = err.Error()
			break
		}
		row.Artifact.SHA = sha
		row.Artifact.Path = rel
	default:
		row.Readable = false
		row.Issue = "artifact has no path or sha"
	}
	return row
}

func reviewPacketAnalyzeCommand(c *entity.Conclusion) []string {
	if strings.TrimSpace(c.CandidateExp) == "" {
		return nil
	}
	cmd := []string{"autoresearch", "analyze", c.CandidateExp}
	if strings.TrimSpace(c.BaselineExp) != "" {
		cmd = append(cmd, "--baseline", c.BaselineExp)
	}
	if strings.TrimSpace(c.CandidateRef) != "" {
		cmd = append(cmd, "--candidate-ref", c.CandidateRef)
	}
	if strings.TrimSpace(c.Effect.Instrument) != "" {
		cmd = append(cmd, "--instrument", c.Effect.Instrument)
	}
	return append(cmd, "--json")
}

func buildReviewPacketDiff(p *ReviewPacket, projectDir string) {
	c := p.Conclusion
	if strings.TrimSpace(c.BaselineSHA) == "" || strings.TrimSpace(c.CandidateSHA) == "" {
		appendPacketIssue(p, "diff", c.ID, "baseline_sha and candidate_sha are required for diff summary")
		return
	}
	p.Diff = ReviewPacketDiff{
		Base:    c.BaselineSHA,
		Target:  c.CandidateSHA,
		Command: []string{"git", "-C", projectDir, "diff", c.BaselineSHA + ".." + c.CandidateSHA},
	}
	files, shortStat, err := worktree.DiffSummary(projectDir, c.BaselineSHA, c.CandidateSHA)
	if err != nil {
		appendPacketIssue(p, "diff", c.ID, err.Error())
		return
	}
	p.Diff.Files = files
	p.Diff.ShortStat = shortStat
}

func appendPacketIssue(p *ReviewPacket, kind, subject, message string) {
	p.ReadIssues = append(p.ReadIssues, ReviewPacketReadIssue{
		Kind:    kind,
		Subject: subject,
		Message: message,
	})
}

func cloneClauseChecks(in []entity.ClauseCheck) []entity.ClauseCheck {
	out := make([]entity.ClauseCheck, len(in))
	copy(out, in)
	return out
}

func cloneEvidenceFailures(in []entity.EvidenceFailure) []entity.EvidenceFailure {
	out := make([]entity.EvidenceFailure, len(in))
	copy(out, in)
	return out
}

func ReviewPacketCommandString(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.ContainsAny(arg, " \t\n\"'") {
			quoted = append(quoted, fmt.Sprintf("%q", arg))
			continue
		}
		quoted = append(quoted, arg)
	}
	return strings.Join(quoted, " ")
}
