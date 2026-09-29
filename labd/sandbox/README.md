# sandbox

`sandbox-base.json` is the OCI runtime spec every lab starts from. `labd` never edits it at runtime. `orch.BuildSpec` applies per-challenge limits, args, the cgroup path and annotations, then refuses the result unless `orch.CheckInvariants` passes. The golden test in `internal/orch/testdata/spec_golden.json` pins the exact output for the default limits.

Change this file only through an ADR (`docs/SECURITY-INVARIANTS.md`).

## Field by field

| Field | Value | Invariant |
| --- | --- | --- |
| `process.terminal` | `true` | ADR 0007: gVisor containers without a terminal hang in `create` |
| `process.user` | uid/gid 1000 | S5 |
| `process.noNewPrivileges` | `true` | S5 |
| `process.capabilities.*` | all five sets empty | S6 |
| `process.rlimits` | `NOFILE` 256, `FSIZE` 32 MiB, `NPROC` = the lab's process limit (enforced by gVisor inside the sandbox), `CORE` 0 | S7 |
| `process.env` | `PATH`, `HOME=/home/lab`, `XDG_CONFIG_HOME=/etc/lab-config` (gdb settings), `TERM`, `PS1` | image contract |
| `root.readonly` | `true`; containerd gives a read-only snapshot view, so no writable layer exists | S3 |
| `mounts` | exactly `/proc`, `/dev` (64 KiB tmpfs), `/dev/pts`, `/tmp` and `/home/lab` (16 MiB tmpfs, `noexec,nosuid,nodev`). No `/sys`, `/run`, `/dev/shm`, mqueue, or any bind mount | S4, S8 |
| `linux.namespaces` | new pid, ipc, uts, mount, network, cgroup; none joined | S2 |
| `linux.resources.memory` | limit = swap limit (no swap); default 128 MiB | S7 |
| `linux.resources.cpu` | quota = millicores × 100 over a 100 000 µs period; default 500 m | S7 |
| `linux.resources.pids` | the lab limit (default 32) plus gVisor headroom under runsc (96): 128. The lab's own cap is `RLIMIT_NPROC` (ADR 0009) | S7 |
| `linux.resources.devices` | deny all | S8 |
| `linux.cgroupsPath` | `/labs/<id>`, i.e. `/sys/fs/cgroup/labs/<id>`; the metrics sampler reads it | spec "Metrics" |
| `linux.maskedPaths`, `readonlyPaths` | containerd defaults minus the `/sys` entries (no `/sys` mount) | defence in depth |

## What gVisor adds on its own (observed 2026-09-28, runsc 20260921)

- **`/sys`:** gVisor mounts its own synthetic sysfs even though the spec has none. It is implemented inside the Sentry and shows no host sysfs. S8 ("no host /proc, no devices") still holds, because both `/proc` and `/sys` are gVisor's, not the host's. P0 records what is visible.
- **The root filesystem** shows up as a read-only `9p` mount served by the gofer.
- **The `nodev` option** does not appear in gVisor's view of the tmpfs mounts. Device nodes cannot be created anyway without `CAP_MKNOD`, which is dropped (S6).
- **The network namespace** has no interfaces at all, not even loopback. `/proc/net/dev` and `/proc/net/route` are empty (S2).

## Running something under this spec

```
./run.sh build
./run.sh vm ssh -- sudo labd/bin/specrun --spec labd/sandbox/sandbox-base.json \
    --image docker.io/gdblabs/labbase:dev -- /bin/sh -c 'cat /proc/mounts'
```

`specrun` always attaches a terminal (ADR 0007). It prints `specrun-stats: {...}` to stderr and exits with the container's status, or 124 on timeout.
