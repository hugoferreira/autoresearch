#!/bin/sh
#
# bootstrap.sh — wire a fresh copy of this example into autoresearch.
#
# Usage:
#   cp -r examples/cortex-m4-synth /tmp/my-fir
#   cd /tmp/my-fir
#   ./bootstrap.sh
#
# Re-running this script is destructive by design: it removes any linked git
# worktrees for this local copy, drops the local git history, deletes
# `.research/`, and recreates the repo from a fresh initial-import commit.
#
# This script assumes you already have `autoresearch` on your PATH. Set
# AR=/path/to/autoresearch to override.
#
set -e

AR="${AR:-autoresearch}"
ROOT="$(pwd -P)"

if [ ! -f src/dsp_fir.c ] || [ ! -f Makefile ]; then
    echo "bootstrap.sh: run this inside a copy of the cortex-m4-synth example." >&2
    exit 1
fi

# Resolve the binary: if $AR is an executable path use it directly,
# otherwise make sure it's on $PATH. The main Claude/Codex session and
# its delegated subagents spawn Bash which looks up executables in
# $PATH, so "it works for the bootstrap" is not sufficient — it must
# also resolve for the actual research loop.
if ! command -v "$AR" >/dev/null 2>&1 && [ ! -x "$AR" ]; then
    cat >&2 <<'EOF'
bootstrap.sh: cannot find `autoresearch` on $PATH.

Install it from the autoresearch source tree:

    cd /path/to/autoresearch && make install

That runs `go install ./cmd/autoresearch`, dropping the binary in
$GOPATH/bin. Make sure $GOPATH/bin is on your $PATH — that's where
the research agents will look for the binary when the main Claude Code
or Codex session invokes them.

Alternatively, pass AR=/absolute/path/to/autoresearch:

    AR=/path/to/autoresearch ./bootstrap.sh

but note: delegated agents won't see that env var, so they will still
need `autoresearch` on $PATH at run time.
EOF
    exit 1
fi

if [ -e .git ]; then
    echo "=> removing linked git worktrees"
    git -C "$ROOT" worktree list --porcelain | while IFS= read -r line; do
        case "$line" in
            "worktree "*) wt=${line#worktree } ;;
            *) continue ;;
        esac
        if [ "$wt" = "$ROOT" ]; then
            continue
        fi
        echo "   removing $wt"
        if [ -e "$wt" ]; then
            git -C "$ROOT" worktree remove --force "$wt"
            if [ -e "$wt" ]; then
                rm -rf "$wt"
            fi
        fi
    done
    git -C "$ROOT" worktree prune --expire now >/dev/null 2>&1 || true

    echo "=> resetting local example copy"
    if git -C "$ROOT" rev-parse --verify HEAD >/dev/null 2>&1; then
        git -C "$ROOT" reset --hard -q HEAD
    fi
    git -C "$ROOT" clean -fdx -q

    echo "=> removing local git history"
    rm -rf .git
fi

echo "=> removing previous .research state"
rm -rf .research

echo "=> initializing git repo"
git init --initial-branch=main -q
git add .
git -c user.email=bootstrap@autoresearch.local \
    -c user.name=bootstrap \
    -c commit.gpgsign=false \
    commit -qm "cortex-m4-synth initial import"

echo "=> autoresearch init"
"$AR" init --build-cmd "make all" --test-cmd "make test"

echo "=> registering instruments"
"$AR" instrument register compile \
    --cmd make,all \
    --parser builtin:passfail \
    --unit pass

"$AR" instrument register test \
    --cmd make,test \
    --parser builtin:passfail \
    --unit pass \
    --requires compile=pass

"$AR" instrument register timing \
    --cmd build/main \
    --parser builtin:timing \
    --unit seconds --min-samples 8 \
    --requires test=pass

"$AR" instrument register binary_size \
    --cmd size,build/main \
    --parser builtin:size \
    --unit bytes \
    --requires compile=pass

if command -v arm-none-eabi-gcc >/dev/null 2>&1 && command -v qemu-system-arm >/dev/null 2>&1; then
    echo "=> building firmware"
    make firmware

    "$AR" instrument register qemu_cycles \
        --cmd sh,-lc,qemu-system-arm\ -machine\ mps2-an386\ -kernel\ build/firmware.elf\ -icount\ shift=0\ -nographic\ -semihosting-config\ enable=on\ -semihosting-config\ target=native \
        --parser builtin:scalar \
        --pattern 'cycles:\s*(\d+)' \
        --unit cycles --min-samples 3 \
        --requires test=pass
else
    echo "=> skipping qemu_cycles (install arm-none-eabi-gcc and qemu-system-arm to enable)"
fi

echo "=> loading goal.md"
"$AR" goal set --file goal.md

echo "=> setting budget"
"$AR" budget set --max-experiments 20

cat <<'EOF'

Bootstrap complete.

Human workflow from here:

  Terminal 1 (observe only):
    autoresearch dashboard --refresh 2

  Terminal 2 (optional, observe only):
    autoresearch log --follow

  Main Claude Code or Codex session, opened in this directory:
    "Read the local autoresearch docs for this project. Use autoresearch as
     the only writer of research state, and start the research loop for the
     current goal. You may delegate to the installed research-orchestrator
     and research-gate-reviewer subagents whenever delegation or
     independent gate review is needed. I will observe via the dashboard."

If you want a narrower first step, ask the main agent:

  "Start by proposing 2 falsifiable hypotheses for the current goal and
   record them through autoresearch. Then recommend which one to pursue
   first."

EOF
