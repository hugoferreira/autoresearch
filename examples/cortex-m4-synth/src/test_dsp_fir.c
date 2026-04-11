#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "dsp_fir.h"

/*
 * Small, self-checking test suite. Any optimization of dsp_fir() must keep
 * these tests passing — that's the host_test firewall autoresearch enforces.
 */

static int fail_count = 0;

#define CHECK(cond, msg)                                           \
    do {                                                           \
        if (!(cond)) {                                             \
            fprintf(stderr, "FAIL: %s (line %d)\n", msg, __LINE__); \
            fail_count++;                                          \
        }                                                          \
    } while (0)

static void test_impulse_slides_through_window(void) {
    /*
     * dsp_fir is a sliding-window correlator: out[i] = sum_k in[i+k] * taps[k].
     * With a single impulse at in[NTAPS-1] and unity taps, the impulse rides
     * through every tap position over the first NTAPS outputs, then leaves
     * the window entirely.
     */
    int32_t in[FIR_NSAMPLES];
    int32_t out[FIR_NSAMPLES];
    int32_t taps[FIR_NTAPS];

    memset(in, 0, sizeof(in));
    memset(out, 0x5A, sizeof(out));
    in[FIR_NTAPS - 1] = 1 << 16; /* 1.0 in Q16 */
    for (int i = 0; i < FIR_NTAPS; i++) taps[i] = 1 << 16;

    dsp_fir(in, taps, out);

    for (int i = 0; i < FIR_NTAPS; i++) {
        CHECK(out[i] == (1 << 16), "impulse passes through each tap with unity coefs");
    }
    for (int i = FIR_NTAPS; i < FIR_NOUT; i++) {
        CHECK(out[i] == 0, "output beyond NTAPS must be zero for an isolated impulse");
    }
}

static void test_zero_input(void) {
    int32_t in[FIR_NSAMPLES];
    int32_t out[FIR_NSAMPLES];
    int32_t taps[FIR_NTAPS];

    memset(in, 0, sizeof(in));
    memset(out, 0x55, sizeof(out));
    for (int i = 0; i < FIR_NTAPS; i++) taps[i] = 1 << 16;

    dsp_fir(in, taps, out);

    for (int i = 0; i < FIR_NOUT; i++) {
        CHECK(out[i] == 0, "zero input produces zero output");
    }
}

static void test_constant_input(void) {
    /* Constant input with unity taps → out[i] = input * sum(taps) >> 16. */
    int32_t in[FIR_NSAMPLES];
    int32_t out[FIR_NSAMPLES];
    int32_t taps[FIR_NTAPS];

    for (int i = 0; i < FIR_NSAMPLES; i++) in[i] = 2 << 16; /* 2.0 in Q16 */
    for (int i = 0; i < FIR_NTAPS; i++) taps[i] = 1 << 16;  /* 1.0 in Q16 */

    dsp_fir(in, taps, out);

    int64_t expected = ((int64_t)(2 << 16) * (int64_t)(1 << 16) * FIR_NTAPS) >> 16;
    for (int i = 0; i < FIR_NOUT; i++) {
        CHECK(out[i] == (int32_t)expected, "constant input gives constant output");
    }
}

int main(void) {
    test_impulse_slides_through_window();
    test_zero_input();
    test_constant_input();

    if (fail_count == 0) {
        printf("PASS (3 tests)\n");
        return 0;
    }
    fprintf(stderr, "FAILED: %d check(s)\n", fail_count);
    return 1;
}
