package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

func reportCommands() []*cobra.Command {
	return []*cobra.Command{
		{
			Use:   "report <hyp-id>",
			Short: "Render a formatted markdown writeup of a hypothesis and its evidence",
			Long: `Produce a human-readable report for a hypothesis: the falsifiable
prediction, every experiment run for it, every observation recorded in
those experiments, every conclusion written against it, and the subset
of the event log that concerns this hypothesis.

Text output is Markdown. --json returns a structured summary without
the markdown rendering. The report is read-only.`,
			Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				w := output.Default(globalJSON)
				s, err := openStore()
				if err != nil {
					return err
				}
				hyp, err := s.ReadHypothesis(args[0])
				if err != nil {
					return err
				}
				rep, err := buildReport(s, hyp)
				if err != nil {
					return err
				}
				if w.IsJSON() {
					return w.JSON(rep)
				}
				w.Textln(renderReportMarkdown(rep))
				return nil
			},
		},
	}
}

// reportData is the structured form returned by --json.
type reportData struct {
	Hypothesis  *entity.Hypothesis   `json:"hypothesis"`
	Experiments []*experimentBlock   `json:"experiments"`
	Conclusions []*entity.Conclusion `json:"conclusions"`
	Lessons     []*entity.Lesson     `json:"lessons"`
	Events      []eventRef           `json:"events"`
}

type experimentBlock struct {
	Experiment   *entity.Experiment    `json:"experiment"`
	Observations []*entity.Observation `json:"observations"`
}

