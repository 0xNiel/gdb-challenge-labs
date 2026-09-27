# CLAUDE.md — gdb Challenge Labs

Read this file first in every session. It is short on purpose; it points to the documents that hold the detail.

## What this project is

A single-VPS web platform where a logged-in developer picks a gdb debugging challenge, gets a browser terminal into a sandboxed Alpine container (gVisor) holding a buggy binary, debugs it with gdb, and submits a CTF-style flag. Four processes on one box: `web` (Django), `labd` (Go orchestrator + WebSocket terminal gateway), `containerd` + `runsc`, and Postgres.

The MVP has two goals, in this order:

1. Produce **measured** numbers for per-lab CPU, memory, disk and bandwidth, and the real concurrent-lab capacity of an 8 vCPU / 32 GB box.
2. Ship a working product: tier 1 curriculum (5 challenges) end to end, then tiers 2–3.

## Start here (read in this order)

1. [docs/STATUS.md](docs/STATUS.md) — the current phase, what is done, what is blocked. Update it before you stop.
2. [docs/plan/IMPLEMENTATION_PLAN.md](docs/plan/IMPLEMENTATION_PLAN.md) — the phase plan and the rules for executing a phase.
3. The phase document for the current phase in [docs/plan/](docs/plan/).
4. [docs/spec/mvp-spec.md](docs/spec/mvp-spec.md) — the source of truth for behaviour. When the plan and the spec disagree, the spec wins unless an ADR in [docs/decisions/](docs/decisions/) says otherwise.
5. [docs/SECURITY-INVARIANTS.md](docs/SECURITY-INVARIANTS.md) — never weaken any of these; each one has a test that enforces it.
6. [docs/CONVENTIONS.md](docs/CONVENTIONS.md) — code and doc conventions.
7. [docs/QUESTIONS.md](docs/QUESTIONS.md) — open questions for the owner and the assumption currently in force for each.

## Working rules

- **Work on the current phase only.** Do not start the next phase until the current phase's gate passes and STATUS.md records it. Do not pull tasks forward "because they are easy".
- **Gates are commands, not opinions.** A phase is done when `./run.sh gate --phase N` exits 0 and its output is pasted into STATUS.md.
- **Record every measured number** in [docs/metrics/](docs/metrics/) with the date, the machine, and the command that produced it. Estimates are labelled *est.*; measured numbers replace them.
- **Decisions get an ADR.** If you must deviate from the spec or the plan, write a short ADR in `docs/decisions/` first, then change the code. Never silently change a security setting, a limit, a timeout, or a protocol frame.
- **Ask by writing, not by blocking.** If something needs the owner's input, add it to `docs/QUESTIONS.md` with your recommended default, proceed under that default, and say so in STATUS.md.
- **Small, verifiable steps.** Each task in a phase doc ends with a "done when" line. Run it. Do not report a task done without having run its check.
- **No new dependencies without a reason** written in the commit message. Go: prefer the standard library. Python: prefer what Django ships. See CONVENTIONS.md for the allow-list.
- **Only `labd` talks to containerd. Only `web` talks to users.** Keep that boundary in code and in docs.
- **No GitHub Actions.** Do not add or change anything under `.github/workflows/` until the owner asks; minutes are limited (ADR 0008). Run checks locally: `./run.sh check && ./run.sh test --all && ./run.sh lint`.
- **Commit at task boundaries** with the phase prefix, e.g. `[P2] orch: FIFO queue with per-user cap`. Do not push unless asked.

## Repo map

| Path | What lives here | Phase |
| --- | --- | --- |
| `labd/` | Go module: orchestrator, terminal gateway, metrics, perf driver | 0, 2, 3, 4, 7 |
| `web/` | Django project and apps | 0, 6, 7 |
| `images/labbase/` | Shared Alpine + gdb base image | 1 |
| `images/build/` | gcc toolchain image, used only to build challenges | 5 |
| `images/perf/` | Perf challenge image and scripted gdb session | 1, 4 |
| `challenges/` | Curriculum content, one dir per challenge, schema | 5, 8 |
| `deploy/` | Lima VM template, provisioning script, systemd units, Caddyfile | 0, 8 |
| `scripts/` | Helper scripts called by `run.sh` and the Makefile | all |
| `docs/` | Spec, plan, decisions, metrics, status | all |
| `run.sh`, `Makefile` | Single entry point for build, test, VM, perf, gates | 0 |

## Commands

Everything goes through `./run.sh` (the Makefile is a thin alias layer). `./run.sh help` lists subcommands.

```
./run.sh doctor                # what this host has, lacks, and how to install it
./run.sh check                 # doctor --strict (used by gates)
./run.sh vm up|verify|ssh|down # macOS: Lima VM. Linux: this host is the lab host
./run.sh build                 # build labd and labd-perf
./run.sh test --all            # Go unit + Django tests (host)
./run.sh test --integration    # Go integration tests (runs inside the VM)
./run.sh perf --scenario P2 --n 100
./run.sh gate --phase 2        # the exit test for a phase
```

Anything that needs containerd or runsc runs on Linux. On macOS, `run.sh` re-executes those commands inside the Lima VM automatically; on a Linux host it runs them directly.

## Environment facts

- Two developers, different machines. `./run.sh doctor` tells any host what it has and lacks; `docs/ONBOARDING.md` is the setup guide.
- Owner's Mac: Apple Silicon (arm64), Go 1.26, `uv`, Docker Desktop, Lima. Container work runs in an arm64 Ubuntu 24.04 Lima VM; `run.sh` handles the redirection.
- Owner's Linux laptop: x86-64. **This is the reference environment**: `run.sh vm up` provisions it directly, integration tests and challenge content run natively, and it produces the authoritative P0 and perf numbers (host label `linux-laptop`).
- **Both architectures must stay green.** Unit tests on both; anything touching containers, images or the sandbox spec is verified on the x86-64 laptop before a gate. Challenge images are x86-64 only; `labbase` and `perf` images are multi-arch. See ADR 0001.
- **gVisor containers must be started with a terminal** (ADR 0007). Without one, the gVisor shim hangs in `Create`. Scripts wrap `ctr run -t` in `script -qec ... /dev/null`; Go code uses `cio.WithTerminal`.
- Production: Hostinger KVM VPS, Ubuntu 24.04, 8 vCPU / 32 GB / 400 GB NVMe, x86-64. Same `provision.sh`, `--role prod`.

## Writing docs

Plain prose, short sentences, tables for parallel facts. Every number carries a unit and a source. Every file path is relative to the repo root. Do not paraphrase the spec inside plan documents; link to the section.
