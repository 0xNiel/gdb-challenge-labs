# Open questions for the owner

Each question has a **default in force**. Work proceeds under the default until the owner answers. When a question is answered, move it to the "Resolved" section with the date and, if the decision has consequences, write an ADR.

## Open

### Q10. Linux laptop specs, and where the 100-lab run happens

The Linux x86-64 laptop is now the reference dev host (Q1 resolved). P2/P3 need about 13 GB for 100 labs plus headroom, so the laptop's RAM and core count decide whether the full capacity run can happen there or only on the VPS.

**Known now (2026-09-27):** the laptop is an ASUS TUF Gaming F16, 16 vCPU, 15 GB RAM, Ubuntu 26.04.1, kernel 7.0, `/dev/kvm` present. At the spec's estimate of ~130 MB per lab plus ~3 GB for the system, 100 labs need ~16 GB before headroom, so **the full 100-lab run cannot happen on the laptop**. At 25 % headroom it fits roughly 60 labs (estimate; Phase 1 measures the real per-lab cost). `/dev/kvm` means the laptop can also benchmark `runsc --platform=kvm`, which the VPS likely cannot.

**Default in force:** Phase 4 runs P1–P9 on the laptop at whatever N fits with 25 % memory headroom (`labd-perf` refuses to exceed it), which establishes per-lab cost, gVisor overhead, churn, recovery and leak numbers. The 100-lab P2/P3 run happens on the VPS in Phase 8 if the laptop cannot host it. Please add the laptop's `nproc` and RAM to `docs/metrics/environment-linux-laptop.md` during Phase 0.

### Q13. gdb does not work under gVisor on arm64 (Mac developers)

P0 on the arm64 dev VM: any program started by gdb under gVisor crashes in the dynamic loader before `main` (10 of 10 runs, both gVisor platforms, any dynamically linked binary). The same gdb session passes 27 of 27 checks under runc. So a Mac developer cannot debug a lab under gVisor locally.

Options:
- (a) Mac developers exercise labs under runc (`specrun --runtime runc`, and a dev-only labd flag in Phase 2), and everything gVisor-specific is verified on an x86-64 host.
- (b) Run an x86-64 Lima VM under emulation for gVisor checks (correct but very slow).
- (c) Report upstream to gVisor and wait.

**Update 2026-09-29:** the x86-64 laptop's P0 passes every gdb check under gVisor. The crash is **arm64-only**, so production is not affected.

**Default in force:** (a). Mac developers use runc for gdb work, and gVisor-specific checks run on x86-64. File (c) upstream when convenient. This matters again only for the arm64 tier (post-MVP tier 6).

### Q14. Keep `set disable-randomization on` in the lab gdbinit?

Under gVisor it cannot work (ADR 0010), and gdb prints `warning: Error disabling address space randomization: Invalid argument` on every `run`. Under runc it works, but production never uses runc. Dropping the line silences the warning; keeping it matches the spec's `.gdbinit` list and does no harm.

**Default in force:** keep it. Lesson 1 explains the warning in one sentence. Recommendation: drop it before the beta if learners report the warning as confusing; that is a one-line change and a `labbase` rebuild.

### Q11. Should challenge images also be built for arm64?

Multi-arch challenge images would let a Mac developer play the labs inside the arm64 VM. But the lessons teach x86 registers and stack layouts, the oracle is x86-specific, and the perf numbers are x86-only anyway.

**Default in force:** challenge images are `linux/amd64` only. The `labbase` and `perf` images are multi-arch so the platform itself is exercised on both. Human checks of challenge content and the Phase 6 end-to-end run happen on an x86-64 host (the laptop). See ADR 0001.

### Q12. Second developer's environment

Unknown. `./run.sh doctor` now reports what their machine has and lacks, per OS and architecture, and `docs/ONBOARDING.md` walks them through setup.

**Default in force:** supported hosts are Linux (x86-64 or arm64) and macOS (via Lima). Windows only through WSL2 and untested. If they are on Windows, the Linux laptop or the VPS can be their lab host over SSH.

### Q2. GitHub organisation and GHCR namespace

Manifests record `ghcr.io/<org>/lab-<slug>@sha256:...`. The workflows and `labd pull` need the real namespace.

**Default in force:** placeholder `<org>` in docs; `GHCR_NAMESPACE` variable in `deploy/env.example`; nothing is pushed until it is set.

### Q3. Python and Django versions

The Mac has Python 3.14 as default. Django 5.2 LTS (supported to April 2028) officially supports Python 3.10–3.13. Django 6.0 supports 3.12–3.14 but is not LTS.

**Default in force:** Django 5.2 LTS on Python 3.13 via `uv`. See ADR 0002. Change only if you want to be on 6.x.

### Q4. Authentication library

Spec allows `django-allauth` or built-in auth. allauth gives email verification and password reset flows for free; built-in needs those written by hand.

**Default in force:** `django-allauth`, email-only (no social providers), console email backend in dev. Email provider itself deferred to Phase 8 as the spec says.

### Q5. Domain name and TLS host

Needed for the Caddyfile, the WebSocket Origin check, and allauth's site settings.

**Default in force:** `labs.example.com` placeholder read from `SITE_HOST` in `.env`.

### Q6. Off-box backup target

Nightly `pg_dump` + `restic`. Needs a destination: Backblaze B2, another provider's S3, or a home NAS over SSH.

**Default in force:** `restic` with the destination read from `RESTIC_REPOSITORY`; Phase 8 gate requires a successful backup and a restore into the dev VM, whatever the target.

### Q7. Repository name

The spec calls the monorepo `gdb-labs`; the folder is `labbing-platform`. Only the Go module path and workflow names care.

**Default in force:** keep the folder name; Go module path is `gdblabs/labd`; product name in the UI is "gdb Challenge Labs".

### Q8. Who executes the phases, and how

The plan is written so that a less capable model can execute one phase per session. Recommended cadence: one branch per phase, gate output pasted into STATUS.md, human review of the diff at each gate before merging to `main`.

**Default in force:** as above. Human review is the only step the plan cannot automate.

### Q9. Should the boss challenge (tier 1, lab 5) ship with zero hints even in the MVP?

The spec says yes. This will show up as a sharp drop in the tier 1 funnel, which is useful data but might frustrate early beta users.

**Default in force:** zero hints, as specified. Revisit after the beta funnel numbers exist (Phase 7 dashboards).

## Resolved

| Date | Question | Decision | ADR |
| --- | --- | --- | --- |
| 2026-09-27 | Q1 Where are authoritative numbers measured | Owner's Linux x86-64 laptop is the reference dev host; VPS final. Both arm64 (Mac/Lima) and x86-64 must stay green | 0001 (amended) |
| 2026-09-26 | Registry | GHCR private, local retention on VPS | spec |
| 2026-09-26 | Flags | Per-deploy HMAC of slug | spec, ADR 0005 |
| 2026-09-26 | Idle timeout | 15 min with one 15 min extension | spec |
| 2026-09-26 | Billing | All tiers free in MVP | spec |
| 2026-09-26 | binutils in image | `objdump`/`readelf`/`nm` in every tier | spec |
| 2026-09-26 | Command capture | Every line, 90-day raw retention | spec |
