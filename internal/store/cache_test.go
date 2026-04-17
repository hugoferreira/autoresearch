package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Create(dir, Config{
		Build: CommandSpec{Command: "true"},
		Test:  CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCache_ReadReturnsSamePointerOnHit(t *testing.T) {
	s := newTestStore(t)
	h := &entity.Hypothesis{
		ID:     "H-0001",
		Claim:  "test",
		Status: entity.StatusOpen,
		Predicts: entity.Predicts{
			Instrument: "x", Target: "y", Direction: "decrease", MinEffect: 0.1,
		},
		KillIf:    []string{"fails"},
		CreatedAt: time.Now().UTC(),
	}
	if err := s.WriteHypothesis(h); err != nil {
		t.Fatal(err)
	}

	first, err := s.ReadHypothesis("H-0001")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ReadHypothesis("H-0001")
	if err != nil {
		t.Fatal(err)
	}
	// Cache returns the shared pointer on hit — this is the whole point.
	if first != second {
		t.Errorf("expected cache hit to return same pointer, got %p vs %p", first, second)
	}
}

func TestCache_InvalidatesAfterWrite(t *testing.T) {
	s := newTestStore(t)
	h := &entity.Hypothesis{
		ID: "H-0001", Claim: "v1", Status: entity.StatusOpen,
		Predicts: entity.Predicts{Instrument: "x", Target: "y", Direction: "decrease", MinEffect: 0.1},
		KillIf:   []string{"fails"}, CreatedAt: time.Now().UTC(),
	}
	if err := s.WriteHypothesis(h); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadHypothesis("H-0001"); err != nil { // populate cache
		t.Fatal(err)
	}

	h.Claim = "v2"
	if err := s.WriteHypothesis(h); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadHypothesis("H-0001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Claim != "v2" {
		t.Errorf("expected claim=v2 after write, got %q", got.Claim)
	}
}

func TestCache_InvalidatesOnMtimeBump(t *testing.T) {
	s := newTestStore(t)
	h := &entity.Hypothesis{
		ID: "H-0001", Claim: "v1", Status: entity.StatusOpen,
		Predicts: entity.Predicts{Instrument: "x", Target: "y", Direction: "decrease", MinEffect: 0.1},
		KillIf:   []string{"fails"}, CreatedAt: time.Now().UTC(),
	}
	if err := s.WriteHypothesis(h); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadHypothesis("H-0001"); err != nil { // populate cache
		t.Fatal(err)
	}

	// Rewrite the file directly with different content.
	path := s.hypothesisPath("H-0001")
	h.Claim = "hand-edited"
	data, err := h.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Ensure mtime differs from what the cache stored.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	got, err := s.ReadHypothesis("H-0001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Claim != "hand-edited" {
		t.Errorf("expected claim=hand-edited after external write + mtime bump, got %q", got.Claim)
	}
}

func TestCache_DropsOnDeletion(t *testing.T) {
	s := newTestStore(t)
	h := &entity.Hypothesis{
		ID: "H-0001", Claim: "v1", Status: entity.StatusOpen,
		Predicts: entity.Predicts{Instrument: "x", Target: "y", Direction: "decrease", MinEffect: 0.1},
		KillIf:   []string{"fails"}, CreatedAt: time.Now().UTC(),
	}
	if err := s.WriteHypothesis(h); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadHypothesis("H-0001"); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(s.hypothesisPath("H-0001")); err != nil {
		t.Fatal(err)
	}
	_, err := s.ReadHypothesis("H-0001")
	if err != ErrHypothesisNotFound {
		t.Errorf("expected ErrHypothesisNotFound after deletion, got %v", err)
	}
}

func TestCache_StateReturnsIndependentCopy(t *testing.T) {
	s := newTestStore(t)
	st1, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	st1.Counters["H"] = 99 // mutate returned copy

	st2, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if st2.Counters["H"] == 99 {
		t.Error("State() returned a shared reference; counter mutation leaked back into cache")
	}
}

func TestEventsSince_IncrementalRead(t *testing.T) {
	s := newTestStore(t)

	// Append three events.
	for i := 0; i < 3; i++ {
		if err := s.AppendEvent(Event{Kind: "test.tick", Subject: "X"}); err != nil {
			t.Fatal(err)
		}
	}

	// First call from offset 0: should see all three.
	all, off, err := s.EventsSince(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 events on initial read, got %d", len(all))
	}
	if off <= 0 {
		t.Fatalf("expected positive new offset, got %d", off)
	}

	// Second call at current offset: no new events.
	empty, off2, err := s.EventsSince(off)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Errorf("expected 0 events at EOF, got %d", len(empty))
	}
	if off2 != off {
		t.Errorf("offset drift: %d → %d", off, off2)
	}

	// Append a fourth event; incremental call picks up only it.
	if err := s.AppendEvent(Event{Kind: "test.tick", Subject: "Y"}); err != nil {
		t.Fatal(err)
	}
	tail, _, err := s.EventsSince(off)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 1 || tail[0].Subject != "Y" {
		t.Fatalf("expected single tail event for Y, got %+v", tail)
	}
}

func TestInvalidateFromEvents_DropsByPrefix(t *testing.T) {
	s := newTestStore(t)
	h := &entity.Hypothesis{
		ID: "H-0001", Claim: "v1", Status: entity.StatusOpen,
		Predicts: entity.Predicts{Instrument: "x", Target: "y", Direction: "decrease", MinEffect: 0.1},
		KillIf:   []string{"fails"}, CreatedAt: time.Now().UTC(),
	}
	if err := s.WriteHypothesis(h); err != nil {
		t.Fatal(err)
	}
	// Populate cache.
	first, err := s.ReadHypothesis("H-0001")
	if err != nil {
		t.Fatal(err)
	}

	// Simulate an external change: rewrite file with a new claim, same
	// mtime — the mtime check would miss this, but the event-driven
	// invalidation catches it.
	h.Claim = "v2"
	data, err := h.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	path := s.hypothesisPath("H-0001")
	info, _ := os.Stat(path)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Pin mtime back to the original so only the event-driven drop wins.
	_ = os.Chtimes(path, info.ModTime(), info.ModTime())

	s.InvalidateFromEvents([]Event{{Kind: "hypothesis.kill", Subject: "H-0001"}})

	second, err := s.ReadHypothesis("H-0001")
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Error("expected fresh pointer after invalidation; still got cached value")
	}
	if second.Claim != "v2" {
		t.Errorf("expected claim=v2 after invalidation, got %q", second.Claim)
	}
}

