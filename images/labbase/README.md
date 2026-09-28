# labbase

The base of every lab image. Build with `images/labbase/build.sh` (Docker on your machine, imported into containerd namespace `labs` on the lab host). Test contents with `images/labbase/test.sh`.

## What is in it

| Item | Why |
| --- | --- |
| `alpine:3.20` pinned by digest | Small, musl; the build image uses the same digest so binaries match |
| `gdb` 14.2, `binutils`, `file` | The spec's toolset. gdb pulls in Python 3.12 (~33 MB) and `musl-dbg` |
| User `lab`, uid/gid 1000, home `/home/lab` | Non-root (S5). `/home/lab` is a 16 MB tmpfs at runtime, so nothing in the image's home survives |
| `/etc/lab-config/gdb/gdbinit` | The four lab settings. gdb reads it because the sandbox sets `XDG_CONFIG_HOME=/etc/lab-config`. Alpine's gdb has no system-wide gdbinit, and `~/.gdbinit` would be hidden by the tmpfs |
| Allow-list of commands (`strip-tools.sh`) | Only `sh ls cat less grep head tail wc hexdump strings file gdb objdump readelf nm` (and `busybox`) remain on PATH; `apk` and `/etc/apk` are removed (S9) |

## gdb settings

| Setting | Value | Reason |
| --- | --- | --- |
| `disable-randomization` | on | ASLR off is a hard requirement for early tiers; binaries are also `-no-pie` |
| `can-use-hw-watchpoints` | 0 | Debug registers may not be available under gVisor; software watchpoints behave the same everywhere. P0 records whether hardware ones work |
| `pagination` | off | Terminal is in a browser |
| `confirm` | off | No y/n prompts |

P0 additions (if any) are listed here with the P0 row that motivated them.

## Known limits

- **`busybox` stays.** It is `/bin/sh`, so `busybox wget` and `busybox nc` still exist as code paths even though their links are gone. The sandbox has no network interface (S2), so they reach nothing. A custom busybox build without network applets is possible later.
- **gdb's `python`, `shell` and `pipe` commands work.** They cannot be disabled without rebuilding gdb (spec, "gdb-specific notes"). The mitigation is that there is nothing to run and nowhere to go.
- **Size.** 95.9 MB uncompressed and 30.6 MB gzipped on arm64 (2026-09-28), against a 45 MB target. Python is most of it. Task 1.9 (gdb built `--without-python`) is the fix.
