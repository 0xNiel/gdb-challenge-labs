# Phase 8 — Production, tiers 2–3, private beta

| | |
| --- | --- |
| Depends on | Phases 4 and 7 |
| Unblocks | beta |
| Spec sections | "Deployment on the Hostinger VPS" (entire), "Lab images → Registry", "Curriculum ladder" tiers 2–3, "Success criteria", "Open questions & risks" |
| Effort | one week for the box; content in parallel |

**Status: optional, deferred by the owner on 2026-10-01 (ADR 0017).** Nothing depends on this phase. The capacity answer for the target VPS comes from Phase 7's laptop run (`docs/metrics/vps-capacity.md`). When a VPS exists, start with 8.1 and 8.8.

## Objective

The platform runs on the Hostinger VPS behind Caddy with TLS, all as systemd units, provisioned by one idempotent script, backed up nightly off-box, with tiers 1–3 (≥ 15 challenges) imported. Ten beta users use it. The capacity numbers are re-checked on the production box itself.

## Design fixed by this document

- **Provisioning**: `deploy/scripts/provision.sh --role prod` completes the stubs from Phase 0: users `web`, `labd`, `postgres`; group `containerd` with socket 0660 root:containerd; `labd` in that group; containerd registers only `io.containerd.runsc.v1`; `runc` binary absent; `hosts.toml` for `ghcr.io` with the read-only PAT from `/etc/labd/ghcr.env`; Caddy from the official repo; `ufw` 22/80/443 (SSH port from `SSH_PORT` env, default non-standard); `fail2ban` sshd jail; unattended-upgrades security only; Postgres `shared_buffers=2GB`, local-only, roles `web` and `labd` (labd read-only on `auth_user`... the Django user table name is `auth_user` unless allauth changes it; confirm and grant SELECT only).
- **Units** in `deploy/systemd/`: `web.service` (gunicorn 4 workers, unix socket `/run/web/web.sock`, `User=web`), `labd.service` (`Restart=always`, `LimitNOFILE=65536`, `After=containerd.service`, `User=labd`, `EnvironmentFile=/etc/labd/labd.env`), `containerd.service` (upstream), `rollup.timer`, `retention.timer`, `backup.timer` + `backup.service` (`pg_dump | restic backup --stdin`), `labd-pull.service` triggered by deploy.
- **Caddyfile** `deploy/caddy/Caddyfile`: site `{$SITE_HOST}`; `reverse_proxy /ws/term/* 127.0.0.1:8082` with WebSocket passthrough; `reverse_proxy unix//run/web/web.sock` for everything else; `request_body { max_size 1MB }`; `header` strips `X-Forwarded-*` from clients; `encode zstd gzip`; static files served by Caddy from `/srv/web/static`.
- **Deploy** `make deploy` on the box (`scripts/deploy.sh`): `git pull`, build `labd`, `uv sync`, `migrate`, `collectstatic`, `import_challenges challenges.json`, `labd pull`, restart `web` then `labd`; print `/healthz` results. Rollback is `git checkout <prev> && make deploy`.
- **Secrets**: `/etc/labd/labd.env` and `/etc/web/web.env` (root:group 0640) with `DEPLOY_SECRET`, `LABD_INTERNAL_SECRET`, `WS_TOKEN_KEY`, `DATABASE_URL`, `SITE_HOST`, `RESTIC_REPOSITORY`, `RESTIC_PASSWORD`, email provider keys. Generated once by `scripts/gen-secrets.sh`, never in git.
- **Backups**: `backup.service` nightly `pg_dump -Fc` piped to `restic`, retention 30 daily / 12 monthly. Restore drill: `scripts/restore-drill.sh` pulls the latest snapshot into the dev VM's Postgres and runs `manage.py check` + a row count comparison.
- **Content**: tiers 2 and 3 authored with the Phase 5 tooling: at least 5 challenges each (spec sample lists), each with a boss. Tier 2 build `-O2 -g` then selective `strip --strip-symbol`; tier 3 `-O1 -g -pthread`. Tier 3 needs the P0 `threads` and `signal` rows to have passed on x86-64.
- **Email**: pick a provider (QUESTIONS Q4 follow-up). Django `EMAIL_BACKEND` SMTP with credentials in `web.env`. Verification mandatory in prod.

## Deliverables

| File | Purpose |
| --- | --- |
| `deploy/scripts/provision.sh` (prod parts), `deploy.sh`, `gen-secrets.sh`, `restore-drill.sh` | Operations |
| `deploy/systemd/*.service`, `*.timer` | Units |
| `deploy/caddy/Caddyfile` | Proxy |
| `deploy/labd.prod.yaml`, `deploy/containerd/config.toml`, `runsc.toml`, `hosts.toml` | Config |
| `deploy/README.md` | Runbook: first install, deploy, rollback, drain for kernel update, restore, rotate `DEPLOY_SECRET` |
| `challenges/tier2-optimized-c/0[1-5]-*/`, `challenges/tier3-concurrency/0[1-5]-*/` | Content |
| `docs/metrics/perf-report-<date>-hostinger.json`, `capacity.md` updated | Production numbers |
| `docs/metrics/beta-week1.md` | Funnel and capacity observations after one week |

