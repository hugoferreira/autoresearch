package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/firewall"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

func lessonCommands() []*cobra.Command {
	l := &cobra.Command{
		Use:   "lesson",
		Short: "Record and read the cumulative lessons layer",
		Long: `The lesson layer is the research notebook: supersedable claims the
loop has learned that should inform the next cycle. Hypotheses, experiments,
observations, and conclusions are per-cycle artifacts; lessons sit above
them and carry insight across cycles.

Two scopes:

  hypothesis  Tied to one or more H-/E-/C- ids the lesson was extracted from.
              Written by the analyst on decisive conclusions, and by the
              critic on cross-cutting downgrades.

  system      Free-floating incidental findings about the target codebase
              or the research apparatus itself ("the fixture is stale",
              "qemu variance correlates with CPU temp"). No hypothesis
              reference required.

Lessons are read by the generator before proposing new hypotheses. That is
the whole point — the loop should not re-derive what it already knows.`,
	}
	l.AddCommand(
		lessonAddCmd(),
		lessonListCmd(),
		lessonShowCmd(),
		lessonSupersedeCmd(),
		lessonAccuracyCmd(),
	)
	return []*cobra.Command{l}
}

func lessonAddCmd() *cobra.Command {
	var (
		claim            string
		scope            string
		subjects         []string
		tags             []string
		body             string
		author           string
		predictInst      string
		predictDir       string
		predictMinEffect float64
		predictMaxEffect float64
	)
	c := &cobra.Command{
		Use:   "add",
		Short: "Record a new lesson",
		Long: `Record a new lesson. A lesson is one sentence a future generator can
use. If you cannot state it in one sentence, it probably isn't a lesson yet.

Scope is inferred from --from when not set explicitly: if --from has any
subjects, the lesson defaults to scope=hypothesis; otherwise it defaults
to scope=system (an incidental finding about the target codebase or the
research apparatus itself). Explicit --scope system cannot be combined
with --from.

Use scope=system only for facts expected to hold across goals, like
harness behavior, measurement caveats, environment quirks, or target-wide
invariants. If the claim comes from a specific H-/E-/C- chain or
recommends continuing an optimization direction, keep it scope=hypothesis.
If unsure, prefer scope=hypothesis.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			if strings.TrimSpace(claim) == "" {
				return errors.New("--claim is required")
			}
			// Infer scope from the presence of --from when not set.
			if scope == "" {
				if len(subjects) > 0 {
					scope = entity.LessonScopeHypothesis
				} else {
					scope = entity.LessonScopeSystem
				}
			}

			s, err := openStoreLive()
			if err != nil {
				return err
			}

			l := &entity.Lesson{
				Claim:     claim,
				Scope:     scope,
				Subjects:  subjects,
				Tags:      tags,
				Author:    or(author, "agent:analyst"),
				CreatedAt: nowUTC(),
			}
			if predictInst != "" {
				l.PredictedEffect = &entity.PredictedEffect{
					Instrument: predictInst,
					Direction:  predictDir,
					MinEffect:  predictMinEffect,
					MaxEffect:  predictMaxEffect,
				}
			}
			if strings.TrimSpace(body) != "" {
				l.Body = entity.AppendMarkdownSection("", "Lesson", body)
			}
			if err := firewall.ValidateLesson(l); err != nil {
				return err
			}
			// Existence check for subjects — mirrors the parent check on
			// hypothesis add. The firewall stays structural; existence is
			// the CLI handler's responsibility.
			for _, sub := range subjects {
				ok, err := entityExists(s, sub)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("subject %s does not exist", sub)
				}
			}
			if err := initializeLessonState(s, l); err != nil {
				return err
			}

			if err := dryRun(w, fmt.Sprintf("add lesson (claim=%q, scope=%s)", claim, scope), map[string]any{"lesson": l}); err != nil {
				return err
			}

			id, err := s.AllocID(store.KindLesson)
			if err != nil {
				return err
			}
			l.ID = id
			if err := s.WriteLesson(l); err != nil {
				return err
			}
			resolvedAuthor := or(author, "agent:analyst")
			eventData := map[string]any{
				"claim":        truncate(claim, 200),
				"scope":        scope,
				"status":       l.Status,
				"source_chain": l.EffectiveSourceChain(),
				"subjects":     subjects,
				"author":       resolvedAuthor,
			}
			if len(tags) > 0 {
				eventData["tags"] = tags
			}
			if err := emitEvent(s, "lesson.add", resolvedAuthor, id, eventData); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("added %s (%s): %s", id, scope, truncate(claim, 80)),
				map[string]any{"status": "ok", "id": id, "lesson": l},
			)
		},
	}
	c.Flags().StringVar(&claim, "claim", "", "one-sentence lesson claim (required)")
	c.Flags().StringVar(&scope, "scope", "", "hypothesis | system (default: inferred from --from; system requires no --from)")
	c.Flags().StringSliceVar(&subjects, "from", nil, "H-/E-/C- ids this lesson was extracted from; may be repeated or comma-separated")
	c.Flags().StringSliceVar(&tags, "tag", nil, "tag; may be repeated")
	c.Flags().StringVar(&body, "body", "", "prose expansion of the claim — required for agents. Expected structure: `## Evidence`, `## Mechanism`, `## Scope and counterexamples`, `## For the next generator`. See the research-orchestrator subagent brief for a worked example. A lesson without a body is a one-liner the next generator cannot act on.")
	c.Flags().StringVar(&predictInst, "predict-instrument", "", "instrument for predicted future effect (sets predicted_effect)")
	c.Flags().StringVar(&predictDir, "predict-direction", "", "increase | decrease (required with --predict-instrument)")
	c.Flags().Float64Var(&predictMinEffect, "predict-min-effect", 0, "minimum predicted fractional effect (required with --predict-instrument)")
	c.Flags().Float64Var(&predictMaxEffect, "predict-max-effect", 0, "maximum predicted fractional effect (optional, 0 = unbounded)")
	addAuthorFlag(c, &author, "")
	return c
}

func lessonListCmd() *cobra.Command {
	var (
		scope    string
		status   string
		subject  string
		tag      string
		goalFlag string
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "List lessons (read by the generator before proposing)",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			goalScope, err := resolveGoalScope(s, goalFlag)
			if err != nil {
				return err
			}
			all, err := s.ListLessons()
			if err != nil {
				return err
			}
			all, err = newGoalScopeResolver(s, goalScope).filterLessons(all)
			if err != nil {
				return err
			}
			var filtered []*entity.Lesson
			for _, l := range all {
				view, err := annotateLessonForRead(s, l)
				if err != nil {
					return err
				}
				if scope != "" && view.Scope != scope {
					continue
				}
				if status != "" && view.Status != status {
					continue
				}
				if subject != "" && !slices.Contains(view.Subjects, subject) {
					continue
				}
				if tag != "" && !slices.Contains(view.Tags, tag) {
					continue
				}
				filtered = append(filtered, view)
			}
			if w.IsJSON() {
				return w.JSON(filtered)
			}
			if len(filtered) == 0 {
				w.Textln("(no lessons)")
				return nil
			}
			for _, l := range filtered {
				subj := "-"
				if len(l.Subjects) > 0 {
					subj = strings.Join(l.Subjects, ",")
				}
				source := "-"
				if l.Provenance != nil && l.Provenance.SourceChain != "" {
					source = l.Provenance.SourceChain
				}
				w.Textf("  %-8s  %-11s  %-11s  %-20s  from=%-20s  %s\n",
					l.ID, l.Scope, l.Status, source, subj, truncate(l.Claim, 60))
			}
			return nil
		},
	}
	c.Flags().StringVar(&scope, "scope", "", "filter by scope (hypothesis | system)")
	c.Flags().StringVar(&status, "status", "", "filter by status (active | provisional | invalidated | superseded)")
	c.Flags().StringVar(&subject, "subject", "", "filter by subject id (returns lessons citing this id)")
	c.Flags().StringVar(&tag, "tag", "", "filter by tag")
	c.Flags().StringVar(&goalFlag, "goal", "", "goal to scope the list to (defaults to active goal; use 'all' for every goal)")
	return c
}

func lessonShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show a single lesson",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			l, err := s.ReadLesson(args[0])
			if err != nil {
				return err
			}
			l, err = annotateLessonForRead(s, l)
			if err != nil {
				return err
			}
			if w.IsJSON() {
				return w.JSON(l)
			}
			w.Textf("id:          %s\n", l.ID)
			w.Textf("scope:       %s\n", l.Scope)
			w.Textf("status:      %s\n", l.Status)
			if l.Provenance != nil && l.Provenance.SourceChain != "" {
				w.Textf("source_chain:%s\n", " "+l.Provenance.SourceChain)
			}
			w.Textf("claim:       %s\n", l.Claim)
			if len(l.Subjects) > 0 {
				w.Textf("from:        %s\n", strings.Join(l.Subjects, ", "))
			}
			if len(l.Tags) > 0 {
				w.Textf("tags:        %s\n", strings.Join(l.Tags, ", "))
			}
			if l.PredictedEffect != nil {
				w.Textf("predicted:   %s\n", formatPredictedEffect(l.PredictedEffect))
			}
			if l.SupersedesID != "" {
				w.Textf("supersedes:  %s\n", l.SupersedesID)
			}
			if l.SupersededByID != "" {
				w.Textf("superseded_by: %s\n", l.SupersededByID)
			}
			w.Textf("author:      %s\n", l.Author)
			w.Textf("created_at:  %s\n", l.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
			if strings.TrimSpace(l.Body) != "" {
				w.Textln("")
				w.Textln(strings.TrimSpace(l.Body))
			}
			return nil
		},
	}
}

