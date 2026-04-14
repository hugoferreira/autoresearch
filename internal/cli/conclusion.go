package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/spf13/cobra"
)

func conclusionCommands() []*cobra.Command {
	c := &cobra.Command{
		Use:   "conclusion",
		Short: "Inspect and (for the critic) downgrade existing conclusions",
	}
	c.AddCommand(conclusionListCmd(), conclusionShowCmd(), conclusionDowngradeCmd(), conclusionAcceptCmd(), conclusionAppealCmd())
	return []*cobra.Command{c}
}

func conclusionListCmd() *cobra.Command {
	var hyp, verdict, goalFlag string
	c := &cobra.Command{
		Use:   "list",
		Short: "List conclusions",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			scope, err := resolveGoalScope(s, goalFlag)
			if err != nil {
				return err
			}
			all, err := s.ListConclusions()
			if err != nil {
				return err
			}
			all, err = newGoalScopeResolver(s, scope).filterConclusions(all)
			if err != nil {
				return err
			}
			filtered := all[:0]
			for _, c := range all {
				if hyp != "" && c.Hypothesis != hyp {
					continue
				}
				if verdict != "" && c.Verdict != verdict {
					continue
				}
				filtered = append(filtered, c)
			}
			if w.IsJSON() {
				return w.JSON(filtered)
			}
			if len(filtered) == 0 {
				w.Textln("(no conclusions)")
				return nil
			}
			for _, c := range filtered {
				dg := ""
				if c.Strict.RequestedFrom != "" {
					dg = fmt.Sprintf("  [downgraded from %s]", c.Strict.RequestedFrom)
				}
				w.Textf("  %-8s  %-12s  hyp=%-8s  delta_frac=%+.4f  p=%.4g%s\n",
					c.ID, c.Verdict, c.Hypothesis, c.Effect.DeltaFrac, c.Effect.PValue, dg)
			}
			return nil
		},
	}
	c.Flags().StringVar(&hyp, "hypothesis", "", "filter by hypothesis id")
	c.Flags().StringVar(&verdict, "verdict", "", "filter by verdict (supported|refuted|inconclusive)")
	c.Flags().StringVar(&goalFlag, "goal", "", "goal to scope the list to (defaults to active goal; use 'all' for every goal)")
	return c
}

func conclusionShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show one conclusion",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			c, err := s.ReadConclusion(args[0])
			if err != nil {
				return err
			}
			if w.IsJSON() {
				return w.JSON(c)
			}
			w.Textf("id:           %s\n", c.ID)
			w.Textf("hypothesis:   %s\n", c.Hypothesis)
			w.Textf("verdict:      %s\n", c.Verdict)
			if c.Strict.RequestedFrom != "" {
				w.Textf("downgraded:   from %q with reasons:\n", c.Strict.RequestedFrom)
				for _, r := range c.Strict.Reasons {
					w.Textf("  - %s\n", r)
				}
			}
			w.Textf("candidate:    %s  (n=%d)\n", c.CandidateExp, c.Effect.NCandidate)
			if c.BaselineExp != "" {
				w.Textf("baseline:     %s  (n=%d)\n", c.BaselineExp, c.Effect.NBaseline)
			}
			w.Textf("effect on %s:\n", c.Effect.Instrument)
			w.Textf("  delta_frac: %+.4f  95%% CI [%+.4f, %+.4f]\n", c.Effect.DeltaFrac, c.Effect.CILowFrac, c.Effect.CIHighFrac)
			w.Textf("  delta_abs:  %+.6g  95%% CI [%+.6g, %+.6g]\n", c.Effect.DeltaAbs, c.Effect.CILowAbs, c.Effect.CIHighAbs)
			w.Textf("  p-value:    %.4g  (%s)\n", c.Effect.PValue, c.StatTest)
			if c.IncrementalExp != "" && c.IncrementalEffect != nil {
				ie := c.IncrementalEffect
				w.Textf("incremental:  %s  (vs frontier best)\n", c.IncrementalExp)
				w.Textf("  delta_frac: %+.4f  95%% CI [%+.4f, %+.4f]\n", ie.DeltaFrac, ie.CILowFrac, ie.CIHighFrac)
				w.Textf("  delta_abs:  %+.6g  95%% CI [%+.6g, %+.6g]\n", ie.DeltaAbs, ie.CILowAbs, ie.CIHighAbs)
				w.Textf("  p-value:    %.4g\n", ie.PValue)
			}
			w.Textf("author:       %s\n", c.Author)
			if c.ReviewedBy != "" {
				w.Textf("reviewed_by:  %s\n", c.ReviewedBy)
			}
			w.Textf("created_at:   %s\n", c.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
			if strings.TrimSpace(c.Body) != "" {
				w.Textln("")
				w.Textln(strings.TrimSpace(c.Body))
			}
			return nil
		},
	}
}

