package integration

import "strings"

// CodexDocRelPath is the Codex reference path written under the target
// project.
const CodexDocRelPath = ".codex/autoresearch.md"

// CodexDoc returns the Codex-facing reference document.
func CodexDoc(moduleVersion string) string {
	doc := ClaudeDoc(moduleVersion)
	doc = strings.ReplaceAll(doc, "autoresearch claude install", "autoresearch codex install")
	doc = strings.ReplaceAll(doc, ".claude/autoresearch.md", ".codex/autoresearch.md")
	doc = strings.ReplaceAll(doc, "@.codex/autoresearch.md", ".codex/autoresearch.md")
	doc = strings.ReplaceAll(doc, "CLAUDE.md", "AGENTS.md")
	doc = strings.ReplaceAll(
		doc,
		"  Edits will be overwritten. Add your own project notes to AGENTS.md instead\n  and, if you want this reference loaded into your main session, put\n  `.codex/autoresearch.md` in your AGENTS.md.\n",
		"  Edits will be overwritten. `autoresearch codex install` also maintains a\n  managed block in AGENTS.md that points Codex at `.codex/autoresearch.md`\n  while preserving any user-owned AGENTS.md content outside that block.\n",
	)
	doc = strings.ReplaceAll(doc, "treats Claude as an agentic researcher", "treats Codex as an agentic researcher")
	return doc
}
