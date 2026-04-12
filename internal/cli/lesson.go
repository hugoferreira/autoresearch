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
	)
	return []*cobra.Command{l}
}

func lessonAddCmd() *cobra.Command {
	var (
		claim    string
		scope    string
		subjects []string
		tags     []string
		body     string
		author   string
	)
	c := &cobra.Command{
		Use:   "add",
		Short: "Record a new lesson",
		Long: `Record a new lesson. A lesson is one sentence a future generator can
use. If you cannot state it in one sentence, it probably isn't a lesson yet.

Scope is inferred from --from when not set explicitly: if --from has any
subjects, the lesson defaults to scope=hypothesis; otherwise it defaults
to scope=system (an incidental finding about the target codebase or the
research apparatus itself).`,
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
				Status:    entity.LessonStatusActive,
				Author:    or(author, "agent:analyst"),
				CreatedAt: nowUTC(),
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
				"claim":    truncate(claim, 200),
				"scope":    scope,
				"subjects": subjects,
				"author":   resolvedAuthor,
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
	c.Flags().StringVar(&scope, "scope", "", "hypothesis | system (default: inferred from --from)")
	c.Flags().StringSliceVar(&subjects, "from", nil, "H-/E-/C- ids this lesson was extracted from; may be repeated or comma-separated")
	c.Flags().StringSliceVar(&tags, "tag", nil, "tag; may be repeated")
	c.Flags().StringVar(&body, "body", "", "prose expansion of the claim — required for agents. Expected structure: `## Evidence`, `## Mechanism`, `## Scope and counterexamples`, `## For the next generator`. See the research-orchestrator subagent brief for a worked example. A lesson without a body is a one-liner the next generator cannot act on.")
	addAuthorFlag(c, &author, "")
	return c
}

func lessonListCmd() *cobra.Command {
	var (
		scope   string
		status  string
		subject string
		tag     string
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
			all, err := s.ListLessons()
			if err != nil {
				return err
			}
			var filtered []*entity.Lesson
			for _, l := range all {
				if scope != "" && l.Scope != scope {
					continue
				}
				if status != "" && l.Status != status {
					continue
				}
				if subject != "" && !slices.Contains(l.Subjects, subject) {
					continue
				}
				if tag != "" && !slices.Contains(l.Tags, tag) {
					continue
				}
				filtered = append(filtered, l)
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
				w.Textf("  %-8s  %-11s  %-10s  from=%-20s  %s\n",
					l.ID, l.Scope, l.Status, subj, truncate(l.Claim, 60))
			}
			return nil
		},
	}
	c.Flags().StringVar(&scope, "scope", "", "filter by scope (hypothesis | system)")
	c.Flags().StringVar(&status, "status", "", "filter by status (active | superseded)")
	c.Flags().StringVar(&subject, "subject", "", "filter by subject id (returns lessons citing this id)")
	c.Flags().StringVar(&tag, "tag", "", "filter by tag")
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
			if w.IsJSON() {
				return w.JSON(l)
			}
			w.Textf("id:          %s\n", l.ID)
			w.Textf("scope:       %s\n", l.Scope)
			w.Textf("status:      %s\n", l.Status)
			w.Textf("claim:       %s\n", l.Claim)
			if len(l.Subjects) > 0 {
				w.Textf("from:        %s\n", strings.Join(l.Subjects, ", "))
			}
			if len(l.Tags) > 0 {
				w.Textf("tags:        %s\n", strings.Join(l.Tags, ", "))
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
			if oldLesson.Status != entity.LessonStatusActive {
				return fmt.Errorf("%s is %q; only active lessons can be superseded", oldID, oldLesson.Status)
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
