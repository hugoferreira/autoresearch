package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/readmodel"
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
		experimentPreflightCmd(),
		experimentWorktreeCmd(),
		experimentListCmd(),
		experimentShowCmd(),
	)
	return []*cobra.Command{e}
}

func persistImplementedExperiment(s *store.Store, e *entity.Experiment, wtPath, branch, implNotes string, appendImplNotes bool) error {
	e.Worktree = wtPath
	e.Branch = branch
	e.Attempt++
	e.Status = entity.ExpImplemented
	if appendImplNotes {
		e.Body = entity.AppendMarkdownSection(e.Body, "Implementation notes", implNotes)
	}
	return s.WriteExperiment(e)
}

func emitExperimentImplementEvent(s *store.Store, id, wtPath, branch string, attempt int, implNotes string) error {
	eventData := map[string]any{
		"from":     entity.ExpDesigned,
		"to":       entity.ExpImplemented,
		"worktree": wtPath,
		"branch":   branch,
		"attempt":  attempt,
	}
	if snippet := truncate(strings.TrimSpace(implNotes), 200); snippet != "" {
		eventData["impl_notes"] = snippet
	}
	return emitEvent(s, "experiment.implement", "system", id, eventData)
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
				GoalID:      h.GoalID,
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

			if err := persistImplementedExperiment(s, e, wtPath, branch, implNotes, true); err != nil {
				return err
			}

			if err := writeWorktreeBrief(s, e, wtPath, implNotes); err != nil {
				return fmt.Errorf("write worktree brief: %w", err)
			}

			if err := emitExperimentImplementEvent(s, id, wtPath, branch, e.Attempt, implNotes); err != nil {
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
				"from":             prevStatus,
				"to":               entity.ExpDesigned,
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
				GoalID:      goal.ID,
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

			if err := persistImplementedExperiment(s, e, wtPath, branch, "", false); err != nil {
				return err
			}
			if err := writeWorktreeBrief(s, e, wtPath, ""); err != nil {
				// Non-fatal for baselines: no hypothesis to brief about.
				_ = err
			}
			if err := emitExperimentImplementEvent(s, id, wtPath, branch, e.Attempt, ""); err != nil {
				return err
			}

			// --- Phase 3: Observe all instruments ---
			scope, priorObs, err := loadCurrentObservations(s, e, "")
			if err != nil {
				return err
			}
			exec, err := observeAll(s, cfg, e, scope, priorObs, instruments, 0, false, or(author, "system"))
			if err != nil {
				return err
			}

			// Output.
			if w.IsJSON() {
				return w.JSON(map[string]any{
					"status":       "ok",
					"id":           id,
					"experiment":   e,
					"observations": observationIDs(exec.CurrentObservations),
				})
			}
			w.Textf("created baseline %s (ref=%s, sha=%s)\n", id, baseline, sha[:12])
			w.Textf("  worktree: %s\n", wtPath)
			w.Textln("  observations:")
			for _, r := range exec.Results {
				w.Textf("    %-16s %s = %g %s\n", r.ID, r.Inst, r.Value, r.Unit)
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
	var status, hyp, classification, goalFlag string
	c := &cobra.Command{
		Use:   "list",
		Short: "List experiments",
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
			if err := validateExperimentClassificationFilter(classification); err != nil {
				return err
			}
			annotated, err := listScopedExperimentsForRead(s, scope)
			if err != nil {
				return err
			}
			var filtered []*experimentReadView
			for _, e := range annotated {
				if status != "" && e.Status != status {
					continue
				}
				if hyp != "" && e.Hypothesis != hyp {
					continue
				}
				if classification != "" && e.Classification != classification {
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
				statusCell := e.Status
				if marker := experimentClassificationMarker(e.Classification); marker != "" {
					statusCell += " " + marker
				}
				w.Textf("  %-8s  %-20s  hyp=%-8s  instruments=%s\n",
					e.ID, statusCell, e.Hypothesis, strings.Join(e.Instruments, ","))
			}
			return nil
		},
	}
	c.Flags().StringVar(&status, "status", "", "filter by status")
	c.Flags().StringVar(&hyp, "hypothesis", "", "filter by hypothesis id")
	c.Flags().StringVar(&classification, "classification", "", "filter by read classification (live|dead)")
	c.Flags().StringVar(&goalFlag, "goal", "", "goal to scope the list to (defaults to active goal; use 'all' for every goal)")
	return c
}

type experimentShowProjectionFlags struct {
	Worktree    bool
	Branch      bool
	BaselineSHA bool
	Env         bool
}

func (f experimentShowProjectionFlags) count() int {
	count := 0
	for _, set := range []bool{f.Worktree, f.Branch, f.BaselineSHA, f.Env} {
		if set {
			count++
		}
	}
	return count
}

func experimentShowCmd() *cobra.Command {
	var projection experimentShowProjectionFlags
	c := &cobra.Command{
		Use:   "show <exp-id>",
		Short: "Show a single experiment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if projection.count() > 1 {
				return errors.New("--worktree, --branch, --baseline-sha, and --env are mutually exclusive")
			}
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			view, err := readmodel.ReadExperimentForRead(s, args[0])
			if err != nil {
				return err
			}
			if projection.count() == 1 {
				return emitExperimentShowProjection(w, view, projection)
			}
			if w.IsJSON() {
				return w.JSON(view)
			}
			w.Textf("id:             %s\n", view.ID)
			if view.GoalID != "" {
				w.Textf("goal:           %s\n", view.GoalID)
			}
			w.Textf("hypothesis:     %s\n", view.Hypothesis)
			w.Textf("status:         %s\n", view.Status)
			w.Textf("classification: %s\n", experimentClassificationSummary(view.Classification, view.HypothesisStatus))
			w.Textf("baseline:       %s", view.Baseline.Ref)
			if view.Baseline.SHA != "" {
				w.Textf(" (%s)", view.Baseline.SHA[:12])
			}
			w.Textln("")
			w.Textf("instruments:    %s\n", strings.Join(view.Instruments, ", "))
			if view.Worktree != "" {
				w.Textf("worktree:       %s\n", view.Worktree)
				w.Textf("branch:         %s\n", view.Branch)
			}
			if view.Budget.WallTimeS > 0 || view.Budget.MaxSamples > 0 {
				w.Textf("budget:         wall_time=%ds max_samples=%d\n", view.Budget.WallTimeS, view.Budget.MaxSamples)
			}
			w.Textf("author:         %s\n", view.Author)
			w.Textf("created_at:     %s\n", view.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
			return nil
		},
	}
	c.Flags().BoolVar(&projection.Worktree, "worktree", false, "print only the experiment worktree path")
	c.Flags().BoolVar(&projection.Branch, "branch", false, "print only the experiment branch")
	c.Flags().BoolVar(&projection.BaselineSHA, "baseline-sha", false, "print only the resolved baseline SHA")
	c.Flags().BoolVar(&projection.Env, "env", false, "print shell-evalable WORKTREE, BRANCH, and BASELINE_SHA assignments")
	return c
}

func emitExperimentShowProjection(w *output.Writer, view *readmodel.ExperimentReadView, projection experimentShowProjectionFlags) error {
	switch {
	case projection.Worktree:
		if view.Worktree == "" {
			return fmt.Errorf("%s has no worktree (status=%s)", view.ID, view.Status)
		}
		return w.Emit(view.Worktree, map[string]string{"worktree": view.Worktree})
	case projection.Branch:
		if view.Branch == "" {
			return fmt.Errorf("%s has no branch (status=%s)", view.ID, view.Status)
		}
		return w.Emit(view.Branch, map[string]string{"branch": view.Branch})
	case projection.BaselineSHA:
		if view.Baseline.SHA == "" {
			return fmt.Errorf("%s has no baseline SHA", view.ID)
		}
		return w.Emit(view.Baseline.SHA, map[string]string{"baseline_sha": view.Baseline.SHA})
	case projection.Env:
		if view.Worktree == "" {
			return fmt.Errorf("%s has no worktree (status=%s)", view.ID, view.Status)
		}
		if view.Branch == "" {
			return fmt.Errorf("%s has no branch (status=%s)", view.ID, view.Status)
		}
		if view.Baseline.SHA == "" {
			return fmt.Errorf("%s has no baseline SHA", view.ID)
		}
		payload := map[string]string{
			"worktree":     view.Worktree,
			"branch":       view.Branch,
			"baseline_sha": view.Baseline.SHA,
		}
		text := strings.Join([]string{
			"WORKTREE=" + shellQuote(view.Worktree),
			"BRANCH=" + shellQuote(view.Branch),
			"BASELINE_SHA=" + shellQuote(view.Baseline.SHA),
		}, "\n")
		return w.Emit(text, payload)
	default:
		return nil
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
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

	if cfg, err := s.Config(); err == nil {
		brief.InstrumentContracts = briefInstrumentContracts(cfg, e.Instruments)
	}
	brief.ForbiddenChanges = []string{
		"target project main checkout",
		"bootstrap or harness scripts outside this worktree",
		"instrument definitions or autoresearch config",
		"unrelated refactors, formatting churn, or cleanup",
		".research state files",
	}

	lessonAccuracy := map[string]lessonAccuracySummary{}
	lessons, err := s.ListLessons()
	if err == nil {
		if _, concls, hyps, accErr := collectLessonAccuracyInputs(s); accErr == nil {
			if _, summaries, accErr := computeLessonAccuracy(s, lessons, concls, buildLessonLinkIndex(hyps)); accErr == nil {
				lessonAccuracy = summaries
			}
		}
		for _, l := range lessons {
			if !lessonIsSteering(s, l) {
				continue
			}
			brief.Lessons = append(brief.Lessons, entity.BriefLesson{
				ID:              l.ID,
				Claim:           l.Claim,
				Scope:           l.Scope,
				Status:          l.EffectiveStatus(),
				SourceChain:     l.EffectiveSourceChain(),
				Tags:            append([]string(nil), l.Tags...),
				PredictedEffect: clonePredictedEffect(l.PredictedEffect),
				Accuracy:        briefLessonAccuracy(lessonAccuracy[l.ID]),
			})
		}
	}

	data, err := json.MarshalIndent(&brief, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(wtPath, entity.BriefFileName)
	tmp, err := os.CreateTemp(wtPath, ".brief-*")
	if err != nil {
		return fmt.Errorf("create temp brief: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp brief: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp brief: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp brief: %w", err)
	}
	return nil
}

func briefInstrumentContracts(cfg *store.Config, instruments []string) []entity.BriefInstrumentContract {
	if cfg == nil || len(instruments) == 0 {
		return nil
	}
	out := make([]entity.BriefInstrumentContract, 0, len(instruments))
	for _, name := range instruments {
		inst, ok := cfg.Instruments[name]
		if !ok {
			continue
		}
		contract := entity.BriefInstrumentContract{
			Name:       name,
			Cmd:        append([]string(nil), inst.Cmd...),
			Parser:     inst.Parser,
			Pattern:    inst.Pattern,
			Unit:       inst.Unit,
			MinSamples: inst.MinSamples,
			Requires:   append([]string(nil), inst.Requires...),
		}
		for _, ev := range inst.Evidence {
			contract.Evidence = append(contract.Evidence, entity.BriefEvidenceSpec{
				Name: ev.Name,
				Cmd:  ev.Cmd,
			})
		}
		out = append(out, contract)
	}
	return out
}

func clonePredictedEffect(pe *entity.PredictedEffect) *entity.PredictedEffect {
	if pe == nil {
		return nil
	}
	out := *pe
	return &out
}

func briefLessonAccuracy(summary lessonAccuracySummary) *entity.BriefLessonAccuracy {
	if summary.Total == 0 {
		return nil
	}
	return &entity.BriefLessonAccuracy{
		Total:      summary.Total,
		Hit:        summary.Hit,
		Overshoot:  summary.Overshoot,
		Undershoot: summary.Undershoot,
		Trend:      summary.trend(),
	}
}
