package cli

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// pagerState wraps a bubbles/viewport.Model with the common scroll keys the
// TUI's three read-only content viewers (reportView, artifactView,
// artifactDiffView) all share. Each of those viewers embeds a pagerState
// and delegates key/mouse/size handling through these methods, so the
// boilerplate — construct-or-resize, g/G, viewport.Update passthrough —
// lives in exactly one place.
//
// The embedder is still free to intercept keys before or after this layer
// (e.g. artifactView handles "/", "d", "Tab" for itself and only forwards
// what remains).
type pagerState struct {
	vp    viewport.Model
	ready bool
}

// ensureSize constructs the viewport on first use, then keeps its size in
// sync with whatever render budget the caller last computed.
func (p *pagerState) ensureSize(width, height int) {
	if !p.ready {
		p.vp = viewport.New(width, height)
		p.ready = true
		return
	}
	p.vp.Width = width
	p.vp.Height = height
}

// setContent swaps the rendered body. Safe to call before ensureSize (it
// becomes a no-op in that case).
func (p *pagerState) setContent(s string) {
	if p.ready {
		p.vp.SetContent(s)
	}
}

// handleKey processes pager-common keys (g/G) and delegates anything else
// to the underlying viewport's own keymap (arrows, j/k, PgUp/PgDn, u/d).
// Returns the resulting tea.Cmd from the viewport update.
//
// Callers are responsible for checking whether they want to intercept a
// key *before* calling this. Anything passed in here is treated as a
// pager-level input.
func (p *pagerState) handleKey(msg tea.KeyMsg) tea.Cmd {
	if !p.ready {
		return nil
	}
	switch msg.String() {
	case "g":
		p.vp.GotoTop()
		return nil
	case "G":
		p.vp.GotoBottom()
		return nil
	}
	var cmd tea.Cmd
	p.vp, cmd = p.vp.Update(msg)
	return cmd
}

// handleMouse forwards a mouse event to the viewport for wheel scrolling.
func (p *pagerState) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if !p.ready {
		return nil
	}
	var cmd tea.Cmd
	p.vp, cmd = p.vp.Update(msg)
	return cmd
}

// view renders the current viewport contents (after ensureSize has been
// called at least once).
func (p *pagerState) view() string {
	if !p.ready {
		return ""
	}
	return p.vp.View()
}

// gotoTop moves the cursor to the first line. Intended for use after a
// content reload so the viewer doesn't appear to hang at a stale offset.
func (p *pagerState) gotoTop() {
	if p.ready {
		p.vp.GotoTop()
	}
}
