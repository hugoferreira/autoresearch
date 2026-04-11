package entity_test

import (
	"bytes"
	"testing"

	"github.com/bytter/autoresearch/internal/entity"
)

func TestFrontmatterRoundTrip(t *testing.T) {
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
}

func TestNoFrontmatter(t *testing.T) {
	_, _, err := entity.ParseFrontmatter([]byte("# just a heading\n"))
	if err != entity.ErrNoFrontmatter {
		t.Errorf("got %v, want ErrNoFrontmatter", err)
	}
}

func TestExtractSection(t *testing.T) {
	body := "# Steering\n\nFocus on dsp_fir.\n\n# Notes\n\nsomething else\n"
	got := entity.ExtractSection(body, "Steering")
	if got != "Focus on dsp_fir." {
		t.Errorf("steering: got %q", got)
	}
}
