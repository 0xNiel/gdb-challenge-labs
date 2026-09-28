# Environment: linux-laptop

The owner's Linux x86-64 laptop, the reference development host (ADR 0001). Authoritative for P0 and per-lab numbers; too little RAM for the 100-lab run (QUESTIONS Q10).
Recorded 2026-09-28, Phase 0, task 0.8. Source: `./run.sh doctor`, `provision.sh --role dev` version summary, `./run.sh vm verify`, run by the owner.

| Item | Value |
| --- | --- |
| Machine | ASUS TUF Gaming F16 (FX608JH) |
| CPU model | not recorded yet; add the output of `lscpu \| grep 'Model name'` |
| OS | Ubuntu 26.04.1 LTS (`resolute`) — not the reference 24.04, see notes |
| Kernel | 7.0.0-34-generic |
| Arch | x86_64 |
| vCPU / RAM | 16 / 15 GB |
| cgroup | v2 (`cgroup2fs`) |
| `/dev/kvm` | present: `runsc --platform=kvm` can be benchmarked here (Phase 4) |
| containerd | v2.4.1 from `/usr/local/bin`, CRI disabled; replaces Docker's packaged `containerd.io` as the running daemon |
| runsc | release-20260921.0, platform `systrap` |
| runc | 1.5.2 (reference runs) |
| Postgres | 16.15 from apt.postgresql.org (`16.15-1.pgdg26.04+2`); Ubuntu 26.04 itself ships only 18 |
| psql client on PATH | 18.6 (distro); works against the 16 server |
| Go | 1.26.4 |
| uv | 0.12.3 in `/usr/local/bin` (installed by `vm up`); the owner's PATH resolves 0.12.19 first. Both are fine |
| Docker | 29.8.1, buildx v0.37.1; no binfmt for arm64 image builds yet |

## Results

| Check | Result |
| --- | --- |
| `./run.sh doctor` | all required present |
| `provision.sh --role dev` second run | no changes |
| `./run.sh vm verify` | `runsc ok`, gVisor boot line, x86_64 |
| `./run.sh gate --phase 0` | passed, 2026-09-28T01:21Z |
| gVisor without a terminal (ADR 0007 check) | hangs: `timeout 25` exited 124. Same as the arm64 VM |

## Notes

- Ubuntu 26.04 differs from production (24.04). Package sources differ (Postgres from PGDG), and kernel 7.0 is newer than the VPS will run. Treat laptop numbers as representative of x86-64 gVisor cost, and re-check on the VPS in Phase 8.
- The owner's shell predated the `containerd` group membership, so `verify` used sudo. After logging out and back in, containerd commands work without sudo.

## Re-test for ADR 0007 (after any containerd or gVisor upgrade)

```
sudo timeout 25 ctr -n labs run --rm --null-io --runtime io.containerd.runsc.v1 docker.io/library/alpine:3.20 t1 /bin/true; echo $?
```

`124` means non-terminal I/O still hangs. `0` means it has been fixed upstream. Either way, clean up afterwards:

```
sudo ctr -n labs tasks kill -s KILL t1; sudo ctr -n labs tasks rm -f t1; sudo ctr -n labs containers rm t1
```
