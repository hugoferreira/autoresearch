package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/instrument"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/worktree"
	"github.com/spf13/cobra"
)

func experimentCommands() []*cobra.Command {
	e := &cobra.Command{
		Use:     "experiment",
		Aliases: []string{"exp"},
		Short:   "Design, implement, and inspect experiments",
	}
	e.AddCommand(
		experimentDesignCmd(),
		experimentImplementCmd(),
		experimentResetCmd(),
		experimentBaselineCmd(),
		experimentWorktreeCmd(),
		experimentListCmd(),
		experimentShowCmd(),
	)
	return []*cobra.Command{e}
}

func experimentDesignCmd() *cobra.Command {
	var (
		baseline    string
		instruments []string
		author      string
		wallTimeS   int
		maxSamples  int
		designNotes string
	)
	c := &cobra.Command{
		Use:   "design <hyp-id>",
		Short: "Design an experiment for a hypothesis",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			hypID := args[0]
			if len(instruments) == 0 {
				return errors.New("--instruments is required (at least one)")
			}
			s, err := openStoreLive()
			if err != nil {
				return err
			}
			cfg, err := s.Config()
			if err != nil {
				return err
			}

			h, err := s.ReadHypothesis(hypID)
			if err != nil {
				return err
			}
			if h.Status == entity.StatusKilled {
				return fmt.Errorf("hypothesis %s is killed; cannot design new experiments for it", hypID)
			}

			state, err := s.State()
			if err != nil {
				return err
			}
			if breach := firewall.CheckBudgetForNewExperiment(state, cfg, nowUTC()); !breach.Ok() {
				return fmt.Errorf("%w (%s): %s", ErrBudgetExhausted, breach.Rule, breach.Message)
			}

			e := &entity.Experiment{
				Hypothesis:  hypID,
				Status:      entity.ExpDesigned,
				Baseline:    entity.Baseline{Ref: baseline},
				Instruments: instruments,
				Budget:      entity.Budget{WallTimeS: wallTimeS, MaxSamples: maxSamples},
				Author:      or(author, "agent:orchestrator"),
				CreatedAt:   nowUTC(),
				Body:        entity.AppendMarkdownSection("", "Design notes", designNotes),
			}
			if err := firewall.ValidateExperiment(e, cfg); err != nil {
				return err
			}

			sha, err := worktree.ResolveRef(globalProjectDir, baseline)
			if err != nil {
				return fmt.Errorf("resolve baseline %q: %w", baseline, err)
			}
			e.Baseline.SHA = sha

			if err := dryRun(w, fmt.Sprintf("design experiment for %s (baseline=%s)", hypID, sha[:12]), map[string]any{"experiment": e}); err != nil {
				return err
			}

			id, err := s.AllocID(store.KindExperiment)
			if err != nil {
				return err
			}
			e.ID = id
			if err := s.WriteExperiment(e); err != nil {
				return err
			}
			eventData := map[string]any{
				"hypothesis":  hypID,
				"baseline":    sha,
				"instruments": instruments,
			}
			if snippet := truncate(strings.TrimSpace(designNotes), 200); snippet != "" {
				eventData["design_notes"] = snippet
			}
			if err := emitEvent(s, "experiment.design", or(author, "agent:orchestrator"), id, eventData); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("designed %s for %s (baseline=%s)", id, hypID, sha[:12]),
				map[string]any{"status": "ok", "id": id, "experiment": e},
			)
		},
	}
	c.Flags().StringVar(&baseline, "baseline", "HEAD", "git ref to use as baseline")
	c.Flags().StringSliceVar(&instruments, "instruments", nil, "comma-separated instrument names (required)")
	addAuthorFlag(c, &author, "")
	c.Flags().IntVar(&wallTimeS, "wall-time-s", 0, "wall-time budget in seconds")
	c.Flags().IntVar(&maxSamples, "max-samples", 0, "max samples for this experiment")
	c.Flags().StringVar(&designNotes, "design-notes", "", "prose notes on why these instruments, this baseline (persisted in the experiment body under `# Design notes`; first 200 chars also land on the experiment.design event)")
	return c
}

