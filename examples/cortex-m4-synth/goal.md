---
objective:
  instrument: timing
  target: dsp_fir
  direction: decrease
completion:
  threshold: 0.20
  on_threshold: ask_human
constraints:
  - instrument: binary_size
    max: 131072
  - instrument: test
    require: pass
  - instrument: compile
    require: pass
---

# Steering

The FIR filter in `src/dsp_fir.c` is a naive direct-form implementation
with `FIR_NTAPS=32` and `FIR_NSAMPLES=1024`, both compile-time constants.

Known optimization opportunities:

- Loop unrolling of the inner tap loop (NTAPS is constant, so 4× or 8×
  unrolling is trivial for the compiler once hinted)
- Strength reduction of the `in[i+k]` address calculation
- Accumulator kept in a register; avoid spill pressure
- Byte-packed / pair loads if the target has native 64-bit multiply-add

Hard rules:

- Do not rewrite in assembly. Portable C only.
- Do not change the test vectors or the output format.
- `test` must remain PASS on every candidate.

Budget: no more than 20 experiments on this hypothesis tree before asking
for human re-steering.
