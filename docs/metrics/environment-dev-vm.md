# Environment: dev-vm

Lima VM `labs` on the owner's Apple Silicon Mac. Not authoritative for performance (ADR 0001).
Recorded 2026-09-27, Phase 0, task 0.8. Source: `uname`, `/proc`, tool `--version` inside the VM.

| Item | Value |
| --- | --- |
| Host | macOS 15.7.7, Apple Silicon, 12 cores, 64 GB; Lima 2.1.3, vmType `vz` |
| Guest OS | Ubuntu 24.04.5 LTS |
| Kernel | 6.8.0-142-generic |
| Arch | aarch64 (arm64) |
| vCPU / RAM | 4 / 7.7 GiB |
| cgroup | v2 (`cgroup2fs`) |
| `/dev/kvm` | absent: runsc uses `systrap` |
| containerd | v2.4.1, CRI plugin disabled |
| runsc | release-20260921.0, platform `systrap`, sidecars in `/usr/local/bin/gvisor-bin/` |
| runc | 1.5.2 (dev only, reference runs) |
| Postgres | 16.15 |
| Go | 1.26.4 linux/arm64 |
| uv | 0.12.3 |

## Timings (curiosity, not metrics)

| Step | Time |
| --- | --- |
| `./run.sh vm up` first run, including Ubuntu image download and full provisioning | 1 min 34 s |
| `provision.sh --role dev` second run (no-op) | a few seconds |

## Known issues on this host

- gVisor containers hang in `create` without a terminal. See ADR 0007. Not yet checked on x86-64.
