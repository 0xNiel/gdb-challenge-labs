# Phase 0 — Bootstrap

| | |
| --- | --- |
| Depends on | nothing |
| Unblocks | Phase 1 |
| Spec sections | "Architecture", "Deployment on the Hostinger VPS" (for the provisioning script), "Local performance test suite → Environment" |
| Effort | one evening plus VM download time |

## Objective

A repository where `./run.sh test --all` passes, and a Lima VM where a container runs under `runsc`. There is no CI (ADR 0008). Nothing here is product code; everything here is what every later phase relies on.

## Deliverables

| File | Purpose |
| --- | --- |
| `labd/go.mod`, `labd/cmd/labd/main.go`, `labd/cmd/labd-perf/main.go` | Go module compiles; `labd --version` and `labd-perf --version` print a version; `labd` serves `GET /healthz` on the configured internal address |
| `labd/internal/config/config.go`, `config_test.go` | Loads `labd.yaml` (schema from the spec), validates that `listen_internal` and `listen_ws` are loopback, applies defaults |
| `labd/labd.example.yaml` | The spec's config block with dev paths |
| `web/pyproject.toml`, `web/manage.py`, `web/config/settings/{base,dev,test}.py`, `web/config/urls.py`, `web/config/views.py` | Minimal Django 5.2 project; `GET /healthz` returns `ok`; one test asserts it |
| `deploy/lima/labs-dev.yaml` | Lima template: Ubuntu 24.04, repo mounted writable at the same path, provisioning runs `provision.sh --role dev` |
| `deploy/scripts/provision.sh` | Idempotent installer: containerd 2.x, runsc + containerd-shim-runsc-v1, `labs` namespace config, Postgres 16, Go, uv, Caddy (prod only). `--role dev|prod`. Shared by VM and VPS |
| `deploy/scripts/verify-runtime.sh` | Runs `alpine` under `io.containerd.runsc.v1` in namespace `labs`, prints `runsc ok`, cleans up |
| `deploy/env.example` | Every environment variable the system reads, with a comment |
| `scripts/gate.sh` phase 0 section | Real checks (already stubbed) |

## Tasks

### 0.1 Initial commit
Commit the scaffold as it stands.
**Done when:** `git log --oneline | wc -l` prints `1` and `git status` is clean.

### 0.2 Go module and binaries
Create `labd/go.mod` (`module gdblabs/labd`, `go 1.25`). Write `cmd/labd/main.go`: flags `--config` (default `labd.yaml`) and `--version`; loads config; starts an HTTP server on `listen_internal` with `/healthz` returning `{"ok":true}`; shuts down cleanly on SIGINT/SIGTERM with a 5 s drain. Write `cmd/labd-perf/main.go` with `--version` only. Add `labd/internal/config` with the struct matching the spec's `labd.yaml`, defaults, and validation (loopback listen addresses, `max_sessions ≥ 0`, `max_queue ≥ 0`, positive timeouts).
**Done when:** `cd labd && go build ./... && go vet ./... && go test ./...` passes and `config_test.go` covers: defaults applied, non-loopback rejected, missing file error, `max_sessions: 0` accepted (drain mode).

### 0.3 Django skeleton
`cd web && uv init --python 3.13`; add `django==5.2.*`, `psycopg[binary]`, `pytest`, `pytest-django`, `ruff`. Create `config/settings/base.py` (reads `DATABASE_URL`-style env via a tiny helper; no third-party env lib), `dev.py` (SQLite, DEBUG), `test.py` (SQLite in memory). One view `healthz` at `/healthz` returning `ok`. `pytest.ini` or `[tool.pytest.ini_options]` pointing at `config.settings.test`.
**Done when:** `cd web && uv run ruff check . && uv run pytest -q` passes with one test, and `uv run python manage.py check` reports no issues.

### 0.4 Lima template
Write `deploy/lima/labs-dev.yaml`: Ubuntu 24.04 image, `cpus: 4`, `memory: 8GiB`, `disk: 40GiB`, arch from env `LIMA_ARCH` (default host arch; see ADR 0001), mount the repo writable at the identical absolute path, `containerd.system: false` and `containerd.user: false` (we install our own), a provision step that runs `deploy/scripts/provision.sh --role dev`. Port forward none.
**Done when:** `./run.sh vm up` creates and starts the VM without manual input and `./run.sh vm ssh -- uname -a` prints an Ubuntu kernel.

