# Phase 2 — labd core

| | |
| --- | --- |
| Depends on | Phase 1 |
| Unblocks | Phase 3 |
| Spec sections | "Orchestrator (labd)" (entire), "Data model" rows for `sessions`, `events`, "Metrics & analytics → How labd collects" (only the cgroup paths), "Local performance test suite → P4, P5, P6" |
| Effort | two weeks |

## Objective

`labd` creates, tracks and destroys lab containers through containerd with the sandbox spec from Phase 1, enforces the concurrency cap, per-user cap, queue, idle and hard timeouts, survives its own restart without leaking a container, and exposes the internal HTTP API. No WebSocket yet: the PTY is created but only a test harness reads it.

## Design fixed by this document

- **Packages**: `internal/config` (Phase 0), `internal/orch` (sessions, semaphore, queue, reconciler, spec builder), `internal/store` (Postgres via pgx; `sessions`, `events`, `samples` writes; embedded SQL migrations), `internal/api` (internal HTTP), `internal/clock` (interface `Now()`, `NewTimer()`, `After()` with a fake for tests).
- **`orch.Runtime` interface** wraps containerd: `Create(ctx, CreateOpts) (Container, error)`, `List(ctx) ([]ContainerInfo, error)`, `Attach(ctx, id) (Container, error)`; `Container` has `Start`, `Kill`, `Delete`, `Wait`, `Resize`, `IO() (stdin io.WriteCloser, stdout io.Reader)`, `CgroupPath()`. `containerdRuntime` is the real one; `fakeRuntime` lives in `orch/fake_runtime_test.go`. Nothing outside `orch` imports containerd.
- **Session struct**: `ID uuid`, `UserID`, `ChallengeSlug`, `ImageDigest`, `Limits`, `State`, timestamps `CreatedAt/StartedAt/EndedAt`, `EndReason`, `ContainerID`, `Extended bool`, `idle *Timer`, `hard *Timer`, `pty` handle. States are exactly the spec's: `queued, creating, running, ending, ended, failed, abandoned`.
- **Manager**: `Start(ctx, StartReq) (StartResp, error)` returns existing session for the user if one is `queued/creating/running`; otherwise tries the semaphore (buffered channel `max_sessions`); on failure enqueues into a FIFO of capacity `max_queue` or returns `ErrQueueFull`. A dispatcher goroutine moves queued sessions into `creating` as slots free. Queue timeout 2 min → `abandoned`. `Stop(ctx, id, reason)`. `Extend(id)` once. `List()`, `Stats()`.
- **Timers**: idle timer reset by `Touch()` (called by the gateway on every input byte in Phase 3; by tests here). Hard TTL fixed at start. Both from `Limits` (manifest) else `default_limits`. Fire → `Stop(reason)`.
- **Reconciler**: on boot, `Runtime.List()` labels `lab.session_id` etc.; for each container: no open row or `created_at + ttl < now` → kill/delete, close row `reconciled`; open row and live task → adopt (state `running`, timers rebuilt from row timestamps, PTY re-attached); open row and dead task → close row `reconciled`, delete container.
- **Store**: `sessions` and `events` tables per spec; migrations in `labd/migrations/NNNN_*.sql` embedded with `embed.FS`, applied by `labd migrate` and on boot; `store.Store` interface with a Postgres implementation and an in-memory fake for unit tests. Writes are batched where the spec says (samples come in Phase 7; `session_ended` and `lab_started` events here).
- **API**: exactly the spec's table. Bearer secret from `LABD_INTERNAL_SECRET`. JSON bodies. `POST /internal/sessions` → 200 `{session_id, ws_token, state, queue_position}` (ws_token is filled in Phase 3; here it is an empty string), 503 `{error:"queue_full", retry_after_s}`, 409 never (existing session is returned, not an error). `DELETE /internal/sessions/{id}` body `{reason}`. `GET /internal/sessions`, `GET /internal/stats`, `POST /internal/reload`, `GET /healthz` (no auth).
- **Config reload**: SIGHUP and `POST /internal/reload` re-read `labd.yaml` and `challenges.json`; lowering `max_sessions` resizes the semaphore by withholding slots as they free; never kills.
- **`challenges.json`**: `{ "challenges": [ {slug, image, limits{...}, enabled} ] }`. Phase 5 produces it; here a hand-written file with the `perf` image is used.
- **CLI**: `labd serve` (default), `labd migrate`, `labd pull` (pulls every digest in `challenges.json` not already present; labels `lab.keep=true`), `labd prune` (Phase 5 completes retention logic; here it only lists).

