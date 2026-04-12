package cli

import (
	"fmt"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

// This file is the shared toolbox every TUI view reaches into. Anything
// that's used by more than one view lives here so individual view files
// can stay focused on their data model and layout. Conceptually it's
// three groups:
//
//   1. line layout: truncation, padding, scroll-window, row highlight
//   2. list scaffolding: cursor navigation + the header+rows+scroll body
//      that every filterable list view renders
//   3. small table renderers (key/value, column-aligned) used by detail
//      views
//
// Keeping it in one file means a single import + grep point for the
// patterns the views share.

// ---- line layout ----

// truncDisplay truncates a (possibly ANSI-styled) string to the given
// visible width, preserving color escapes and appending "…" when the
// string is actually clipped. Used everywhere a panel or header line
// would otherwise overflow its column and word-wrap.
func truncDisplay(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return xansi.Truncate(s, width, "…")
}

// truncLines clips each line to `width` in-place-style via a fresh slice.
func truncLines(lines []string, width int) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = truncDisplay(l, width)
	}
	return out
}

// fmtValue formats a numeric value with a compact unit suffix.
// Seconds are scaled to ns/us/ms/s; bytes shortened to B; everything
// else gets 2 decimal places + the raw unit.
func fmtValue(v float64, unit string) string {
	switch unit {
	case "seconds", "s":
		switch {
		case v == 0:
			return "0s"
		case v < 1e-6:
			return fmt.Sprintf("%.2fns", v*1e9)
		case v < 1e-3:
			return fmt.Sprintf("%.2fus", v*1e6)
		case v < 1:
			return fmt.Sprintf("%.2fms", v*1e3)
		default:
			return fmt.Sprintf("%.2fs", v)
		}
	case "bytes":
		if v >= 1024*1024 {
			return fmt.Sprintf("%.1fMB", v/(1024*1024))
		}
		if v >= 1024 {
			return fmt.Sprintf("%.1fKB", v/1024)
		}
		return fmt.Sprintf("%.0fB", v)
	default:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d%s", int64(v), shortUnit(unit))
		}
		return fmt.Sprintf("%.2f%s", v, shortUnit(unit))
	}
}

func shortUnit(u string) string {
	switch u {
	case "cycles":
		return "cyc"
	case "instructions":
		return "ins"
	case "pass":
		return ""
	default:
		return u
	}
}

// stripLeftMargin removes up to `n` leading spaces from each line of s.
func stripLeftMargin(s string, n int) string {
	prefix := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(l, prefix)
	}
	return strings.Join(lines, "\n")
}

