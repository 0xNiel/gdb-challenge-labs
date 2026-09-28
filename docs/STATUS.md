# Project status

Update this file at the end of every working session. Keep it factual. Newest log entry at the top.

## Current phase

**Phase 0 — Bootstrap.** Gate passed on both hosts: the Mac (2026-09-27) and the Linux laptop (2026-09-28). Everything is on `main`. One task remains open and does not block Phase 1 unless the owner says so:

- **0.9:** the second developer runs `./run.sh doctor` and follows `docs/ONBOARDING.md`.

**Phase 1 is next and has not started.** The owner decides when it starts.

<details><summary>Earlier checklist (done)</summary>


- **0.8 on the Linux x86-64 laptop:** clone, `./run.sh doctor`, `./run.sh vm up`, `./run.sh vm verify`, then `./run.sh gate --phase 0`. Write `docs/metrics/environment-linux-laptop.md` in the same format as `environment-dev-vm.md`. Also check whether non-terminal gVisor I/O hangs there too (ADR 0007): `sudo timeout 25 ctr -n labs run --rm --null-io --runtime io.containerd.runsc.v1 docker.io/library/alpine:3.20 t1 /bin/true; echo $?` (124 means it hangs).
- **0.9:** second developer runs `./run.sh doctor` and follows `docs/ONBOARDING.md`.
- **0.7:** removed. The owner deleted the CI workflow to save Actions minutes; do not add workflows (ADR 0008).
- **0.10:** gate output from the laptop pasted below. **Do not start Phase 1** until the owner has run Phase 0 on the Linux laptop.

</details>

## Phase board

| Phase | Name | State | Gate result | Date |
| --- | --- | --- | --- | --- |
| 0 | Bootstrap: repo, toolchain, dev VM | done (0.9 open, non-blocking) | passed on Mac and laptop | 2026-09-28 |
| 1 | gVisor + gdb spike (labbase, sandbox spec, P0) | not started, waiting for owner | — | — |
| 2 | labd core (sessions, semaphore, reconciler) | blocked on 1 | — | — |
| 3 | Terminal gateway (WebSocket ↔ PTY) | blocked on 2 | — | — |
| 4 | Perf suite and measured capacity | blocked on 3 | — | — |
| 5 | Challenge pipeline and tier 1 content | blocked on 1 | — | — |
| 6 | Django web app | blocked on 3, 5 | — | — |
| 7 | Metrics, rollups, admin live view | blocked on 6 | — | — |
| 8 | Production on the VPS, tiers 2–3, beta | blocked on 4, 7 | — | — |

States: `not started`, `in progress`, `gate failing`, `done`, `blocked on N`.

## Open blockers

- None yet. Decisions awaiting the owner are in [QUESTIONS.md](QUESTIONS.md); each has a default that is in force.

## Measured numbers so far

None. Every figure in [metrics/capacity.md](metrics/capacity.md) is still an estimate from the spec.

## Log

### 2026-09-28 — Phase 0 gate passes on the Linux laptop
- The owner ran Phase 0 on the laptop: `doctor` all required present, `vm up` a no-op, `vm verify` passes on x86_64, and the gate passes (output below). Environment in `docs/metrics/environment-linux-laptop.md`.
- ADR 0007 is confirmed on x86-64: gVisor without a terminal hangs (`timeout 25` exited 124), as on the arm64 VM. Terminal mode works on both, and labs always use it.
- Open: task 0.9 (second developer onboarding) and the laptop's CPU model for the environment file.

Phase 0 gate on the Linux laptop:
```
==> gate for phase 0 — 2026-09-28T01:21Z — linux-laptop
  PASS  run.sh check
  PASS  run.sh test --all
  PASS  run.sh vm verify prints runsc ok + cgroup2fs
  PASS  working tree clean
==> GATE 0 PASSED.
```

### 2026-09-27 — `vm verify` hung silently on the laptop
- On the laptop, `./run.sh vm verify` printed `cgroup2fs` and then nothing. Likely cause: the shell predates the `containerd` group membership that `vm up` added, so verify used `sudo ctr`. That call ran inside `script` (the PTY required by ADR 0007), which is a new terminal, so sudo asked for the password again, and the prompt was swallowed by the captured output. The Mac VM never showed this because Lima has passwordless sudo.
- Fix: verify asks for sudo once, visibly, before doing anything, and runs the whole `script` under sudo, so nothing inside can prompt. It prints each step to stderr. On a timeout or bad output it dumps diagnostics (task state, gVisor processes, runsc log, containerd journal for that container) and then cleans up. `VERIFY_TIMEOUT` overrides the 60 s limit.
- Tested in the VM: the normal path passes, and a simulated hang produces the diagnostics and leaves no containers or sandboxes. The gVisor container finishes in under 0.3 s.
- Still unconfirmed on the laptop: whether the sudo prompt was the whole story, or whether gVisor also hangs there with a terminal. The new diagnostics will show which.

