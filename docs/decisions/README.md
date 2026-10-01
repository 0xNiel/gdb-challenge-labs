# Architecture decision records

One file per decision, numbered sequentially. Decisions already made in the spec are listed in the index below and are not repeated as files; only decisions made after the spec (or that refine it) get a file.

## Template

```markdown
# NNNN — Title

Date: YYYY-MM-DD · Status: accepted | superseded by NNNN · Phase: N

## Context
What forced the decision. One paragraph.

## Decision
What we do. Imperative, specific.

## Consequences
What becomes easier, what becomes harder, what to revisit and when.
```

## Index

| # | Title | Status | Origin |
| --- | --- | --- | --- |
| spec | Registry: GHCR private, local retention on the VPS | accepted | spec |
| spec | Flags are per-deploy HMAC of the slug, XOR-encoded against runtime state | accepted | spec |
| spec | Idle timeout 15 min, one 15 min extension | accepted | spec |
| spec | All tiers free in MVP | accepted | spec |
| spec | `objdump`/`readelf`/`nm` in every tier | accepted | spec |
| spec | Every command line captured, 90-day raw retention | accepted | spec |
| spec | One Go daemon for orchestrator and gateway | accepted | spec |
| spec | ASLR off by two mechanisms; hardware watchpoints off by default | ASLR part superseded by 0010 | spec |
| spec | No Kubernetes, no Docker daemon on the VPS, containerd + runsc directly | accepted | spec |
| [0001](0001-dev-environment.md) | Dev on arm64 Lima VM; authoritative numbers on x86-64 Linux | accepted | scaffold |
| [0002](0002-python-django-versions.md) | Django 5.2 LTS on Python 3.13 via uv | accepted | scaffold |
| [0003](0003-migrations-ownership.md) | labd owns its tables with embedded SQL migrations; Django models unmanaged | accepted | scaffold |
| [0004](0004-websocket-library.md) | `github.com/coder/websocket` | accepted | scaffold |
| [0005](0005-flag-derivation.md) | Exact flag derivation and encoding | accepted | scaffold |
| [0006](0006-phase-gates.md) | Phases with command gates; perf failures need a decision, not a pass | accepted | scaffold |
| [0009](0009-pids-limit-under-gvisor.md) | Lab process limit via RLIMIT_NPROC; cgroup pids gets gVisor headroom | accepted | Phase 1 finding |
| [0008](0008-no-github-actions.md) | No GitHub Actions until the owner asks; all checks run locally | accepted | owner, 2026-09-27 |
| [0007](0007-gvisor-terminal-io-only.md) | Start gVisor containers in terminal mode only; non-terminal I/O hangs | accepted | Phase 0 finding |
| [0010](0010-aslr-under-gvisor-static-linking.md) | gVisor cannot turn off ASLR; every lab binary is `-static -no-pie -fno-pie` | accepted | owner, 2026-09-29 |
| [0011](0011-sessions-table-columns.md) | `sessions` stores `challenge_slug`, not web's `challenge_id`; adds `image`, `extended` | accepted | Phase 2 |
| [0012](0012-ws-token-nonce.md) | WebSocket tokens carry a random nonce, so two per second are distinct | accepted | Phase 3 |
| [0013](0013-max-sessions.md) | Production `max_sessions` is 100: CPU under abuse is the limit, not memory (derived 446) | accepted | Phase 4 |