// padRight right-pads s with spaces to the given *visible* width. Shorter
// strings are extended; longer ones are returned unchanged (truncate first
// if you need a hard cap).
func padRight(s string, width int) string {
	n := lipgloss.Width(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

// scrollWindow returns the slice of `lines` containing `cursor` inside an
// `inner` height viewport. Preserves leading context when possible.
func scrollWindow(lines []string, cursor, inner int) []string {
	if len(lines) <= inner {
		return lines
	}
	start := cursor - inner/2
	if start < 0 {
		start = 0
	}
	end := start + inner
	if end > len(lines) {
		end = len(lines)
		start = end - inner
	}
	return lines[start:end]
}

// highlightRow paints the row at `cursor` with the selection background.
// Assumes all input rows are already truncated to `width`. ANSI escapes on
// the selected row are stripped (the background supersedes per-token
// coloring) and the cell is padded to exactly `width` so the highlight
// spans the full row.
func highlightRow(lines []string, cursor, width int) []string {
	if cursor < 0 || cursor >= len(lines) {
		return lines
	}
	out := make([]string, len(lines))
	copy(out, lines)
	plain := stripANSI(lines[cursor])
	if lipgloss.Width(plain) > width {
		plain = xansi.Truncate(plain, width, "")
	}
	out[cursor] = tuiSelected.Render(padRight(plain, width))
	return out
}

// ---- list scaffolding ----

// clampCursor returns cursor snapped into [0, length). For empty lists
// this is 0.
func clampCursor(cursor, length int) int {
	if cursor < 0 || length <= 0 {
		return 0
	}
	if cursor >= length {
		return length - 1
	}
	return cursor
}

// moveCursor shifts *cursor by delta, clamped to [0, length).
func moveCursor(cursor *int, delta, length int) {
	*cursor = clampCursor(*cursor+delta, length)
}

// handleListNav processes the common up/down/j/k navigation for any list
// view and returns true if it consumed the key. Each list view then only
// has to add its own filter/enter cases.
func handleListNav(msg tea.KeyMsg, cursor *int, length int) bool {
	switch msg.String() {
	case "up", "k":
		moveCursor(cursor, -1, length)
		return true
	case "down", "j":
		moveCursor(cursor, 1, length)
		return true
	}
	return false
}

// renderFilteredListBody produces the "header + blank line + truncated,
// highlighted, scrolled rows" body used by every filterable list view in
// the TUI. Callers only produce the header line and the raw per-row
// strings; this helper handles truncation, cursor highlight, and the
// scroll window so the pattern lives in exactly one place.
func renderFilteredListBody(header string, rows []string, cursor, width, height int) string {
	inner := max(height-3, 1)
	for i, r := range rows {
		rows[i] = truncDisplay(r, width-2)
	}
	rows = highlightRow(rows, cursor, width-2)
	rows = scrollWindow(rows, cursor, inner)
	return header + "\n\n" + strings.Join(rows, "\n")
}

// nextStatusFilter cycles through a ring of filter tokens. Used by every
// list view's `f` key.
func nextStatusFilter(cur string, cycle []string) string {
	for i, s := range cycle {
		if s == cur {
			return cycle[(i+1)%len(cycle)]
		}
	}
	return cycle[0]
}

// filterLabel prettifies an empty filter as "all" for display in the
// header / hint bar.
func filterLabel(s string) string {
	if s == "" {
		return "all"
	}
	return s
}

// ---- small text helpers ----

// emptyDash returns "-" for the empty string, else s.
func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// wrap soft-wraps s at width boundaries, breaking on spaces when possible.
// Used by detail views that render long claim/body strings.
func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var lines []string
	cur := ""
	for _, w := range words {
		if cur == "" {
			cur = w
			continue
		}
		if len(cur)+1+len(w) <= width {
			cur += " " + w
		} else {
			lines = append(lines, cur)
			cur = w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return strings.Join(lines, "\n")
}

// ---- tables ----

// renderKeyValueTable prints a two-column aligned key/value list. The key
// column is dim-styled and right-padded to the longest key. Used by detail
// views that want a consistent "label   value" block.
func renderKeyValueTable(pairs [][2]string, indent string) string {
	if len(pairs) == 0 {
		return ""
	}
	maxKey := 0
	for _, p := range pairs {
		if w := lipgloss.Width(p[0]); w > maxKey {
			maxKey = w
		}
	}
	var b strings.Builder
	for i, p := range pairs {
		pad := maxKey - lipgloss.Width(p[0])
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(indent)
		b.WriteString(tuiDim.Render(p[0]))
		b.WriteString(strings.Repeat(" ", pad+2))
		b.WriteString(p[1])
	}
	return b.String()
}

// renderTable prints an aligned text table. Row 0 is the header (dim).
// Columns are left-aligned and right-padded to the longest visible cell
// per column (ANSI-aware). Used for the observation/summary tables in
// experiment detail.
func renderTable(rows [][]string, indent string) string {
	if len(rows) == 0 {
		return ""
	}
	numCols := 0
	for _, r := range rows {
		if len(r) > numCols {
			numCols = len(r)
		}
	}
	widths := make([]int, numCols)
	for _, r := range rows {
		for i, cell := range r {
			if w := lipgloss.Width(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}
	var b strings.Builder
	for ri, r := range rows {
		if ri > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(indent)
		for i, cell := range r {
			if i > 0 {
				b.WriteString("  ")
			}
			pad := widths[i] - lipgloss.Width(cell)
			if ri == 0 {
				b.WriteString(tuiDim.Render(cell))
			} else {
				b.WriteString(cell)
			}
			if i < len(r)-1 && pad > 0 {
				b.WriteString(strings.Repeat(" ", pad))
			}
		}
	}
	return b.String()
}

// ---- hypothesis tree rendering (shared by dashboard + tree view) ----

// countTreeNodes is the DFS count of every node in a forest.
func countTreeNodes(nodes []*treeNode) int {
	n := 0
	for _, t := range nodes {
		n++
		n += countTreeNodes(t.Children)
	}
	return n
}

// flattenTree returns the DFS-ordered linear list of nodes, which is what
// cursor-driven navigation iterates over.
func flattenTree(nodes []*treeNode) []*treeNode {
	out := []*treeNode{}
	var walk func(ns []*treeNode)
	walk = func(ns []*treeNode) {
		for _, t := range ns {
			out = append(out, t)
			walk(t.Children)
		}
	}
	walk(nodes)
	return out
}

// renderTreeLines produces one styled line per node in the tree (DFS
// order). Prefixes are deliberately compact: a nested line is
// `│ ├ <glyph> <ID> <claim>`, so the status glyph lands at column 4 and
// the ID at column 6 for single-level-nested rows.
func renderTreeLines(nodes []*treeNode, claimWidth int) []string {
	out := []string{}
	var walk func(ns []*treeNode, prefix string)
	walk = func(ns []*treeNode, prefix string) {
		for i, n := range ns {
			last := i == len(ns)-1
			branch := "├ "
			nextPrefix := prefix + "│ "
			if last {
				branch = "└ "
				nextPrefix = prefix + "  "
			}
			glyph := tuiStatusGlyph(n.Status)
			claim := truncate(n.Claim, claimWidth)
			out = append(out, fmt.Sprintf("%s%s%s %s %s", prefix, branch, glyph, n.ID, claim))
			walk(n.Children, nextPrefix)
		}
	}
	walk(nodes, "")
	return out
}

// ---- budget meter ----

// tuiMeterColor picks a traffic-light color for a used/limit pair.
//   - <50%  → green
//   - 50-80% → yellow
//   - ≥80%  → red
//
// Thresholds live next to the callers that know what "full" means.
func tuiMeterColor(used, limit float64, s string) string {
	if limit <= 0 {
		return s
	}
	r := used / limit
	switch {
	case r >= 0.8:
		return tuiRed.Render(s)
	case r >= 0.5:
		return tuiYellow.Render(s)
	default:
		return tuiGreen.Render(s)
	}
}

// Compile-time anchor: all view files can refer to entity.Hypothesis via
// this without each having to carry a dead import. Replaces the per-file
// `var _ = entity.Hypothesis{}` anchors that used to clutter the views.
var _ = entity.Hypothesis{}
