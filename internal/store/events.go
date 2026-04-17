package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Event struct {
	Ts      time.Time       `json:"ts"`
	Kind    string          `json:"kind"`
	Actor   string          `json:"actor,omitempty"`
	Subject string          `json:"subject,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (s *Store) initEvents() error {
	f, err := os.OpenFile(s.EventsPath(), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create events log: %w", err)
	}
	return f.Close()
}

func (s *Store) AppendEvent(e Event) error {
	if e.Ts.IsZero() {
		e.Ts = time.Now().UTC()
	}
	line, err := json.Marshal(&e)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	f, err := os.OpenFile(s.EventsPath(), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("open events log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write event: %w", err)
	}
	ts := e.Ts
	return s.UpdateState(func(st *State) error {
		st.LastEventAt = &ts
		return nil
	})
}

// EventsSince reads events from byte offset off to current EOF. Returns
// the events and the new offset (pass to the next call).
//
// If off is 0 or negative, behaves like a full read. If the file has
// shrunk since off (truncation / rotation / rebuild), off is reset to 0
// and the full file is read — matches the defensive behavior of the
// log --follow loop that this method replaces.
//
// Invalid / malformed JSON lines are skipped (matching the follow loop's
// tolerance — the events log is append-only, but callers shouldn't
// crash on an in-flight partial line at EOF).
func (s *Store) EventsSince(off int64) (events []Event, newOff int64, err error) {
	path := s.EventsPath()
	info, err := os.Stat(path)
	if err != nil {
		return nil, off, fmt.Errorf("stat events: %w", err)
	}
	if off < 0 || info.Size() < off {
		off = 0
	}
	if info.Size() == off {
		return nil, off, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, off, fmt.Errorf("open events: %w", err)
	}
	defer f.Close()
	if off > 0 {
		if _, err := f.Seek(off, 0); err != nil {
			return nil, off, fmt.Errorf("seek events: %w", err)
		}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		events = append(events, e)
	}
	if err := sc.Err(); err != nil {
		return events, off, fmt.Errorf("scan events: %w", err)
	}
	return events, info.Size(), nil
}

func (s *Store) Events(limit int) ([]Event, error) {
	f, err := os.Open(s.EventsPath())
	if err != nil {
		return nil, fmt.Errorf("open events log: %w", err)
	}
	defer f.Close()

	var events []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("decode event: %w", err)
		}
		events = append(events, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan events: %w", err)
	}
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, nil
}
