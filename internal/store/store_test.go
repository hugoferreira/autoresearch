package store_test

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
)

func mustCreate(t testkit.T) (*store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return s, dir
}

var _ = testkit.Spec("TestCreateLayout", func(t testkit.T) {
	s, dir := mustCreate(t)

	for _, f := range []string{
		filepath.Join(dir, ".research", "config.yaml"),
		filepath.Join(dir, ".research", "state.json"),
		filepath.Join(dir, ".research", "events.jsonl"),
	} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("expected file %s: %v", f, err)
		}
	}
	for _, d := range []string{
		s.HypothesesDir(),
		s.ExperimentsDir(),
		s.ObservationsDir(),
		s.ConclusionsDir(),
		s.ArtifactsDir(),
		s.LessonsDir(),
	} {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("expected dir %s: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", d)
		}
	}

	// Worktrees directory is NOT created inside .research/ — it lives in the
	// user cache dir by default, and even the local directory should be absent.
	if _, err := os.Stat(filepath.Join(dir, ".research", "worktrees")); !os.IsNotExist(err) {
		t.Errorf(".research/worktrees/ should not exist; stat err=%v", err)
	}

	// The configured worktrees root is absolute, outside the project tree,
	// and under the user cache dir.
	cfg, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Worktrees.Root == "" {
		t.Fatal("worktrees.root not populated by Create")
	}
	if !filepath.IsAbs(cfg.Worktrees.Root) {
		t.Errorf("worktrees.root should be absolute, got %q", cfg.Worktrees.Root)
	}
	absDir, _ := filepath.Abs(dir)
	if strings.HasPrefix(cfg.Worktrees.Root, absDir) {
		t.Errorf("worktrees.root should live outside the project tree, got %q (project=%q)", cfg.Worktrees.Root, absDir)
	}
	gotRoot, err := s.WorktreesRoot()
	if err != nil {
		t.Fatal(err)
	}
	if gotRoot != cfg.Worktrees.Root {
		t.Errorf("WorktreesRoot() mismatch: %q vs %q", gotRoot, cfg.Worktrees.Root)
	}
})

var _ = testkit.Spec("TestProjectKeyStability", func(t testkit.T) {
	r1, err := store.DefaultWorktreesRoot("/tmp/firmware")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := store.DefaultWorktreesRoot("/tmp/firmware")
	if err != nil {
		t.Fatal(err)
	}
	if r1 != r2 {
		t.Errorf("same project should produce the same root: %q vs %q", r1, r2)
	}
	r3, err := store.DefaultWorktreesRoot("/other/firmware")
	if err != nil {
		t.Fatal(err)
	}
	if r1 == r3 {
		t.Errorf("different project paths should produce different roots, got same: %q", r1)
	}
})

var _ = testkit.Spec("TestCreateRejectsDouble", func(t testkit.T) {
	_, dir := mustCreate(t)
	_, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != store.ErrAlreadyInitialized {
		t.Errorf("second Create: got %v, want ErrAlreadyInitialized", err)
	}
})

var _ = testkit.Spec("TestOpenRejectsUninitialized", func(t testkit.T) {
	dir := t.TempDir()
	_, err := store.Open(dir)
	if err != store.ErrNotInitialized {
		t.Errorf("Open: got %v, want ErrNotInitialized", err)
	}
})

var _ = testkit.Spec("TestOpenWalksUpToFindStore", func(t testkit.T) {
	root, _ := mustCreate(t)

	// Make a nested subdirectory inside the project.
	sub := filepath.Join(root.Root(), "src", "inner", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	// Opening from the deep subdir should find the project's .research/.
	s, err := store.Open(sub)
	if err != nil {
		t.Fatalf("upward walk Open: %v", err)
	}
	if s.Root() != root.Root() {
		t.Errorf("root: got %q want %q", s.Root(), root.Root())
	}
})

var _ = testkit.Spec("TestOpenWalkStopsAtFilesystemRoot", func(t testkit.T) {
	// A directory somewhere with no .research/ and no ancestor with one.
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := store.Open(sub)
	if err != store.ErrNotInitialized {
		t.Errorf("deep dir with no ancestor store: got %v, want ErrNotInitialized", err)
	}
})

var _ = testkit.Spec("TestConfigRoundTrip", func(t testkit.T) {
	_, dir := mustCreate(t)
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Build.Command != "true" || cfg.Test.Command != "true" {
		t.Errorf("config round trip: got build=%q test=%q", cfg.Build.Command, cfg.Test.Command)
	}
	if cfg.Mode != "strict" {
		t.Errorf("default mode: got %q, want strict", cfg.Mode)
	}
	if cfg.SchemaVersion != 1 {
		t.Errorf("schema version: got %d, want 1", cfg.SchemaVersion)
	}
})

var _ = testkit.Spec("TestAllocID", func(t testkit.T) {
	s, _ := mustCreate(t)

	id1, err := s.AllocID(store.KindHypothesis)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != "H-0001" {
		t.Errorf("first hypothesis id: got %q, want H-0001", id1)
	}
	id2, err := s.AllocID(store.KindHypothesis)
	if err != nil {
		t.Fatal(err)
	}
	if id2 != "H-0002" {
		t.Errorf("second hypothesis id: got %q, want H-0002", id2)
	}

	eid, err := s.AllocID(store.KindExperiment)
	if err != nil {
		t.Fatal(err)
	}
	if eid != "E-0001" {
		t.Errorf("first experiment id: got %q, want E-0001", eid)
	}

	// Counters must persist across reopen.
	s2, err := store.Open(s.Root())
	if err != nil {
		t.Fatal(err)
	}
	id3, err := s2.AllocID(store.KindHypothesis)
	if err != nil {
		t.Fatal(err)
	}
	if id3 != "H-0003" {
		t.Errorf("after reopen: got %q, want H-0003", id3)
	}
})

var _ = testkit.Spec("TestEventsAppendAndRead", func(t testkit.T) {
	s, _ := mustCreate(t)

	if err := s.AppendEvent(store.Event{Kind: "init", Actor: "system"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(store.Event{Kind: "hypothesis.add", Actor: "human:alice", Subject: "H-0001"}); err != nil {
		t.Fatal(err)
	}

	events, err := s.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events count: got %d, want 2", len(events))
	}
	if events[0].Kind != "init" || events[1].Kind != "hypothesis.add" {
		t.Errorf("events out of order: %+v", events)
	}
	if events[1].Subject != "H-0001" {
		t.Errorf("subject: got %q, want H-0001", events[1].Subject)
	}

	st, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if st.LastEventAt == nil {
		t.Error("LastEventAt not set after AppendEvent")
	}
})

var _ = testkit.Spec("TestEventsLimit", func(t testkit.T) {
	s, _ := mustCreate(t)
	for i := 0; i < 5; i++ {
		if err := s.AppendEvent(store.Event{Kind: "noop"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Events(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("limited events: got %d, want 3", len(got))
	}
})

var _ = testkit.Spec("TestCounts", func(t testkit.T) {
	s, _ := mustCreate(t)
	counts, err := s.Counts()
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"hypotheses", "experiments", "observations", "conclusions", "lessons"} {
		if counts[k] != 0 {
			t.Errorf("%s: got %d, want 0", k, counts[k])
		}
	}

	// Drop a fake file into hypotheses/ and re-count.
	if err := os.WriteFile(filepath.Join(s.HypothesesDir(), "H-0001.md"), []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	counts, err = s.Counts()
	if err != nil {
		t.Fatal(err)
	}
	if counts["hypotheses"] != 1 {
		t.Errorf("hypotheses: got %d, want 1", counts["hypotheses"])
	}
})
