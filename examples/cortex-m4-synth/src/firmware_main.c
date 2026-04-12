/*
 * Firmware benchmark entry point for Cortex-M4 under QEMU semihosting.
 *
 * Uses SysTick as a cycle counter (QEMU emulates SysTick but not DWT CYCCNT).
 * With `-icount shift=0`, each instruction is deterministic, so SysTick ticks
 * are reproducible across runs.
 *
 * Prints "cycles: N" via semihosting. The autoresearch instrument "qemu_cycles"
 * parses this output with builtin:scalar.
 */

#include <stdint.h>
#include "dsp_fir.h"

/* --- SysTick registers (24-bit down-counter, all Cortex-M) --- */
#define SYST_CSR (*(volatile uint32_t *)0xE000E010)
#define SYST_RVR (*(volatile uint32_t *)0xE000E014)
#define SYST_CVR (*(volatile uint32_t *)0xE000E018)

#define SYST_ENABLE    (1u << 0)
#define SYST_CLKSOURCE (1u << 2)  /* processor clock */

/* --- ARM semihosting via BKPT 0xAB (Cortex-M convention) --- */

static inline void semi_write0(const char *s) {
    __asm__ volatile(
        "mov r1, %0\n"
        "mov r0, #0x04\n"
        "bkpt 0xAB\n"
        :: "r"(s) : "r0", "r1", "memory"
    );
}

static inline void semi_exit(int code) {
    uint32_t args[2] = { 0x20026u, (uint32_t)code }; /* ADP_Stopped_ApplicationExit */
    __asm__ volatile(
        "mov r1, %0\n"
        "mov r0, #0x18\n"
        "bkpt 0xAB\n"
        :: "r"(args) : "r0", "r1", "memory"
    );
}

static void u32_to_str(uint32_t v, char *buf) {
    char tmp[11];
    int i = 0;
    if (v == 0) { buf[0] = '0'; buf[1] = '\0'; return; }
    while (v > 0) { tmp[i++] = '0' + (char)(v % 10); v /= 10; }
    int j = 0;
    while (i > 0) buf[j++] = tmp[--i];
    buf[j] = '\0';
}

#define FIRMWARE_ITERATIONS 10

static int32_t in_buf[FIR_NSAMPLES];
static int32_t out_buf[FIR_NSAMPLES];
static int32_t taps[FIR_NTAPS];

static void init_input(void) {
    uint32_t state = 0x12345678u;
    for (int i = 0; i < FIR_NSAMPLES; i++) {
        state = state * 1103515245u + 12345u;
        in_buf[i] = (int32_t)(state >> 8);
    }
    for (int i = 0; i < FIR_NTAPS; i++) {
        int t = (i < FIR_NTAPS / 2) ? (i + 1) : (FIR_NTAPS - i);
        taps[i] = t * 1024;
    }
}

int main(void) {
    init_input();

    /* Configure SysTick: max reload (24-bit), processor clock, enable. */
    SYST_RVR = 0x00FFFFFFu;
    SYST_CVR = 0;
    SYST_CSR = SYST_ENABLE | SYST_CLKSOURCE;

    /* Read start value (SysTick counts DOWN). */
    uint32_t start = SYST_CVR;

    for (int rep = 0; rep < FIRMWARE_ITERATIONS; rep++) {
        dsp_fir(in_buf, taps, out_buf);
    }

    uint32_t end = SYST_CVR;

    /* SysTick counts down, so elapsed = start - end (mod 24-bit). */
    uint32_t elapsed = (start - end) & 0x00FFFFFFu;

    char line[32] = "cycles: ";
    u32_to_str(elapsed, line + 8);
    int len = 0;
    while (line[len]) len++;
    line[len] = '\n';
    line[len + 1] = '\0';

    semi_write0(line);
    semi_exit(0);

    for (;;);
    return 0;
}
