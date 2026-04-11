#
# cortex-m4-synth — host-tier Makefile for autoresearch M7.
#
# Defaults to a portable host build with `cc`. An arm-none-eabi cross build
# will land in M8 alongside the qemu instrument; the variable layout below
# is intentional so adding a PLATFORM=cortex-m4 branch stays a small diff.
#

CC       ?= cc
CFLAGS   ?= -O0 -g -Wall -Wextra -Isrc
LDFLAGS  ?=
BUILDDIR ?= build

SRCS_LIB  := src/dsp_fir.c
SRCS_MAIN := src/main.c $(SRCS_LIB)
SRCS_TEST := src/test_dsp_fir.c $(SRCS_LIB)

.PHONY: all test clean

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

clean:
	rm -rf $(BUILDDIR)
