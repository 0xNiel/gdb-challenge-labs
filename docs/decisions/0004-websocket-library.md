# 0004 — WebSocket library: `github.com/coder/websocket`

Date: 2026-09-26 · Status: accepted · Phase: 3

## Context

The gateway needs a WebSocket server with binary and text frames, per-message read limits, context cancellation and close codes. The candidates are `gorilla/websocket` (widely used, maintenance revived in 2023 but slow), `coder/websocket` (formerly `nhooyr.io/websocket`; small, `context`-native, minimal API, actively maintained), and `golang.org/x/net/websocket` (deprecated for new work).

## Decision

Use `github.com/coder/websocket`. Read limit 64 KiB per message; compression disabled (terminal traffic is small and latency matters more than bytes). Ping/pong handled by the protocol's own JSON `ping` frame, not WebSocket control frames, so behaviour is identical through Caddy.

## Consequences

- One small dependency with a `context`-first API that fits the per-session goroutine design.
- If it were ever abandoned, the bridge touches it in one file (`internal/term/server.go`) and swapping is contained.
