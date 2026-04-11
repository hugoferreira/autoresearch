#!/bin/sh
#
# fake_qemu.sh — stand-in for a real `qemu-system-arm` invocation.
#
# Reads cycles.txt from the current directory (which, under autoresearch,
# is the experiment's worktree) and prints a qemu-flavored cycle line that
# builtin:qemu_cycles parses. Adds a small deterministic jitter keyed off
# the worktree path so repeated samples don't collapse to a point mass.
#
# To replace this with a real qemu invocation, swap the body for something
# like:
#
#     make firmware.elf >/dev/null 2>&1
#     qemu-system-arm -machine mps2-an386 \
#         -kernel firmware.elf \
#         -icount shift=0 -nographic -no-reboot \
#         -semihosting-config enable=on,target=native
#
# and have your firmware print `cycles: N` via semihosting at the end of
# the benchmark. The instrument parser and smoke test stay unchanged.
#
set -e

if [ ! -f cycles.txt ]; then
    echo "fake_qemu.sh: cycles.txt not found in $(pwd)" >&2
    exit 1
fi

BASE=$(tr -dc '0-9' < cycles.txt | head -c 10)
if [ -z "$BASE" ]; then
    echo "fake_qemu.sh: cycles.txt has no digits" >&2
    exit 1
fi

# Deterministic per-sample jitter via /dev/urandom so the bootstrap has
# something to work with but the mean stays tight.
JITTER=$(od -An -N2 -tu2 /dev/urandom | tr -d ' ')
JITTER=$(( JITTER % 200 ))

echo "qemu: emulation complete (fake)"
echo "cycles: $(( BASE + JITTER ))"
