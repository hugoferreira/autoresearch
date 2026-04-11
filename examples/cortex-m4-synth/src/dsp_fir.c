#include "dsp_fir.h"

void dsp_fir(const int32_t *in, const int32_t *taps, int32_t *out) {
    for (int i = 0; i < FIR_NOUT; i++) {
        int64_t acc = 0;
        for (int k = 0; k < FIR_NTAPS; k++) {
            acc += (int64_t)in[i + k] * (int64_t)taps[k];
        }
        out[i] = (int32_t)(acc >> 16);
    }
}
