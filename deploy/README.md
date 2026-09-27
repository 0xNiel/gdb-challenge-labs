# deploy

Everything needed to stand up an environment. One script provisions all three environments; only `--role` differs (ADR 0001).

| Path | Purpose | Phase |
| --- | --- | --- |
| `lima/labs-dev.yaml` | Lima template for the dev VM (rendered by `run.sh vm up`) | 0 |
| `scripts/provision.sh --role dev\|prod` | Idempotent installer: containerd 2.x, runsc, Postgres 16, Go, uv; prod adds Caddy, users, firewall, fail2ban | 0, 8 |
| `scripts/verify-runtime.sh` | Runs a container under runsc in namespace `labs`; asserts cgroup v2 | 0 |
| `env.example` | Every environment variable, documented | 0 |
| `containerd/config.toml`, `runsc.toml`, `hosts.toml` | containerd and gVisor configuration templates | 0, 8 |
| `systemd/*.service`, `*.timer` | web, labd, rollup, retention, backup, labd-pull | 7, 8 |
| `caddy/Caddyfile` | TLS, routing, WebSocket passthrough, body limit | 8 |
| `labd.prod.yaml` | Production labd config; `max_sessions` from the Phase 4 ADR | 4, 8 |

The operations runbook (install, deploy, rollback, drain, restore, secret rotation) is written in Phase 8, task 8.10, into this file.
