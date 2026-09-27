# 0001 — Development environments: native Linux x86-64 is authoritative; macOS develops through an arm64 Lima VM; both must stay green

Date: 2026-09-26 (amended 2026-09-27) · Status: accepted · Phase: 0

## Context

Two developers with different machines. The owner has an Apple Silicon Mac and a Linux x86-64 laptop; the second developer's environment is unknown. Production is Linux x86-64 (Hostinger KVM). containerd and gVisor need Linux. gVisor supports arm64, so a native Lima VM on the Mac is fast for developing `labd`, the gateway and the Django app, but every challenge binary is x86-64 (`-no-pie` C for amd64), the lessons teach x86 registers, and per-lab memory and CPU under `runsc` differ by architecture. Emulating x86-64 under Lima is too slow to measure anything.

## Decision

- **Linux x86-64 is the reference environment.** The owner's Linux laptop is the day-to-day x86-64 host; the Hostinger VPS is the final one. `run.sh` treats a Linux host as the lab host directly: `vm up` provisions the machine with `deploy/scripts/provision.sh --role dev`, `vm verify`/`vm ssh` run locally, `--integration` tests run natively.
- **macOS develops through Lima.** `deploy/lima/labs-dev.yaml` defaults to the host architecture (arm64 on Apple Silicon). `LIMA_ARCH=x86_64` selects emulation for correctness-only checks. `run.sh` re-executes container-related commands in the VM automatically.
- **Both architectures must stay green.** `./run.sh test --all` passes on both; CI runs on x86-64. Anything touching containers, images or the sandbox spec is verified with `./run.sh test --integration` on an x86-64 Linux host before a phase gate. Gate scripts must not assume either architecture.
- **Images.** `labbase` and the `perf` image are built for `linux/amd64` and `linux/arm64` so the platform can be exercised on either. Challenge images are `linux/amd64` only; human checks of challenge content and the Phase 6 end-to-end test run on an x86-64 host.
- **Numbers.** Metrics files carry a host label: `linux-laptop`, `dev-vm`, `hostinger`. Only x86-64 hosts replace an *est.* in `capacity.md`. Capacity at 100 labs needs 32 GB; if the laptop has less, Phase 4 runs a reduced N there to establish per-lab cost and the full 100-lab run happens on the VPS in Phase 8 (QUESTIONS.md Q10).
- **One provisioning script** for the laptop, the VM and the VPS; only `--role` differs. `./run.sh doctor` reports, per host, what is present, missing and how to install it, so any new developer can self-serve.

## Consequences

- Fast inner loop on the Mac and real x86-64 numbers without renting anything before beta.
- Every container-related change is tested twice (arm64 VM and x86-64 laptop) before a gate. That is the cost of "runs on both".
- P0 must pass on the x86-64 laptop before Phase 5's gate; the arm64 run is informative only.
