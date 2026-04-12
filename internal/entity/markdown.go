package entity

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	ErrNoFrontmatter           = errors.New("file does not start with YAML frontmatter")
	ErrUnterminatedFrontmatter = errors.New("YAML frontmatter is not terminated with ---")
)

// ParseFrontmatter splits a markdown file with YAML frontmatter into its
// YAML bytes and the body (everything after the closing ---).
func ParseFrontmatter(data []byte) (yamlBytes, body []byte, err error) {
	data = bytes.TrimPrefix(data, []byte("\ufeff"))
	if !bytes.HasPrefix(data, []byte("---\n")) && !bytes.HasPrefix(data, []byte("---\r\n")) {
		return nil, nil, ErrNoFrontmatter
	}
	skip := 4
	if bytes.HasPrefix(data, []byte("---\r\n")) {
		skip = 5
	}
	rest := data[skip:]

	end := bytes.Index(rest, []byte("\n---\n"))
	if end == -1 {
		end = bytes.Index(rest, []byte("\n---\r\n"))
	}
	if end == -1 {
		// Allow trailing --- without newline.
		if bytes.HasSuffix(rest, []byte("\n---")) {
			return rest[:len(rest)-4], nil, nil
		}
		return nil, nil, ErrUnterminatedFrontmatter
	}
	yamlBytes = rest[:end]
	// Advance past "\n---\n" (or "\n---\r\n").
	after := rest[end+1:]
	after = bytes.TrimPrefix(after, []byte("---\r\n"))
	after = bytes.TrimPrefix(after, []byte("---\n"))
	// WriteFrontmatter writes a blank line separator after the closing ---.
	// Consume it so Body round-trips exactly as the caller set it.
	after = bytes.TrimPrefix(after, []byte("\r\n"))
	after = bytes.TrimPrefix(after, []byte("\n"))
	return yamlBytes, after, nil
}

// WriteFrontmatter marshals v as YAML and joins it with body under standard ---
// delimiters. The body is written verbatim; callers control any heading inside it.
func WriteFrontmatter(v any, body string) ([]byte, error) {
	yb, err := yaml.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode yaml: %w", err)
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(yb)
	buf.WriteString("---\n\n")
	buf.WriteString(body)
	if body != "" && !strings.HasSuffix(body, "\n") {
		buf.WriteString("\n")
	}
	return buf.Bytes(), nil
}

// AppendMarkdownSection returns body with a new "# <heading>" section
// containing content appended at the end, separated by a single blank line.
// Content is TrimSpace'd; empty content is a no-op (returns body unchanged).
//
// The semantics are strict-append: if a previous section with the same heading
// already exists in body, a new one is written after it rather than merged or
// overwritten. This preserves the sequence of updates and keeps the notebook
// layer auditable — a reader sees what was added when, not just the latest
// revision.
func AppendMarkdownSection(body, heading, content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return body
	}
	section := fmt.Sprintf("# %s\n\n%s\n", heading, content)
	if body == "" {
		return section
	}
	return strings.TrimRight(body, "\n") + "\n\n" + section
}

// ExtractSection returns the contents of a top-level markdown section
// introduced by a line exactly matching "# <heading>", up to the next top-level
// heading or end of text. Returns "" if not found.
func ExtractSection(body, heading string) string {
	lines := strings.Split(body, "\n")
	want := "# " + heading
	start := -1
	for i, ln := range lines {
		if ln == want {
			start = i + 1
			break
		}
	}
	if start == -1 {
		return ""
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "# ") {
			end = i
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
}
