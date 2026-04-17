# CLAUDE.md

Guidance for Claude Code working **on** the `autoresearch` repo itself.

> Not to be confused with `.claude/autoresearch.md` / `.codex/autoresearch.md`,
> which `autoresearch install claude` / `autoresearch install codex` write into
> *target* projects to teach an agent how to **use** the CLI. This file is
> about hacking on the CLI's own source.

## What this project is

A Go CLI (`github.com/bytter/autoresearch`) that drives Claude Code or Codex
through a disciplined optimization research loop on a target codebase. Read
`README.md` for the user-facing summary; the rest of this file is about the
internals.

Two load-bearing invariants — both have to survive every change:

1. **The CLI is the single writer of `.research/` state.** Subagents never
   edit files in `.research/` directly; they shell out to `autoresearch
   <verb>`. Any new feature has to preserve that.
2. **The human's interface is the main agent session; `.research/` is the
   durable substrate.** Humans steer by talking to the agent, which translates
   intent into CLI calls. Read-only views (dashboard, log, tree, frontier,
   report) are *windows* onto state — they are never a steering surface. Don't
   add interactive keystroke handling, pause-from-dashboard, or "quick edit"
   verbs to anything in the read-only set.

## Build / test

```sh
make install   # go install ./cmd/autoresearch
make build     # local ./autoresearch
make test      # go vet ./... && go test ./...
make vet
make tidy
```

Go 1.26.2. Always run `make test` before declaring a change done. There is no
separate lint step beyond `go vet`.

## Layout

```
cmd/autoresearch/main.go     entry point; maps exit codes (0/1/2/3/4)
internal/cli/                cobra commands, one file per group + tui_*.go
internal/entity/             domain types (Goal, Hypothesis, Experiment, …)
internal/store/              .research/ filesystem store, atomic writes
internal/firewall/           strict-mode validators + dependency gates + budget
internal/instrument/         runner + four built-in parsers
internal/stats/              gonum-backed BCa bootstrap, Mann–Whitney U
internal/worktree/           git worktree shell-outs
internal/integration/        .claude/.codex docs + prompts/briefs + settings/gitignore
internal/output/             JSON/text rendering helpers
examples/cortex-m4-synth/    end-to-end FIR optimization example
```

Each cobra group lives in its own `internal/cli/<group>.go` and exposes a
`<group>Commands() []*cobra.Command` constructor wired into `Root()` in
`internal/cli/root.go`. New verbs go in the matching group file; new groups
get a new file plus an entry in the `groups` slice in `Root()`.

## Conventions

- **Persistent flags** (`--json`, `-C/--project-dir`, `--dry-run`) are defined
  on root and consumed via the package-level globals `globalJSON`,
  `globalProjectDir`, `globalDryRun`. Don't redeclare them per command.
- **Read vs. write verbs.** Mutating verbs must check the pause flag via the
  store and return `ErrPaused` (exit 3) when paused. Read-only verbs
  (`status`, `log`, `tree`, `frontier`, `report`, `artifact <read>`,
  `conclusion show`, `dashboard`, `*-list`, `*-show`) work even when paused.
- **Output.** Every verb supports `--json`. Use the `output` package; don't
  hand-roll JSON. Text output is for humans; JSON is the agent contract and
  must stay stable across patches.
- **Errors.** Sentinel errors live in `internal/cli/errors.go`. `ErrPaused`
  → exit 3, `ErrBudgetExhausted` → exit 4. Don't invent new exit codes
  without updating `cmd/autoresearch/main.go` and the README/agent doc.
- **Entity IDs** are `H-NNNN`, `E-NNNN`, `O-NNNN`, `C-NNNN`, allocated
  monotonically by the store. Never mint IDs anywhere else.
- **Atomic writes.** All `.research/` writes go through the store, which
  writes to a temp file and renames. Don't add direct `os.WriteFile` calls
  to state files.
- **Event log.** Every mutation appends to `.research/events.jsonl` with
  `{kind, actor, subject, ts, data}`. New mutating verbs must emit an event;
  the dashboard, `log`, and `report` all read from it. The shape of `data`
  follows the **Event payload rule** (below).
