package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/bytter/autoresearch/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// ---- row type shared by list + loaded message ----

type artifactRow struct {
	Observation string
	Instrument  string
	Name        string
	SHA         string
	Bytes       int64
	Path        string
}

// ---- artifact list view ----

type artifactListView struct {
	scope      goalScope
	all        []artifactRow
	filtered   []artifactRow
	cursor     int
	instFilter string
	err        error
}

type artifactListLoadedMsg struct {
	rows []artifactRow
	err  error
}

func newArtifactListView(scope goalScope) *artifactListView { return &artifactListView{scope: scope} }

func (v *artifactListView) title() string { return "Artifacts" }

func (v *artifactListView) init(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		obs, err := s.ListObservations()
		if err != nil {
			return artifactListLoadedMsg{err: err}
		}
		obs, err = newGoalScopeResolver(s, v.scope).filterObservations(obs)
		if err != nil {
			return artifactListLoadedMsg{err: err}
		}
		var rows []artifactRow
		for _, o := range obs {
			for _, a := range o.Artifacts {
				rows = append(rows, artifactRow{
					Observation: o.ID,
					Instrument:  o.Instrument,
					Name:        a.Name,
					SHA:         a.SHA,
					Bytes:       a.Bytes,
					Path:        a.Path,
				})
			}
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Observation != rows[j].Observation {
				return rows[i].Observation < rows[j].Observation
			}
			return rows[i].Name < rows[j].Name
		})
		return artifactListLoadedMsg{rows: rows}
	}
}

func (v *artifactListView) applyFilter() {
	v.filtered = v.filtered[:0]
	for _, r := range v.all {
		if v.instFilter != "" && r.Instrument != v.instFilter {
			continue
		}
		v.filtered = append(v.filtered, r)
	}
	v.cursor = clampCursor(v.cursor, len(v.filtered))
}

func (v *artifactListView) instrumentList() []string {
	seen := map[string]bool{"": true}
	out := []string{""}
	for _, r := range v.all {
		if !seen[r.Instrument] {
			seen[r.Instrument] = true
			out = append(out, r.Instrument)
		}
	}
	return out
}

func (v *artifactListView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case artifactListLoadedMsg:
		v.all = msg.rows
		v.err = msg.err
		v.applyFilter()
		return v, nil
	case tuiTickMsg:
		return v, v.init(s)
	case tea.KeyMsg:
		if handleListNav(msg, &v.cursor, len(v.filtered)) {
			return v, nil
		}
		switch msg.String() {
		case "f":
			v.instFilter = nextStatusFilter(v.instFilter, v.instrumentList())
			v.applyFilter()
		case "enter":
			if v.cursor >= 0 && v.cursor < len(v.filtered) {
				return v, tuiPush(newArtifactView(v.filtered[v.cursor]))
			}
		}
	}
	return v, nil
}

func (v *artifactListView) hints() []tuiHint {
	return []tuiHint{{"↑↓", "move"}, {"Enter", "open"}, {"f", "inst:" + filterLabel(v.instFilter)}}
}

func (v *artifactListView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	if len(v.all) == 0 {
		return tuiDim.Render("(no artifacts)")
	}
	header := tuiBold.Render(fmt.Sprintf("%d artifacts  ·  filter=%s",
		len(v.filtered), filterLabel(v.instFilter)))
	rows := make([]string, len(v.filtered))
	for i, r := range v.filtered {
		sha := r.SHA
		if len(sha) > 12 {
			sha = sha[:12]
		}
		rows[i] = fmt.Sprintf("%-10s  %-14s  %-8s  %s…  %10s  %s",
			r.Observation, r.Instrument, r.Name, sha, humanBytes(r.Bytes), r.Path)
	}
	return renderFilteredListBody(header, rows, v.cursor, width, height)
}