func conclusionDowngradeCmd() *cobra.Command {
	var (
		reason     string
		reviewedBy string
	)
	c := &cobra.Command{
		Use:   "downgrade <id>",
		Short: "Critic's verb: flip an existing conclusion to inconclusive with a reason",
		Long: `Downgrade a conclusion from supported or refuted to inconclusive. The
original verdict is preserved in the conclusion's strict_check.downgraded_from
field, the reason is appended to strict_check.reasons, and the hypothesis's
status updates to inconclusive.

This is the sole mutation the critic subagent is allowed to make on
existing conclusions. It cannot create new conclusions, it cannot
upgrade a conclusion, and it cannot re-downgrade something already
marked inconclusive.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			if strings.TrimSpace(reason) == "" {
				return errors.New("--reason is required")
			}
			s, err := openStoreLive()
			if err != nil {
				return err
			}
			c, err := s.ReadConclusion(args[0])
			if err != nil {
				return err
			}
			switch c.Verdict {
			case entity.VerdictSupported, entity.VerdictRefuted:
			case entity.VerdictInconclusive:
				return fmt.Errorf("%s is already inconclusive; nothing to downgrade", c.ID)
			default:
				return fmt.Errorf("%s has unknown verdict %q", c.ID, c.Verdict)
			}
			prev := c.Verdict
			c.Verdict = entity.VerdictInconclusive
			if c.Strict.RequestedFrom == "" {
				c.Strict.RequestedFrom = prev
			}
			c.Strict.Passed = false
			c.Strict.Reasons = append(c.Strict.Reasons, "critic downgrade: "+reason)
			if reviewedBy != "" {
				c.ReviewedBy = reviewedBy
			}
			if err := dryRun(w, fmt.Sprintf("downgrade %s from %s to inconclusive (%s)", c.ID, prev, reason), map[string]any{"id": c.ID, "from": prev, "reason": reason}); err != nil {
				return err
			}
			if err := s.WriteConclusion(c); err != nil {
				return err
			}
			// Update hypothesis status to match.
			hyp, err := s.ReadHypothesis(c.Hypothesis)
			if err == nil {
				hyp.Status = entity.VerdictInconclusive
				_ = s.WriteHypothesis(hyp)
			}
			lessonChanges, err := syncHypothesisLessons(s, c.Hypothesis, lessonSyncOnDowngrade)
			if err != nil {
				return err
			}
			for _, change := range lessonChanges {
				if err := emitEvent(s, lessonEventKindForStatus(change.ToStatus), or(reviewedBy, "agent:critic"), change.LessonID, map[string]any{
					"from_status": change.FromStatus,
					"to_status":   change.ToStatus,
					"from_source": change.FromSource,
					"to_source":   change.ToSource,
					"hypothesis":  c.Hypothesis,
					"conclusion":  c.ID,
				}); err != nil {
					return err
				}
			}
			if err := emitEvent(s, "conclusion.critic_downgrade", "agent:critic", c.ID, map[string]any{
				"from":        prev,
				"reason":      reason,
				"reviewed_by": reviewedBy,
				"hypothesis":  c.Hypothesis,
			}); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("downgraded %s: %s → inconclusive (%s)", c.ID, prev, reason),
				map[string]any{
					"status":     "ok",
					"id":         c.ID,
					"from":       prev,
					"to":         entity.VerdictInconclusive,
					"reason":     reason,
					"hypothesis": c.Hypothesis,
				},
			)
		},
	}
	c.Flags().StringVar(&reason, "reason", "", "why the conclusion is being downgraded (required)")
	c.Flags().StringVar(&reviewedBy, "reviewed-by", "", "critic agent identifier (e.g. agent:critic or human:alice)")
	return c
}

func conclusionAcceptCmd() *cobra.Command {
	var (
		reviewedBy string
		rationale  string
	)
	c := &cobra.Command{
		Use:   "accept <id>",
		Short: "Gate reviewer's verb: accept a conclusion and promote the hypothesis",
		Long: `Accept a conclusion after independent review. This records who reviewed
the conclusion and why, and promotes the hypothesis from "unreviewed" to
its final verdict (supported or refuted).

The rationale is required and must address three points: (1) the
statistical evidence is sound, (2) the code change matches the claimed
mechanism, and (3) no gaming or metric manipulation was detected.

This is a prerequisite for hypothesis apply in strict mode — a
conclusion cannot be shipped until it has been reviewed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			if strings.TrimSpace(reviewedBy) == "" {
				return errors.New("--reviewed-by is required (e.g. agent:gate-reviewer or human:alice)")
			}
			if strings.TrimSpace(rationale) == "" {
				return errors.New("--rationale is required (must cover: stats confirmed, code matches mechanism, no gaming)")
			}
			s, err := openStoreLive()
			if err != nil {
				return err
			}
			c, err := s.ReadConclusion(args[0])
			if err != nil {
				return err
			}
			if c.ReviewedBy != "" {
				return fmt.Errorf("%s has already been reviewed by %s", c.ID, c.ReviewedBy)
			}
			switch c.Verdict {
			case entity.VerdictSupported, entity.VerdictRefuted:
			case entity.VerdictInconclusive:
				return fmt.Errorf("%s is inconclusive — inconclusive conclusions do not need review", c.ID)
			default:
				return fmt.Errorf("%s has unknown verdict %q", c.ID, c.Verdict)
			}

			if err := dryRun(w, fmt.Sprintf("accept %s (verdict=%s, reviewed-by=%s)", c.ID, c.Verdict, reviewedBy), map[string]any{"id": c.ID, "verdict": c.Verdict, "reviewed_by": reviewedBy}); err != nil {
				return err
			}

			c.ReviewedBy = reviewedBy
			c.Body = entity.AppendMarkdownSection(c.Body, "Review", fmt.Sprintf("**Reviewed by:** %s\n\n%s", reviewedBy, rationale))
			if err := s.WriteConclusion(c); err != nil {
				return err
			}

			// Promote hypothesis from unreviewed to the verdict.
			hyp, err := s.ReadHypothesis(c.Hypothesis)
			if err == nil && hyp.Status == entity.StatusUnreviewed {
				hyp.Status = c.Verdict
				_ = s.WriteHypothesis(hyp)
			}
			lessonChanges, err := syncHypothesisLessons(s, c.Hypothesis, lessonSyncOnAccept)
			if err != nil {
				return err
			}
			for _, change := range lessonChanges {
				if err := emitEvent(s, lessonEventKindForStatus(change.ToStatus), reviewedBy, change.LessonID, map[string]any{
					"from_status": change.FromStatus,
					"to_status":   change.ToStatus,
					"from_source": change.FromSource,
					"to_source":   change.ToSource,
					"hypothesis":  c.Hypothesis,
					"conclusion":  c.ID,
				}); err != nil {
					return err
				}
			}

			if err := emitEvent(s, "conclusion.accept", reviewedBy, c.ID, map[string]any{
				"reviewed_by": reviewedBy,
				"verdict":     c.Verdict,
				"hypothesis":  c.Hypothesis,
				"rationale":   truncate(rationale, 200),
			}); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("accepted %s (verdict=%s, reviewed by %s)", c.ID, c.Verdict, reviewedBy),
				map[string]any{
					"status":      "ok",
					"id":          c.ID,
					"verdict":     c.Verdict,
					"reviewed_by": reviewedBy,
					"hypothesis":  c.Hypothesis,
				},
			)
		},
	}
	c.Flags().StringVar(&reviewedBy, "reviewed-by", "", "reviewer identifier (required, e.g. agent:gate-reviewer)")
	c.Flags().StringVar(&rationale, "rationale", "", "review rationale: must cover stats, mechanism, and no-gaming (required)")
	return c
}

