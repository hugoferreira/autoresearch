package cli

import (
	"fmt"
	"time"

	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

func budgetCommands() []*cobra.Command {
	b := &cobra.Command{
		Use:   "budget",
		Short: "View or set research budgets (max_experiments, max_wall_time_h, frontier_stall_k)",
	}
	b.AddCommand(budgetShowCmd(), budgetSetCmd())
	return []*cobra.Command{b}
}

func budgetShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the current budgets and usage",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			cfg, err := s.Config()
			if err != nil {
				return err
			}
			st, err := s.State()
			if err != nil {
				return err
			}

			expCount := st.Counters["E"]
			elapsed := time.Duration(0)
			if st.ResearchStartedAt != nil {
				elapsed = nowUTC().Sub(*st.ResearchStartedAt)
			}

			payload := map[string]any{
				"limits": map[string]any{
					"max_experiments":          cfg.Budgets.MaxExperiments,
					"max_wall_time_h":          cfg.Budgets.MaxWallTimeH,
					"frontier_stall_k":         cfg.Budgets.FrontierStallK,
					"stale_experiment_minutes": cfg.Budgets.StaleExperimentMinutes,
				},
				"usage": map[string]any{
					"experiments":         expCount,
					"elapsed_h":           elapsed.Hours(),
					"research_started_at": st.ResearchStartedAt,
				},
			}
			if w.IsJSON() {
				return w.JSON(payload)
			}
			w.Textln("limits:")
			w.Textf("  max_experiments:  %s\n", fmtOptionalInt(cfg.Budgets.MaxExperiments))
			w.Textf("  max_wall_time_h:  %s\n", fmtOptionalInt(cfg.Budgets.MaxWallTimeH))
			w.Textf("  frontier_stall_k: %s\n", fmtOptionalInt(cfg.Budgets.FrontierStallK))
			w.Textf("  stale_exp_min:    %s\n", fmtOptionalInt(cfg.Budgets.StaleExperimentMinutes))
			w.Textln("usage:")
			w.Textf("  experiments:      %d\n", expCount)
			if st.ResearchStartedAt != nil {
				w.Textf("  elapsed:          %s\n", elapsed.Round(time.Minute))
			} else {
				w.Textln("  elapsed:          (not yet started)")
			}
			return nil
		},
	}
}

func budgetSetCmd() *cobra.Command {
	var (
		maxExperiments         int
		maxWallTimeH           int
		frontierStallK         int
		staleExperimentMinutes int
		author                 string
	)
	c := &cobra.Command{
		Use:   "set",
		Short: "Set or clear budget limits (pass -1 to clear a specific limit)",
		Long: `Update one or more budget limits. Passing 0 leaves the limit
unchanged; passing -1 clears it. Passing a positive integer sets it.

    autoresearch budget set --max-experiments 50 --frontier-stall-k 5

Budgets are enforced in "dry-up" mode: when a new experiment design would
exceed a limit, the CLI refuses with exit code 4 (budget exhausted).
Experiments already in flight (implemented/measured/analyzing) are not
touched — finish what you started, open no new fronts.

These limits are ceilings, not quotas. It is correct to stop early when
there is no credible next experiment, the frontier is exhausted, or a
decisive conclusion is still waiting on gate review.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStoreLive()
			if err != nil {
				return err
			}
			cfg, err := s.Config()
			if err != nil {
				return err
			}
			prev := cfg.Budgets
			applyBudgetDelta(&cfg.Budgets.MaxExperiments, maxExperiments)
			applyBudgetDelta(&cfg.Budgets.MaxWallTimeH, maxWallTimeH)
			applyBudgetDelta(&cfg.Budgets.FrontierStallK, frontierStallK)
			applyBudgetDelta(&cfg.Budgets.StaleExperimentMinutes, staleExperimentMinutes)

			if err := dryRun(w, fmt.Sprintf("update budgets: %+v", cfg.Budgets), map[string]any{"budgets": cfg.Budgets}); err != nil {
				return err
			}
			if err := s.UpdateConfig(func(c *store.Config) error {
				c.Budgets = cfg.Budgets
				return nil
			}); err != nil {
				return err
			}
			if err := emitEvent(s, "budget.set", author, "", map[string]any{
				"previous": prev,
				"updated":  cfg.Budgets,
			}); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("budgets updated: max_experiments=%s max_wall_time_h=%s frontier_stall_k=%s stale_experiment_minutes=%s",
					fmtOptionalInt(cfg.Budgets.MaxExperiments),
					fmtOptionalInt(cfg.Budgets.MaxWallTimeH),
					fmtOptionalInt(cfg.Budgets.FrontierStallK),
					fmtOptionalInt(cfg.Budgets.StaleExperimentMinutes)),
				map[string]any{"status": "ok", "budgets": cfg.Budgets},
			)
		},
	}
	c.Flags().IntVar(&maxExperiments, "max-experiments", 0, "ceiling on total experiments; not a target (-1 to clear, 0 to leave unchanged)")
	c.Flags().IntVar(&maxWallTimeH, "max-wall-time-h", 0, "wall-time budget in hours since init (-1 to clear)")
	c.Flags().IntVar(&frontierStallK, "frontier-stall-k", 0, "stop suggestion after K conclusions without frontier improvement (-1 to clear)")
	c.Flags().IntVar(&staleExperimentMinutes, "stale-experiment-minutes", 0, "flag experiments idle longer than N minutes in status/dashboard (-1 to clear)")
	addAuthorFlag(c, &author, "")
	return c
}

func applyBudgetDelta(target *int, delta int) {
	switch {
	case delta == 0:
		// unchanged
	case delta < 0:
		*target = 0 // clear
	default:
		*target = delta
	}
}

func fmtOptionalInt(v int) string {
	if v <= 0 {
		return "(none)"
	}
	return fmt.Sprintf("%d", v)
}
