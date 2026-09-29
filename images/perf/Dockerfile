# perf — labbase + the P0/perf test program. Test-only; never deployed.
# The build context is staged by images/perf/build.sh (binaries compiled twice in the build
# image and checked for identical hashes, plus a generated core file).
FROM gdblabs/labbase:dev

COPY --chown=0:0 opt/perf/ /opt/perf/
USER 1000:1000
WORKDIR /home/lab
