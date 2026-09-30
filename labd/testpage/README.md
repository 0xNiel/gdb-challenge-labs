# Dev test page

`index.html` is a minimal browser client for labd's terminal gateway, served at `/dev/term` on `listen_ws` only when `dev_testpage: true` (never in production). It implements the protocol in `labd/internal/term/README.md` and is the reference for the Phase 6 lab page.

## Vendored files

Downloaded from the npm registry on 2026-09-30. No CDN at runtime (CONVENTIONS).

| File | Package | sha256 |
| --- | --- | --- |
| `vendor/xterm-5.5.0.js` | `@xterm/xterm` 5.5.0, `lib/xterm.js` | `1f991ac3b4b283ebf96e60ae23a00a52765dd3a2e46fa6fdda9f1aab032f7495` |
| `vendor/xterm-5.5.0.css` | `@xterm/xterm` 5.5.0, `css/xterm.css` | `ba8e6985669488981ccf40c0cefe3aba80722cb6c92de7ad628b0bd717faf2b6` |
| `vendor/addon-fit-0.10.0.js` | `@xterm/addon-fit` 0.10.0, `lib/addon-fit.js` | `bdaefa370b1bfc42ee88d46fe6072400902a4d4b2d45cd93438dda9b23c97089` |
| `vendor/LICENSE-xterm.txt` | MIT licence of both packages (same authors) | `b569f629d00f2626a8100df2a1798210535621e42164dfd426a6fe5aac7b0ccd` |

## Using it

```
LABD_INTERNAL_SECRET=dev WS_TOKEN_KEY=dev ./run.sh labd        # labd.dev.yaml has dev_testpage: true
curl -s -H 'Authorization: Bearer dev' -d '{"user_id":1,"challenge_slug":"perf"}' \
  http://127.0.0.1:8081/internal/sessions                      # -> session_id, ws_token
open "http://127.0.0.1:8082/dev/term?session=<session_id>&t=<ws_token>"   # within 60 s
```

The token is single use and valid for 60 s. POST again for a fresh one (the same session comes back).
