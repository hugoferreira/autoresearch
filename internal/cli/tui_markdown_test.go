package cli

import (
	"reflect"
	"strings"

	"github.com/bytter/autoresearch/internal/testkit"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// forceColorProfile forces lipgloss to emit ANSI escapes in tests so we can
// assert on the exact wrapper output highlightEntityIDs produces. The test
// process would otherwise see TERM=dumb / non-TTY and silently strip colors,
// turning tuiYellow.Render(x) into the identity function.
func forceColorProfile(t testkit.T) {
	t.Helper()
	lipgloss.SetColorProfile(termenv.ANSI256)
}

var _ = testkit.Spec("TestEntityIDPattern", func(t testkit.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "all six prefixes",
			in:   "see G-0001 H-0002 E-0003 O-0004 C-0005 L-0006 for details",
			want: []string{"G-0001", "H-0002", "E-0003", "O-0004", "C-0005", "L-0006"},
		},
		{
			name: "at start of string",
			in:   "L-0001 is the lesson",
			want: []string{"L-0001"},
		},
		{
			name: "at start of line after newline",
			in:   "intro\nH-0001 paragraph two",
			want: []string{"H-0001"},
		},
		{
			name: "five-digit IDs still match",
			in:   "refers to H-12345 which is fine",
			want: []string{"H-12345"},
		},
		{
			name: "too-short IDs are skipped",
			in:   "H-12 and H-123 are not entities",
			want: nil,
		},
		{
			name: "wrong prefix letters are skipped",
			in:   "X-0001 and Z-0002 look similar",
			want: nil,
		},
		{
			name: "at line boundaries",
			in:   "H-0042\nE-0099",
			want: []string{"H-0042", "E-0099"},
		},
		{
			name: "followed by punctuation",
			in:   "Per H-0001, see E-0002; also O-0003.",
			want: []string{"H-0001", "E-0002", "O-0003"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t testkit.T) {
			got := entityIDPattern.FindAllString(tc.in, -1)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
})

var _ = testkit.Spec("TestHighlightEntityIDs_WrapsIDs", func(t testkit.T) {
	forceColorProfile(t)
	in := "see L-0001 and H-0002 please"
	out := highlightEntityIDs(in)
	for _, id := range []string{"L-0001", "H-0002"} {
		wrapped := tuiYellow.Render(id)
		if !strings.Contains(out, wrapped) {
			t.Errorf("missing %s highlight in %q", id, out)
		}
	}
})

var _ = testkit.Spec("TestHighlightEntityIDs_StartOfStringAndLine", func(t testkit.T) {
	forceColorProfile(t)
	cases := []string{
		"L-0001 is the lesson",             // start of string
		"intro\nH-0002 second paragraph",   // start of line after newline
		"first\n\nL-0003 after blank line", // blank-line-separated paragraph
	}
	for _, in := range cases {
		out := highlightEntityIDs(in)
		// Find any wrapped ID.
		wrapped := false
		for _, id := range []string{"L-0001", "H-0002", "L-0003"} {
			if strings.Contains(out, tuiYellow.Render(id)) {
				wrapped = true
				break
			}
		}
		if !wrapped {
			t.Errorf("no ID highlighted in %q", in)
		}
	}
})

var _ = testkit.Spec("TestHighlightEntityIDs_AfterGlamourANSI", func(t testkit.T) {
	forceColorProfile(t)
	// Glamour wraps paragraphs in ANSI CSI sequences. The first char of the
	// prose sits right after an escape ending in "m" — which \b treats as a
	// word char, so a naive pattern wouldn't match. Simulate that.
	in := "\x1b[38;5;252mL-0001 opens the paragraph\x1b[0m"
	out := highlightEntityIDs(in)
	if !strings.Contains(out, tuiYellow.Render("L-0001")) {
		t.Errorf("ID immediately after ANSI escape not highlighted: %q", out)
	}
})

var _ = testkit.Spec("TestHighlightEntityIDs_PreservesNonMatches", func(t testkit.T) {
	forceColorProfile(t)
	in := "prose with H-12 and X-0001 neither of which should change"
	out := highlightEntityIDs(in)
	if out != in {
		t.Errorf("non-matching input mutated:\n got: %q\nwant: %q", out, in)
	}
})

var _ = testkit.Spec("TestHighlightEntityIDs_SkipsInsideHyphenatedWord", func(t testkit.T) {
	forceColorProfile(t)
	// "file-H-0001-foo" must NOT highlight — the preceding "-" is a boundary
	// we deliberately reject, since an ID embedded in a slug isn't a real
	// reference.
	in := "report file-H-0001-foo.txt stays intact"
	out := highlightEntityIDs(in)
	if strings.Contains(out, tuiYellow.Render("H-0001")) {
		t.Errorf("unexpectedly highlighted ID inside hyphenated word: %q", out)
	}
})
