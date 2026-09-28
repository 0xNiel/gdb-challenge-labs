# images/build — the toolchain that compiles lab binaries. Used on developer machines only;
# never shipped to the VPS and never a parent of a lab image.
#
# Alpine (musl), same pinned release as labbase: a binary built here runs in the lab image
# with identical libc and loader. A glibc toolchain (e.g. gcc:13) would produce binaries
# that do not run on Alpine.
FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc

# build-base = gcc, musl-dev, make, binutils. gdb is here for the challenge oracle and for
# generating core files; file for sanity checks.
RUN apk add --no-cache build-base gdb file

# Reproducible builds (spec: "SOURCE_DATE_EPOCH for reproducibility").
ENV SOURCE_DATE_EPOCH=0
WORKDIR /src
