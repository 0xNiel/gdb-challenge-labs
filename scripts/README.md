# scripts

Helpers called by `run.sh` and the Makefile. All bash, `set -euo pipefail`, shellcheck clean.

| Script | Purpose | Phase |
| --- | --- | --- |
| `gate.sh <phase>` | Exit test per phase; mirrors the "Gate" section of each phase document | 0, wired per phase |
| `challenge-build.sh <dir> [--push]` | Lint, flag, compile, leak check, oracle, image, digest write-back | 5 |
| `challenges-json.sh` | Generate `challenges.json` from all manifests | 5 |
| `challenge-new.sh <tier> <NN-slug>` | Scaffold a challenge from `challenges/TEMPLATE` | 5 |
| `deploy.sh` | Pull, build, migrate, import, pull images, restart (on the VPS) | 8 |
| `gen-secrets.sh` | Generate the env files with random secrets | 8 |
| `restore-drill.sh` | Restore the latest backup into the dev VM and verify | 8 |
