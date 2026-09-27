# images

| Dir | Image | Built where | Phase |
| --- | --- | --- | --- |
| `labbase/` | `alpine:3.20` (digest-pinned) + `gdb binutils`, network applets removed, user `lab` uid 1000, `.gdbinit`. Target < 45 MB | CI (`labbase.yml`) and locally with `build.sh` | 1 |
| `build/` | gcc toolchain used only to compile challenge binaries and run the CI oracle; never shipped to the VPS | CI and locally | 1, 5 |
| `perf/` | `FROM labbase` + the perf program (`/opt/perf/perf`, source, a core file) and `session.gdb`; used by P0–P9 | locally | 1 |

Every `build.sh` builds `linux/amd64` (and `linux/arm64` for `labbase` and `perf` so the arm64 dev VM can run them), exports an OCI tar, imports it into the VM's containerd namespace `labs`, and prints the digest. Images are always referenced by digest once they leave the dev machine.

Challenge images are not here; they are built from `challenges/<tier>/<NN-slug>/` by `scripts/challenge-build.sh`.
