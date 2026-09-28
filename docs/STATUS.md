# Project status

Update this file at the end of every working session. Keep it factual. Newest log entry at the top.

## Current phase

**Phase 0 — Bootstrap.** In progress. All Phase 0 work is on `main` (pushed to `origin/main`); the `phase-0-bootstrap` branch no longer exists. Tasks 0.1–0.7 done; the gate passes on the Mac. Remaining, all needing a person or another machine:

- **0.8 on the Linux x86-64 laptop:** clone, `./run.sh doctor`, `./run.sh vm up`, `./run.sh vm verify`, then `./run.sh gate --phase 0`. Write `docs/metrics/environment-linux-laptop.md` in the same format as `environment-dev-vm.md`. Also check whether non-terminal gVisor I/O hangs there too (ADR 0007): `sudo timeout 25 ctr -n labs run --rm --null-io --runtime io.containerd.runsc.v1 docker.io/library/alpine:3.20 t1 /bin/true; echo $?` (124 means it hangs).
- **0.9:** second developer runs `./run.sh doctor` and follows `docs/ONBOARDING.md`.
- **0.7:** removed. The owner deleted the CI workflow to save Actions minutes; do not add workflows (ADR 0008).
- **0.10:** gate output from the laptop pasted below. **Do not start Phase 1** until the owner has run Phase 0 on the Linux laptop.

## Phase board

| Phase | Name | State | Gate result | Date |
| --- | --- | --- | --- | --- |
| 0 | Bootstrap: repo, toolchain, dev VM | in progress | passed on Mac; laptop pending | 2026-09-27 |
| 1 | gVisor + gdb spike (labbase, sandbox spec, P0) | blocked on 0 | — | — |
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