## Deliverables

| File | Purpose |
| --- | --- |
| `labd/internal/clock/` | Clock interface and fake |
| `labd/internal/orch/runtime.go`, `runtime_containerd.go`, `fake_runtime_test.go` | Runtime abstraction and implementations |
| `labd/internal/orch/spec.go`, `spec_test.go`, `testdata/spec_golden.json` | Loads `sandbox-base.json`, applies limits and labels, golden test |
| `labd/internal/orch/session.go`, `manager.go`, `queue.go`, `reconciler.go` and tests | The core |
| `labd/internal/store/store.go`, `postgres.go`, `memory.go`, `migrations/0001_init.sql` | Persistence |
| `labd/internal/api/server.go`, `server_test.go` | Internal API |
| `labd/cmd/labd/main.go` | Subcommands, wiring, signal handling |
| `labd/integration/*_test.go` (`//go:build integration`) | Real containerd tests |
| `labd/perf/p4lite.sh`, `labd/perf/p5.sh` | Churn and crash-recovery scripts driving the internal API |
| `docs/metrics/create-latency-<date>-<host>.md` | Create-to-running latency for an empty container |

## Tasks

### 2.1 Clock and fake
**Done when:** `clock_test.go` shows a fake timer firing on `Advance`.

### 2.2 Spec builder and golden test
`orch.BuildSpec(base []byte, limits Limits, labels map[string]string, image ImageConfig) (*specs.Spec, error)`. Sets memory limit bytes, `memory.swap` equal to limit (no swap), CPU quota from millicores over period 100000, pids limit, rlimits, labels into annotations, rootfs from the image. `spec_test.go` marshals the result for the default limits and compares to `testdata/spec_golden.json` byte for byte (with `-update` flag to regenerate). A second test walks the golden file and asserts S2–S8 structurally: caps lists empty, `root.readonly`, network namespace without path, mount list exact, `noNewPrivileges`, uid 1000. That second test is what protects the invariants even if someone regenerates the golden.
**Done when:** both tests pass; deliberately setting `readonly: false` in the base makes the structural test fail.

### 2.3 Fake runtime and containerd runtime
Fake: in-memory containers with controllable task state, `io.Pipe` for the PTY, error injection (`FailCreateFor(slug)`). Real: `containerd.New(socket, WithDefaultNamespace("labs"))`, `NewContainer(ctx, id, WithImage, WithSpec(spec), WithRuntime(runtime, nil), WithSnapshotter("overlayfs"), WithNewSnapshot, WithContainerLabels)`, `NewTask(ctx, cio.NewCreator(cio.WithTerminal, cio.WithStreams(...)))`. `Kill` sends SIGKILL, waits with 5 s timeout, deletes task then container and snapshot.
**Done when:** integration test `TestRuntime_CreateStartKillDelete` passes in the VM against the `perf` image and `ctr -n labs c ls` is empty afterwards.

### 2.4 Session and Manager: semaphore and per-user cap
Implement `Start`, `Stop`, `List`, `Stats`. Unit tests with the fake runtime and fake clock: cap of 2 admits 2 and queues the third; same user twice returns same session id; `Stop` frees a slot which admits the queued one in FIFO order; `Stop` on unknown id returns `ErrNotFound`; failure during create → `failed` state, slot released.
**Done when:** `go test ./internal/orch -run 'Manager'` passes.

### 2.5 Queue
Bounded FIFO with positions, 2-minute abandonment, `Position(id)`. Tests: positions shift when the head is admitted; abandonment after timeout; `ErrQueueFull` at `max_queue`.
**Done when:** queue tests pass, including a 150-into-100 test that mirrors P6 (50 queued, positions 1–50, none admitted over cap, FIFO drain).

### 2.6 Timers and extend
Idle timer reset by `Touch`; hard TTL; `Extend` adds `extend_minutes` once. Tests with fake clock: idle fires → `ended/idle_timeout`; hard fires → `ended/hard_ttl`; extend once ok, twice returns `ErrAlreadyExtended`; `Touch` after `ending` is ignored.
**Done when:** tests pass.

