# Conventions

These apply to every phase. A phase document may add rules; it may not relax these.

## Repository

- One monorepo. Directory layout is in `CLAUDE.md`; do not add top-level directories without an ADR.
- Branch per phase: `phase-N-<slug>`. Merge to `main` only when the gate passes.
- Commit subject: `[PN] <area>: <what changed>` where area is `labd`, `web`, `images`, `challenges`, `deploy`, `docs`, `perf`. Body explains why, and names any new dependency and why it was needed.
- Never commit secrets. `DEPLOY_SECRET`, `LABD_INTERNAL_SECRET`, `WS_TOKEN_KEY`, registry tokens and DSNs with passwords live in `.env` files (ignored) or `/etc/labd/*.env` on the box. `deploy/env.example` documents every variable.

## Go (`labd/`)

- `go` directive in `go.mod` is whatever our dependencies require (1.26.6 since containerd v2.4.1); `provision.sh` installs at least that version. Module path `gdblabs/labd`.
- Standard library first. Allowed third-party modules without an ADR:
  - `github.com/containerd/containerd/v2` (client), `github.com/containerd/errdefs`
  - `github.com/opencontainers/runtime-spec`
  - `github.com/jackc/pgx/v5`
  - `github.com/coder/websocket`
  - `gopkg.in/yaml.v3`
  - `golang.org/x/sync`, `golang.org/x/time/rate`, `golang.org/x/sys`
  - Anything else needs a one-line ADR entry in `docs/decisions/0007-go-dependencies.md` (create it when first needed).
- Layout: `cmd/labd`, `cmd/labd-perf`, `internal/<pkg>`. Nothing outside `internal/` except `cmd/`. Packages are named by responsibility: `orch`, `term`, `metrics`, `config`, `api`, `store`.
- Every blocking call takes a `context.Context`. No package-level mutable state. Errors are wrapped with `fmt.Errorf("...: %w", err)`.
- The containerd client is used only behind the `orch.Runtime` interface so tests use a fake. Nothing outside `internal/orch` imports containerd.
- Tests: table-driven, `t.Parallel()` where safe, `testdata/` for golden files. Integration tests carry `//go:build integration` and are run only inside the VM via `./run.sh test --integration`.
- Logging: `log/slog`, JSON handler in production, text in dev. Every log line for a session carries `session_id`.
- Config comes from `labd.yaml` plus environment overrides for secrets only. No flags for things that belong in config.
- `gofmt` and `go vet` clean; `staticcheck` clean when installed.

## Python (`web/`)

- Python 3.13 managed by `uv`; `web/pyproject.toml` pins Django 5.2 LTS. `uv sync` is the only install step.
- Apps as in the spec: `accounts`, `curriculum`, `labs`, `progress`, `analytics`, `adminpanel`. Project settings package is `web/config/`.
- Allowed third-party packages without an ADR: `django`, `psycopg[binary]`, `gunicorn`, `django-allauth`, `markdown-it-py`, `nh3` (HTML sanitiser), `pytest`, `pytest-django`, `ruff`, `playwright` (dev only). Anything else needs an ADR entry.
- Tables owned by `labd` (`sessions`, `events`, `samples`) are represented in Django as models with `managed = False`. Django migrations never touch them. See ADR 0003.
- No business logic in templates. HTMX partials live under `templates/<app>/partials/`.
- No CDN. xterm.js and Chart.js are vendored under `web/static/vendor/` with the version in the file name.
- `ruff check` and `ruff format` clean. Tests with `pytest-django`, settings module `config.settings.test`.

## Shell

- `#!/usr/bin/env bash`, `set -euo pipefail`, `shellcheck` clean.
- Scripts print what they are about to do with a `==>` prefix and fail loudly. Idempotent where they touch a system (`provision.sh`).

## Images and challenges

- Every image is pinned by digest, never by tag, once it leaves the dev machine.
- `challenges/<tier>/<NN-slug>/` is the only place challenge content lives. Nothing is built on the VPS.
- Build flags for every tier include `-static -no-pie -fno-pie` (ADR 0010: gVisor cannot turn off ASLR, so libc must not be a shared library). Stack and heap addresses still change on every run; no exercise may depend on them. `SOURCE_DATE_EPOCH` is set. Builds are reproducible: building twice yields the same binary hash, and the gate checks it.

## Documentation

- Plan documents describe *what* and *done when*; they link to the spec for *why*.
- ADRs use the template in `docs/decisions/README.md` and are numbered sequentially.
- Metrics go in `docs/metrics/` as JSON plus a Markdown summary. File names carry the date and the host: `perf-report-2026-10-12-hostinger.json`.
- `docs/STATUS.md` is updated at the end of every session, including failures.
