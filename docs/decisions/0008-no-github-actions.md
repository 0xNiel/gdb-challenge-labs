# 0008 — No GitHub Actions until the owner says otherwise

Date: 2026-09-27 · Status: accepted · Phase: all · Supersedes: Phase 0 task 0.7, Phase 5 task 5.13

## Context

The owner removed the CI workflow after the first push because Actions minutes are limited. The plan had CI in Phase 0 (tests on push) and Phase 5 (challenge image builds and a weekly base image rebuild).

## Decision

- **Do not add, re-add or modify anything under `.github/workflows/`** until the owner explicitly asks. This includes "small" workflows, scheduled jobs and `workflow_dispatch` jobs. `.github/workflows/` holds only a README explaining this.
- Every check runs locally through `run.sh`. Before pushing, on your machine:
  ```
  ./run.sh check && ./run.sh test --all && ./run.sh lint
  ```
  Anything touching containers, images or the sandbox spec also needs `./run.sh test --integration` on the x86-64 Linux laptop (ADR 0001).
- The challenge build pipeline (Phase 5) is `scripts/challenge-build.sh` and `scripts/challenges-json.sh`, run by a developer. The spec allows this ("GitHub Actions or a local script, both produce identical output"). Pushing to GHCR uses a personal token from the developer's environment.
- Rebuilding `labbase` is a manual step (`./run.sh images labbase`), done when its Dockerfile changes and monthly for security updates. It is listed in the Phase 8 runbook.
- Where a phase document or ADR mentions CI, read it as "the local equivalent".

## Consequences

- No Actions minutes are used.
- Nothing enforces checks on push. The phase gates and the "run before push" line above are the only guard, so reviewers should ask for gate output.
- Two developers must run the same commands. `run.sh` keeps them identical.
- Revisit when the owner wants CI; the previous `ci.yml` is in git history (commit `482be38`).