func experimentImplementCmd() *cobra.Command {
	var implNotes string
	c := &cobra.Command{
		Use:   "implement <exp-id>",
		Short: "Spawn the experiment's worktree and mark it implemented",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			id := args[0]

			s, err := openStoreLive()
			if err != nil {
				return err
			}
			e, err := s.ReadExperiment(id)
			if err != nil {
				return err
			}
			if e.Status != entity.ExpDesigned {
				return fmt.Errorf("%s is in status %q, expected %q", id, e.Status, entity.ExpDesigned)
			}

			wtRoot, err := s.WorktreesRoot()
			if err != nil {
				return err
			}
			wtPath := filepath.Join(wtRoot, id)
			branch := "autoresearch/" + id

			if err := dryRun(w, fmt.Sprintf("create worktree at %s on branch %s", wtPath, branch), map[string]any{"worktree": wtPath, "branch": branch}); err != nil {
				return err
			}

			if err := os.MkdirAll(wtRoot, 0o755); err != nil {
				return err
			}
			if err := worktree.Add(globalProjectDir, wtPath, branch, e.Baseline.SHA); err != nil {
				return fmt.Errorf("create worktree: %w", err)
			}

			e.Worktree = wtPath
			e.Branch = branch
			e.Status = entity.ExpImplemented
			e.Body = entity.AppendMarkdownSection(e.Body, "Implementation notes", implNotes)
			if err := s.WriteExperiment(e); err != nil {
				return err
			}

			if err := writeWorktreeBrief(s, e, wtPath, implNotes); err != nil {
				return fmt.Errorf("write worktree brief: %w", err)
			}

			eventData := map[string]any{"worktree": wtPath, "branch": branch}
			if snippet := truncate(strings.TrimSpace(implNotes), 200); snippet != "" {
				eventData["impl_notes"] = snippet
			}
			if err := emitEvent(s, "experiment.implement", "system", id, eventData); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("implemented %s\n  worktree: %s\n  branch:   %s", id, wtPath, branch),
				map[string]any{"status": "ok", "id": id, "worktree": wtPath, "branch": branch},
			)
		},
	}
	c.Flags().StringVar(&implNotes, "impl-notes", "", "prose notes on what you noticed while applying the change — trade-offs, edge cases, anomalies (appended to the experiment body under `# Implementation notes`; first 200 chars also land on the experiment.implement event)")
	return c
}