// humanBytes formats an int64 as a short human-readable size.
func humanBytes(n int64) string {
	const (
		_        = iota
		kb int64 = 1 << (10 * iota)
		mb
		gb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1fG", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1fM", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1fK", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// ---- artifact viewer ----

type artifactMode int

const (
	artifactModeHead artifactMode = iota // default: first N lines
	artifactModeTail                     // last N lines
	artifactModeFull                     // entire file (capped)
	artifactModeGrep                     // regex-filtered lines with context
)

const (
	artifactDefaultChunk = 400
	artifactMaxFull      = 20000
	artifactGrepMax      = 500
)

type artifactView struct {
	row       artifactRow
	sha       string
	absPath   string
	relPath   string
	mode      artifactMode
	lines     []string
	total     int
	fsBytes   int64
	grep      string
	grepInput textinput.Model
	editing   bool   // editing grep or diff input
	editKind  string // "grep" | "diff"
	pager     pagerState
	err       error
	owners    []string
}

type artifactLoadedMsg struct {
	sha    string
	abs    string
	rel    string
	lines  []string
	total  int
	bytes  int64
	owners []string
	err    error
}

func newArtifactView(row artifactRow) *artifactView {
	ti := textinput.New()
	ti.Placeholder = "regex"
	ti.CharLimit = 200
	ti.Prompt = "/ "
	return &artifactView{
		row:       row,
		mode:      artifactModeHead,
		grepInput: ti,
	}
}

func (v *artifactView) title() string {
	sha := v.row.SHA
	if len(sha) > 12 {
		sha = sha[:12]
	}
	return "Artifact " + sha
}

func (v *artifactView) init(s *store.Store) tea.Cmd {
	row := v.row
	mode := v.mode
	grep := v.grep
	return func() tea.Msg {
		sha, rel, abs, err := s.ArtifactLocation(row.SHA)
		if err != nil {
			return artifactLoadedMsg{err: err}
		}
		fi, err := os.Stat(abs)
		if err != nil {
			return artifactLoadedMsg{err: err}
		}
		lines, total, err := loadArtifactLines(abs, mode, grep)
		if err != nil {
			return artifactLoadedMsg{err: err}
		}
		owners := findArtifactOwners(s, sha)
		return artifactLoadedMsg{
			sha:    sha,
			abs:    abs,
			rel:    rel,
			lines:  lines,
			total:  total,
			bytes:  fi.Size(),
			owners: owners,
		}
	}
}

// loadArtifactLines reads the artifact at abs in the requested mode.
func loadArtifactLines(abs string, mode artifactMode, grep string) ([]string, int, error) {
	switch mode {
	case artifactModeHead:
		return readSomeLines(abs, artifactDefaultChunk)
	case artifactModeTail:
		return readLastLines(abs, artifactDefaultChunk)
	case artifactModeFull:
		return readSomeLines(abs, artifactMaxFull)
	case artifactModeGrep:
		if grep == "" {
			return nil, 0, nil
		}
		re, err := regexp.Compile(grep)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid regex: %w", err)
		}
		f, err := os.Open(abs)
		if err != nil {
			return nil, 0, err
		}
		defer f.Close()
		var matches []string
		total := 0
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			total++
			if re.MatchString(sc.Text()) && len(matches) < artifactGrepMax {
				matches = append(matches, fmt.Sprintf("%d: %s", total, sc.Text()))
			}
		}
		return matches, total, sc.Err()
	}
	return nil, 0, nil
}

func (v *artifactView) update(msg tea.Msg, s *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case artifactLoadedMsg:
		v.err = msg.err
		v.sha = msg.sha
		v.absPath = msg.abs
		v.relPath = msg.rel
		v.lines = msg.lines
		v.total = msg.total
		v.fsBytes = msg.bytes
		v.owners = msg.owners
		v.pager.setContent(strings.Join(v.lines, "\n"))
		v.pager.gotoTop()
		return v, nil
	case artifactDiffReadyMsg:
		return v, tuiPush(msg.view)
	case tea.KeyMsg:
		if v.editing {
			switch msg.String() {
			case "esc":
				v.editing = false
				v.grepInput.Blur()
				return v, nil
			case "enter":
				value := v.grepInput.Value()
				v.editing = false
				v.grepInput.Blur()
				switch v.editKind {
				case "grep":
					v.grep = value
					if value == "" {
						v.mode = artifactModeHead
					} else {
						v.mode = artifactModeGrep
					}
					return v, v.init(s)
				case "diff":
					if value == "" {
						return v, nil
					}
					return v, v.loadDiff(s, value)
				}
			}
			var cmd tea.Cmd
			v.grepInput, cmd = v.grepInput.Update(msg)
			return v, cmd
		}
		switch msg.String() {
		case "/":
			v.editing = true
			v.editKind = "grep"
			v.grepInput.Prompt = "/ "
			v.grepInput.Placeholder = "regex"
			v.grepInput.SetValue(v.grep)
			v.grepInput.Focus()
			return v, nil
		case "d":
			v.editing = true
			v.editKind = "diff"
			v.grepInput.Prompt = "diff vs: "
			v.grepInput.Placeholder = "sha prefix"
			v.grepInput.SetValue("")
			v.grepInput.Focus()
			return v, nil
		case "tab":
			v.mode = (v.mode + 1) % 3 // cycle head/tail/full, skip grep
			return v, v.init(s)
		}
		return v, v.pager.handleKey(msg)
	case tea.MouseMsg:
		return v, v.pager.handleMouse(msg)
	}
	return v, nil
}