func conclusionAppealCmd() *cobra.Command {
	var (
		rebuttal string
		author   string
	)
	c := &cobra.Command{
		Use:   "appeal <id>",
		Short: "Appeal a critic downgrade: restore the original verdict and request re-review",
		Long: `Appeal a conclusion that was downgraded by the gate reviewer. This
restores the original verdict, clears the reviewed-by field, and records
the rebuttal. The main session should then dispatch the gate reviewer
again with the rebuttal context.

Appeals are only valid for critic downgrades — you cannot appeal a
firewall downgrade (the numbers are the numbers). The rebuttal should
reference the reviewer's specific downgrade reason and explain why the
verdict should stand.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			if strings.TrimSpace(rebuttal) == "" {
				return errors.New("--rebuttal is required (must reference the reviewer's downgrade reason)")
			}
			s, err := openStoreLive()
			if err != nil {
				return err
			}
			c, err := s.ReadConclusion(args[0])
			if err != nil {
				return err
			}
			if c.Verdict != entity.VerdictInconclusive {
				return fmt.Errorf("%s has verdict %q — appeals are only valid for downgraded conclusions", c.ID, c.Verdict)
			}
			if c.Strict.RequestedFrom == "" {
				return fmt.Errorf("%s was not downgraded (no original verdict recorded) — nothing to appeal", c.ID)
			}
			// Only allow appeal of critic downgrades.
			hasCriticDowngrade := false
			for _, r := range c.Strict.Reasons {
				if strings.HasPrefix(r, "critic downgrade:") {
					hasCriticDowngrade = true
					break
				}
			}
			if !hasCriticDowngrade {
				return fmt.Errorf("%s was downgraded by the firewall, not a critic — you cannot appeal statistical evidence; collect better data instead", c.ID)
			}

			originalVerdict := c.Strict.RequestedFrom
			if err := dryRun(w, fmt.Sprintf("appeal %s: restore %s, request re-review", c.ID, originalVerdict), map[string]any{"id": c.ID, "original_verdict": originalVerdict, "rebuttal": rebuttal}); err != nil {
				return err
			}

			c.Verdict = originalVerdict
			c.ReviewedBy = ""
			c.Body = entity.AppendMarkdownSection(c.Body, "Appeal", rebuttal)
			if err := s.WriteConclusion(c); err != nil {
				return err
			}

			// Restore hypothesis to unreviewed.
			hyp, err := s.ReadHypothesis(c.Hypothesis)
			if err == nil {
				hyp.Status = entity.StatusUnreviewed
				_ = s.WriteHypothesis(hyp)
			}
			lessonChanges, err := syncHypothesisLessons(s, c.Hypothesis, lessonSyncOnAppeal)
			if err != nil {
				return err
			}
			for _, change := range lessonChanges {
				if err := emitEvent(s, lessonEventKindForStatus(change.ToStatus), or(author, "agent:orchestrator"), change.LessonID, map[string]any{
					"from_status": change.FromStatus,
					"to_status":   change.ToStatus,
					"from_source": change.FromSource,
					"to_source":   change.ToSource,
					"hypothesis":  c.Hypothesis,
					"conclusion":  c.ID,
				}); err != nil {
					return err
				}
			}

			if err := emitEvent(s, "conclusion.appeal", or(author, "agent:orchestrator"), c.ID, map[string]any{
				"original_verdict": originalVerdict,
				"hypothesis":       c.Hypothesis,
				"rebuttal":         truncate(rebuttal, 200),
			}); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("appealed %s: verdict restored to %s — dispatch the gate reviewer for re-review", c.ID, originalVerdict),
				map[string]any{
					"status":          "ok",
					"id":              c.ID,
					"verdict":         originalVerdict,
					"hypothesis":      c.Hypothesis,
					"awaiting_review": true,
				},
			)
		},
	}
	c.Flags().StringVar(&rebuttal, "rebuttal", "", "specific disagreement with the downgrade reason (required)")
	addAuthorFlag(c, &author, "")
	return c
}
