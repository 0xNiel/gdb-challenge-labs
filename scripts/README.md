# scripts

Helpers called by `run.sh` and the Makefile. All bash, `set -euo pipefail`, shellcheck clean.

| Script | Purpose | Phase |
| --- | --- | --- |
| `gate.sh <phase>` | Exit test per phase; mirrors the "Gate" section of each phase document | 0, wired per phase |
| `labs.sh ls\|clean\|preflight` | Labs left in namespace `labs`: list, remove (refused while a labd runs), or fail fast before a gate or perf run (`./run.sh labs ...`) | 3 |
| `with-containerd-group.sh CMD` | Run CMD with the `containerd` group, not root (labd, integration tests, perf) | 2 |
| `challenge-build.sh <dir> [--push]` | Lint, flag, compile, leak check, oracle, image, digest write-back | 5 |
| `challenges-json.sh` | Generate `challenges.json` from all manifests | 5 |
| `challenge-new.sh <tier> <NN-slug>` | Scaffold a challenge from `challenges/TEMPLATE` | 5 |
| `deploy.sh` | Pull, build, migrate, import, pull images, restart (on the VPS) | 8 |
| `gen-secrets.sh` | Generate the env files with random secrets | 8 |
| `restore-drill.sh` | Restore the latest backup into the dev VM and verify | 8 |
