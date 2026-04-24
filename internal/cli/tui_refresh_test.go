package cli

import (
	"strings"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var _ = Describe("TUI refresh behavior", func() {
	Describe("pager-backed reloads", func() {
		It("preserves lesson detail scroll across background reloads", func() {
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

			nv, _ := v.update(lessonDetailLoadedMsg{l: l}, nil)
			detail := nv.(*lessonDetailView)
			_ = detail.view(80, 20)
			nv, _ = detail.update(lessonDetailLoadedMsg{l: l}, nil)
			detail = nv.(*lessonDetailView)
			_ = detail.view(80, 20)
			Expect(detail.pager.ready).To(BeTrue())

			_ = detail.pager.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
			_ = detail.pager.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
			preOffset := detail.pager.vp.YOffset
			Expect(preOffset).To(BeNumerically(">", 0))

			nv, _ = detail.update(lessonDetailLoadedMsg{l: l}, nil)
			detail = nv.(*lessonDetailView)
			_ = detail.view(80, 20)

			Expect(detail.pager.vp.YOffset).To(Equal(preOffset))
		})

		It("resets artifact scroll only for explicit user content changes", func() {
			v := newArtifactView(artifactRow{SHA: "abcdef", Path: "test.txt"})
			lines := make([]string, 200)
			for i := range lines {
				lines[i] = "line body"
			}
			msg := artifactLoadedMsg{
				sha:   "abcdef1234567890",
				abs:   "/tmp/x",
				rel:   "test.txt",
				lines: lines,
				total: 200,
				bytes: 4000,
			}

			nv, _ := v.update(msg, nil)
			art := nv.(*artifactView)
			_ = art.view(80, 20)
			nv, _ = art.update(msg, nil)
			art = nv.(*artifactView)
			_ = art.view(80, 20)
			Expect(art.pager.ready).To(BeTrue())

			_ = art.pager.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
			_ = art.pager.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
			preOffset := art.pager.vp.YOffset
			Expect(preOffset).To(BeNumerically(">", 0))

			nv, _ = art.update(msg, nil)
			art = nv.(*artifactView)
			Expect(art.pager.vp.YOffset).To(Equal(preOffset))

			art.scrollResetPending = true
			nv, _ = art.update(msg, nil)
			art = nv.(*artifactView)
			Expect(art.pager.vp.YOffset).To(Equal(0))
			Expect(art.scrollResetPending).To(BeFalse())
		})
	})

	Describe("store change dispatch", func() {
		It("advances the event offset without reloading non-tick views on quiet polls", func() {
			m := newTuiModel(nil, goalScope{All: true}, 2*time.Second)
			m.stack = []tuiView{&staticTUIView{}}
			m.eventOffset = 42

			updated, cmd := m.Update(storeChangedMsg{events: nil, newOff: 99})
			nm, ok := updated.(tuiModel)
			Expect(ok).To(BeTrue())
			Expect(nm.eventOffset).To(Equal(int64(99)))
			Expect(cmd).To(BeNil())
		})

		It("sends quiet ticks to views that opted into elapsed-time repainting", func() {
			at := time.Date(2026, 4, 19, 12, 30, 0, 0, time.UTC)
			m := newTuiModel(nil, goalScope{All: true}, 2*time.Second)
			m.stack = []tuiView{&quietTickTestView{}}
			m.eventOffset = 42

			updated, cmd := m.Update(storeChangedMsg{events: nil, newOff: 99, polledAt: at})
			nm := updated.(tuiModel)
			Expect(nm.eventOffset).To(Equal(int64(99)))
			Expect(cmd).To(BeNil())

			v, ok := nm.top().(*quietTickTestView)
			Expect(ok).To(BeTrue())
			Expect(v.ticks).To(Equal(1))
			Expect(v.lastAt).To(Equal(at))
		})

		It("dispatches non-empty event batches through the reload path", func() {
			s := createCLIStore()
			m := newTuiModel(s, goalScope{All: true}, 2*time.Second)

			events := []store.Event{{Kind: "hypothesis.add", Subject: "H-0001", Actor: "human"}}
			updated, cmd := m.Update(storeChangedMsg{events: events, newOff: 100})
			nm := updated.(tuiModel)
			Expect(nm.eventOffset).To(Equal(int64(100)))
			Expect(cmd).NotTo(BeNil())
		})
	})
})
