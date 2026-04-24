package entity_test

import (
	"bytes"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/testkit"
	"github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("TestFrontmatterRoundTrip", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		type doc struct {
			Name string   `yaml:"name"`
			Tags []string `yaml:"tags"`
		}
		in := doc{Name: "hello", Tags: []string{"a", "b"}}
		data, err := entity.WriteFrontmatter(in, "# Notes\n\nhello world\n")
		if err != nil {
			t.Fatal(err)
		}
		yb, body, err := entity.ParseFrontmatter(data)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(yb, []byte("name: hello")) {
			t.Errorf("yaml missing name field: %s", yb)
		}
		if !bytes.Contains(body, []byte("hello world")) {
			t.Errorf("body missing content: %s", body)
		}
	})
})

var _ = ginkgo.Describe("TestNoFrontmatter", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		_, _, err := entity.ParseFrontmatter([]byte("# just a heading\n"))
		if err != entity.ErrNoFrontmatter {
			t.Errorf("got %v, want ErrNoFrontmatter", err)
		}
	})
})

var _ = ginkgo.Describe("TestExtractSection", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		body := "# Steering\n\nFocus on dsp_fir.\n\n# Notes\n\nsomething else\n"
		got := entity.ExtractSection(body, "Steering")
		if got != "Focus on dsp_fir." {
			t.Errorf("steering: got %q", got)
		}
	})
})

var _ = ginkgo.Describe("TestAppendMarkdownSection", func() {
	ginkgo.It("runs", func() {
		t := testkit.NewT()

		t.Run("empty body", func(t testkit.T) {
			got := entity.AppendMarkdownSection("", "Rationale", "cache-friendly stride")
			want := "# Rationale\n\ncache-friendly stride\n"
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
		t.Run("append to existing body", func(t testkit.T) {
			body := "# Design notes\n\nhost tier only\n"
			got := entity.AppendMarkdownSection(body, "Implementation notes", "unrolled by 4")
			want := "# Design notes\n\nhost tier only\n\n# Implementation notes\n\nunrolled by 4\n"
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
		t.Run("duplicate headings append rather than merge", func(t testkit.T) {
			body := "# Notes\n\nfirst pass\n"
			got := entity.AppendMarkdownSection(body, "Notes", "second pass")
			want := "# Notes\n\nfirst pass\n\n# Notes\n\nsecond pass\n"
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
		t.Run("empty content is a no-op", func(t testkit.T) {
			body := "# Rationale\n\nwhy\n"
			got := entity.AppendMarkdownSection(body, "Notes", "   ")
			if got != body {
				t.Errorf("whitespace-only content should be a no-op, got %q", got)
			}
		})
		t.Run("trailing newlines normalized", func(t testkit.T) {
			body := "# Notes\n\nexisting\n\n\n\n"
			got := entity.AppendMarkdownSection(body, "Rationale", "new")
			want := "# Notes\n\nexisting\n\n# Rationale\n\nnew\n"
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	})
})