### 0.5 Provisioning script
Write `deploy/scripts/provision.sh`. It must be re-runnable. Steps, each guarded by an "already done" check:
1. `apt-get install` base tools (`curl`, `jq`, `git`, `make`, `shellcheck`, `gdb` for host-side debugging, `strace`).
2. containerd 2.x from the official GitHub release tarball (not the distro package), systemd unit, `config.toml` with cgroup v2 + systemd cgroup driver, and a `[plugins."io.containerd.cri.v1.runtime"...]`-free config: we do not use CRI. Register runtime `io.containerd.runsc.v1` with `runtime_type = "io.containerd.runsc.v1"` and options `TypeUrl`/`ConfigPath` pointing to `/etc/containerd/runsc.toml` with `platform = "systrap"`. In `--role dev` also register `runc` (needed for the gVisor overhead comparison). In `--role prod` do **not** register `runc`.
3. `runsc` and `containerd-shim-runsc-v1` from the gVisor release bucket, pinned version, checksum verified.
4. Postgres 16 from the distro, a `labs` database, roles `web` and `labd` with passwords read from `deploy/env.example` defaults in dev.
5. Go toolchain tarball (version from `labd/go.mod`), `uv`.
6. `--role prod` only: Caddy from the official apt repo, `ufw`, `fail2ban`, unattended-upgrades, users `web` and `labd`, group `containerd` owning the socket with mode 0660. Prod parts are completed in Phase 8; in Phase 0 they may be stubs that print "prod step: TODO Phase 8" — this is the one place a TODO is allowed, because Phase 8 owns it.
Print versions at the end: `containerd --version`, `runsc --version`, `psql --version`, `go version`, `uv --version`.
**Done when:** running the script twice in a fresh VM succeeds both times and the second run installs nothing.

### 0.6 Runtime verification
Write `deploy/scripts/verify-runtime.sh`: `ctr -n labs images pull docker.io/library/alpine:3.20`, `ctr -n labs run --rm --runtime io.containerd.runsc.v1 docker.io/library/alpine:3.20 verify-$$ /bin/sh -c 'echo runsc ok; uname -r; cat /proc/version'`. Assert output contains `runsc ok` and the kernel string is gVisor's (contains `gVisor` or a `4.4.0` fake version). Check `stat -fc %T /sys/fs/cgroup` prints `cgroup2fs`.
**Done when:** `./run.sh vm verify` prints `runsc ok` and `cgroup2fs`.

### 0.7 CI — removed
Superseded by ADR 0008: the owner removed the workflow to save Actions minutes. The local equivalent is `./run.sh check && ./run.sh test --all && ./run.sh lint`, run on both hosts before pushing.
**Done when:** nothing to do; do not re-add a workflow.

### 0.8 Both hosts green
Run tasks 0.4–0.6 on the Linux x86-64 laptop as well (`./run.sh vm up` provisions the laptop itself; no VM). Record `docs/metrics/environment-linux-laptop.md` and `environment-dev-vm.md`: `nproc`, RAM, kernel, containerd and runsc versions, cgroup mode, `/dev/kvm` presence (QUESTIONS Q10).
**Done when:** `./run.sh vm verify` prints `runsc ok` and `cgroup2fs` on both the Mac (VM) and the laptop, and both environment files exist.

### 0.9 Second developer onboarding
Have the second developer clone the repo and run `./run.sh doctor`, then follow `docs/ONBOARDING.md` through `./run.sh vm verify`. Fix anything `doctor` missed for their OS or package manager (add the hint to `install_hint` in `run.sh`).
**Done when:** their `doctor` output shows no "STILL NEEDED" entries and `vm verify` passes; STATUS.md records their host label and OS.

### 0.10 Wire the gate
`phase_0` in `scripts/gate.sh` already runs `run.sh check`, `run.sh test --all`, `run.sh vm verify`, and a clean-tree check. Run it on both hosts.
**Done when:** `./run.sh gate --phase 0` exits 0 on the Mac and on the Linux laptop.

## Tests

- `labd/internal/config/config_test.go` as described in 0.2.
- `web/config/tests/test_healthz.py`.
- `deploy/scripts/provision.sh` idempotency: run twice (manual in VM; the second run's output must contain no `Installing` lines).

## Gate

```
./run.sh gate --phase 0
```
Checks, in order, on **both** the Mac and the Linux laptop:
1. `./run.sh check` (doctor in strict mode) finds every required tool for that host.
2. `./run.sh test --all` exit 0.
3. `./run.sh vm verify` prints `runsc ok` and `cgroup2fs`.
4. Working tree clean.

## Metrics to record

`docs/metrics/environment-linux-laptop.md` and `environment-dev-vm.md` (task 0.8). Record VM boot time to a usable shell in STATUS.md as a curiosity only.

## Non-goals

- Any container spec work (Phase 1).
- Any Django app beyond `healthz` (Phase 6).
- Production hardening (Phase 8).

## Handoff

- STATUS.md: phase 0 `done`, gate output pasted, current phase → 1.
- Note the exact containerd and runsc versions installed in `docs/metrics/environment.md` (create it): kernel, containerd, runsc, cgroup mode, VM arch, CPU model, RAM.