- **Validation lives in `internal/firewall/`.** Don't scatter `if
  goal.Objective == "" { … }` checks through CLI handlers — add a validator
  and call it.
- **Statistics.** Use `internal/stats`. Default 2000 bootstrap iterations,
  seeded PRNG for reproducibility. Don't import gonum directly from CLI
  handlers.

## Event payload rule

Events log semantic transitions, not state snapshots. A good `data` field
captures what the reader can't recover from the subject's entity file
later. Always include `kind`, `subject`, `actor`, `ts`, and `reason`
(when the verb takes one). Additionally:

- **When the event transitions a status field on the subject, record
  both `from` and `to` explicitly.** Don't rely on the event `kind` alone
  to imply the new state — make the log self-describing. Example:
  `hypothesis.kill` emits `{from: "open", to: "killed", reason: "…"}`.
- **Include time-sensitive context** that gets clobbered or recomputed
  (e.g. `conclusion.write` captures `delta_frac` / CI bounds at the
  moment of conclusion; `experiment.reset` captures the soon-to-be-
  renamed branch).
- **Include cross-references** the reader can't discover from the
  subject alone (e.g. `observations[]` / `baseline` / `candidate` on
  `conclusion.write`; `inspired_by` on `hypothesis.add`).
- **Don't include** high-volume data (`per_sample` arrays), prose beyond
  a ≤ 200-character snippet, whole entity structs, or anything
  reconstructible from the subject's entity file (claim body, full
  experiment budget, etc.). Spell out individual fields deliberately —
  don't pass an entity struct as `data` and let struct growth leak in.

Events are a human-readable audit log and an advisory source for cache
invalidation. They are **not** a replay stream — reconstructing full
entity state requires reading the entity files. Consequence: when a
mutation on one entity side-effects another (e.g. `observation.record`
bumps the experiment from `implemented` to `measured`; `conclusion.write`
bumps the hypothesis to `unreviewed`), we do **not** synthesize a second
event for the side-effect. Readers that need to know what changed
consult the entity files; cache layers treat the event log as advisory
and fall back to mtime for correctness.

## The strict-mode firewall

This is the project's central design idea. Two enforcement points:

1. **Validators** (`internal/firewall/validators.go`) — `ValidateGoal`,
   `ValidateHypothesis`, `ValidateExperiment`, `CheckObservationRequest`,
   `CheckInstrumentDependencies`, `CheckBudgetForNewExperiment`. Run them at the CLI
   boundary, before touching state.
2. **Conclusion downgrade** — when `conclude` runs in strict mode, if the
   percentile-bootstrap CI on the fractional delta crosses zero in the wrong
   direction, or `|delta_frac| < hypothesis.min_effect`, the verdict is
   forcibly downgraded from `supported` to `inconclusive` and the reason is
   recorded in `Conclusion.strict`. Critic agents can additionally call
   `conclusion downgrade` to flip a verdict to `inconclusive` with a reason.

If you're tempted to weaken either path "to make a test pass" — stop and
rethink. The whole point of the tool is that supported conclusions are hard
to get.

## Instrument dependencies

Instruments may declare a `Requires` list of `"instrument=pass"` strings
at registration time (e.g. `--requires host_test=pass`). The firewall
checks these at observe time: if a required instrument has not yet been
observed with a passing result on the same experiment, `observe` refuses.
`observe --force` bypasses the gate. Ordering is expressed
per-instrument via explicit dependency edges, not as a fixed escalation
ladder.

## Worktrees

`internal/worktree/worktree.go` shells out to `git worktree` (no `go-git`
dependency, intentionally — match the user's git version). Each experiment
gets its own worktree branched off the recorded baseline SHA. `experiment
reset` archives an abandoned attempt by renaming the branch before removing
the worktree, so nothing is lost. Worktree root defaults to the user cache
dir keyed by project path hash; users override via `worktrees.root` in
`config.yaml` (e.g. for fast SSD).

The store deliberately does **not** find `.research/` when run from inside
a worktree — different tree, different store. Don't "fix" that.

## Subagent integration

`internal/integration/` owns everything that touches `.claude/`:

- `claude_doc.go` / `codex_doc.go` generate the agent-facing reference docs.
  When you add or rename a verb, update the verb table here too.
- `agents.go` embeds the two Claude prompts under `agents/`
  (`research-orchestrator.md` + `research-gate-reviewer.md`).
  `codex_agents.go` derives the Codex custom-agent configs from the same
  contracts.
- `claude_settings.go` merges a Bash allow entry into `.claude/settings.json`
  so subagents can invoke `autoresearch` without permission prompts.
- `codex_instructions.go` maintains the managed autoresearch block in the
  target project's root `AGENTS.md`.
- `gitignore.go` adds `.research/` to the project's `.gitignore`.

`autoresearch install claude`, `autoresearch install claude agents`,
`autoresearch install codex`, and `autoresearch install codex agents`
re-run these without re-running `init`.

## Dashboard and `log --follow`

These two verbs were scoped together as the M9 follow-on (the "live
dashboard" plan). The constraints are explicit and worth re-stating because
they're easy to violate accidentally:

- **Read-only.** No keystroke handling in the CLI dashboard. No pause/resume
  from the dashboard. Steering is conversational with the main session.
  This applies to `dashboard tui` too — the rendering tech changed, the
  read-only constraint did not.
- **Pure composition of existing read methods.** `captureDashboard` in
  `internal/cli/dashboard.go` calls `Store.State`, `ReadGoal`, `Config`,
  `Counts`, `ListHypotheses`, `computeFrontier`, `ListExperiments`, and
  `Events(10)`. Don't add new store methods just for the dashboard — if you
  need data the dashboard doesn't already see, check whether an existing
  read method covers it first.
- **Refresh + JSON is refused.** `dashboard --refresh N --json` returns an
  error. Streaming JSON is the job of an external polling loop.
- **`--refresh` requires a TTY.** When stdout isn't a terminal, fall through
  with the documented error rather than rendering ANSI into a pipe. For
  scripting, `dashboard --json` (one-shot) is the contract.
- **No fsnotify in `log --follow`.** 200 ms `os.Stat` polling on
  `events.jsonl`, byte-offset-tracked. Cross-platform, zero deps. Don't
  "improve" this with a file watcher.
- **Color modes are `auto|always|never`.** `auto` enables on a TTY,
  disables when piped. `always` exists specifically so `watch -c
  autoresearch dashboard --color always` works — keep that path alive.

The Bubble Tea TUI (`dashboard tui`, `internal/cli/tui_*.go`) is the
second face of the same snapshot. The CLI `dashboard` is the source of
truth for what data appears; if you extend the dashboard data model,
update both views and keep `captureDashboard` as the single capture
path. The TUI is deliberately a superset of the one-shot view — it
navigates every read-only CLI surface (hypotheses, experiments,
conclusions, event log, artifacts, report, tree, frontier, goal,
status, instruments) but never mutates `.research/`.

TUI package layout (all under `internal/cli/`):

- `tui_command.go` — `dashboard tui` cobra wiring
- `tui_app.go` — root bubbletea model, view stack, key router, chrome
- `tui_view_<entity>.go` — per-entity list + detail views
- `tui_view_dashboard.go` / `tui_view_misc.go` — dashboard home + the
  small single-screen views (tree, frontier, goal, status, instruments)
- `tui_view_artifact.go` — artifact list + viewport viewer + diff
- `tui_view_report.go` — glamour-rendered markdown report
- `tui_view_kinds.go` — stable view-kind identifiers used by `jumpTo`
  to canonicalize the breadcrumb on top-level jumps
- `tui_helpers.go` — shared line layout, list scaffolding, table
  renderers, tree helpers (reach into this instead of duplicating)
- `tui_pager.go` — `pagerState` wrapping `bubbles/viewport`, used by
  the three scrollable content viewers
- `tui_json.go` — `prettyJSON` tokenizer for colorized event payloads
- `tui_style.go` — lipgloss palette aligned with the `ansi` package

When adding a new TUI view: pick `list`/`detail`/`pager` shape, add a
`kind()` method to `tui_view_kinds.go`, and use the helpers in
`tui_helpers.go` / `tui_pager.go` instead of hand-rolling cursor or
scroll logic.

**Refresh model.** The TUI is event-driven, not tick-driven. A single
poll loop in `tui_app.go` calls `Store.EventsSince(offset)` every
`--refresh` seconds; when new events arrive, the root emits a
`storeChangedMsg{events, newOff}` and dispatches it to the top view.
Quiet polls produce no view churn at all — cursor, scroll, filter,
and pager offset are preserved whenever `.research/` hasn't changed.

Per-view contract:

- List and aggregate views (`hypothesis list`, `dashboard`, `tree`,
  `frontier`, …) handle `case storeChangedMsg` by calling
  `v.init(s)`. That's the current default; narrower relevance filters
  can be added later (e.g. a hypothesis list only reloading when
  `events[].Subject` starts with `H-`).
- Detail views do the same — reload their single entity. The entity
  cache makes the reload O(stat) when unchanged.
- Pager-backed views (lesson detail, report, artifact) must NOT call
  `pager.gotoTop()` on a reload. Reloads fire only when the entity
  actually changed; scroll belongs to the user, not the document.
  Explicit content-replacement actions (artifact Tab cycling, grep
  applied) set `scrollResetPending` on the view and the load handler
  consumes it.

Do not handle `tuiTickMsg` in views — it's internal to the root model's
poll loop. Views exclusively handle `storeChangedMsg`.

## When making changes

- **New verb.** Add to the matching `internal/cli/<group>.go`, validate via
  `internal/firewall`, persist via `internal/store`, emit an event, support
  `--json`, update the agent docs in `internal/integration/` so agents can
  discover it, add a test.
- **New instrument parser.** Add to `internal/instrument/`, register in the
  parser switch, document the `--parser` value in the `instrument register`
  help text, add a parser test with a fixture.
- **New entity field.** Update `internal/entity/`, the markdown
  serialization in `internal/store/`, any validators in `internal/firewall/`,
  and the report renderer.
- **Touching statistics.** Keep the seeded PRNG. Reproducibility of CIs
  across runs is part of the contract — agents diff `analyze` output.

## Things to avoid

- Adding a hidden flag, env var, or config key that lets agents bypass the
  firewall, the pause gate, the dependency gate, or the budget check.
- Writing to `.research/` from anywhere outside `internal/store/`.
- Importing `gonum`, `git`, or cobra-internal packages from outside their
  designated wrapper packages.
- Creating new top-level directories. The `cmd/`, `internal/`, `examples/`
  layout is deliberate.
- Adding feature-delivery verbs (`generate`, `scaffold`, `refactor`, …).
  This is an optimization tool, not a synthesis tool. If a verb doesn't fit
  the goal → hypothesis → experiment → observe → analyze → conclude loop,
  it probably doesn't belong.
