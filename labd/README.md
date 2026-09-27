# labd

Go module `gdblabs/labd`: the orchestrator, terminal gateway, metrics sampler and perf driver in one binary (`cmd/labd`) plus the load driver (`cmd/labd-perf`).

| Path | Package | Phase |
| --- | --- | --- |
| `cmd/labd/` | CLI: `serve` (default), `migrate`, `pull`, `prune` | 0, 2, 5 |
| `cmd/labd-perf/` | Perf driver CLI: `run`, `report`, `profiles` | 0, 4 |
| `internal/config/` | `labd.yaml` loading, validation, SIGHUP reload | 0, 2 |
| `internal/clock/` | Clock interface and fake | 2 |
| `internal/orch/` | Sessions, semaphore, queue, timers, reconciler, spec builder, containerd runtime | 2, 5 |
| `internal/store/` | Postgres (pgx) and in-memory stores; embedded SQL migrations | 2 |
| `internal/api/` | Internal HTTP API on loopback with bearer auth | 2 |
| `internal/term/` | WebSocket ↔ PTY bridge, tokens, rate limits, command capture; `client/` scripted client | 3 |
| `internal/metrics/` | cgroup and /proc sampler → `samples` | 7 |
| `internal/perf/` | Profiles, virtual users, collector, report | 4 |
| `internal/flag/` | Flag derivation (ADR 0005) | 5 |
| `sandbox/` | `sandbox-base.json` OCI spec and its README | 1 |
| `migrations/` | `NNNN_*.sql` applied by `labd migrate` | 2 |
| `perf/` | Shell wrappers: `p0/`, `p1.sh`, `p4lite.sh`, `p5.sh`, `p7.sh`, `p9.sh`, `runall.sh` | 1–4 |
| `testpage/` | Dev-only xterm.js page and vendored assets | 3 |
| `integration/` | `//go:build integration` tests against real containerd (run in the VM) | 2, 3 |
| `testdata/` | Golden files | 2 |

Rules: standard library first (allow-list in `docs/CONVENTIONS.md`); nothing outside `internal/orch` imports containerd; every blocking call takes a context.

Run in the VM: `./run.sh build && ./run.sh labd`. Tests: `./run.sh test --go`, `./run.sh test --integration`.
