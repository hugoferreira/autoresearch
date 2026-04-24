package cli

import (
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
	tea "github.com/charmbracelet/bubbletea"
)

type staticTUIView struct{}

func (v *staticTUIView) title() string                                       { return "static" }
func (v *staticTUIView) kind() string                                        { return "test.static" }
func (v *staticTUIView) init(_ *store.Store) tea.Cmd                         { return nil }
func (v *staticTUIView) update(_ tea.Msg, _ *store.Store) (tuiView, tea.Cmd) { return v, nil }
func (v *staticTUIView) view(_, _ int) string                                { return "" }
func (v *staticTUIView) hints() []tuiHint                                    { return nil }

type quietTickTestView struct {
	ticks  int
	lastAt time.Time
}

func (v *quietTickTestView) title() string                                       { return "tick" }
func (v *quietTickTestView) kind() string                                        { return "test.tick" }
func (v *quietTickTestView) init(_ *store.Store) tea.Cmd                         { return nil }
func (v *quietTickTestView) update(_ tea.Msg, _ *store.Store) (tuiView, tea.Cmd) { return v, nil }
func (v *quietTickTestView) view(_, _ int) string                                { return "" }
func (v *quietTickTestView) hints() []tuiHint                                    { return nil }
func (v *quietTickTestView) quietTick(at time.Time, _ *store.Store) (tuiView, tea.Cmd) {
	v.ticks++
	v.lastAt = at
	return v, nil
}

// TestTUI_LessonDetail_PreservesScrollAcrossReloads is the regression anchor
// for issue #25. Before the fix, every refresh tick re-issued a reload of
// the current entity and the loadedMsg handler called pager.gotoTop(),
// rubber-banding the user back to line 1 every 2 seconds.
//
// Now the reload path does not call gotoTop(); user scroll is preserved.
// The only way to scroll to top is the `g` key handled by pagerState.
var _ = testkit.Spec("TestTUI_LessonDetail_PreservesScrollAcrossReloads", func(t testkit.T) {
	v := newLessonDetailView("L-0001")
	l := &entity.Lesson{
		ID:        "L-0001",
		Claim:     "long lesson",
		Scope:     entity.LessonScopeHypothesis,
		Status:    entity.LessonStatusActive,
		Subjects:  []string{"H-0001"},
		Author:    "agent:analyst",
		CreatedAt: time.Now().UTC(),
		Body:      strings.Repeat("line of content long enough to scroll\n", 200),
	}

	// First render: prime the pager with width/height and content.
	// We call .view() first because ensureSize() runs there, which
	// constructs the viewport.
	nv, _ := v.update(lessonDetailLoadedMsg{l: l}, nil)
	detail := nv.(*lessonDetailView)
	_ = detail.view(80, 20)
	// Re-run load now that pager.ready is true so setContent sticks.
	nv, _ = detail.update(lessonDetailLoadedMsg{l: l}, nil)
	detail = nv.(*lessonDetailView)
	_ = detail.view(80, 20)
	if !detail.pager.ready {
		t.Fatal("pager failed to initialize")
	}

	// Scroll down a few pages.
	_ = detail.pager.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
	_ = detail.pager.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
	preOffset := detail.pager.vp.YOffset
	if preOffset <= 0 {
		t.Fatalf("pager should have scrolled down; YOffset=%d", preOffset)
	}

	// Simulate a reload arriving (as if storeChangedMsg triggered
	// v.init(s) and the async load fired back). Pager scroll must
	// survive.
	nv, _ = detail.update(lessonDetailLoadedMsg{l: l}, nil)
	detail = nv.(*lessonDetailView)
	_ = detail.view(80, 20)

	postOffset := detail.pager.vp.YOffset
	if postOffset != preOffset {
		t.Errorf("scroll offset changed across reload: pre=%d post=%d — gotoTop leaked back into the load handler",
			preOffset, postOffset)
	}
})

