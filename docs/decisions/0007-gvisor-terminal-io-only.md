# 0007 — Start gVisor containers in terminal mode only; non-terminal I/O hangs in `create`

Date: 2026-09-27 · Status: accepted · Phase: 0 (affects 1, 2, 5)

## Context

During Phase 0 on the arm64 dev VM (Ubuntu 24.04, containerd 2.4.1), `ctr run --runtime io.containerd.runsc.v1` hung forever unless the container had a terminal. Findings:

| I/O mode | runsc 20260921 | runsc 20260817 | runc 1.5.2 |
| --- | --- | --- | --- |
| terminal (`ctr run -t`, PTY via console socket) | works | not tested | works |
| FIFO stdio (`ctr run`, default) | hangs in Create | hangs | works |
| null I/O (`ctr run --null-io`) | hangs in Create | hangs | works |
| `runsc do` directly, no containerd | works | works | n/a |

A goroutine dump of the stuck shim shows `runscService.Create → runsccmd.(*Runsc).Create → cmdOutput → exec.Cmd.Wait`. With no terminal, the shim runs `runsc create` with its output captured through a Go pipe. The sandbox is a long-lived descendant of that process and inherits the pipe as the container's stdio, so the pipe never closes and `Wait` never returns. The sandbox itself is booted and waiting for `start`. Since the problem shows up on two releases a month apart, it is not a one-release regression.

Not yet checked on the x86-64 Linux laptop. If it does not reproduce there, the problem is specific to arm64 or Lima, but the decision below still holds.

Also found: gVisor releases since 20260831 ship as one tarball, and `runsc` requires the sidecar directory `gvisor-bin/` next to itself. `provision.sh` installs the whole tarball.

## Decision

- Every lab container is created with a terminal (`cio.WithTerminal`), which the spec already requires for the browser PTY. labd never creates a gVisor container without a terminal.
- Tools that run a container non-interactively allocate a PTY: shell scripts wrap `ctr run -t` in `script -qec ... /dev/null`, and Go helpers use `cio.WithTerminal` and read the PTY. This applies to `deploy/scripts/verify-runtime.sh` (done), the Phase 1 `specrun` helper and P0 checks, and the Phase 5 challenge oracle (`gdb -batch` under runsc).
- PTY output contains `\r` and sometimes NUL bytes. Scripts strip them (`tr -d '\r\000'`) before matching.
- Re-test non-terminal mode when upgrading containerd or gVisor, and on the x86-64 laptop in Phase 0 task 0.8. If it works everywhere, this ADR can be relaxed but not removed.

## Consequences

- Nothing changes for the product path; labs were always going to be terminal-attached.
- Batch tooling is slightly more awkward: output arrives through a PTY instead of clean stdout and stderr, and exit codes come through `ctr`/`script`.
- Worth reporting upstream to gVisor with the goroutine dump once it has been reproduced on x86-64.
