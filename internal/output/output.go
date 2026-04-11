package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type Writer struct {
	out  io.Writer
	err  io.Writer
	json bool
}

func New(out, err io.Writer, jsonMode bool) *Writer {
	return &Writer{out: out, err: err, json: jsonMode}
}

func Default(jsonMode bool) *Writer {
	return New(os.Stdout, os.Stderr, jsonMode)
}

func (w *Writer) IsJSON() bool { return w.json }

// Raw returns the underlying output writer. Use sparingly — most code should
// go through Emit/Text/JSON so formatting stays consistent.
func (w *Writer) Raw() io.Writer { return w.out }

func (w *Writer) Textf(format string, args ...any) {
	if w.json {
		return
	}
	fmt.Fprintf(w.out, format, args...)
}

func (w *Writer) Textln(line string) {
	if w.json {
		return
	}
	fmt.Fprintln(w.out, line)
}

// Emit writes the text line in text mode or the structured payload in JSON mode.
func (w *Writer) Emit(text string, payload any) error {
	if w.json {
		enc := json.NewEncoder(w.out)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}
	if text != "" {
		fmt.Fprintln(w.out, text)
	}
	return nil
}

func (w *Writer) JSON(payload any) error {
	if !w.json {
		return nil
	}
	enc := json.NewEncoder(w.out)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}
