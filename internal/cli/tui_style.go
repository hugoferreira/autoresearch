package cli

import "github.com/charmbracelet/lipgloss"

// TUI palette. Colors are chosen to match the existing `ansi` helper used by
// the plain-text dashboard so the two renderings feel like the same product.
var (
	tuiBold   = lipgloss.NewStyle().Bold(true)
	tuiDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	tuiRed    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	tuiGreen  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	tuiYellow = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	tuiBlue   = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	tuiMag    = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	tuiCyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))

	tuiBoldYellow = tuiBold.Foreground(lipgloss.Color("11"))

	tuiHeaderBar = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("237")).
			Padding(0, 1)

	tuiHintBar = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	tuiPanelTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))

	tuiPanelBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	tuiPanelBorderActive = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("14")).
				Padding(0, 1)

	tuiSelected = lipgloss.NewStyle().
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("14"))

	tuiHelpBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("14")).
			Padding(1, 2)
)

// tuiStatusGlyph returns a colored status glyph for a hypothesis status.
func tuiStatusGlyph(status string) string {
	switch status {
	case "supported":
		return tuiGreen.Render("✓")
	case "refuted":
		return tuiRed.Render("✗")
	case "inconclusive":
		return tuiYellow.Render("?")
	case "killed":
		return tuiDim.Render("☠")
	default:
		return tuiCyan.Render("•")
	}
}

// tuiStatusBadge colors an experiment status string.
func tuiExpStatusBadge(status string) string {
	switch status {
	case "designed":
		return tuiDim.Render(status)
	case "implemented":
		return tuiCyan.Render(status)
	case "measured":
		return tuiYellow.Render(status)
	case "analyzed":
		return tuiGreen.Render(status)
	case "failed":
		return tuiRed.Render(status)
	default:
		return status
	}
}

// tuiVerdictBadge colors a conclusion verdict string.
func tuiVerdictBadge(verdict string) string {
	switch verdict {
	case "supported":
		return tuiGreen.Render(verdict)
	case "refuted":
		return tuiRed.Render(verdict)
	case "inconclusive":
		return tuiYellow.Render(verdict)
	default:
		return verdict
	}
}

// tuiEventKindColor colors an event kind token by its category prefix, same
// palette as the plain dashboard.
func tuiEventKindColor(kind string) string {
	switch {
	case startsWith(kind, "hypothesis."):
		return tuiCyan.Render(kind)
	case startsWith(kind, "experiment."):
		return tuiYellow.Render(kind)
	case startsWith(kind, "observation."):
		return tuiBlue.Render(kind)
	case startsWith(kind, "conclusion."):
		return tuiGreen.Render(kind)
	case kind == "pause" || kind == "resume":
		return tuiMag.Render(kind)
	case kind == "init":
		return tuiBold.Render(kind)
	default:
		return kind
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
