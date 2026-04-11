#ifndef DSP_FIR_H
#define DSP_FIR_H

#include <stdint.h>

#define FIR_NTAPS    32
#define FIR_NSAMPLES 1024
#define FIR_NOUT     (FIR_NSAMPLES - FIR_NTAPS + 1)

/*
 * Direct-form FIR filter, Q16 fixed-point.
 *
 *   out[i] = (sum_{k=0..NTAPS-1} in[i+k] * taps[k]) >> 16
 *
 * This baseline is deliberately naive: no unrolling, no block-load, no
 * strength reduction of the inner loop. The point is to give autoresearch
 * something with real, measurable optimization headroom.
 */
void dsp_fir(const int32_t *in, const int32_t *taps, int32_t *out);

#endif
