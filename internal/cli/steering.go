package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/output"
	"github.com/spf13/cobra"
)

func steeringCommands() []*cobra.Command {
	steering := &cobra.Command{
		Use:   "steering",
		Short: "View or edit the human steering section of the goal",
	}
	steering.AddCommand(
		steeringShowCmd(),
		steeringAppendCmd(),
		&cobra.Command{
			Use:   "edit",
			Short: "Open the steering section in $EDITOR (not yet wired up; use `steering append`)",
			RunE:  stub("steering edit"),
		},
	)
	return []*cobra.Command{steering}
}

func steeringShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the steering section",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}
			g, err := s.ActiveGoal()
			if err != nil {
				return err
			}
			text := g.Steering()
			if w.IsJSON() {
				return w.JSON(map[string]string{"steering": text})
			}
			if text == "" {
				w.Textln("(no steering notes)")
				return nil
			}
			w.Textln(text)
			return nil
		},
	}
}

func steeringAppendCmd() *cobra.Command {
	var author string
	c := &cobra.Command{
		Use:   "append <note>",
		Short: "Append a bullet to the # Steering section of goal.md",
		Long: `Append a one-line note to the goal's # Steering section. If the goal
has no # Steering section yet, one is created at the end of the body.
The note is written as a Markdown bullet ('- <note>\n').

This is the verb the main agent session uses when a human says something
like "also, don't touch the FFT code" or "start with loop unrolling".
The session calls 'autoresearch steering append' with the human's
phrasing and echoes back a confirmation; the human never has to open
goal.md themselves.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			note := strings.TrimSpace(strings.Join(args, " "))
			if note == "" {
				return errors.New("note is empty")
			}
			if strings.ContainsAny(note, "\n\r") {
				return errors.New("note must not contain newlines; make multiple append calls or use `steering edit`")
			}
			s, err := openStoreLive()
			if err != nil {
				return err
			}
			g, err := s.ActiveGoal()
			if err != nil {
				return err
			}
			newBody := appendSteeringBullet(g.Body, note)
			if err := dryRun(w, fmt.Sprintf("append to steering: %s", note), map[string]any{"note": note}); err != nil {
				return err
			}
			g.Body = newBody
			if err := s.WriteGoal(g); err != nil {
				return err
			}
			if err := emitEvent(s, "steering.append", author, "", map[string]string{"note": note}); err != nil {
				return err
			}
			return w.Emit(
				fmt.Sprintf("appended to steering: %s", note),
				map[string]any{"status": "ok", "note": note},
			)
		},
	}
	addAuthorFlag(c, &author, "")
	return c
}

// appendSteeringBullet returns a new goal body with the note added as a
// Markdown bullet under the # Steering section. If the section doesn't exist,
// one is created at the end of the body. Existing non-steering content is
// preserved byte-for-byte.
func appendSteeringBullet(body, note string) string {
	bullet := "- " + note + "\n"
	if !strings.Contains(body, "# Steering") {
		prefix := body
		if prefix != "" && !strings.HasSuffix(prefix, "\n") {
			prefix += "\n"
		}
		if prefix != "" {
			prefix += "\n"
		}
		return prefix + "# Steering\n\n" + bullet
	}
	// Find the # Steering heading and the start of the next top-level heading
	// (or end of body). Insert the bullet just before that next section.
	lines := strings.Split(body, "\n")
	start := -1
	for i, ln := range lines {
		if ln == "# Steering" {
			start = i
			break
		}
	}
	if start == -1 {
		return body + "\n# Steering\n\n" + bullet
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "# ") {
			end = i
			break
		}
	}
	section := lines[start:end]
	// Strip trailing blank lines from the section so the new bullet sits
	// cleanly at the bottom, then re-add one blank line after.
	for len(section) > 0 && strings.TrimSpace(section[len(section)-1]) == "" {
		section = section[:len(section)-1]
	}
	// If the section body consists only of the heading (or the heading + an
	// "_No steering notes yet._" placeholder), drop the placeholder.
	if len(section) >= 2 && strings.TrimSpace(section[1]) == "_No steering notes yet._" {
		section = append(section[:1], section[2:]...)
	}
	newSection := append([]string(nil), section...)
	newSection = append(newSection, "- "+note, "")
	out := append([]string(nil), lines[:start]...)
	out = append(out, newSection...)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}