func BenchmarkReadHypothesis_CacheHit(b *testing.B) {
	dir := b.TempDir()
	s, err := Create(dir, Config{
		Build: CommandSpec{Command: "true"},
		Test:  CommandSpec{Command: "true"},
	})
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		h := &entity.Hypothesis{
			ID: fmtID("H", i+1), Claim: "x", Status: entity.StatusOpen,
			Predicts: entity.Predicts{Instrument: "x", Target: "y", Direction: "decrease", MinEffect: 0.1},
			KillIf:   []string{"fails"}, CreatedAt: time.Now().UTC(),
		}
		if err := s.WriteHypothesis(h); err != nil {
			b.Fatal(err)
		}
	}
	// Warm the cache.
	if _, err := s.ListHypotheses(); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.ListHypotheses(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadHypothesis_NoCache(b *testing.B) {
	dir := b.TempDir()
	s, err := Create(dir, Config{
		Build: CommandSpec{Command: "true"},
		Test:  CommandSpec{Command: "true"},
	})
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		h := &entity.Hypothesis{
			ID: fmtID("H", i+1), Claim: "x", Status: entity.StatusOpen,
			Predicts: entity.Predicts{Instrument: "x", Target: "y", Direction: "decrease", MinEffect: 0.1},
			KillIf:   []string{"fails"}, CreatedAt: time.Now().UTC(),
		}
		if err := s.WriteHypothesis(h); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Bump every file's mtime to force cache misses, simulating a
		// no-cache baseline cost.
		now := time.Now().Add(time.Duration(i) * time.Second)
		entries, _ := os.ReadDir(s.HypothesesDir())
		for _, e := range entries {
			_ = os.Chtimes(filepath.Join(s.HypothesesDir(), e.Name()), now, now)
		}
		if _, err := s.ListHypotheses(); err != nil {
			b.Fatal(err)
		}
	}
}

func fmtID(prefix string, n int) string {
	// Simple %s-%04d without pulling fmt into this tight test file.
	digits := "0123456789"
	out := make([]byte, 0, len(prefix)+5)
	out = append(out, prefix...)
	out = append(out, '-')
	buf := [4]byte{'0', '0', '0', '0'}
	for i := 3; i >= 0 && n > 0; i-- {
		buf[i] = digits[n%10]
		n /= 10
	}
	out = append(out, buf[:]...)
	return string(out)
}
