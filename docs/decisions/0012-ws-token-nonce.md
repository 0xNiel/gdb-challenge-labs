# 0012 — WebSocket tokens carry a random nonce

Date: 2026-09-30 · Status: accepted · Phase: 3 · Refines: spec "Terminal gateway → Auth", invariant S12

## Context

The spec's token is HMAC-SHA256 over `session_id|user_id|exp`, valid 60 s and consumed on first use. `exp` has one-second resolution, so two tokens for the same session and user minted in the same second are byte-identical. The gateway then rejects the second one as already used. This happens in practice: two browser tabs, or a reconnect right after a drop, both fetch a token within a second. Phase 3's tests hit it at once.

## Decision

- The payload is `session_id|user_id|exp_unix|nonce`, where `nonce` is 8 random bytes in base64url. The token is `base64url(payload) "." base64url(HMAC-SHA256(key, payload))`.
- Everything else in S12 stands: 60 s expiry, single use, session id must match, key `WS_TOKEN_KEY` shared by web (mints, Phase 6) and labd (verifies).
- The exact format is specified in `labd/internal/term/README.md`. Phase 6's Python minter must follow it and is tested against a Go-minted token.

## Consequences

- Any number of tokens can be minted per second, and each is still single use.
- The used-token set grows by one entry per connection for 60 s. The sweep removes expired entries.
