#!/bin/sh
#
# bootstrap.sh — wire a fresh copy of this example into autoresearch.
#
# Usage:
#   cp -r examples/cortex-m4-synth /tmp/my-fir
#   cd /tmp/my-fir
#   ./bootstrap.sh
#
# This script assumes you already have `autoresearch` on your PATH. Set
# AR=/path/to/autoresearch to override.
#
set -e

AR="${AR:-autoresearch}"

if [ ! -f src/dsp_fir.c ] || [ ! -f Makefile ]; then
    echo "bootstrap.sh: run this inside a copy of the cortex-m4-synth example." >&2
    exit 1
fi

# Resolve the binary: if $AR is an executable path use it directly,
# otherwise make sure it's on $PATH. Claude Code subagents spawn Bash
# which looks up executables in $PATH, so "it works for the bootstrap"
# is not sufficient — it must also resolve for the main session.
if ! command -v "$AR" >/dev/null 2>&1 && [ ! -x "$AR" ]; then
    cat >&2 <<'EOF'
bootstrap.sh: cannot find `autoresearch` on $PATH.

Install it from the autoresearch source tree:

    cd /path/to/autoresearch && make install

That runs `go install ./cmd/autoresearch`, dropping the binary in
$GOPATH/bin. Make sure $GOPATH/bin is on your $PATH — that's where
the research subagents will look for the binary when the main Claude
Code session invokes them.

Alternatively, pass AR=/absolute/path/to/autoresearch:

    AR=/path/to/autoresearch ./bootstrap.sh

but note: the Claude Code subagents won't see that env var, so they
will still need `autoresearch` on $PATH at run time.
EOF
    exit 1
fi

if [ ! -d .git ]; then
    echo "=> initializing git repo"
    git init --initial-branch=main -q
    git add .
    git -c user.email=bootstrap@autoresearch.local \
        -c user.name=bootstrap \
        -c commit.gpgsign=false \
        commit -qm "cortex-m4-synth initial import"
fi

echo "=> autoresearch init"
"$AR" init --build-cmd "make all" --test-cmd "make test"

echo "=> registering instruments"
"$AR" instrument register host_compile \
    --cmd make,all \
    --parser builtin:passfail \
    --unit pass --tier host

"$AR" instrument register host_test \
    --cmd make,test \
    --parser builtin:passfail \
    --unit pass --tier host

"$AR" instrument register host_timing \
    --cmd build/main \
    --parser builtin:timing \
    --unit seconds --tier host --min-samples 8

"$AR" instrument register size_flash \
    --cmd size,build/main \
    --parser builtin:size \
    --unit bytes --tier host

if [ -x ./fake_qemu.sh ]; then
    # qemu_cycles is a user-chosen name; the PARSER is the generic
    # builtin:scalar which just runs a command N times and captures an
    # integer via a regex. Swap the cmd for a real `qemu-system-arm ...`
    # invocation when you have the toolchain; the pattern stays the same
    # as long as your firmware still prints `cycles: N` via semihosting.
    "$AR" instrument register qemu_cycles \
        --cmd ./fake_qemu.sh \
        --parser builtin:scalar \
        --pattern 'cycles:\s*(\d+)' \
        --unit cycles --tier qemu --min-samples 3
fi

echo "=> loading goal.md"
"$AR" goal set --file goal.md

cat <<'EOF'

Bootstrap complete. Try:

  autoresearch hypothesis add \
      --claim "unrolling dsp_fir inner loop 4x reduces runtime by >= 15%" \
      --predicts-instrument host_timing \
      --predicts-target dsp_fir \
      --predicts-direction decrease \
      --predicts-min-effect 0.15 \
      --kill-if "host_test fails" \
      --author human:you

  autoresearch experiment design H-0001 --tier host \
      --instruments host_compile,host_test,host_timing,size_flash

  autoresearch experiment implement E-0001
  # edit src/dsp_fir.c inside $(autoresearch experiment worktree E-0001)
  # commit your change on the experiment branch

  autoresearch observe E-0001 --instrument host_compile
  autoresearch observe E-0001 --instrument host_test
  autoresearch observe E-0001 --instrument host_timing --samples 12
  autoresearch observe E-0001 --instrument size_flash

  autoresearch analyze  E-0001 --baseline <baseline-E-id> --instrument host_timing
  autoresearch conclude H-0001 --verdict supported --observations <O-id>

EOF
