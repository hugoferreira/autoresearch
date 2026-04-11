#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

#include "dsp_fir.h"

/*
 * Host-side bench entry point. Runs dsp_fir() repeatedly so that wall-clock
 * time is dominated by filter work, not by process startup. Prints an
 * XOR checksum of the output so the compiler can't dead-code-eliminate
 * the body.
 */

static int32_t in_buf[FIR_NSAMPLES];
static int32_t out_buf[FIR_NSAMPLES];
static int32_t taps[FIR_NTAPS];

static void init_input(void) {
    /* Deterministic LCG so every run sees the same input. */
    uint32_t state = 0x12345678u;
    for (int i = 0; i < FIR_NSAMPLES; i++) {
        state = state * 1103515245u + 12345u;
        in_buf[i] = (int32_t)(state >> 8);
    }
    /* Triangular tap set — maximum at the center. */
    for (int i = 0; i < FIR_NTAPS; i++) {
        int t = (i < FIR_NTAPS / 2) ? (i + 1) : (FIR_NTAPS - i);
        taps[i] = t * 1024; /* scaled a bit above unity in Q16 */
    }
}

/*
 * Default iteration count is chosen so a -O0 build takes a few hundred ms per
 * run — long enough that process-startup and thermal-jitter effects are a
 * small fraction of wall time. If you register host_timing with a different
 * command line, size it so steady-state runtime >> startup overhead.
 */
#define DEFAULT_ITERATIONS 30000

int main(int argc, char **argv) {
    int iterations = (argc > 1) ? atoi(argv[1]) : DEFAULT_ITERATIONS;
    if (iterations <= 0) iterations = DEFAULT_ITERATIONS;

    init_input();

    /*
     * Non-involutive accumulator: a Fowler/Noll/Vo-style fold so repeated runs
     * don't cancel each other out (which would both give us a fake-zero result
     * AND let the compiler hollow the loop). Any agent modifying dsp_fir is
     * expected to leave this function alone.
     */
    uint64_t checksum = 0xcbf29ce484222325ull;
    for (int rep = 0; rep < iterations; rep++) {
        dsp_fir(in_buf, taps, out_buf);
        for (int i = 0; i < FIR_NOUT; i++) {
            checksum ^= (uint64_t)(uint32_t)out_buf[i];
            checksum *= 0x100000001b3ull;
        }
    }
    printf("checksum=%llx iterations=%d\n", (unsigned long long)checksum, iterations);
    return 0;
}
