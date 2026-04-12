#
# cortex-m4-synth — host + firmware builds for autoresearch.
#
# Host build: portable cc, used by host_compile / host_test / host_timing.
# Firmware build: arm-none-eabi-gcc targeting MPS2-AN386 under QEMU,
# used by the qemu_cycles instrument.
#

# ---- host build ----

CC       ?= cc
CFLAGS   ?= -O0 -g -Wall -Wextra -Isrc
LDFLAGS  ?=
BUILDDIR ?= build

SRCS_LIB  := src/dsp_fir.c
SRCS_MAIN := src/main.c $(SRCS_LIB)
SRCS_TEST := src/test_dsp_fir.c $(SRCS_LIB)

# ---- firmware build ----

ARM_CC      ?= arm-none-eabi-gcc
ARM_OBJCOPY ?= arm-none-eabi-objcopy
ARM_CFLAGS  := -mcpu=cortex-m4 -mthumb -O0 -g -Wall -Wextra -Isrc \
               -ffreestanding -nostdlib
ARM_LDFLAGS := -T mps2_an386.ld -nostdlib -lgcc

SRCS_FW := src/startup_m4.c src/firmware_main.c src/dsp_fir.c

.PHONY: all test clean firmware

all: $(BUILDDIR)/main $(BUILDDIR)/test
	@echo "built: $(BUILDDIR)/main $(BUILDDIR)/test"

$(BUILDDIR):
	@mkdir -p $(BUILDDIR)

$(BUILDDIR)/main: | $(BUILDDIR)
$(BUILDDIR)/main: $(SRCS_MAIN) src/dsp_fir.h
	$(CC) $(CFLAGS) $(SRCS_MAIN) -o $@ $(LDFLAGS)

$(BUILDDIR)/test: | $(BUILDDIR)
$(BUILDDIR)/test: $(SRCS_TEST) src/dsp_fir.h
	$(CC) $(CFLAGS) $(SRCS_TEST) -o $@ $(LDFLAGS)

test: $(BUILDDIR)/test
	./$(BUILDDIR)/test

# ---- firmware (requires arm-none-eabi-gcc) ----

firmware: $(BUILDDIR)/firmware.elf
	@echo "built: $(BUILDDIR)/firmware.elf"

$(BUILDDIR)/firmware.elf: | $(BUILDDIR)
$(BUILDDIR)/firmware.elf: $(SRCS_FW) src/dsp_fir.h mps2_an386.ld
	$(ARM_CC) $(ARM_CFLAGS) $(SRCS_FW) -o $@ $(ARM_LDFLAGS)

clean:
	rm -rf $(BUILDDIR)
