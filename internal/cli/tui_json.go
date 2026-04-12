package cli

import (
	"bytes"
	"encoding/json"
	"strings"
)

// prettyJSON returns a 2-space-indented, ANSI-colored rendering of a raw
// JSON payload. Every line is prefixed with `indent` (so the caller controls
// nesting under a heading). If the payload doesn't parse as JSON the raw
// bytes are returned unchanged, prefixed by `indent`, so the user still
// sees *something* rather than a silent failure.
//
// Color scheme:
//   - object keys      → cyan
//   - string values    → green
//   - numbers          → yellow
//   - true/false/null  → magenta
//   - punctuation / ws → uncolored
func prettyJSON(raw []byte, indent string) string {
	if len(raw) == 0 {
		return indent + tuiDim.Render("(empty)")
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		// Not valid JSON — render it verbatim so the user can still see the
		// payload instead of a silent error.
		return indent + string(raw)
	}
	colored := colorizeJSON(buf.String())
	if indent == "" {
		return colored
	}
	return indent + strings.ReplaceAll(colored, "\n", "\n"+indent)
}

// colorizeJSON walks an already-indented JSON string and wraps each token in
// the appropriate ANSI style. It is deliberately a tiny hand-rolled scanner
// rather than a regex pipeline so multi-pass double-styling is impossible.
//
// The caller is expected to pass well-formed, json.Indent-produced input;
// for malformed input the function still terminates (it just copies bytes
// through) but the styling may be inconsistent.
func colorizeJSON(s string) string {
	var out strings.Builder
	out.Grow(len(s) + 32)
	i := 0
	n := len(s)
	for i < n {
		c := s[i]
		switch {
		case c == '"':
			// Scan to the matching closing quote, honoring backslash escapes.
			end := i + 1
			for end < n {
				if s[end] == '\\' && end+1 < n {
					end += 2
					continue
				}
				if s[end] == '"' {
					break
				}
				end++
			}
			if end >= n {
				// Unterminated string — copy the rest through and bail.
				out.WriteString(s[i:])
				return out.String()
			}
			end++ // step past closing quote
			token := s[i:end]
			// A JSON key is a string token immediately followed (after any
			// ASCII whitespace) by ':'. Anything else is a value.
			j := end
			for j < n && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			if j < n && s[j] == ':' {
				out.WriteString(tuiCyan.Render(token))
			} else {
				out.WriteString(tuiGreen.Render(token))
			}
			i = end
		case c == '-' || (c >= '0' && c <= '9'):
			end := i
			if c == '-' {
				end++
			}
			for end < n {
				d := s[end]
				if (d >= '0' && d <= '9') || d == '.' || d == 'e' || d == 'E' || d == '+' || d == '-' {
					end++
					continue
				}
				break
			}
			out.WriteString(tuiYellow.Render(s[i:end]))
			i = end
		case c == 't' && strings.HasPrefix(s[i:], "true"):
			out.WriteString(tuiMag.Render("true"))
			i += 4
		case c == 'f' && strings.HasPrefix(s[i:], "false"):
			out.WriteString(tuiMag.Render("false"))
			i += 5
		case c == 'n' && strings.HasPrefix(s[i:], "null"):
			out.WriteString(tuiMag.Render("null"))
			i += 4
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}