// loadDiff reads two artifacts and shows the unified diff in a new view.
func (v *artifactView) loadDiff(s *store.Store, shaB string) tea.Cmd {
	absA := v.absPath
	relA := v.relPath
	shaA := v.sha
	return func() tea.Msg {
		_, relB, absB, err := s.ArtifactLocation(shaB)
		if err != nil {
			return tuiErrMsg{err: fmt.Errorf("b: %w", err)}
		}
		diffBin, err := exec.LookPath("diff")
		if err != nil {
			return tuiErrMsg{err: fmt.Errorf("diff binary not found in PATH")}
		}
		lines, err := runDiff(diffBin, absA, absB, 3)
		if err != nil {
			return tuiErrMsg{err: err}
		}
		return artifactDiffReadyMsg{
			view: newArtifactDiffView(shaA, relA, shaB, relB, lines),
		}
	}
}

type artifactDiffReadyMsg struct {
	view tuiView
}

func (v *artifactView) hints() []tuiHint {
	if v.editing {
		return []tuiHint{{"Enter", "submit"}, {"Esc", "cancel"}}
	}
	modeName := []string{"head", "tail", "full", "grep"}[v.mode]
	return []tuiHint{
		{"Tab", "mode:" + modeName},
		{"/", "grep"},
		{"d", "diff"},
		{"g/G", "top/bot"},
		{"↑↓/PgUp/PgDn", "scroll"},
	}
}

func (v *artifactView) view(width, height int) string {
	if v.err != nil {
		return tuiRed.Render("error: " + v.err.Error())
	}
	headerLines := []string{}
	headerLines = append(headerLines,
		tuiBold.Render(v.sha)+"  "+tuiDim.Render(v.relPath))
	headerLines = append(headerLines, tuiDim.Render(fmt.Sprintf(
		"bytes=%s  lines=%d  mode=%s",
		humanBytes(v.fsBytes), v.total,
		[]string{"head", "tail", "full", "grep"}[v.mode],
	)))
	if len(v.owners) > 0 {
		headerLines = append(headerLines, tuiDim.Render("owners="+strings.Join(v.owners, ", ")))
	}
	if v.editing {
		headerLines = append(headerLines, v.grepInput.View())
	} else if v.mode == artifactModeGrep && v.grep != "" {
		headerLines = append(headerLines, tuiDim.Render("grep=/"+v.grep+"/"))
	}
	header := strings.Join(headerLines, "\n")
	vpHeight := max(height-len(headerLines)-1, 3)
	v.pager.ensureSize(width, vpHeight)
	v.pager.setContent(strings.Join(v.lines, "\n"))
	return header + "\n" + v.pager.view()
}

// ---- artifact diff view ----

type artifactDiffView struct {
	shaA, shaB string
	relA, relB string
	lines      []string
	pager      pagerState
}

func newArtifactDiffView(shaA, relA, shaB, relB string, lines []string) *artifactDiffView {
	return &artifactDiffView{shaA: shaA, shaB: shaB, relA: relA, relB: relB, lines: lines}
}

func (v *artifactDiffView) title() string {
	return fmt.Sprintf("Diff %s…%s", shortSHA(v.shaA), shortSHA(v.shaB))
}

func (v *artifactDiffView) init(_ *store.Store) tea.Cmd { return nil }

func (v *artifactDiffView) update(msg tea.Msg, _ *store.Store) (tuiView, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return v, v.pager.handleKey(msg)
	case tea.MouseMsg:
		return v, v.pager.handleMouse(msg)
	}
	return v, nil
}

func (v *artifactDiffView) hints() []tuiHint {
	return []tuiHint{{"g/G", "top/bot"}, {"↑↓/PgUp/PgDn", "scroll"}}
}

func (v *artifactDiffView) view(width, height int) string {
	head := tuiBold.Render("--- ") + v.relA + "\n" + tuiBold.Render("+++ ") + v.relB
	if len(v.lines) == 0 {
		return head + "\n" + tuiDim.Render("(no differences)")
	}
	v.pager.ensureSize(width, max(height-3, 3))
	v.pager.setContent(colorizeDiff(v.lines))
	return head + "\n" + v.pager.view()
}

// colorizeDiff applies minimal red/green styling to unified diff lines.
func colorizeDiff(lines []string) string {
	var buf bytes.Buffer
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "@@"):
			buf.WriteString(tuiCyan.Render(l))
		case strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++"):
			buf.WriteString(tuiGreen.Render(l))
		case strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---"):
			buf.WriteString(tuiRed.Render(l))
		default:
			buf.WriteString(l)
		}
		if i < len(lines)-1 {
			buf.WriteByte('\n')
		}
	}
	return buf.String()
}
