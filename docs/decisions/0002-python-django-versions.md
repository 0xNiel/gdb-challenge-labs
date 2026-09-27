# 0002 — Django 5.2 LTS on Python 3.13, managed with uv

Date: 2026-09-26 · Status: accepted · Phase: 0

## Context

The spec says "Django 5". The dev machine defaults to Python 3.14. Django 5.2 is the current LTS (supported to April 2028) and officially supports Python 3.10–3.13. Django 6.0 supports 3.12–3.14 but is not LTS. Ubuntu 24.04 ships Python 3.12.

## Decision

- Django `5.2.*`, Python `3.13`, both pinned in `web/pyproject.toml` and `.python-version`.
- `uv` is the only Python tool used: `uv sync` installs, `uv run` executes, `uv tool run` for one-off tools. No `pip`, no `venv` by hand, no `requirements.txt`.
- The provisioning script installs `uv`, and `uv` installs Python 3.13 on the box; the distro Python is not used for the app.
- Revisit when Django 6.2 LTS ships (expected 2027) or if a required library drops 3.13.

## Consequences

- Same Python everywhere (Mac, VM, VPS) regardless of what the OS ships.
- Slightly older Python than the Mac default; nothing in the spec needs 3.14 features.
