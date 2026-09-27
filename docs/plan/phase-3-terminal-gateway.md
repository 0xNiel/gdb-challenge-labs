# Phase 3 — Terminal gateway

| | |
| --- | --- |
| Depends on | Phase 2 |
| Unblocks | Phases 4 and 6 |
| Spec sections | "Terminal gateway" (entire), "Start-a-lab flow" steps 5–7, "Local performance test suite → P1", "Unit and integration tests" |
| Effort | one to two weeks |

## Objective

A browser can open a WebSocket to `labd`, authenticate with a short-lived HMAC token, and get a live gdb session bridged to the container's PTY, with resize, extend, TTL frames, rate limits, reconnect grace and command capture. A scripted Go client can do the same, which is the seed of the Phase 4 load driver. The first full single-session profile (P1) is measured.

## Design fixed by this document

- Package `internal/term`. WebSocket library `github.com/coder/websocket` (ADR 0004). One handler `GET /ws/term/{session_id}?t=<token>` on `listen_ws`.
- **Token**: `internal/term/token.go`. `Mint(key, sessionID, userID, exp) string` and `Verify(key, token, sessionID) (userID, error)`. Encoding: `base64url(session_id|user_id|exp_unix)` + `.` + `base64url(HMAC-SHA256(key, payload))`. Single use enforced by an in-memory `usedTokens` map with expiry sweep. `web` mints tokens with the same key (`WS_TOKEN_KEY`) in Phase 6; until then `labd` mints them in `POST /internal/sessions` and returns them (the spec's `ws_token` field).
- **Origin**: `Origin` header must equal `site_host` from config (scheme + host); missing Origin allowed only when `dev_allow_no_origin: true` (dev test page and Go client).
- **Frames**: exactly the spec table. Binary = PTY bytes. Text = JSON with `type` in `resize, extend, ping` (client→server) and `queued, state, ttl, warn, extend` (server→client). Unknown types are ignored and counted.
- **Bridge**: one goroutine reads WS → PTY stdin (through the input limiter and the capture splitter, calling `Manager.Touch` on every byte); one goroutine reads PTY stdout → a bounded channel (256 frames) → WS. If the channel is full for 10 s, close the WS with code 1008 and reason `slow_consumer`; the session stays alive for the grace window.
- **Rate limits**: input `golang.org/x/time/rate` 2048 B/s, burst 16384; excess dropped and one `warn` frame per second at most. Output limiter 262144 B/s on the writer side.
- **Reconnect**: on WS close, `Manager` starts a grace timer `ws_reconnect_grace_s`; a new WS for the same session cancels it and takes over the PTY (the previous WS, if still present, is closed with 1000 `replaced`). Grace expiry → `Stop(reason: ws_closed)`.
- **TTL frames**: every 30 s send `{"type":"ttl", idle_remaining_s, hard_remaining_s, extend_available}`.
- **Capture**: `internal/term/capture.go`. Splitter feeds on input bytes, strips ANSI CSI/OSC sequences and lone control bytes, handles backspace (0x7f/0x08) by deleting the previous rune, emits a line on `\r` or `\n`; empty lines are not emitted. Each line → `Store.AppendEvent(command_entered{seq, line})` through a buffered channel flushed every second or 100 events. `Ctrl-C` (0x03) is emitted as a `command_entered` with line `^C` so stuck-point analysis sees interrupts.
- **Dev test page**: `labd/testpage/index.html` + vendored `xterm.js` files, served at `GET /dev/term?session=<id>&t=<token>` only when `dev_testpage: true`. It implements the client protocol in ~100 lines and is the reference for the Phase 6 page.
- **Scripted client**: `internal/term/client` package used by tests and by `labd-perf`: `Dial(ctx, url, token)`, `Send(line)`, `Expect(regexp, timeout)`, `Resize`, `Extend`, `Ping`, records per-command echo latency (time from last byte sent to first byte received).

## Deliverables

| File | Purpose |
| --- | --- |
| `labd/internal/term/token.go`, `token_test.go` | Mint/verify, single use, expiry |
| `labd/internal/term/server.go`, `bridge.go`, `frames.go` | WS handler, bridge, JSON frames |
| `labd/internal/term/limiter.go`, `limiter_test.go` | Input/output limits and warn frame |
| `labd/internal/term/capture.go`, `capture_test.go` | Line splitter |
| `labd/internal/term/server_test.go` | Protocol tests with the fake runtime |
| `labd/internal/term/client/` | Scripted client |
| `labd/testpage/` | Dev page with vendored xterm.js (`xterm-5.x.js`, `addon-fit`) |
| `labd/integration/term_test.go` | Real gdb over WS |
| `labd/perf/p1.sh` | Single-session stepper profile |
| `docs/metrics/p1-<date>-<host>.{json,md}` | Results |

## Tasks

### 3.1 Token
**Done when:** tests: valid token verifies; expired (exp in past) rejected; reused rejected; wrong session id rejected; tampered signature rejected; a token minted with another key rejected; sweep removes expired entries.

### 3.2 Frames and handler skeleton
Parse query token, verify, check Origin, upgrade, send `{"type":"state","state":"running"}` or `queued` with position. Reject with HTTP 401/403 before upgrade on auth failure.
**Done when:** `server_test.go` covers 401 (bad token), 403 (bad origin), 404 (unknown session), 101 (ok).

### 3.3 Bridge with fake PTY
Wire the fake runtime's `io.Pipe` PTY. Tests: bytes sent arrive on the PTY; PTY output arrives as binary frames; resize frame calls `Resize` with cols/rows; ping does not call `Touch`; input does; a second connection replaces the first, which receives close code 1000.
**Done when:** tests pass under `-race`.

### 3.4 Rate limits
**Done when:** tests: 20 KB burst sent at once → at most 16 KB reaches the PTY and one `warn` frame received; output limiter caps a 1 MB PTY burst to about 256 KB in the first second (allow 20 % tolerance); slow consumer (test client stops reading) closes with 1008 after 10 s using the fake clock.

### 3.5 Capture
**Done when:** table test: `"break main\r"` → `break main`; `"ne\x7fxt\n"` → `next`; `"\x1b[Anext\n"` (arrow-up then text) → `next`; `"\x03"` → `^C`; `"\r\r"` → nothing; a 5 KB line without newline is emitted when the buffer exceeds 4 KB with `truncated: true`. Events reach the in-memory store with increasing `seq`.

### 3.6 Reconnect grace and TTL frames
**Done when:** tests with fake clock: close WS → session still running at 59 s, `ended/ws_closed` at 61 s; reconnect at 30 s cancels; `ttl` frame received every 30 s with decreasing values; `extend` once → `ok:true`, twice → `ok:false`.

### 3.7 Scripted client
**Done when:** client test against the fake server sends `break main`, expects `Breakpoint 1`, records a latency.

### 3.8 Dev test page
Vendor xterm.js (download the release files into `labd/testpage/vendor/`, record version and sha256 in `labd/testpage/README.md`). Page: connects, binary → `term.write`, keystrokes → binary, fit addon → resize frames, shows TTL countdown and Extend button, shows `queued` position, shows `state` changes. **Human check**: start a session against the perf image via `curl` to the internal API, open the page, run gdb through `break main`, `run`, `next`, `watch`, `bt`; paste 100 KB of text and observe the warn.
**Done when:** the human check is recorded in STATUS.md with date and initials.

### 3.9 Integration: real gdb over WS
`labd/integration/term_test.go`: start `labd` in-process against real containerd in the VM, create a session for the perf image, dial, send `gdb /opt/perf/perf\n`, expect `(gdb)`, send `break main\nrun\n`, expect `Breakpoint 1, main`, send `quit\ny\n`, stop session, assert container gone.
**Done when:** passes in the VM.

### 3.10 P1
`labd/perf/p1.sh` runs `labd-perf` (Phase 4 binary is not ready; use `go run ./internal/term/client/cmd/replay` — a small command that replays `images/perf/session.gdb` with a pacing of 20 commands/min for 10 minutes and records echo latency). Meanwhile sample cgroup memory, Sentry RSS and CPU (`cpu.stat usage_usec` delta) every 5 s. Output JSON + Markdown.
**Done when:** `docs/metrics/p1-<date>-<host>.md` shows start latency, echo latency p50/p95, RSS p50/p95/max, CPU % average, bytes in/out. Pass criterion from spec: start latency p95 < 2 s. If it fails, record and continue; the gate requires the number, not the pass.

### 3.11 Wire the gate
**Done when:** `./run.sh gate --phase 3` exits 0.

## Tests

Unit: token, frames, bridge, limiter, capture, grace/TTL, client. Integration: real gdb over WS. Perf: P1. Human: browser session.

## Gate

```
./run.sh gate --phase 3
```
1. `go test -race ./...` green.
2. `./run.sh test --integration` green including `term_test.go`.
3. `labd/perf/p1.sh` exits 0 and `docs/metrics/p1-*.json` exists with `echo_latency_ms.p95` present.
4. STATUS.md contains a line `Phase 3 human check: <date> <who> OK`.
5. Zero containers left in namespace `labs`.

## Metrics to record

`p1-<date>-<host>.json`:
```json
{"host":"","arch":"","runtime":"runsc","profile":"stepper","duration_s":600,
 "start_to_prompt_ms":0,"echo_latency_ms":{"p50":0,"p95":0,"max":0},
 "cgroup_mem_mb":{"p50":0,"p95":0,"max":0},"sentry_rss_mb":{"p50":0,"p95":0,"max":0},
 "cpu_pct_avg":0,"ws_bytes_in":0,"ws_bytes_out":0,"commands":0}
```
Update `capacity.md` rows "CPU per lab" and "Bandwidth per lab" with measured values.

## Non-goals

- Django page (Phase 6 copies the dev page's protocol handling).
- Multi-session load (Phase 4).
- `samples` table writes (Phase 7); P1 sampling is done by the shell script, not by `labd`.

## Handoff

- `labd/internal/term/README.md` documents the protocol as implemented, with any deviation from the spec table called out and ADR'd.
- STATUS.md updated with the P1 numbers in the log entry.
