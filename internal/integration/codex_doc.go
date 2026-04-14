package integration

import "strings"

// CodexDocRelPath is the Codex reference path written under the target
// project.
const CodexDocRelPath = ".codex/autoresearch.md"

// CodexDoc returns the Codex-facing reference document.
func CodexDoc(moduleVersion string) string {
	doc := ClaudeDoc(moduleVersion)
	doc = strings.ReplaceAll(doc, "autoresearch install claude", "autoresearch install codex")
	doc = strings.ReplaceAll(doc, "@.claude/autoresearch.md", ".codex/autoresearch.md")
	doc = strings.ReplaceAll(doc, ".claude/autoresearch.md", ".codex/autoresearch.md")
	doc = strings.ReplaceAll(doc, "CLAUDE.md", "AGENTS.md")
	doc = strings.ReplaceAll(
		doc,
		"  Edits will be overwritten. Add your own project notes to AGENTS.md instead\n  and, if you want this reference loaded into your main session, put\n  `.codex/autoresearch.md` in your AGENTS.md.\n",
		"  Edits will be overwritten. `autoresearch install codex` also maintains a\n  managed block in AGENTS.md that points Codex at `.codex/autoresearch.md`\n  while preserving any user-owned AGENTS.md content outside that block.\n",
	)
	doc = strings.ReplaceAll(doc, "treats Claude as an agentic researcher", "treats Codex as an agentic researcher")
	// Catch-all: any remaining bare ".claude/" references (e.g. the file
	// layout section that describes `<project>/.claude/` and the prose
	// sentence about shared team infra) get flipped to ".codex/". Order
	// matters — this must run AFTER the specific ".claude/autoresearch.md"
	// replacement above, which has already handled its own form.
	doc = strings.ReplaceAll(doc, ".claude/", ".codex/")
	doc = strings.ReplaceAll(doc, ".codex/agents/research-orchestrator.md", ".codex/agents/research-orchestrator.toml")
	doc = strings.ReplaceAll(doc, ".codex/agents/research-gate-reviewer.md", ".codex/agents/research-gate-reviewer.toml")
	doc = strings.ReplaceAll(doc, "the two prompts under\n`.codex/agents/`", "the two custom-agent configs under\n`.codex/agents/`")
	doc = strings.ReplaceAll(doc, "The subagent briefs under\n`.codex/agents/research-orchestrator.toml` has a full worked\nexample.", "The custom agent at\n`.codex/agents/research-orchestrator.toml` has a full worked\nexample.")
	doc = strings.ReplaceAll(doc, "agents/research-orchestrator.toml   — full hypothesis cycle prompt, managed", "agents/research-orchestrator.toml   — full hypothesis cycle custom agent, managed")
	doc = strings.ReplaceAll(doc, "agents/research-gate-reviewer.toml  — independent adversarial reviewer, managed", "agents/research-gate-reviewer.toml  — independent adversarial reviewer custom agent, managed")
	return doc
}
