package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

func logCommands() []*cobra.Command {
	var (
		tail     int
		kind     string
		since    string
		follow   bool
		goalFlag string
	)
	c := &cobra.Command{
		Use:   "log",
		Short: "Show the append-only event log",
		Long: `Print recent events from .research/events.jsonl. Defaults to the last
20 events. Filterable by --kind (exact match or prefix, e.g. "conclusion.")
and --since (RFC3339 timestamp). Every default is disclosed as a header
line so the output is never mistaken for the full log.

--follow prints the historical log then tails events.jsonl by byte offset,
emitting new events as they land (200 ms poll). In text mode each new
event is appended with the standard formatter; in --json mode emits JSONL
(one event object per line). Ctrl-C exits cleanly.`,
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
			resolver := newGoalScopeResolver(s, scope)

			limits := map[string]any{"tail": tail}
			if kind != "" {
				limits["kind"] = kind
			}
			if since != "" {
				limits["since"] = since
			}
			if follow {
				limits["follow"] = true
			}
			for k, v := range scope.payload() {
				limits[k] = v
			}

			all, err := s.Events(0)
			if err != nil {
				return err
			}
			all, err = resolver.filterEvents(all)
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

			keep := func(e store.Event) bool {
				if kind != "" {
					if !strings.HasPrefix(e.Kind, kind) && e.Kind != kind {
						return false
					}
				}
				if !sinceTime.IsZero() && e.Ts.Before(sinceTime) {
					return false
				}
				return true
			}

			filtered := make([]store.Event, 0, len(all))
			for _, e := range all {
				if keep(e) {
					filtered = append(filtered, e)
				}
			}

			total := len(filtered)
			if !follow && tail > 0 && len(filtered) > tail {
				filtered = filtered[len(filtered)-tail:]
			} else if follow && tail > 0 && len(filtered) > tail {
				// Show the last `tail` historical events, then stream.
				filtered = filtered[len(filtered)-tail:]
			}
			truncated := !follow && total > len(filtered)

			if w.IsJSON() && !follow {
				return w.JSON(mergeGoalScopePayload(map[string]any{
					"limits":       limits,
					"total_events": total,
					"shown_events": len(filtered),
					"truncated":    truncated,
					"events":       filtered,
				}, scope))
			}

			if w.IsJSON() && follow {
				// Historical JSONL preamble.
				enc := json.NewEncoder(os.Stdout)
				for _, e := range filtered {
					if err := enc.Encode(e); err != nil {
						return err
					}
				}
			} else {
				printLimits(w, limits)
				if follow {
					w.Textf("[events: %d historical, following for new]\n", len(filtered))
				} else {
					w.Textf("[events: showing %d of %d]\n", len(filtered), total)
				}
				for _, e := range filtered {
					writeEventLine(os.Stdout, e)
				}
				if truncated {
					w.Textf("[truncated: raise --tail or drop --tail 0 for the full log]\n")
				}
			}

			if follow {
				return followEvents(s, func(e store.Event) bool {
					ok, err := resolver.eventMatches(e)
					if err != nil || !ok {
						return false
					}
					return keep(e)
				}, w.IsJSON())
			}
			return nil
		},
	}
	c.Flags().IntVar(&tail, "tail", 20, "show only the last N events (0 = no limit)")
	c.Flags().StringVar(&kind, "kind", "", "filter by event kind (exact or prefix, e.g. 'conclusion.')")
	c.Flags().StringVar(&since, "since", "", "only events at or after this RFC3339 timestamp")
	c.Flags().BoolVar(&follow, "follow", false, "after printing history, tail events.jsonl for new events (Ctrl-C to exit)")
	c.Flags().StringVar(&goalFlag, "goal", "", "goal to scope the log to (defaults to active goal; use 'all' for every goal)")
	return []*cobra.Command{c}
}

// writeEventLine emits one event in the standard text-mode format used by
// `autoresearch log` (and reused by the dashboard). Kept as a top-level
// helper so dashboard.go can call the same formatter.
func writeEventLine(w io.Writer, e store.Event) {
	subject := e.Subject
	if subject == "" {
		subject = "-"
	}
	fmt.Fprintf(w, "  %s  %-24s  %-16s  %s\n",
		e.Ts.UTC().Format("2006-01-02T15:04:05Z"),
		e.Kind,
		subject,
		e.Actor,
	)
}

// followEvents is the tail loop. It polls events.jsonl by byte offset every
// 200 ms. When the file grows, reads from the last offset to EOF, parses each
// line, filters via keep(), and emits to stdout (JSONL if jsonMode else text).
// Stops cleanly on SIGINT/SIGTERM.
func followEvents(s *store.Store, keep func(store.Event) bool, jsonMode bool) error {
	path := s.EventsPath()
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat events.jsonl: %w", err)
	}
	offset := info.Size()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()
	defer signal.Stop(sigCh)

	enc := json.NewEncoder(os.Stdout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.Size() <= offset {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var e store.Event
			if err := json.Unmarshal(line, &e); err != nil {
				continue
			}
			if !keep(e) {
				continue
			}
			if jsonMode {
				_ = enc.Encode(e)
			} else {
				writeEventLine(os.Stdout, e)
			}
		}
		offset = info.Size()
		f.Close()
	}
}
