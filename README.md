# autoresearch

> Autonomous, agentic research over an existing codebase.

`autoresearch` turns [Claude Code](https://claude.com/claude-code) into a
disciplined scientific researcher operating on a working codebase. It generates
falsifiable hypotheses, runs instrument-backed experiments in isolated git
worktrees, and draws statistically-sound conclusions — with a strict firewall
between speculation and observation.

It is for **optimizing existing, working systems against measurable goals**.
It is **not** a feature-delivery or program-synthesis tool. If your goal cannot
be expressed as a number an instrument can measure, autoresearch is the wrong
tool.

The human's interface is the main Claude Code session; `.research/` is the
durable substrate. You steer by talking to the agent, not by editing state
files or typing into a dashboard.

## What it does

You define a goal: an objective metric (with a direction and target effect)
plus a set of constraints (other instruments that must pass or stay within
bounds). Claude Code, driven by six embedded subagent prompts, then loops:

```
goal → hypothesis → experiment → observe → analyze → conclude → report
```

Every experiment runs in its own git worktree against the same baseline. Every
observation is content-addressed and logged. Every conclusion goes through a
strict-mode firewall — if the bootstrap CI crosses zero in the wrong direction,
or the effect is smaller than the hypothesis predicted, "supported" is
automatically downgraded to "inconclusive."

The CLI is the only writer of state. Subagents only ever invoke `autoresearch`
verbs; they never edit `.research/` directly.

## Install

```sh
make install      # go install ./cmd/autoresearch → $GOPATH/bin/autoresearch
make build        # local ./autoresearch (gitignored)
make test         # go vet + go test ./...
```

Requires Go 1.26+.

## Quickstart

```sh
cd path/to/your/project
autoresearch init --build-cmd "make all" --test-cmd "make test"
autoresearch instrument register host_timing \
    --cmd "./build/main" --parser builtin:timing --unit s --min-samples 30
autoresearch instrument register size_flash \
    --cmd "size ./build/main" --parser builtin:size --unit bytes
autoresearch goal set --file goal.md
autoresearch claude install
autoresearch claude agents install
autoresearch dashboard --refresh 2
```

Then, inside Claude Code in the same project, ask the agent to start
researching. It will read `.claude/autoresearch.md` and the
`.claude/agents/research-*.md` subagent prompts and begin the loop.

A worked example lives in [`examples/cortex-m4-synth/`](examples/cortex-m4-synth)
— optimizing a naive FIR filter against `host_timing` with `size_flash` and
test-pass constraints, capped at 20 experiments.

## Command map

All commands accept `--json` (machine-readable output), `-C/--project-dir`
(target project), and `--dry-run`. Read-only verbs work even when the project
is paused.

| Group | Verbs |
| --- | --- |
| **lifecycle** | `init`, `status`, `pause`, `resume` |
| **goal** | `goal set`, `goal show` |
| **steering** | `steering show`, `steering append`, `steering edit` |
| **hypothesis** | `add`, `list`, `show`, `promote`, `kill`, `reopen` |
| **experiment** | `design`, `implement`, `reset`, `worktree`, `list`, `show`, `promote` |
| **observe** | `observe <exp> --instrument <name>` |
| **analyze** | `analyze <exp> [--baseline <exp>]` |
| **conclude** | `conclude <hyp> --verdict ... --observations ...` |
| **conclusion** | `list`, `show`, `downgrade` (critic-only) |
| **tree / frontier** | `tree`, `frontier` |
| **log** | `log [--tail --kind --since --follow]` |
| **report** | `report <hyp>` |
| **artifact** | `list`, `stat`, `path`, `head`, `tail`, `range`, `grep`, `diff`, `show` |
| **instrument** | `list`, `register`, `run` |
| **budget** | `show`, `set` |
| **gc** | `gc` |
| **claude** | `claude install`, `claude agents install` |
| **dashboard** | `dashboard [--refresh N] [--color auto\|always\|never]`, `dashboard tui` |

Exit codes: `0` success, `1` generic error, `2` cobra usage, `3` paused,
`4` budget exhausted. The orchestrator loop uses 3/4 to decide when to stop.

## Goal format

Goals are markdown with YAML frontmatter:

```yaml
---
objective:
  instrument: host_timing
  target: dsp_fir
  direction: decrease
  target_effect: 0.20
constraints:
  - instrument: size_flash
    max: 131072
  - instrument: host_test
    require: pass
---

# Steering

Free-form notes the agent uses to guide hypothesis generation.
Hard rules go here too.
```

## Instruments

An instrument is a shell command plus a parser. Four parsers ship built-in:

| Parser | Behaviour |
| --- | --- |
| `builtin:passfail` | Run once; value = 1 if exit 0 else 0. |
| `builtin:timing`   | Run N times; mean seconds + BCa 95% bootstrap CI. |
| `builtin:size`     | Run once; first numeric column from `size`-style output. |
| `builtin:scalar`   | Run N times; extract integer via regex; per-sample + BCa CI. |

Tiers are `host` → `qemu` → `hardware`; the firewall enforces escalation order
(no qemu observations until host has run, etc.).

## Statistics

Single-sample summaries use BCa bootstrap 95% CIs (gonum, seeded for
reproducibility). Comparison against a baseline uses a percentile bootstrap on
the fractional delta plus a two-tailed Mann–Whitney U p-value. Default 2000
resamples. Strict-mode `conclude` downgrades a "supported" verdict whenever
the CI crosses zero in the wrong direction or the observed effect is smaller
than the hypothesis's declared `min_effect`.

## State layout

`autoresearch init` creates a `.research/` directory at the project root
(gitignored). Everything is plain files:

```
.research/
  config.yaml          # build/test cmds, instruments, budgets
  state.json           # pause flag, counters, started_at
  events.jsonl         # append-only event log
  goal.md              # frontmatter + steering
  hypotheses/H-NNNN.md
  experiments/E-NNNN.md
  observations/O-NNNN.md
  conclusions/C-NNNN.md
  artifacts/<sha256>/…
```

The store walks upward from the working directory the way git does for
`.git/`, so any subcommand run from inside the project finds it. Worktrees
default to the user cache dir keyed by project hash; override
`worktrees.root` in `config.yaml` to put them on a fast SSD.

## Watching the loop

The dashboard is read-only by design: a window onto the research state, never
a steering surface. Leave it running in a second tmux pane or terminal while
you drive research from the main session.

```sh
autoresearch dashboard                  # one-shot composite snapshot
autoresearch dashboard --refresh 2      # live, auto-redraws every 2s (TTY only)
autoresearch dashboard --json           # structured snapshot for tools
watch -c autoresearch dashboard --color always   # alternate way to live-poll
autoresearch log --follow               # tail events.jsonl as they arrive
```

`--refresh` requires a TTY and is rejected together with `--json` (use an
external polling loop if you want streaming JSON). `log --follow` polls
`events.jsonl` every 200 ms — no fsnotify dep, works the same over SSH.
Both verbs work while the project is paused.

## Status

Milestones M1–M9 are landed: full hypothesis → experiment → observation →
conclusion loop, strict firewall, budgets, gitignore, the example project,
and the live dashboard. The Bubble Tea TUI (`dashboard tui`,
`internal/cli/tui_*.go`) is in active development as a richer second face
on the same read-only snapshot. Concurrent multi-subagent locking is the
next major piece.

## License

TBD.
