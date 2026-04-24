package integration_test

import (
	"os"
	"path/filepath"

	"github.com/bytter/autoresearch/internal/integration"
	"github.com/bytter/autoresearch/internal/testkit"
)

var _ = testkit.Spec("TestEnsureGitignoreLine_Created", func(t testkit.T) {
	dir := t.TempDir()
	r, err := integration.EnsureGitignoreLine(dir, ".research/")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Created || r.Added || r.AlreadyPresent {
		t.Errorf("expected Created, got %+v", r)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != ".research/\n" {
		t.Errorf("file contents: %q", string(b))
	}
})

var _ = testkit.Spec("TestEnsureGitignoreLine_AppendWithTrailingNewline", func(t testkit.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	const pre = "node_modules/\n*.log\n"
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := integration.EnsureGitignoreLine(dir, ".research/")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Added {
		t.Errorf("expected Added, got %+v", r)
	}
	b, _ := os.ReadFile(path)
	want := pre + ".research/\n"
	if string(b) != want {
		t.Errorf("contents: got %q want %q", string(b), want)
	}
})

var _ = testkit.Spec("TestEnsureGitignoreLine_AppendWithoutTrailingNewline", func(t testkit.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	const pre = "node_modules/"
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := integration.EnsureGitignoreLine(dir, ".research/")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Added {
		t.Errorf("expected Added, got %+v", r)
	}
	b, _ := os.ReadFile(path)
	want := "node_modules/\n.research/\n"
	if string(b) != want {
		t.Errorf("contents: got %q want %q", string(b), want)
	}
})

var _ = testkit.Spec("TestEnsureGitignoreLine_AlreadyPresentMiddleOfFile", func(t testkit.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	const pre = "node_modules/\n.research/\n*.log\n"
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := integration.EnsureGitignoreLine(dir, ".research/")
	if err != nil {
		t.Fatal(err)
	}
	if !r.AlreadyPresent {
		t.Errorf("expected AlreadyPresent, got %+v", r)
	}
	b, _ := os.ReadFile(path)
	if string(b) != pre {
		t.Errorf("file mutated unexpectedly: %q", string(b))
	}
})

var _ = testkit.Spec("TestEnsureGitignoreLine_AlreadyPresentWithWhitespace", func(t testkit.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	const pre = "  .research/  \n"
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := integration.EnsureGitignoreLine(dir, ".research/")
	if err != nil {
		t.Fatal(err)
	}
	if !r.AlreadyPresent {
		t.Errorf("whitespace-trimmed match should be recognized, got %+v", r)
	}
})

var _ = testkit.Spec("TestPreviewGitignoreLine", func(t testkit.T) {
	dir := t.TempDir()
	// Absent → Created
	r, err := integration.PreviewGitignoreLine(dir, ".research/")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Created {
		t.Errorf("absent: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err == nil {
		t.Error("preview should not create")
	}

	// Present → AlreadyPresent
	_ = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".research/\n"), 0o644)
	r, _ = integration.PreviewGitignoreLine(dir, ".research/")
	if !r.AlreadyPresent {
		t.Errorf("present: %+v", r)
	}

	// Exists without line → Added
	_ = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o644)
	r, _ = integration.PreviewGitignoreLine(dir, ".research/")
	if !r.Added {
		t.Errorf("missing: %+v", r)
	}
})

var _ = testkit.Spec("TestEnsureGitignoreLine_RejectsNewline", func(t testkit.T) {
	dir := t.TempDir()
	if _, err := integration.EnsureGitignoreLine(dir, "foo\nbar"); err == nil {
		t.Error("should reject lines with newlines")
	}
})
