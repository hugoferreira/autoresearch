package cli

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func forceColorProfile() {
	lipgloss.SetColorProfile(termenv.ANSI256)
}

var _ = Describe("entity ID markdown highlighting", func() {
	DescribeTable("matches standalone entity IDs",
		func(in string, want []string) {
			Expect(entityIDPattern.FindAllString(in, -1)).To(Equal(want))
		},
		Entry("all six prefixes", "see G-0001 H-0002 E-0003 O-0004 C-0005 L-0006 for details", []string{"G-0001", "H-0002", "E-0003", "O-0004", "C-0005", "L-0006"}),
		Entry("at start of string", "L-0001 is the lesson", []string{"L-0001"}),
		Entry("at start of line after newline", "intro\nH-0001 paragraph two", []string{"H-0001"}),
		Entry("five-digit IDs still match", "refers to H-12345 which is fine", []string{"H-12345"}),
		Entry("too-short IDs are skipped", "H-12 and H-123 are not entities", nil),
		Entry("wrong prefix letters are skipped", "X-0001 and Z-0002 look similar", nil),
		Entry("at line boundaries", "H-0042\nE-0099", []string{"H-0042", "E-0099"}),
		Entry("followed by punctuation", "Per H-0001, see E-0002; also O-0003.", []string{"H-0001", "E-0002", "O-0003"}),
	)

	It("wraps each recognized ID with the TUI highlight style", func() {
		forceColorProfile()
		out := highlightEntityIDs("see L-0001 and H-0002 please")
		Expect(out).To(ContainSubstring(tuiYellow.Render("L-0001")))
		Expect(out).To(ContainSubstring(tuiYellow.Render("H-0002")))
	})

	It("highlights IDs at string, line, and paragraph starts", func() {
		forceColorProfile()
		for _, in := range []string{
			"L-0001 is the lesson",
			"intro\nH-0002 second paragraph",
			"first\n\nL-0003 after blank line",
		} {
			out := highlightEntityIDs(in)
			Expect(out).To(SatisfyAny(
				ContainSubstring(tuiYellow.Render("L-0001")),
				ContainSubstring(tuiYellow.Render("H-0002")),
				ContainSubstring(tuiYellow.Render("L-0003")),
			))
		}
	})

	It("highlights IDs immediately after Glamour ANSI escapes", func() {
		forceColorProfile()
		out := highlightEntityIDs("\x1b[38;5;252mL-0001 opens the paragraph\x1b[0m")
		Expect(out).To(ContainSubstring(tuiYellow.Render("L-0001")))
	})

	It("preserves non-matching IDs and skips IDs embedded in hyphenated words", func() {
		forceColorProfile()
		in := "prose with H-12 and X-0001 neither of which should change"
		Expect(highlightEntityIDs(in)).To(Equal(in))

		out := highlightEntityIDs("report file-H-0001-foo.txt stays intact")
		Expect(strings.Contains(out, tuiYellow.Render("H-0001"))).To(BeFalse())
	})
})
