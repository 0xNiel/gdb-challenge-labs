# 0015 — web mints WebSocket tokens; labd mints only when `dev_mint_tokens` is on

Date: 2026-10-01 · Status: accepted · Phase: 6 · Refines: plan 6 "Token minting", ADR 0012

## Context

The spec puts token issue in web: the browser fetches a fresh token from web, which re-checks the Django session, so a session cookie is needed to open a terminal (spec "Terminal gateway → Auth"). Until Phase 6, labd minted one in every `POST /internal/sessions` response so Phases 3 and 4 could work without web.

Plan 6 says to remove that. Three tools still rely on it: `labd-perf` (P1–P9, rerun on the VPS in Phase 8 task 8.8), the `replay` client, and the `/dev/term` test page. Removing it would mean teaching each of them to mint, with a copy of the key, for no gain in production.

The minting itself is not the risk. The internal API is loopback-only and needs the bearer secret (S11); a caller that has the secret can already start and stop any session. What matters is that production's browser path goes through web.

## Decision

- **web mints** every token the browser uses: `GET /lab/<slug>/session/token` checks the Django session and that the session row belongs to the user, then mints with `WS_TOKEN_KEY`. Same format as ADR 0012.
- **labd mints in `POST /internal/sessions` only when `dev_mint_tokens: true`.** The default is off and `deploy/labd.prod.yaml` leaves it off; `labd/labd.dev.yaml` turns it on, as it does `dev_testpage`. With it off, the response's `ws_token` is `""`. labd logs a warning at start when it is on.
- **Shared vectors.** `challenges/schema/ws_token_vectors.json` holds tokens with fixed nonces, computed with `openssl` and coreutils, not with either implementation. Go (`labd/internal/term`) and Python (`web/labs/tokens.py`) must reproduce each one, and Go must verify each before its expiry.
- A perf run on the VPS (Phase 8) uses a config with `dev_mint_tokens: true` for the run only, the same way it would need `dev_allow_no_origin`.

## Consequences

- The response field stays, so nothing in the internal API's shape changes. Production web ignores it.
- One key, two minters: if web's and labd's minting ever disagree, the vectors fail in one of the two test suites.
- Revisit if labd-perf moves to driving the web app instead of labd directly; then `dev_mint_tokens` can go.
