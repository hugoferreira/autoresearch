package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

func conclusionCommands() []*cobra.Command {
	c := &cobra.Command{
		Use:   "conclusion",
		Short: "Inspect and (for the critic) downgrade existing conclusions",
	}
	c.AddCommand(conclusionListCmd(), conclusionShowCmd(), conclusionDowngradeCmd())
	return []*cobra.Command{c}
}

func conclusionListCmd() *cobra.Command {
	var hyp, verdict string
	c := &cobra.Command{
		Use:   "list",
		Short: "List conclusions",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			all, err := s.ListConclusions()
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
			if globalDryRun {
				return w.Emit(
					fmt.Sprintf("[dry-run] would downgrade %s from %s to inconclusive (%s)", c.ID, prev, reason),
					map[string]any{"status": "dry-run", "id": c.ID, "from": prev, "reason": reason},
				)
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
			if err := s.AppendEvent(store.Event{
				Kind:    "conclusion.critic_downgrade",
				Actor:   "agent:critic",
				Subject: c.ID,
				Data: jsonRaw(map[string]any{
					"from":        prev,
					"reason":      reason,
					"reviewed_by": reviewedBy,
					"hypothesis":  c.Hypothesis,
				}),
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
