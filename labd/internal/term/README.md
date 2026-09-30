# Terminal gateway protocol (as implemented)

The spec's "Terminal gateway" section is the source; this file records exactly what labd does, including where it adds to or refines the spec. Phase 6's lab page and web's token minter are built against this file. `labd/testpage/index.html` is a working client in about 100 lines.

## Endpoint

`GET /ws/term/{session_id}?t=<ws_token>` on `listen_ws` (loopback; Caddy proxies it in production).

The handshake checks, in this order:

| Check | Failure |
| --- | --- |
| `Origin` equals `site_host` exactly (scheme and host). A missing `Origin` is accepted only with `dev_allow_no_origin: true`. Origin goes first so a cross-site page cannot burn a user's single-use token | 403 |
| Token signature, expiry, session id, not used before | 401 |
| Session exists and is live (queued, creating or running) | 404 |
| The token's user owns the session | 401 |

Then the socket is upgraded. Compression is off, and incoming messages are capped at 64 KiB (ADR 0004).

## Token (S12, ADR 0012)

```
payload = session_id "|" user_id "|" exp_unix "|" nonce        nonce = base64url(8 random bytes)
token   = base64url(payload) "." base64url(HMAC-SHA256(WS_TOKEN_KEY, payload))
```

`base64url` is RFC 4648 URL-safe with no padding. `exp_unix` is the mint time plus 60 s. A token verifies once; replays are rejected until it expires. Until Phase 6, `POST /internal/sessions` returns a fresh token on every call. Calling it again for a live session returns the same session with a new token, which is how a client reconnects.

## Frames

Binary frames carry terminal bytes both ways. Text frames carry JSON control messages.

| Direction | Frame | Behaviour |
| --- | --- | --- |
| client → server | binary | Keystrokes, after the input limit. They count as activity (reset the idle timer) and feed command capture. Ignored while the lab is queued or creating |
| client → server | `{"type":"resize","cols":C,"rows":R}` | `task.Resize`; 1 ≤ C, R ≤ 1000, otherwise ignored |
| client → server | `{"type":"extend"}` | Answered with `{"type":"extend","ok":true}` the first time, `{"ok":false}` after. Extends the idle window by `extend_minutes` for the rest of the session; the hard TTL does not move |
| client → server | `{"type":"ping"}` | No reply and not activity. Keeps proxies from closing an idle socket; the dev page pings every 25 s |
| client → server | anything else | Ignored and counted (`Server.UnknownFrames`) |
| server → client | binary | Terminal output, in frames of at most 32 KiB. After `state: running`, the first frames replay up to 64 KiB of scrollback |
| server → client | `{"type":"queued","position":n}` | On connect while queued, and whenever the position changes (checked every second) |
| server → client | `{"type":"state","state":"running"}` | When the terminal is attached |
| server → client | `{"type":"state","state":"ended","reason":"..."}` | Just before the socket closes with 1000 `ended`. Reasons: `idle_timeout`, `hard_ttl`, `ws_closed`, `user_stop`, `admin_kill`, `solved`, `task_exited`, `queue_timeout`, `create_failed` |
| server → client | `{"type":"ttl","idle_remaining_s":n,"hard_remaining_s":n,"extend_available":b}` | Once on attach, then every 30 s. The UI counts down between frames and shows Extend at 2 minutes left |
| server → client | `{"type":"warn","message":"..."}` | Keystrokes were dropped by the input limit; at most one per second |

## Limits (S13)

| Limit | Value | On excess |
| --- | --- | --- |
| Input, per session (survives reconnects) | 2 KiB/s sustained, 16 KiB burst | Excess bytes dropped; `warn` frame |
| Output, per connection | 256 KiB/s, 32 KiB burst | Paced |
| Output buffer, per connection | 256 frames | The terminal waits (backpressure). If the buffer stays full for 10 s, the socket closes with **1008 `slow_consumer`**; the lab keeps running for the reconnect grace |
| Connections per session | 1 | A new connection closes the old one with **1000 `replaced`** |

## Reconnect grace

When the socket closes for any reason other than being replaced, a running lab waits `ws_reconnect_grace_s` (60 s) and then ends with reason `ws_closed`. A new connection within the grace cancels it and gets the scrollback. After a labd restart, adopted labs start in this same grace.

## Command capture (S19)

Every line typed is recorded as an event `command_entered {seq, line[, truncated]}`. The lines are written in batches every second or every 100 events. `seq` starts at 1 per session and survives reconnects. The rules:
- CSI, OSC and SS3 escape sequences and other control bytes are dropped.
- Backspace (0x7f or 0x08) deletes a rune; Ctrl-U clears the line.
- CR or LF ends a line, and empty lines are not recorded.
- Ctrl-C is recorded as `^C`.
- A line over 4 KiB is recorded truncated.

This is what was typed, not what gdb ran; history recall is invisible. Raw output is not stored. `sessions.commands` and `lab_ended.data.commands` hold the count.

**Correction to the plan:** Phase 3 task 3.5 gives `"ne\x7fxt\n" → next`. Under its own rule, "backspace deletes the previous rune", that input yields `nxt`. The implementation follows the rule, and the tests say so.

## Close codes

| Code | Reason | When |
| --- | --- | --- |
| 1000 | `ended` | The session ended; preceded by `state: ended` |
| 1000 | `replaced` | Another connection took over |
| 1008 | `slow_consumer` | Output stayed backed up for 10 s |
| 1000 | (empty) | Server shutdown or client close |
