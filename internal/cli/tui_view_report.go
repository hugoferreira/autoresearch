package cli

import (
	"strings"

	"github.com/bytter/autoresearch/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	gansi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

// reportView renders the markdown report for a single hypothesis in a
// scrollable viewport, styled with glamour so headings/bold/italic/code
// spans actually render instead of leaking the raw Markdown sigils.
//
// Rendering is width-aware: glamour hard-wraps to a fixed word-wrap length,
// so whenever the terminal is resized we re-render the cached markdown
// against the new viewport width.

type reportView struct {
	id            string
	md            string // raw markdown returned by renderReportMarkdown
	rendered      string // glamour-styled ANSI output, cached
	renderedWidth int    // width used to produce `rendered` (-1 = stale)
	err           error
	pager         pagerState
}

type reportLoadedMsg struct {
	md  string
	err error
}

func newReportView(hypID string) *reportView { return &reportView{id: hypID, renderedWidth: -1} }

func (v *reportView) title() string { return "Report " + v.id }

func (v *reportView) init(s *store.Store) tea.Cmd {
	id := v.id
	return func() tea.Msg {
		h, err := s.ReadHypothesis(id)
		if err != nil {
			return reportLoadedMsg{err: err}
		}
		rep, err := buildReport(s, h)
		if err != nil {
			return reportLoadedMsg{err: err}
		}
		return reportLoadedMsg{md: renderReportMarkdown(rep)}
	}
}

func (v *reportView) update(msg tea.Msg, _ *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case reportLoadedMsg:
		v.md = msg.md
		v.err = msg.err
		v.renderedWidth = -1 // invalidate glamour cache
		if v.pager.ready {
			v.pager.setContent(v.ensureRendered(v.pager.vp.Width))
			v.pager.gotoTop()
		}
		return v, nil
	case tea.KeyMsg:
		return v, v.pager.handleKey(msg)
	case tea.MouseMsg:
		return v, v.pager.handleMouse(msg)
	}
	return v, nil
}

func (v *reportView) hints() []tuiHint {
	return []tuiHint{{"g/G", "top/bot"}, {"↑↓/PgUp/PgDn", "scroll"}}
}

// renderMarkdown renders markdown for TUI panels. Uses glamour for
// styling (headings, bold, italic, code) but with a very wide word-wrap
// to prevent glamour's aggressive line-breaking (which splits on hyphens
// and equals). Then re-wraps the output with space-only word wrapping.
func renderMarkdown(width int, md string) string {
	if width <= 0 {
		width = 80
	}
	return renderMarkdownRewrap(width, md)
}

// renderMarkdownRewrap renders with glamour at unlimited width (no
// wrapping), then re-wraps each line using space-only breaking.
func renderMarkdownRewrap(width int, md string) string {
	style := flushDarkStyle()
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(0), // no wrapping — we do it ourselves
	)
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	// Re-wrap each line with space-only breaking.
	var rewrapped []string
	for _, line := range strings.Split(out, "\n") {
		visible := lipgloss.Width(line)
		if visible <= width {
			rewrapped = append(rewrapped, line)
		} else {
			rewrapped = append(rewrapped, wrapANSI(line, width))
		}
	}
	return strings.Join(rewrapped, "\n")
}

// wrapANSI wraps an ANSI-styled line at the given visible width,
// breaking only on spaces. Preserves escape sequences across breaks.
func wrapANSI(line string, width int) string {
	// Split on spaces, preserving ANSI codes attached to words.
	words := strings.Split(line, " ")
	var lines []string
	cur := ""
	curW := 0
	for _, w := range words {
		if w == "" {
			continue
		}
		ww := lipgloss.Width(w)
		if cur == "" {
			cur = w
			curW = ww
			continue
		}
		if curW+1+ww <= width {
			cur += " " + w
			curW += 1 + ww
		} else {
			lines = append(lines, cur)
			cur = w
			curW = ww
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return strings.Join(lines, "\n")
}

// flushDarkStyle returns glamour's dark style with all block margins
// set to zero so rendered markdown sits flush inside TUI panels.
func flushDarkStyle() gansi.StyleConfig {
	s := styles.DarkStyleConfig
	zero := uint(0)
	s.Document.Margin = &zero
	s.CodeBlock.Margin = &zero
	return s
}

// ensureRendered returns the glamour-styled body, rendering (or re-rendering)
// it if the cache is stale against `width`. Glamour wraps paragraphs to a
// fixed column budget, so width changes invalidate the cache. Errors fall
// back to the raw markdown so the user at least sees *something*.
func (v *reportView) ensureRendered(width int) string {
	if width <= 0 {
		width = 80
	}
	if v.rendered != "" && v.renderedWidth == width {
		return v.rendered
	}
	v.rendered = renderMarkdown(width, v.md)
	v.renderedWidth = width
	return v.rendered
}

func (v *reportView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if v.md == "" {
		return tuiDim.Render("loading…")
	}
	v.pager.ensureSize(width, height)
	v.pager.setContent(v.ensureRendered(width))
	return v.pager.view()
}
