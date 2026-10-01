#!/bin/sh
# Compiles the challenge inside images/build; scripts/challenge-build.sh runs it twice and
# compares the results. In: this directory at /opt/lab (read-only, the same path as in the
# lab image, so gdb's `list` finds the source), /gen/flag_blob.h, $CFLAGS (the manifest's
# build.flags plus the mandatory -static -no-pie -fno-pie) and $ENTRY. Out: /out/$ENTRY.
set -eu
# shellcheck disable=SC2086 # CFLAGS is a flag list
cc $CFLAGS -I/gen -o "/out/$ENTRY" /opt/lab/src/main.c
