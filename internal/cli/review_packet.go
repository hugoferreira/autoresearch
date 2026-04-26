package cli

import (
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/readmodel"
	"github.com/spf13/cobra"
)

func reviewPacketCommands() []*cobra.Command {
	return []*cobra.Command{reviewPacketCmd()}
}

func reviewPacketCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "review-packet <C-id>",
		Short: "Aggregate the read-only evidence packet for reviewing a conclusion",
		Long: `Build a read-only packet for independently reviewing one conclusion.
The packet joins the conclusion, hypothesis, experiments, cited observations,
artifact metadata, constraint checks, analysis refs, diff summary, and explicit
read issues for missing cross-entity evidence.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			packet, err := readmodel.BuildReviewPacket(s, args[0], globalProjectDir)
			if err != nil {
				return err
			}
			if w.IsJSON() {
				return w.JSON(packet)
			}
			renderReviewPacketText(w, packet)
			return nil
		},
	}
}

func renderReviewPacketText(w *output.Writer, p *readmodel.ReviewPacket) {
	c := p.Conclusion
	w.Textf("review_packet: %s\n", c.ID)
	w.Textf("verdict:       %s\n", c.Verdict)
	w.Textf("hypothesis:    %s\n", c.Hypothesis)
	if p.Hypothesis != nil {
		w.Textf("claim:         %s\n", p.Hypothesis.Claim)
		w.Textf("hyp_status:    %s\n", p.Hypothesis.Status)
	}
	if p.Goal != nil {
		w.Textf("goal:          %s\n", p.Goal.ID)
	}
	w.Textf("candidate:     %s", c.CandidateExp)
	if c.CandidateRef != "" {
		w.Textf(" ref=%s", c.CandidateRef)
	}
	if c.CandidateSHA != "" {
		w.Textf(" sha=%s", shortSHA(c.CandidateSHA))
	}
	w.Textln("")
	if c.BaselineExp != "" || c.BaselineSHA != "" || c.BaselineRef != "" {
		w.Textf("baseline:      %s", c.BaselineExp)
		if c.BaselineRef != "" {
			w.Textf(" ref=%s", c.BaselineRef)
		}
		if c.BaselineSHA != "" {
			w.Textf(" sha=%s", shortSHA(c.BaselineSHA))
		}
		w.Textln("")
	}
	renderReviewPacketExperiment(w, "candidate_experiment", p.CandidateExperiment)
	renderReviewPacketExperiment(w, "baseline_experiment", p.BaselineExperiment)

	w.Textf("effect:        %s delta_frac=%+.4f  CI [%+.4f, %+.4f]  p=%.4g\n",
		c.Effect.Instrument, c.Effect.DeltaFrac, c.Effect.CILowFrac, c.Effect.CIHighFrac, c.Effect.PValue)
	if len(c.SecondaryChecks) > 0 {
		w.Textln("constraint_checks:")
		for _, check := range c.SecondaryChecks {
			status := "fail"
			if check.Passed {
				status = "pass"
			}
			w.Textf("  - %s role=%s status=%s\n", check.Instrument, check.Role, status)
		}
	}
	if len(p.Analysis.Command) > 0 {
		w.Textf("analysis:      %s\n", readmodel.ReviewPacketCommandString(p.Analysis.Command))
	}
	if len(p.Diff.Command) > 0 {
		w.Textf("diff:          %s\n", readmodel.ReviewPacketCommandString(p.Diff.Command))
		if p.Diff.ShortStat != "" {
			w.Textf("diff_stat:     %s\n", p.Diff.ShortStat)
		}
		for _, file := range p.Diff.Files {
			w.Textf("  - %s\n", file)
		}
	}

	renderReviewPacketObservations(w, p.Observations)
	if len(p.ReadIssues) > 0 {
		w.Textln("read_issues:")
		for _, issue := range p.ReadIssues {
			w.Textf("  - %s %s: %s\n", issue.Kind, issue.Subject, issue.Message)
		}
	}
}

func renderReviewPacketExperiment(w *output.Writer, label string, e *readmodel.ExperimentReadView) {
	if e == nil {
		return
	}
	w.Textf("%s: %s status=%s", label, e.ID, e.Status)
	if e.Worktree != "" {
		w.Textf(" worktree=%s", e.Worktree)
	}
	if e.Branch != "" {
		w.Textf(" branch=%s", e.Branch)
	}
	w.Textln("")
	if notes := strings.TrimSpace(entity.ExtractSection(e.Body, "Design notes")); notes != "" {
		w.Textf("  design_notes: %s\n", reviewPacketOneLine(notes))
	}
	if notes := strings.TrimSpace(entity.ExtractSection(e.Body, "Implementation notes")); notes != "" {
		w.Textf("  impl_notes:   %s\n", reviewPacketOneLine(notes))
	}
}

func renderReviewPacketObservations(w *output.Writer, rows []readmodel.ReviewPacketObservation) {
	if len(rows) == 0 {
		w.Textln("observations:  (none cited)")
		return
	}
	w.Textln("observations:")
	for _, row := range rows {
		if row.Observation == nil {
			w.Textf("  - %s: read issue: %s\n", row.ID, row.ReadIssue)
			continue
		}
		o := row.Observation
		w.Textf("  - %s %s value=%.6g%s samples=%d", o.ID, o.Instrument, o.Value, unitSuffix(o.Unit), o.Samples)
		if o.Pass != nil {
			w.Textf(" pass=%t", *o.Pass)
		}
		if o.CandidateRef != "" {
			w.Textf(" ref=%s", o.CandidateRef)
		}
		if o.CandidateSHA != "" {
			w.Textf(" sha=%s", shortSHA(o.CandidateSHA))
		}
		w.Textln("")
		for _, artifact := range row.Artifacts {
			a := artifact.Artifact
			status := "ok"
			if !artifact.Readable {
				status = "read_issue: " + artifact.Issue
			}
			w.Textf("      artifact %s sha=%s bytes=%d path=%s [%s]\n", a.Name, shortSHA(a.SHA), a.Bytes, a.Path, status)
		}
		for _, failure := range row.EvidenceFailures {
			w.Textf("      evidence_failure %s\n", formatEvidenceFailure(failure))
		}
	}
}

func unitSuffix(unit string) string {
	if unit == "" {
		return ""
	}
	return " " + unit
}

func reviewPacketOneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		return fmt.Sprintf("%s...", s[:157])
	}
	return s
}