func lessonSupersedeCmd() *cobra.Command {
	var (
		by     string
		reason string
	)
	c := &cobra.Command{
		Use:   "supersede <L-id>",
		Short: "Mark an existing lesson as superseded by a newer one",
		Long: `Mark lesson <L-id> as superseded by the lesson passed via --by. Both
records are updated: the old lesson's status flips to "superseded" and
its superseded_by field points to the new lesson; the new lesson's
supersedes field points back to the old one.

The new lesson must already exist — create it first with "lesson add",
then run "lesson supersede" to form the chain. This makes supersession
an explicit two-step rather than a one-shot, keeping the new lesson a
first-class record that can itself be superseded later.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			oldID := args[0]
			if strings.TrimSpace(by) == "" {
				return errors.New("--by <L-id> is required")
			}
			if strings.TrimSpace(reason) == "" {
				return errors.New("--reason is required")
			}
			if oldID == by {
				return errors.New("a lesson cannot supersede itself")
			}

			s, err := openStoreLive()
			if err != nil {
				return err
			}
			oldLesson, err := s.ReadLesson(oldID)
			if err != nil {
				return err
			}
			oldView, err := annotateLessonForRead(s, oldLesson)
			if err != nil {
				return err
			}
			if oldView.Status != entity.LessonStatusActive && oldView.Status != entity.LessonStatusProvisional {
				return fmt.Errorf("%s is %q; only active or provisional lessons can be superseded", oldID, oldView.Status)
			}
			newLesson, err := s.ReadLesson(by)
			if err != nil {
				return fmt.Errorf("--by target: %w", err)
			}

			if err := dryRun(w, fmt.Sprintf("supersede %s by %s (%s)", oldID, by, reason), map[string]any{"from": oldID, "by": by, "reason": reason}); err != nil {
				return err
			}

			oldLesson.Status = entity.LessonStatusSuperseded
			oldLesson.SupersededByID = by
			if err := s.WriteLesson(oldLesson); err != nil {
				return err
			}
			newLesson.SupersedesID = oldID
			if err := s.WriteLesson(newLesson); err != nil {
				return err
			}
			if err := emitEvent(s, "lesson.supersede", newLesson.Author, oldID, map[string]any{
				"from":   oldID,
				"by":     by,
				"reason": reason,
			}); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("superseded %s by %s (%s)", oldID, by, reason),
				map[string]any{"status": "ok", "from": oldID, "by": by, "reason": reason},
			)
		},
	}
	c.Flags().StringVar(&by, "by", "", "newer lesson id (must already exist, required)")
	c.Flags().StringVar(&reason, "reason", "", "why this lesson is being superseded (required)")
	return c
}

func lessonAccuracyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "accuracy",
		Short: "Compare predicted effects against actual conclusion outcomes",
		Long: `For each active lesson with a predicted_effect, find subsequent
conclusions on the same instrument and compare the predicted range
against the actual absolute delta_frac. Classify each as HIT
(within range), OVERSHOOT (predicted more than actual), or
UNDERSHOOT (predicted less than actual).

This is a read-only diagnostic for detecting diminishing returns:
when predictions consistently overshoot, the optimization direction
may be exhausted.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}

			lessons, concls, hyps, err := collectLessonAccuracyInputs(s)
			if err != nil {
				return err
			}
			results, _, err := computeLessonAccuracy(s, lessons, concls, buildLessonLinkIndex(hyps))
			if err != nil {
				return err
			}
			var totalHit, totalOver, totalUnder int
			for _, la := range results {
				for _, r := range la.Comparisons {
					switch r.Classification {
					case lessonAccuracyOvershoot:
						totalOver++
					case lessonAccuracyUndershoot:
						totalUnder++
					default:
						totalHit++
					}
				}
			}

			total := totalHit + totalOver + totalUnder

			if w.IsJSON() {
				return w.JSON(map[string]any{
					"lessons":    results,
					"total":      total,
					"hit":        totalHit,
					"overshoot":  totalOver,
					"undershoot": totalUnder,
				})
			}

			if len(results) == 0 {
				w.Textln("(no lessons with predicted effects or no subsequent conclusions to compare)")
				return nil
			}

			for _, la := range results {
				pred := formatPredictedEffect(&entity.PredictedEffect{
					Instrument: la.Instrument,
					Direction:  la.Direction,
					MinEffect:  la.MinEffect,
					MaxEffect:  la.MaxEffect,
				})
				w.Textf("  %s  predicted: %s\n", la.LessonID, pred)
				w.Textf("         %s\n", truncate(la.Claim, 70))
				for _, r := range la.Comparisons {
					link := ""
					if r.Linked {
						link = " (linked via inspired_by)"
					}
					w.Textf("    %s (%s): actual delta_frac=%+.4f  %s%s\n",
						r.ConclusionID, r.HypothesisID, r.ActualDelta, r.Classification, link)
				}
				w.Textln("")
			}

			w.Textf("  prediction accuracy: %d total — %d hit, %d overshoot, %d undershoot\n",
				total, totalHit, totalOver, totalUnder)
			return nil
		},
	}
}

// entityExists checks whether a referenced subject id (H-/E-/C-) still lives
// in the store. Dispatches on prefix rather than adding a new generic store
// method — lesson subjects are the only current caller.
func entityExists(s *store.Store, id string) (bool, error) {
	switch {
	case strings.HasPrefix(id, "H-"):
		return s.HypothesisExists(id)
	case strings.HasPrefix(id, "E-"):
		_, err := s.ReadExperiment(id)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, store.ErrExperimentNotFound) {
			return false, nil
		}
		return false, err
	case strings.HasPrefix(id, "C-"):
		_, err := s.ReadConclusion(id)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, store.ErrConclusionNotFound) {
			return false, nil
		}
		return false, err
	default:
		return false, fmt.Errorf("unknown entity id prefix: %q", id)
	}
}
