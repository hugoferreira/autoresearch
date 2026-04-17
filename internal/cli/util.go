package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/bytter/autoresearch/internal/output"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/spf13/cobra"
)

func openStore() (*store.Store, error) {
	return store.Open(globalProjectDir)
}

// openStoreLive opens the store and additionally enforces the pause gate:
// if the store is paused, it returns an error wrapping ErrPaused so main()
// exits 3 and orchestrators can stop cleanly. Mutating CLI verbs use this
// instead of openStore(); read-only verbs keep openStore() so `status`,
// `log`, `tree`, `frontier`, `report`, and all the show/list verbs still
// work while paused.
func openStoreLive() (*store.Store, error) {
	s, err := openStore()
	if err != nil {
		return nil, err
	}
	st, err := s.State()
	if err != nil {
		return nil, err
	}
	if st.Paused {
		reason := st.PauseReason
		if reason == "" {
			reason = "(no reason given)"
		}
		return nil, fmt.Errorf("%w: %s — run `autoresearch resume` to continue", ErrPaused, reason)
	}
	return s, nil
}

// addAuthorFlag registers a --author string flag on c, storing into *dst.
// defaultVal is the flag's Go default: "human" for goal verbs, "" for
// everything else (the caller decides).
func addAuthorFlag(c *cobra.Command, dst *string, defaultVal string) {
	c.Flags().StringVar(dst, "author", defaultVal, "author (e.g. human:alice, agent:orchestrator)")
}

// or returns fallback when s is empty.
func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// emitEvent is a shorthand for appending a structured event to the store.
func emitEvent(s *store.Store, kind, actor, subject string, data any) error {
	return s.AppendEvent(store.Event{
		Kind:    kind,
		Actor:   actor,
		Subject: subject,
		Data:    jsonRaw(data),
	})
}

// dryRun checks globalDryRun and, if set, emits the "[dry-run] would ..."
// preview and returns ErrDryRun. Callers use it as a guard:
//
//	if err := dryRun(w, "add hypothesis", payload); err != nil { return err }
//
// main() recognizes ErrDryRun and exits 0 silently (the preview has
// already been emitted). When globalDryRun is false it returns nil and
// the caller continues to the mutation path.
//
// Returning a real sentinel here — rather than just nil after the Emit —
// is load-bearing. Previously the function returned w.Emit(...), which
// is nil on success, so every caller's `if err := dryRun(...); err != nil`
// guard fell straight through and the mutation ran anyway.
func dryRun(w *output.Writer, action string, payload map[string]any) error {
	if !globalDryRun {
		return nil
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["status"] = "dry-run"
	if err := w.Emit(fmt.Sprintf("[dry-run] would %s", action), payload); err != nil {
		return err
	}
	return ErrDryRun
}

func jsonRaw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}

func nowUTC() time.Time { return time.Now().UTC() }
