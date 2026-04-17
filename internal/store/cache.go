package store

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"
)

// entryCache is an in-memory mtime+size-keyed cache over a single file per
// path. It turns repeated reads of unchanged files into O(stat) instead of
// O(stat + read + parse).
//
// Correctness rests on the single-writer invariant (see CLAUDE.md): the CLI
// is the only mutator of .research/, so every file mutation either comes
// through Write<Entity> (which drops the cached entry) or is an external
// edit (which bumps mtime and invalidates the entry on the next read).
// Deletions surface as os.ErrNotExist from Stat; the cache drops the entry
// and propagates the error.
//
// Values returned by getOrLoad should be treated as read-only by callers.
// Mutating a returned pointer mutates the cached copy too. The current
// store API makes this easy to get right in practice — callers that intend
// to mutate call Write<Entity> with the updated pointer, which invalidates
// the cache, so the next read re-parses.
type entryCache[T any] struct {
	mu      sync.RWMutex
	entries map[string]cachedEntry[T]
}

type cachedEntry[T any] struct {
	mtime time.Time
	size  int64
	value T
}

func newEntryCache[T any]() *entryCache[T] {
	return &entryCache[T]{entries: map[string]cachedEntry[T]{}}
}

// getOrLoad stats path. If mtime+size match the cached entry, returns the
// cached value. Otherwise runs loader, stores the result, and returns it.
// If Stat reports ErrNotExist, the entry is dropped and the error is
// propagated so callers still see deletions.
func (c *entryCache[T]) getOrLoad(path string, loader func(string) (T, error)) (T, error) {
	var zero T
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		c.drop(path)
		return zero, err
	}
	if err != nil {
		return zero, err
	}

	c.mu.RLock()
	if e, ok := c.entries[path]; ok && e.mtime.Equal(info.ModTime()) && e.size == info.Size() {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()

	value, err := loader(path)
	if err != nil {
		return zero, err
	}

	c.mu.Lock()
	c.entries[path] = cachedEntry[T]{
		mtime: info.ModTime(),
		size:  info.Size(),
		value: value,
	}
	c.mu.Unlock()
	return value, nil
}

// drop removes a single entry — called by writers after atomicWrite so the
// next read re-parses the freshly-written file.
func (c *entryCache[T]) drop(path string) {
	c.mu.Lock()
	delete(c.entries, path)
	c.mu.Unlock()
}

// InvalidateFromEvents is an advisory hint: given a batch of recently-
// appended events, drop any cache entries whose subject they touch. Long-
// running readers (e.g. the TUI) call this after EventsSince so the next
// Read picks up the change even when the file mtime resolution would
// otherwise make the cache appear fresh.
//
// It is purely advisory — the mtime check in getOrLoad remains the
// authoritative invalidation. Missing an event here cannot cause
// incorrectness, only a brief staleness window.
func (s *Store) InvalidateFromEvents(events []Event) {
	for _, e := range events {
		switch {
		case len(e.Subject) < 2:
			continue
		case e.Subject[0] == 'H' && e.Subject[1] == '-':
			s.hypCache.drop(s.hypothesisPath(e.Subject))
		case e.Subject[0] == 'E' && e.Subject[1] == '-':
			s.expCache.drop(s.experimentPath(e.Subject))
		case e.Subject[0] == 'O' && e.Subject[1] == '-':
			s.obsCache.drop(s.observationPath(e.Subject))
			// observation.record bumps experiment.status on first obs; drop
			// the referenced experiment too so the TUI reflects the change.
			if e.Kind == "observation.record" && e.Data != nil {
				if expID := subjectFromEventData(e.Data, "experiment"); expID != "" {
					s.expCache.drop(s.experimentPath(expID))
				}
			}
		case e.Subject[0] == 'C' && e.Subject[1] == '-':
			s.conclCache.drop(s.conclusionPath(e.Subject))
		case e.Subject[0] == 'L' && e.Subject[1] == '-':
			s.lessonCache.drop(s.lessonPath(e.Subject))
		case e.Subject[0] == 'G' && e.Subject[1] == '-':
			s.goalCache.drop(s.goalPath(e.Subject))
		}
	}
}

// subjectFromEventData peeks at a JSON-encoded event payload for a field
// that should contain an entity ID. Returns "" if the field is missing or
// not a string. Tolerant of partial/malformed payloads — this is an
// advisory path.
func subjectFromEventData(data []byte, field string) string {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	s, _ := m[field].(string)
	return s
}
