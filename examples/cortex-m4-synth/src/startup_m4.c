/*
 * Minimal Cortex-M4 startup for QEMU MPS2-AN386.
 *
 * Provides the vector table (initial SP + reset vector) and a
 * Reset_Handler that copies .data, zeroes .bss, then calls main().
 * No interrupts, no SystemInit — QEMU provides defaults.
 */

#include <stdint.h>

extern uint32_t _stack_top;
extern uint32_t _etext, _sdata, _edata, _sbss, _ebss;
extern int main(void);

void Reset_Handler(void) {
    uint32_t *src = &_etext;
    uint32_t *dst = &_sdata;
    while (dst < &_edata) *dst++ = *src++;

    dst = &_sbss;
    while (dst < &_ebss) *dst++ = 0;

    main();
    for (;;) __asm__ volatile("wfi");
}

__attribute__((section(".isr_vector"), used))
const uint32_t vector_table[2] = {
    (uint32_t)&_stack_top,
    (uint32_t)Reset_Handler,
};
