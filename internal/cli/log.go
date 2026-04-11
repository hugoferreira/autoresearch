package cli

import (
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

func logCommands() []*cobra.Command {
	var (
		tail  int
		kind  string
		since string
	)
	c := &cobra.Command{
		Use:   "log",
		Short: "Show the append-only event log",
		Long: `Print recent events from .research/events.jsonl. Defaults to the last
20 events. Filterable by --kind (exact match or prefix, e.g. "conclusion.")
and --since (RFC3339 timestamp). Every default is disclosed as a header
line so the output is never mistaken for the full log.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Default(globalJSON)
			s, err := openStore()
			if err != nil {
				return err
			}

			limits := map[string]any{"tail": tail}
			if kind != "" {
				limits["kind"] = kind
			}
			if since != "" {
				limits["since"] = since
			}

			all, err := s.Events(0)
			if err != nil {
				return err
			}

			var sinceTime time.Time
			if since != "" {
				sinceTime, err = time.Parse(time.RFC3339, since)
				if err != nil {
					return err
				}
			}

			// Filter.
			filtered := make([]store.Event, 0, len(all))
			for _, e := range all {
				if kind != "" {
					if !strings.HasPrefix(e.Kind, kind) && e.Kind != kind {
						continue
					}
				}
				if !sinceTime.IsZero() && e.Ts.Before(sinceTime) {
					continue
				}
				filtered = append(filtered, e)
			}

			total := len(filtered)
			if tail > 0 && len(filtered) > tail {
				filtered = filtered[len(filtered)-tail:]
			}
			truncated := total > len(filtered)

			if w.IsJSON() {
				return w.JSON(map[string]any{
					"limits":       limits,
					"total_events": total,
					"shown_events": len(filtered),
					"truncated":    truncated,
					"events":       filtered,
				})
			}
			printLimits(w, limits)
			w.Textf("[events: showing %d of %d]\n", len(filtered), total)
			for _, e := range filtered {
				subject := e.Subject
				if subject == "" {
					subject = "-"
				}
				w.Textf("  %s  %-24s  %-16s  %s\n",
					e.Ts.UTC().Format("2006-01-02T15:04:05Z"),
					e.Kind,
					subject,
					e.Actor,
				)
			}
			if truncated {
				w.Textf("[truncated: raise --tail or drop --tail 0 for the full log]\n")
			}
			return nil
		},
	}
	c.Flags().IntVar(&tail, "tail", 20, "show only the last N events (0 = no limit)")
	c.Flags().StringVar(&kind, "kind", "", "filter by event kind (exact or prefix, e.g. 'conclusion.')")
	c.Flags().StringVar(&since, "since", "", "only events at or after this RFC3339 timestamp")
	return []*cobra.Command{c}
}
