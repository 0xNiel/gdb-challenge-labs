# 0009 — Enforce the lab's process limit with RLIMIT_NPROC; give the cgroup gVisor headroom

Date: 2026-09-28 (amended 2026-09-29: runc) · Status: accepted · Phase: 1 · Refines: spec "Sandbox security profile → PIDs 32", invariant S7

## Context

The spec caps each lab at 32 processes with the cgroup pids controller. Under gVisor, that cgroup contains the whole sandbox on the host, not only the lab's processes. The Sentry's threads, the gofer, and with the `systrap` platform one or more stub processes per guest process all count. P0 on the arm64 dev VM measured:

| Situation | Host tasks in the lab's cgroup |
| --- | --- |
| Idle `sh` in the sandbox | 20 to 21 |
| Fork storm stopped at the lab's limit (30 children) | 85 |

With `pids.max = 32`, the lab had room for about 11 processes of its own. A fork storm then exhausted the limit and **killed the whole sandbox**: exit 128, no output, not even the shell after the probe. The CPU quota and memory limit behave as expected under gVisor; only the pids limit has this double meaning.

## Decision

- **The lab's own limit is `RLIMIT_NPROC`**, equal to the manifest's `limits.pids` (default 32). gVisor enforces it inside the sandbox: the fork probe stops at 30 children with `EAGAIN`, and the sandbox keeps running.
- **The cgroup `pids.max` is `limits.pids + RunscHostPidsOverhead`**, 32 + 96 = 128 by default under runsc, and `limits.pids` under runc. It remains a hard host-side cap, so a Sentry bug cannot spawn unbounded host tasks.
- **Under runc** (dev hosts only, for reference runs), the lab limit is the cgroup `pids.max` = `limits.pids`, and `RLIMIT_NPROC` is **not set**. Without a user namespace, `RLIMIT_NPROC` counts every host process owned by uid 1000. On the owner's laptop that uid is the desktop user, so runc labs could not even `exec` gdb (EAGAIN). This was reproduced on the dev VM with 40 host processes owned by uid 1000, and passes 27 of 27 P0 checks after the fix.
- `orch.BuildSpec` takes `SpecParams.Runtime` and applies the right scheme. It refuses a gVisor spec whose base lacks `RLIMIT_NPROC`. `CheckInvariants` checks `RLIMIT_NPROC` when present: between 1 and 256, and no higher than the cgroup limit.
- S7 in `docs/SECURITY-INVARIANTS.md` now reads "pids ≤ 32 for the lab (RLIMIT_NPROC); cgroup pids = lab limit + gVisor headroom".

## Consequences

- Fork bombs stay contained to their own lab, and the lab survives them, which is better than the spec's "session continues or is killed".
- Under gVisor the guest uid 1000 is not the host uid 1000, so a host user with uid 1000 (common on desktops and cloud images) does not affect production labs.
- The overhead is a measured constant. Phase 1 re-measures on x86-64 and Phase 4 under load (threads, gdb, many sessions). If the peak approaches the limit, raise the constant and note it here.
- The per-lab host task count (about 20 idle) matters for the box-wide limits in Phase 4: `kernel.pid_max` and systemd `TasksMax` for `containerd.service`, which is already `infinity`.