## Tasks

### 8.1 First checks on the box
`grep -c vmx /proc/cpuinfo; ls /dev/kvm; uname -r; stat -fc %T /sys/fs/cgroup; nproc; free -g; df -h`. Record in `docs/metrics/environment-hostinger.md`.
**Done when:** the file exists.

### 8.2 Provision prod
Run `provision.sh --role prod` twice.
**Done when:** second run is a no-op; `verify-runtime.sh` prints `runsc ok`; `ctr -n labs run --runtime io.containerd.runc.v2 ...` fails (runc absent); `sudo -u web ctr -n labs c ls` is denied (S10).

### 8.3 Secrets, units, Caddy
**Done when:** `systemctl status` shows all units active; `curl -I https://$SITE_HOST/healthz` is 200 with a valid Let's Encrypt cert; `curl http://127.0.0.1:8081/healthz` works locally and `curl https://$SITE_HOST/internal/stats` is 404 (not proxied).

### 8.4 Registry credentials and pull
GHCR PAT (read-only) in `/etc/labd/ghcr.env`; `hosts.toml`. `labd pull` on `challenges.json`.
**Done when:** every digest in `challenges.json` is in the content store with `lab.keep=true`; admin live shows 0 pending pulls.

### 8.5 Deploy script and first deploy
**Done when:** `make deploy` runs end to end; a signup with email verification works through the real provider; tier 1 lab 1 solved in production by the owner.

### 8.6 Hardening checks
**Done when:** `ufw status` shows only 22 (or custom port), 80, 443; `fail2ban-client status sshd` active; SSH password auth disabled; no Docker installed (`which docker` empty); `labd` and `web` run as their users (`ps -o user,cmd`).

### 8.7 Backups and restore drill
**Done when:** `backup.service` ran once successfully (`restic snapshots` shows 1); `scripts/restore-drill.sh` restores it into the dev VM and the user count matches.

### 8.8 Production perf check
Run `labd-perf` P2 with `--n 20 --hold 10m` from the box against itself (through Caddy: point the driver at `wss://$SITE_HOST/ws/term` with real tokens minted via a staff-only `POST /internal/perf-token` disabled after the run, or run against loopback and accept that Caddy is skipped; choose loopback for the driver and measure Caddy egress separately with `vnstat` or `/proc/net/dev` on the public interface during a 20-user human session). Then, in a maintenance window, run the full P2 at the production `max_sessions` once.
**Done when:** `perf-report-<date>-hostinger.json` exists; `capacity.md` "host" column now says `hostinger`; any delta versus the Phase 4 box is noted.

### 8.9 Tiers 2 and 3
Ten challenges via `challenge-new.sh`, following `challenges/README.md`. Each passes the build script and a human check.
**Done when:** `challenges.json` has ≥ 15 enabled challenges across 3 tiers; each has a STATUS.md human-check line.

### 8.10 Runbook
`deploy/README.md` covers: fresh install, deploy, rollback, drain and reboot for kernel updates (`max_sessions: 0`, wait for `active == 0`, reboot), restore, rotate `DEPLOY_SECRET` (rebuild all challenges, redeploy, all old writeups invalid), rotate GHCR PAT, what to do when `pending_pull > 0`.
**Done when:** a second person (or a fresh session) follows only the runbook to do a deploy.

### 8.11 Private beta
Invite 10 users. After one week write `docs/metrics/beta-week1.md` from the Phase 7 dashboards: signups, labs started, tier 1 funnel, median time to solve per challenge, peak concurrency, memory per lab in production, any abuse seen.
**Done when:** the file exists.

### 8.12 Wire the gate
**Done when:** `./run.sh gate --phase 8` exits 0 (run on the box).

## Tests

Provisioning idempotency; S10 socket access denial; Caddy routing (internal API not exposed); TLS; backup + restore; production P2; all challenge oracles on the production image digests (`scripts/challenge-build.sh --verify-only` against pulled images).

## Gate

```
./run.sh gate --phase 8   # on the VPS
```
1. All systemd units active; `/healthz` 200 over TLS.
2. `provision.sh --role prod` re-run is a no-op.
3. Internal API not reachable through Caddy; containerd socket not readable by `web`.
4. `restic snapshots` non-empty and `restore-drill.sh` succeeded within the last 7 days (timestamp file).
5. `challenges.json` ≥ 15 enabled; every digest present in the content store.
6. `perf-report-*-hostinger.json` exists.
7. `beta-week1.md` exists (this final check is allowed to be pending at the code-complete point; STATUS.md must say so).

## Metrics to record

Everything from Phase 4 re-measured on the real box, plus: TLS handshake time, Caddy egress per session, real-user echo latency distribution from `samples` (`ws.bytes_*`) and funnel data from `events`.

## Non-goals

- Multi-node, billing, tiers 4–8, alerting, blue/green.

## Handoff

- STATUS.md: MVP complete or the exact remaining items.
- `docs/QUESTIONS.md` all resolved or explicitly deferred to post-MVP with an owner.
- Open the post-MVP plan document `docs/plan/post-mvp.md` with the tier 4–8 roadmap from the spec table and whatever the beta surfaced.
