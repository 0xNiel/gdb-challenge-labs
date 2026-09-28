# Onboarding

For a developer joining the project on any machine. Fifteen minutes plus download time.

## 1. Clone and check your machine

```
git clone <repo-url> labbing-platform
cd labbing-platform
./run.sh doctor
```

`doctor` detects your OS and architecture and prints three lists: what is ok, what is still needed (with the install command for your package manager), and what is optional. Install what is under "STILL NEEDED" and run it again until it says all required dependencies are present.

Supported hosts:

| Host | How labs run | Notes |
| --- | --- | --- |
| Linux x86-64 (laptop, VPS) | directly on the machine: `./run.sh vm up` installs containerd, gVisor, Postgres 16 (server), Go, uv | This is what production is. Authoritative for every number and for challenge content |
| Linux arm64 | same as above | Platform works; challenge binaries are x86-64 and will not run here |

On Linux, `vm up` changes the machine itself: it installs system services (containerd, Postgres) with sudo. Any Debian/Ubuntu-family distro works (Ubuntu 24.04 is the reference; Debian 12, Ubuntu 22.04, Mint and Pop!_OS are fine). If the distro does not ship Postgres 16, `vm up` adds the official PostgreSQL apt repository so every host runs the same major version. Non-apt distros (Fedora, Arch) are not supported by `provision.sh`.

`doctor` lists `psql` as optional: that is the Postgres *client* for inspecting the database by hand. The Postgres *server* is never a `doctor` item; `vm up` installs it.
| macOS (Apple Silicon or Intel) | inside a Lima VM: `./run.sh vm up` creates and provisions it | The VM is arm64 on Apple Silicon, x86-64 on Intel. Everything container-related is executed in the VM for you by `run.sh` |
| Windows | not supported directly; use WSL2 (untested) or a Linux VM | `doctor` warns about cgroup v2 under WSL2 |

## 2. Bring up the lab runtime

```
./run.sh vm up       # Linux: provisions this host. macOS: creates + provisions the Lima VM
./run.sh vm verify   # must print "runsc ok" and "cgroup2fs"
```

`vm up` is idempotent; run it again after pulling changes to `deploy/scripts/provision.sh`.

## 3. Build and test

```
./run.sh build
./run.sh test --all            # Go + Django unit tests
./run.sh test --integration    # against real containerd (in the VM on macOS)
```

`make help` shows the same commands as Make targets.

## 4. Read before writing code

1. [CLAUDE.md](../CLAUDE.md): rules that apply to humans too, especially "work on the current phase only" and "gates are commands".
2. [STATUS.md](STATUS.md): where the project is; the log tells you what the last person did.
3. The current phase document in [plan/](plan/).
4. [SECURITY-INVARIANTS.md](SECURITY-INVARIANTS.md) and [CONVENTIONS.md](CONVENTIONS.md).

## 5. Working agreement

- One branch per phase, `[PN] area: what` commits, gate must pass before merging to `main`.
- Both architectures must stay green: a change is done when `./run.sh test --all` passes on your machine, along with `./run.sh check` and `./run.sh lint`. There is no CI (ADR 0008). Anything touching containers or images is also verified with `./run.sh test --integration` on an x86-64 Linux host before the phase gate.
- Measured numbers go in `docs/metrics/` with the host label (`linux-laptop`, `dev-vm`, `hostinger`). Only x86-64 hosts produce authoritative numbers.
- Questions for the owner go in [QUESTIONS.md](QUESTIONS.md) with a proposed default; do not block on them.
- Update STATUS.md at the end of every session, including what failed.

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| `doctor` says `containerd socket` needs sudo | `sudo usermod -aG containerd $USER` then log out and in; `provision.sh` creates the group |
| `vm verify` fails with an image pull error | you are offline, or the corporate proxy blocks `docker.io`; retry, or `ctr -n labs images import` a saved tar |
| `vm up` on macOS is slow to first boot | the Ubuntu cloud image download is once; later starts take seconds |
| `test --integration` on macOS says VM not running | `./run.sh vm up` |
| Go version too old | `doctor` prints the install line; on Linux `vm up` installs the pinned toolchain |