### 2026-09-27 — doctor crash with shellcheck installed; laptop is Ubuntu 26.04
- On the laptop, `./run.sh doctor` stopped silently after the `jq` line. The cause: `shellcheck --version` has no number on its first line, so the generic version parser's `grep` failed, and `set -euo pipefail` aborted the script. It never showed up before because no tested host had shellcheck. Fixed: shellcheck gets its own parser, and a failed version lookup can no longer abort `doctor`. Verified in a container with shellcheck and inside the fully provisioned VM, which covers every Linux branch of `doctor`.
- The laptop runs **Ubuntu 26.04.1** (kernel 7.0), whose repositories ship only Postgres 18. That is what broke `vm up`. With the previous fix, an Ubuntu 26.04 container gets Postgres 16.15 from apt.postgresql.org (`resolute-pgdg`), and every base package exists on 26.04.
- New warning in `doctor` and `provision.sh` when a Docker or distro containerd package is installed: `vm up` replaces the running containerd daemon with the pinned build.
- Laptop specs: 16 vCPU, 15 GB RAM, `/dev/kvm` present. Too little RAM for the 100-lab run (QUESTIONS Q10 updated).

### 2026-09-27 — Postgres 16 on non-reference distros
- On the owner's Linux laptop, `./run.sh vm up` failed with `Unable to locate package postgresql-16`. `provision.sh` assumed Ubuntu 24.04, whose repositories ship Postgres 16; the laptop's distro does not.
- Fix: `provision.sh` detects the distro, refuses non-apt distros with a clear message, and adds the official PostgreSQL apt repository (apt.postgresql.org) when the distro lacks `postgresql-16`. Derivatives map to their Ubuntu or Debian base codename.
- Tested in containers: Debian 12, Ubuntu 22.04 and a Mint-style derivative get 16.15 from the PostgreSQL repo; Ubuntu 24.04 still uses the distro package. All are idempotent. Fedora is refused. The Mac's VM is unchanged.
- `doctor` now shows the distro and reports the Postgres server separately from the optional `psql` client.

### 2026-09-27 — remote added, CI removed
- Owner added a remote and pushed. `main` now holds every Phase 0 commit (fast-forward, linear history) plus the owner's "removed CI" commit.
- No GitHub Actions until the owner asks (ADR 0008). Phase 0 task 0.7 and Phase 5 task 5.13 were rewritten to use local `run.sh` commands and a local rebuild-changed helper.
- Commits pushed so far carry the placeholder author email `your.email@example.com`. Fixing them now means rewriting pushed history and force-pushing, which only the owner should decide. Setting `git config user.email` fixes future commits.
- Next: the owner runs Phase 0 on the Linux laptop (task 0.8). Phase 1 has not started.

### 2026-09-27 — Phase 0 tasks 0.1–0.7
- Committed the scaffold, then on `phase-0-bootstrap`: Go module with config loader and `/healthz` (0.2), Django 5.2 skeleton (0.3), Lima VM + `provision.sh` + `verify-runtime.sh` (0.4–0.6), CI workflow (0.7).
- Pinned: containerd 2.4.1, gVisor release-20260921.0, runc 1.5.2, Postgres 16, Go 1.26.4, uv 0.12.3, Django 5.2.17, Python 3.13.14.
- `vm up` from nothing took 1 min 34 s. `provision.sh` second run is a no-op. Go and Django tests pass on macOS arm64 and inside the Linux arm64 VM with `-race`.
- **Finding, ADR 0007:** gVisor containers started through containerd hang in `Create` unless they have a terminal. A shim goroutine dump shows it waiting on a pipe the sandbox inherited. It reproduces on two gVisor releases; runc is fine. labs always use a terminal, so the product path is unaffected, but P0 scripts and the challenge oracle must allocate a PTY. Not yet checked on x86-64.
- **Finding:** gVisor releases since 2026-08-31 ship as one tarball with a `gvisor-bin/` sidecar directory that must sit next to `runsc`.
- Dev hosts now get gcc (needed by `go test -race`); production still gets no compiler.
- Git author email on the Mac is the placeholder `your.email@example.com`; fix with `git config user.email` and amend before the first push.

Phase 0 gate on the Mac:
```
==> gate for phase 0 — 2026-09-27T21:29Z — macbook.local
  PASS  run.sh check
  PASS  run.sh test --all
  PASS  run.sh vm verify prints runsc ok + cgroup2fs
  PASS  working tree clean
==> GATE 0 PASSED.
```

### 2026-09-27 — two hosts, two developers
- Owner has a Linux x86-64 laptop: it becomes the reference environment (ADR 0001 amended, Q1 resolved). Both arm64 (Mac via Lima) and x86-64 must stay green.
- `run.sh` now works natively on Linux (`vm up` provisions the host, `vm ssh`/`verify` run locally) and gained `doctor`, a per-host dependency report with install hints; `check` is `doctor --strict`. Verified on the Mac and in a bare x86-64 Ubuntu container.
- Added `docs/ONBOARDING.md` for the second developer; Phase 0 gained tasks 0.8 (both hosts green + environment files) and 0.9 (second developer onboarded).
- New questions Q10 (laptop specs, where the 100-lab run happens), Q11 (challenge images arm64?), Q12 (second developer's OS).

### 2026-09-26 — project scaffolded
- Folder structure, CLAUDE.md, phase plan (0–8), conventions, security invariants, ADRs 0001–0006, `run.sh`, `Makefile`, Lima template, gate dispatcher created.
- Spec moved from the repo root to `docs/spec/mvp-spec.md`.
- Git initialised on `main`; nothing committed yet.
- Next: Phase 0, task 0.1.
