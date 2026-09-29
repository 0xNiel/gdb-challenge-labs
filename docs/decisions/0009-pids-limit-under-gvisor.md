# 0009 — Enforce the lab's process limit with RLIMIT_NPROC; give the cgroup gVisor headroom

Date: 2026-09-28 · Status: accepted · Phase: 1 · Refines: spec "Sandbox security profile → PIDs 32", invariant S7

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
- `orch.BuildSpec` takes `SpecParams.HostPidsOverhead`. `orch.RunOnce`, and the Phase 2 session manager, set it to `RunscHostPidsOverhead` for runsc. `CheckInvariants` requires `RLIMIT_NPROC` to be present, at most 256, and no higher than the cgroup limit.
- S7 in `docs/SECURITY-INVARIANTS.md` now reads "pids ≤ 32 for the lab (RLIMIT_NPROC); cgroup pids = lab limit + gVisor headroom".

## Consequences

- Fork bombs stay contained to their own lab, and the lab survives them, which is better than the spec's "session continues or is killed".
- The overhead is a measured constant. Phase 1 re-measures on x86-64 and Phase 4 under load (threads, gdb, many sessions). If the peak approaches the limit, raise the constant and note it here.
- The per-lab host task count (about 20 idle) matters for the box-wide limits in Phase 4: `kernel.pid_max` and systemd `TasksMax` for `containerd.service`, which is already `infinity`.