type eventRef struct {
	Ts      string          `json:"ts"`
	Kind    string          `json:"kind"`
	Actor   string          `json:"actor,omitempty"`
	Subject string          `json:"subject,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func buildReport(s *store.Store, hyp *entity.Hypothesis) (*reportData, error) {
	r := &reportData{Hypothesis: hyp}

	exps, err := s.ListExperimentsForHypothesis(hyp.ID)
	if err != nil {
		return nil, err
	}
	for _, e := range exps {
		obs, err := s.ListObservationsForExperiment(e.ID)
		if err != nil {
			return nil, err
		}
		r.Experiments = append(r.Experiments, &experimentBlock{
			Experiment:   e,
			Observations: obs,
		})
	}

	concls, err := s.ListConclusionsForHypothesis(hyp.ID)
	if err != nil {
		return nil, err
	}
	r.Conclusions = concls

	// Lessons tied to this hypothesis: any lesson whose Subjects include
	// the hypothesis id OR any of its conclusion ids. Deduplicate by
	// lesson id so a lesson citing both H and its C is not rendered twice.
	seenLessons := map[string]bool{}
	collectLessons := func(subjectID string) error {
		ls, err := s.ListLessonsForSubject(subjectID)
		if err != nil {
			return err
		}
		for _, l := range ls {
			if seenLessons[l.ID] {
				continue
			}
			view, err := annotateLessonForRead(s, l)
			if err != nil {
				return err
			}
			seenLessons[l.ID] = true
			r.Lessons = append(r.Lessons, view)
		}
		return nil
	}
	if err := collectLessons(hyp.ID); err != nil {
		return nil, err
	}
	for _, c := range concls {
		if err := collectLessons(c.ID); err != nil {
			return nil, err
		}
	}

	// Subset of the event log: events whose subject is this hypothesis,
	// any of its experiments, observations, or conclusions.
	relevant := map[string]bool{hyp.ID: true}
	for _, e := range exps {
		relevant[e.ID] = true
	}
	for _, blk := range r.Experiments {
		for _, o := range blk.Observations {
			relevant[o.ID] = true
		}
	}
	for _, c := range concls {
		relevant[c.ID] = true
	}
	for _, l := range r.Lessons {
		relevant[l.ID] = true
	}
	all, err := s.Events(0)
	if err != nil {
		return nil, err
	}
	for _, ev := range all {
		if !relevant[ev.Subject] {
			continue
		}
		r.Events = append(r.Events, eventRef{
			Ts:      ev.Ts.UTC().Format("2006-01-02T15:04:05Z"),
			Kind:    ev.Kind,
			Actor:   ev.Actor,
			Subject: ev.Subject,
			Data:    ev.Data,
		})
	}
	return r, nil
}

func renderReportMarkdown(r *reportData) string {
	var sb strings.Builder

	h := r.Hypothesis
	fmt.Fprintf(&sb, "# %s — %s\n\n", h.ID, oneLine(h.Claim))
	fmt.Fprintf(&sb, "**Status**: %s  \n", h.Status)
	if h.Priority != "" {
		fmt.Fprintf(&sb, "**Priority**: %s  \n", h.Priority)
	}
	fmt.Fprintf(&sb, "**Author**: %s  \n", h.Author)
	fmt.Fprintf(&sb, "**Created**: %s  \n", h.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"))
	if h.Parent != "" {
		fmt.Fprintf(&sb, "**Parent**: %s  \n", h.Parent)
	}
	sb.WriteString("\n")

	if rationale := strings.TrimSpace(entity.ExtractSection(h.Body, "Rationale")); rationale != "" {
		sb.WriteString("## Rationale\n\n")
		sb.WriteString(rationale)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Prediction\n\n")
	fmt.Fprintf(&sb, "- **Instrument**: `%s`\n", h.Predicts.Instrument)
	fmt.Fprintf(&sb, "- **Target**: `%s`\n", h.Predicts.Target)
	fmt.Fprintf(&sb, "- **Direction**: %s\n", h.Predicts.Direction)
	fmt.Fprintf(&sb, "- **Minimum effect**: %.4f (fractional)\n", h.Predicts.MinEffect)
	sb.WriteString("- **Kill criteria**:\n")
	for _, k := range h.KillIf {
		fmt.Fprintf(&sb, "  - %s\n", k)
	}
	sb.WriteString("\n")

	if len(r.Experiments) == 0 {
		sb.WriteString("## Experiments\n\n_No experiments yet._\n\n")
	} else {
		sb.WriteString("## Experiments\n\n")
		for _, blk := range r.Experiments {
			e := blk.Experiment
			fmt.Fprintf(&sb, "### %s — %s\n\n", e.ID, e.Status)
			fmt.Fprintf(&sb, "- **Baseline**: `%s`", e.Baseline.Ref)
			if e.Baseline.SHA != "" {
				fmt.Fprintf(&sb, " at `%s`", shortSHA(e.Baseline.SHA))
			}
			sb.WriteString("\n")
			if e.Baseline.Experiment != "" {
				fmt.Fprintf(&sb, "- **Compares to**: %s\n", e.Baseline.Experiment)
			}
			fmt.Fprintf(&sb, "- **Instruments**: %s\n", strings.Join(e.Instruments, ", "))
			if e.Worktree != "" {
				fmt.Fprintf(&sb, "- **Worktree**: `%s`\n", e.Worktree)
				fmt.Fprintf(&sb, "- **Branch**: `%s`\n", e.Branch)
			}
			if len(blk.Observations) > 0 {
				sb.WriteString("- **Observations**:\n")
				for _, o := range blk.Observations {
					fmt.Fprintf(&sb, "  - %s `%s` = %.6g %s", o.ID, o.Instrument, o.Value, o.Unit)
					if o.Samples > 1 && o.CILow != nil && o.CIHigh != nil {
						fmt.Fprintf(&sb, "  [%.6g, %.6g]  n=%d", *o.CILow, *o.CIHigh, o.Samples)
					}
					if o.Pass != nil {
						fmt.Fprintf(&sb, "  pass=%v", *o.Pass)
					}
					sb.WriteString("\n")
					if len(o.EvidenceFailures) > 0 {
						sb.WriteString("    - Evidence failures:\n")
						for _, failure := range o.EvidenceFailures {
							fmt.Fprintf(&sb, "      - %s\n", formatEvidenceFailure(failure))
						}
					}
				}
			}
			if design := strings.TrimSpace(entity.ExtractSection(e.Body, "Design notes")); design != "" {
				sb.WriteString("\n**Design notes**\n\n")
				sb.WriteString(design)
				sb.WriteString("\n")
			}
			if impl := strings.TrimSpace(entity.ExtractSection(e.Body, "Implementation notes")); impl != "" {
				sb.WriteString("\n**Implementation notes**\n\n")
				sb.WriteString(impl)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
	}

	if len(r.Conclusions) == 0 {
		sb.WriteString("## Conclusions\n\n_No conclusions yet._\n\n")
	} else {
		sb.WriteString("## Conclusions\n\n")
		for _, c := range r.Conclusions {
			heading := c.Verdict
			if c.Strict.RescuedBy != "" {
				heading = fmt.Sprintf("%s (rescued by `%s`)", c.Verdict, c.Strict.RescuedBy)
			} else if c.Strict.Directional && c.Verdict == entity.VerdictSupported {
				heading = fmt.Sprintf("%s (directional)", c.Verdict)
			}
			fmt.Fprintf(&sb, "### %s — %s\n\n", c.ID, heading)
			if c.Strict.RequestedFrom != "" {
				fmt.Fprintf(&sb, "> **Downgraded** from `%s` — strict firewall rejected the requested verdict:\n", c.Strict.RequestedFrom)
				for _, r := range c.Strict.Reasons {
					fmt.Fprintf(&sb, "> - %s\n", r)
				}
				sb.WriteString("\n")
			} else if c.Strict.RescuedBy != "" {
				fmt.Fprintf(&sb, "> **Rescued** — primary was neutral (within the goal's `neutral_band_frac`); verdict carried by rescuer `%s`:\n", c.Strict.RescuedBy)
				for _, r := range c.Strict.Reasons {
					fmt.Fprintf(&sb, "> - %s\n", r)
				}
				sb.WriteString("\n")
			} else if c.Strict.Directional && c.Verdict == entity.VerdictSupported {
				sb.WriteString("> **Directional** — the hypothesis predicted direction only (`min_effect: 0`); any clean-CI effect in the predicted direction counts as supported. Follow-up hypotheses should refine this into a quantitative claim once the magnitude is known.\n\n")
			}
			fmt.Fprintf(&sb, "- **Candidate experiment**: %s\n", c.CandidateExp)
			if c.BaselineExp != "" {
				fmt.Fprintf(&sb, "- **Baseline experiment**: %s\n", c.BaselineExp)
			}
			fmt.Fprintf(&sb, "- **Effect on `%s`**: %+.4f (fractional)", c.Effect.Instrument, c.Effect.DeltaFrac)
			if c.Effect.CILowFrac != 0 || c.Effect.CIHighFrac != 0 {
				fmt.Fprintf(&sb, "  95%% CI [%+.4f, %+.4f]", c.Effect.CILowFrac, c.Effect.CIHighFrac)
			}
			sb.WriteString("\n")
			if c.Effect.PValue > 0 {
				fmt.Fprintf(&sb, "- **p-value**: %.4g (%s)\n", c.Effect.PValue, c.StatTest)
			}
			fmt.Fprintf(&sb, "- **Samples**: n_candidate=%d, n_baseline=%d\n", c.Effect.NCandidate, c.Effect.NBaseline)
			if len(c.Observations) > 0 {
				fmt.Fprintf(&sb, "- **Observations**: %s\n", strings.Join(c.Observations, ", "))
			}
			fmt.Fprintf(&sb, "- **Author**: %s", c.Author)
			if c.ReviewedBy != "" {
				fmt.Fprintf(&sb, ", reviewed by %s", c.ReviewedBy)
			}
			sb.WriteString("\n\n")
			if strings.TrimSpace(c.Body) != "" {
				sb.WriteString(strings.TrimSpace(c.Body))
				sb.WriteString("\n\n")
			}
		}
	}

	if len(r.Lessons) > 0 {
		sb.WriteString("## Lessons tied to this hypothesis\n\n")
		for _, l := range r.Lessons {
			source := ""
			if l.Provenance != nil && l.Provenance.SourceChain != "" {
				source = ", " + l.Provenance.SourceChain
			}
			fmt.Fprintf(&sb, "- **%s** (%s, %s%s) — %s\n", l.ID, l.Scope, l.EffectiveStatus(), source, oneLine(l.Claim))
			if l.SupersededByID != "" {
				fmt.Fprintf(&sb, "  _superseded by %s_\n", l.SupersededByID)
			}
			if l.SupersedesID != "" {
				fmt.Fprintf(&sb, "  _supersedes %s_\n", l.SupersedesID)
			}
		}
		sb.WriteString("\n")
	}

	if len(r.Events) > 0 {
		sb.WriteString("## Event log (this hypothesis)\n\n")
		for _, ev := range r.Events {
			fmt.Fprintf(&sb, "- `%s`  **%s**  %s", ev.Ts, ev.Kind, ev.Subject)
			if ev.Actor != "" {
				fmt.Fprintf(&sb, "  _by %s_", ev.Actor)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func oneLine(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
}

func shortSHA(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}