// TestTUI_Artifact_ScrollResetOnlyOnUserModeChange verifies the
// scrollResetPending flag does its job: background reloads preserve
// scroll, but explicit Tab/grep-driven mode changes reset to top
// because the content fundamentally changed.
var _ = testkit.Spec("TestTUI_Artifact_ScrollResetOnlyOnUserModeChange", func(t testkit.T) {
	v := newArtifactView(artifactRow{SHA: "abcdef", Path: "test.txt"})
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = "line body"
	}
	// Prime pager with initial content.
	nv, _ := v.update(artifactLoadedMsg{
		sha: "abcdef1234567890", abs: "/tmp/x", rel: "test.txt",
		lines: lines, total: 200, bytes: 4000,
	}, nil)
	art := nv.(*artifactView)
	_ = art.view(80, 20)
	nv, _ = art.update(artifactLoadedMsg{
		sha: "abcdef1234567890", abs: "/tmp/x", rel: "test.txt",
		lines: lines, total: 200, bytes: 4000,
	}, nil)
	art = nv.(*artifactView)
	_ = art.view(80, 20)
	if !art.pager.ready {
		t.Fatal("pager failed to initialize")
	}

	// Scroll down.
	_ = art.pager.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
	_ = art.pager.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
	preOffset := art.pager.vp.YOffset
	if preOffset <= 0 {
		t.Fatalf("pager should have scrolled; YOffset=%d", preOffset)
	}

	// Background reload (scrollResetPending=false). Scroll preserved.
	nv, _ = art.update(artifactLoadedMsg{
		sha: "abcdef1234567890", abs: "/tmp/x", rel: "test.txt",
		lines: lines, total: 200, bytes: 4000,
	}, nil)
	art = nv.(*artifactView)
	if art.pager.vp.YOffset != preOffset {
		t.Errorf("background reload reset scroll: pre=%d post=%d",
			preOffset, art.pager.vp.YOffset)
	}

	// User-initiated Tab cycle sets scrollResetPending. Next load
	// resets scroll (the content fundamentally changed — we're now
	// looking at tail instead of head).
	art.scrollResetPending = true
	nv, _ = art.update(artifactLoadedMsg{
		sha: "abcdef1234567890", abs: "/tmp/x", rel: "test.txt",
		lines: lines, total: 200, bytes: 4000,
	}, nil)
	art = nv.(*artifactView)
	if art.pager.vp.YOffset != 0 {
		t.Errorf("user-initiated mode change should reset scroll, got YOffset=%d", art.pager.vp.YOffset)
	}
	if art.scrollResetPending {
		t.Error("scrollResetPending should be cleared after consumption")
	}
})

// TestTUI_StoreChangedMsg_EmptyBatchIsNoopForNonTickViews verifies the
// app-level behavior that a quiet poll (no new events) still produces no
// reload path for views that have not opted into elapsed-time repaints.
var _ = testkit.Spec("TestTUI_StoreChangedMsg_EmptyBatchIsNoopForNonTickViews", func(t testkit.T) {
	m := newTuiModel(nil, goalScope{All: true}, 2*time.Second)
	m.stack = []tuiView{&staticTUIView{}}
	m.eventOffset = 42

	updated, cmd := m.Update(storeChangedMsg{events: nil, newOff: 99})
	nm, ok := updated.(tuiModel)
	if !ok {
		t.Fatalf("expected tuiModel back, got %T", updated)
	}
	if nm.eventOffset != 99 {
		t.Errorf("eventOffset should advance even on empty batch: got %d, want 99", nm.eventOffset)
	}
	if cmd != nil {
		t.Errorf("empty batch should not emit commands; got %v", cmd)
	}
})

var _ = testkit.Spec("TestTUI_StoreChangedMsg_EmptyBatchDispatchesQuietTickToOptInView", func(t testkit.T) {
	at := time.Date(2026, 4, 19, 12, 30, 0, 0, time.UTC)
	m := newTuiModel(nil, goalScope{All: true}, 2*time.Second)
	m.stack = []tuiView{&quietTickTestView{}}
	m.eventOffset = 42

	updated, cmd := m.Update(storeChangedMsg{events: nil, newOff: 99, polledAt: at})
	nm := updated.(tuiModel)
	if nm.eventOffset != 99 {
		t.Fatalf("eventOffset = %d, want 99", nm.eventOffset)
	}
	if cmd != nil {
		t.Fatalf("quiet tick repaint should not emit commands; got %v", cmd)
	}
	v, ok := nm.top().(*quietTickTestView)
	if !ok {
		t.Fatalf("top view = %T, want *quietTickTestView", nm.top())
	}
	if v.ticks != 1 {
		t.Fatalf("quietTick count = %d, want 1", v.ticks)
	}
	if !v.lastAt.Equal(at) {
		t.Fatalf("quietTick time = %s, want %s", v.lastAt, at)
	}
})

// TestTUI_StoreChangedMsg_NonEmptyBatchDispatches verifies that when there
// ARE new events, the top view gets the message and chrome is refreshed.
// We can't easily assert the exact commands fired from here, but we can
// at least assert a non-nil command comes back (chrome at minimum).
var _ = testkit.Spec("TestTUI_StoreChangedMsg_NonEmptyBatchDispatches", func(t testkit.T) {
	// Need a non-nil store so fetchChrome/InvalidateFromEvents don't nil-
	// dereference. A throwaway is fine.
	dir := t.TempDir()
	s, err := store.Create(dir, store.Config{
		Build: store.CommandSpec{Command: "true"},
		Test:  store.CommandSpec{Command: "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := newTuiModel(s, goalScope{All: true}, 2*time.Second)

	events := []store.Event{{Kind: "hypothesis.add", Subject: "H-0001", Actor: "human"}}
	updated, cmd := m.Update(storeChangedMsg{events: events, newOff: 100})
	nm := updated.(tuiModel)
	if nm.eventOffset != 100 {
		t.Errorf("eventOffset = %d, want 100", nm.eventOffset)
	}
	if cmd == nil {
		t.Error("non-empty batch should emit at least chrome + top-view commands")
	}
})
