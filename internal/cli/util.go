package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/bytter/autoresearch/internal/store"
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

func jsonRaw(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}

func nowUTC() time.Time { return time.Now().UTC() }