func experimentResetCmd() *cobra.Command {
	var reason, author string
	c := &cobra.Command{
		Use:   "reset <exp-id>",
		Short: "Reset an experiment back to 'designed' (preserves the abandoned branch)",
		Long: `Remove the experiment's worktree and reset its status to 'designed' so
it can be re-implemented. The previous branch is kept and renamed to
autoresearch/<exp-id>@<timestamp> so any commits the implementer made
remain inspectable. An explicit --reason is required and logged to
events.jsonl — the research history tells the truth about retries.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			id := args[0]
			if strings.TrimSpace(reason) == "" {
				return errors.New("--reason is required (retries must be auditable)")
			}

			s, err := openStoreLive()
			if err != nil {
				return err
			}
			e, err := s.ReadExperiment(id)
			if err != nil {
				return err
			}
			if e.Status == entity.ExpDesigned {
				return fmt.Errorf("%s is already in 'designed' status; nothing to reset", id)
			}

			abandoned := fmt.Sprintf("%s@%d", e.Branch, nowUTC().UnixMilli())

			if err := dryRun(w, fmt.Sprintf("reset %s (preserving %s as %s)", id, e.Branch, abandoned), map[string]any{"id": id, "abandoned_branch": abandoned, "reason": reason}); err != nil {
				return err
			}

			if e.Worktree != "" {
				if err := worktree.Remove(globalProjectDir, e.Worktree); err != nil {
					return fmt.Errorf("remove worktree: %w", err)
				}
			}
			if e.Branch != "" {
				if err := worktree.RenameBranch(globalProjectDir, e.Branch, abandoned); err != nil {
					return fmt.Errorf("rename abandoned branch: %w", err)
				}
			}

			prevStatus := e.Status
			prevBranch := e.Branch
			e.Status = entity.ExpDesigned
			e.Worktree = ""
			e.Branch = ""
			if err := s.WriteExperiment(e); err != nil {
				return err
			}
			if err := emitEvent(s, "experiment.reset", or(author, "human"), id, map[string]any{
				"reason":           reason,
				"from_status":      prevStatus,
				"abandoned_branch": abandoned,
				"preserved_from":   prevBranch,
			}); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("reset %s (%s); previous attempt preserved as branch %s", id, reason, abandoned),
				map[string]any{"status": "ok", "id": id, "abandoned_branch": abandoned, "reason": reason},
			)
		},
	}
	c.Flags().StringVar(&reason, "reason", "", "why the experiment is being reset (required)")
	addAuthorFlag(c, &author, "")
	return c
}

func experimentBaselineCmd() *cobra.Command {
	var (
		baseline    string
		author      string
		designNotes string
	)
	c := &cobra.Command{
		Use:   "baseline",
		Short: "Create a baseline experiment: design, implement, and observe in one shot",
		Long: `Create a baseline experiment at the given git ref (default HEAD), spawn a
worktree, and observe all instruments relevant to the active goal. This
establishes reference measurements that subsequent experiments compare
against.

A baseline experiment has no hypothesis — it measures the unmodified code.
The returned experiment ID is used as --baseline-experiment in conclude.`,
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
			state, err := s.State()
			if err != nil {
				return err
			}
			goal, err := s.ActiveGoal()
			if err != nil {
				return err
			}

			// Collect instruments: objective + all constraints, deduplicated.
			seen := map[string]bool{}
			var instruments []string
			add := func(name string) {
				if !seen[name] {
					seen[name] = true
					instruments = append(instruments, name)
				}
			}
			add(goal.Objective.Instrument)
			for _, c := range goal.Constraints {
				add(c.Instrument)
			}

			if breach := firewall.CheckBudgetForNewExperiment(state, cfg, nowUTC()); !breach.Ok() {
				return fmt.Errorf("%w (%s): %s", ErrBudgetExhausted, breach.Rule, breach.Message)
			}

			sha, err := worktree.ResolveRef(globalProjectDir, baseline)
			if err != nil {
				return fmt.Errorf("resolve baseline %q: %w", baseline, err)
			}

			e := &entity.Experiment{
				IsBaseline:  true,
				Status:      entity.ExpDesigned,
				Baseline:    entity.Baseline{Ref: baseline, SHA: sha},
				Instruments: instruments,
				Author:      or(author, "system"),
				CreatedAt:   nowUTC(),
				Body:        entity.AppendMarkdownSection("", "Design notes", or(designNotes, "Auto-generated baseline experiment for "+goal.ID)),
			}
			if err := firewall.ValidateExperiment(e, cfg); err != nil {
				return err
			}

			if err := dryRun(w, fmt.Sprintf("create baseline experiment (ref=%s, sha=%s)", baseline, sha[:12]), map[string]any{"experiment": e}); err != nil {
				return err
			}

			// --- Phase 1: Design ---
			id, err := s.AllocID(store.KindExperiment)
			if err != nil {
				return err
			}
			e.ID = id
			if err := s.WriteExperiment(e); err != nil {
				return err
			}
			if err := emitEvent(s, "experiment.baseline", or(author, "system"), id, map[string]any{
				"baseline":    sha,
				"instruments": instruments,
				"goal":        goal.ID,
			}); err != nil {
				return err
			}

			// --- Phase 2: Implement (create worktree) ---
			wtRoot, err := s.WorktreesRoot()
			if err != nil {
				return err
			}
			wtPath := filepath.Join(wtRoot, id)
			branch := "autoresearch/" + id

			if err := os.MkdirAll(wtRoot, 0o755); err != nil {
				return err
			}
			if err := worktree.Add(globalProjectDir, wtPath, branch, sha); err != nil {
				return fmt.Errorf("create worktree: %w", err)
			}

			e.Worktree = wtPath
			e.Branch = branch
			e.Status = entity.ExpImplemented
			if err := s.WriteExperiment(e); err != nil {
				return err
			}
			if err := writeWorktreeBrief(s, e, wtPath, ""); err != nil {
				// Non-fatal for baselines: no hypothesis to brief about.
				_ = err
			}
			if err := emitEvent(s, "experiment.implement", "system", id, map[string]any{
				"worktree": wtPath,
				"branch":   branch,
			}); err != nil {
				return err
			}

			// --- Phase 3: Observe all instruments ---
			// Iterate in dependency-safe order: skip instruments whose
			// requirements are not yet met, retry until all done or stuck.
			type obsResult struct {
				id    string
				inst  string
				value float64
				unit  string
			}
			var results []obsResult
			observed := map[string]bool{}
			var priorObs []*entity.Observation

			remaining := make([]string, len(instruments))
			copy(remaining, instruments)

			for len(remaining) > 0 {
				progress := false
				var deferred []string
				for _, instName := range remaining {
					if err := firewall.CheckInstrumentDependencies(instName, cfg, priorObs); err != nil {
						deferred = append(deferred, instName)
						continue
					}

					inst := cfg.Instruments[instName]
					ctx := context.Background()
					result, err := instrument.Run(ctx, instrument.Config{
						ProjectDir:  globalProjectDir,
						WorktreeDir: wtPath,
						Name:        instName,
						Instrument:  inst,
					})
					if err != nil {
						return fmt.Errorf("instrument %s failed: %w", instName, err)
					}

					var obsArts []entity.Artifact
					for _, ac := range result.Artifacts {
						artSHA, rel, err := s.WriteArtifact(ac.Content, ac.Filename)
						if err != nil {
							return fmt.Errorf("write artifact %q: %w", ac.Name, err)
						}
						obsArts = append(obsArts, entity.Artifact{
							Name:  ac.Name,
							SHA:   artSHA,
							Path:  rel,
							Bytes: int64(len(ac.Content)),
							Mime:  ac.Mime,
						})
					}

					oid, err := s.AllocID(store.KindObservation)
					if err != nil {
						return err
					}
					unit := result.Unit
					if unit == "" {
						unit = inst.Unit
					}
					obs := &entity.Observation{
						ID:          oid,
						Experiment:  id,
						Instrument:  instName,
						MeasuredAt:  result.FinishedAt.UTC(),
						Value:       result.Value,
						Unit:        unit,
						Samples:     result.SamplesN,
						PerSample:   result.PerSample,
						CILow:       result.CILow,
						CIHigh:      result.CIHigh,
						CIMethod:    result.CIMethod,
						Pass:        result.Pass,
						Artifacts:   obsArts,
						Command:     result.Command,
						ExitCode:    result.ExitCode,
						Worktree:    wtPath,
						BaselineSHA: sha,
						Author:      or(author, "system"),
						Aux:         result.Aux,
					}
					obs.Normalize()
					if err := s.WriteObservation(obs); err != nil {
						return fmt.Errorf("write observation: %w", err)
					}
					priorObs = append(priorObs, obs)

					artShas := make([]string, 0, len(obsArts))
					for _, a := range obsArts {
						artShas = append(artShas, a.SHA)
					}
					if err := emitEvent(s, "observation.record", or(author, "system"), oid, map[string]any{
						"experiment":    id,
						"instrument":    instName,
						"value":         result.Value,
						"unit":          unit,
						"samples":       result.SamplesN,
						"artifact_shas": artShas,
					}); err != nil {
						return err
					}

					results = append(results, obsResult{id: oid, inst: instName, value: result.Value, unit: unit})
					observed[instName] = true
					progress = true
				}
				if !progress {
					return fmt.Errorf("stuck: instruments %v have unsatisfied dependencies", deferred)
				}
				remaining = deferred
			}

			// Update experiment status.
			e.Status = entity.ExpMeasured
			if err := s.WriteExperiment(e); err != nil {
				return err
			}

			// Output.
			if w.IsJSON() {
				obsIDs := make([]string, 0, len(results))
				for _, r := range results {
					obsIDs = append(obsIDs, r.id)
				}
				return w.JSON(map[string]any{
					"status":       "ok",
					"id":           id,
					"experiment":   e,
					"observations": obsIDs,
				})
			}
			w.Textf("created baseline %s (ref=%s, sha=%s)\n", id, baseline, sha[:12])
			w.Textf("  worktree: %s\n", wtPath)
			w.Textln("  observations:")
			for _, r := range results {
				w.Textf("    %-16s %s = %g %s\n", r.id, r.inst, r.value, r.unit)
			}
			return nil
		},
	}
	c.Flags().StringVar(&baseline, "baseline", "HEAD", "git ref to use as baseline")
	addAuthorFlag(c, &author, "")
	c.Flags().StringVar(&designNotes, "design-notes", "", "optional notes for the baseline experiment")
	return c
}

func experimentWorktreeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "worktree <exp-id>",
		Short: "Print the absolute path of an experiment's worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			e, err := s.ReadExperiment(args[0])
			if err != nil {
				return err
			}
			if e.Worktree == "" {
				return fmt.Errorf("%s has no worktree (status=%s)", args[0], e.Status)
			}
			return w.Emit(e.Worktree, map[string]string{"worktree": e.Worktree})
		},
	}
}

func experimentListCmd() *cobra.Command {
	var status, hyp string
	c := &cobra.Command{
		Use:   "list",
		Short: "List experiments",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			all, err := s.ListExperiments()
			if err != nil {
				return err
			}
			var filtered []*entity.Experiment
			for _, e := range all {
				if status != "" && e.Status != status {
					continue
				}
				if hyp != "" && e.Hypothesis != hyp {
					continue
				}
				filtered = append(filtered, e)
			}
			if w.IsJSON() {
				return w.JSON(filtered)
			}
			if len(filtered) == 0 {
				w.Textln("(no experiments)")
				return nil
			}
			for _, e := range filtered {
				w.Textf("  %-8s  %-12s  hyp=%-8s  instruments=%s\n",
					e.ID, e.Status, e.Hypothesis, strings.Join(e.Instruments, ","))
			}
			return nil
		},
	}
	c.Flags().StringVar(&status, "status", "", "filter by status")
	c.Flags().StringVar(&hyp, "hypothesis", "", "filter by hypothesis id")
	return c
}

func experimentShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <exp-id>",
		Short: "Show a single experiment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			e, err := s.ReadExperiment(args[0])
			if err != nil {
				return err
			}
			if w.IsJSON() {
				return w.JSON(e)
			}
			w.Textf("id:           %s\n", e.ID)
			w.Textf("hypothesis:   %s\n", e.Hypothesis)
			w.Textf("status:       %s\n", e.Status)
			w.Textf("baseline:     %s", e.Baseline.Ref)
			if e.Baseline.SHA != "" {
				w.Textf(" (%s)", e.Baseline.SHA[:12])
			}
			w.Textln("")
			w.Textf("instruments:  %s\n", strings.Join(e.Instruments, ", "))
			if e.Worktree != "" {
				w.Textf("worktree:     %s\n", e.Worktree)
				w.Textf("branch:       %s\n", e.Branch)
			}
			if e.Budget.WallTimeS > 0 || e.Budget.MaxSamples > 0 {
				w.Textf("budget:       wall_time=%ds max_samples=%d\n", e.Budget.WallTimeS, e.Budget.MaxSamples)
			}
			w.Textf("author:       %s\n", e.Author)
			w.Textf("created_at:   %s\n", e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
			return nil
		},
	}
}

// writeWorktreeBrief assembles a frozen context snapshot and writes it into
// the worktree root as .autoresearch-brief.json. Subagents (implementer,
// observer) read this file instead of reaching back to the main store,
// which is unreachable from inside a worktree.
func writeWorktreeBrief(s *store.Store, e *entity.Experiment, wtPath, implNotes string) error {
	hyp, err := s.ReadHypothesis(e.Hypothesis)
	if err != nil {
		return fmt.Errorf("read hypothesis %s for brief: %w", e.Hypothesis, err)
	}

	brief := entity.Brief{
		GeneratedAt: nowUTC(),
		GeneratedBy: "autoresearch experiment implement",
		Hypothesis: entity.BriefHypothesis{
			ID:       hyp.ID,
			Claim:    hyp.Claim,
			Predicts: hyp.Predicts,
			KillIf:   hyp.KillIf,
		},
		Experiment: entity.BriefExperiment{
			ID:          e.ID,
			Instruments: e.Instruments,
			Baseline:    e.Baseline.Ref,
			BaselineSHA: e.Baseline.SHA,
			Worktree:    e.Worktree,
			Branch:      e.Branch,
			DesignNotes: strings.TrimSpace(entity.ExtractSection(e.Body, "Design notes")),
			ImplNotes:   strings.TrimSpace(implNotes),
		},
		Lessons: []entity.BriefLesson{},
	}

	if g, err := s.ActiveGoal(); err == nil {
		brief.Goal = entity.BriefGoal{
			ID:          g.ID,
			Objective:   g.Objective,
			Constraints: g.Constraints,
			Steering:    g.Steering(),
		}
	}

	if lessons, err := s.ListLessons(); err == nil {
		for _, l := range lessons {
			if l.Status == entity.LessonStatusSuperseded {
				continue
			}
			brief.Lessons = append(brief.Lessons, entity.BriefLesson{
				ID:    l.ID,
				Claim: l.Claim,
				Scope: l.Scope,
			})
		}
	}

	data, err := json.MarshalIndent(&brief, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(wtPath, entity.BriefFileName), data, 0o644)
}