### 2.7 Config reload
`Manager.SetCap(n)`: raising adds slots; lowering withholds slots as they free. SIGHUP handler and `/internal/reload`. Test: cap 3 with 3 running, lower to 1, stop two → no admission until the third stops, then admission resumes at cap 1.
**Done when:** test passes.

### 2.8 Store
Migration `0001_init.sql` creates `sessions`, `events`, `samples` and the indexes from the spec. Postgres implementation with pgx pool. In-memory implementation for unit tests. `labd migrate` applies. Tests: in-memory store round-trips; a Postgres test (integration tag) applies the migration on an empty DB twice without error.
**Done when:** both pass; `psql -c '\d sessions'` in the VM matches the spec columns.

### 2.9 Reconciler
Implement against `Runtime` and `Store`. Tests with fakes: orphan container (no row) → killed and deleted; container with open row and live task → adopted, timers rebuilt, `List()` shows it; container with open row and dead task → row closed `reconciled`; container older than TTL → killed regardless of row.
**Done when:** four tests pass.

### 2.10 Internal API
Handlers, bearer middleware, loopback enforcement (refuse to start if `listen_internal` is not loopback; already in config validation, assert again at bind). Tests with `httptest`: 401 without bearer; start returns 200 with state `creating`/`running`; second start same user returns the same id; delete; list; stats fields present; reload returns 200 and re-reads config from a temp file.
**Done when:** tests pass.

### 2.11 `labd pull`
Reads `challenges.json`, pulls each digest missing from the content store, labels `lab.keep=true`. Integration test: with the perf image already imported, pull is a no-op; with a bogus digest, it logs and returns non-zero with a clear message.
**Done when:** integration test passes.

### 2.12 P4-lite and P5 scripts
`labd/perf/p4lite.sh`: start `labd` in the VM with `max_sessions: 20`; loop for 10 minutes: every 2 s start a session via the API and stop a random running one; at the end assert `ctr -n labs c ls | wc -l` equals the `active` count from `/internal/stats`, and both are ≤ 20; record create latency p50/p95 from the API response times to `docs/metrics/create-latency-<date>-<host>.md`.
`labd/perf/p5.sh`: start 20 sessions; `kill -9` labd; restart; assert within 15 s that `/internal/stats.active == 20`, that every original session id is still `running` in `GET /internal/sessions`, and that `ctr -n labs c ls` count is 20; then stop all and assert 0.
**Done when:** both scripts exit 0 in the VM.

### 2.13 Wire the gate
`phase_2` in `scripts/gate.sh`: `go vet`, `go test ./...`, `test --integration`, `p4lite.sh`, `p5.sh`, container count 0 at the end.
**Done when:** `./run.sh gate --phase 2` exits 0.

## Tests

Unit (host): spec golden + structural, manager, queue, timers, reload, reconciler, API, store memory. Integration (VM): runtime create/kill, store migration, pull, P4-lite, P5.

## Gate

```
./run.sh gate --phase 2
```
1. `go vet ./... && go test -race ./...` in `labd/`.
2. `./run.sh test --integration` green in the VM.
3. `labd/perf/p4lite.sh` and `labd/perf/p5.sh` exit 0.
4. `ctr -n labs c ls -q | wc -l` is `0` after the run.
5. `docs/metrics/create-latency-*.md` exists.

## Metrics to record

Create-to-running latency (p50/p95/max) for the perf image under runsc, N = 300 from P4-lite; steady-state memory of the `labd` process itself at 20 sessions; number of goroutines at 0 and at 20 sessions (`runtime.NumGoroutine` exposed on `/internal/stats` as `labd.goroutines`).

## Non-goals

- WebSocket, tokens, rate limits, command capture (Phase 3).
- `samples` collection (Phase 7). The table exists; nothing writes to it yet.
- `labd prune` retention logic (Phase 5).

## Handoff

- STATUS.md updated; metrics files present.
- `labd/README.md` documents the package layout and how to run `labd` in the VM against the perf image.
- List in STATUS.md any spec API field left empty (`ws_token`) and which phase fills it.
