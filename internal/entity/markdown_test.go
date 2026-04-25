package entity_test

import (
	"github.com/bytter/autoresearch/internal/entity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("frontmatter markdown", func() {
	type doc struct {
		Name string   `yaml:"name"`
		Tags []string `yaml:"tags"`
	}

	It("round-trips YAML frontmatter and markdown body", func() {
		in := doc{Name: "hello", Tags: []string{"a", "b"}}
		data, err := entity.WriteFrontmatter(in, "# Notes\n\nhello world\n")
		Expect(err).NotTo(HaveOccurred())

		yb, body, err := entity.ParseFrontmatter(data)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(yb)).To(ContainSubstring("name: hello"))
		Expect(string(body)).To(ContainSubstring("hello world"))
	})

	It("rejects documents without frontmatter", func() {
		_, _, err := entity.ParseFrontmatter([]byte("# just a heading\n"))
		Expect(err).To(MatchError(entity.ErrNoFrontmatter))
	})
})

var _ = Describe("ExtractSection", func() {
	It("returns the named markdown section body", func() {
		body := "# Steering\n\nFocus on dsp_fir.\n\n# Notes\n\nsomething else\n"
		Expect(entity.ExtractSection(body, "Steering")).To(Equal("Focus on dsp_fir."))
	})
})

var _ = Describe("AppendMarkdownSection", func() {
	DescribeTable("appends a top-level markdown section",
		func(body, title, content, want string) {
			got := entity.AppendMarkdownSection(body, title, content)
			Expect(got).To(Equal(want))
		},
		Entry("to an empty body",
			"", "Rationale", "cache-friendly stride",
			"# Rationale\n\ncache-friendly stride\n",
		),
		Entry("after existing content",
			"# Design notes\n\nhost tier only\n", "Implementation notes", "unrolled by 4",
			"# Design notes\n\nhost tier only\n\n# Implementation notes\n\nunrolled by 4\n",
		),
		Entry("without merging duplicate headings",
			"# Notes\n\nfirst pass\n", "Notes", "second pass",
			"# Notes\n\nfirst pass\n\n# Notes\n\nsecond pass\n",
		),
		Entry("as a no-op when content is blank",
			"# Rationale\n\nwhy\n", "Notes", "   ",
			"# Rationale\n\nwhy\n",
		),
		Entry("after normalizing trailing newlines",
			"# Notes\n\nexisting\n\n\n\n", "Rationale", "new",
			"# Notes\n\nexisting\n\n# Rationale\n\nnew\n",
		),
	)
})
