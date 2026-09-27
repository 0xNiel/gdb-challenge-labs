# Phase 5 — Challenge pipeline and tier 1 content

| | |
| --- | --- |
| Depends on | Phase 1 (labbase, build image). Can run in parallel with Phases 2–4 |
| Unblocks | Phase 6 |
| Spec sections | "Lab images → Per-challenge layer, Flag mechanism, Build pipeline, Registry", "Curriculum ladder" tier 1 row, "First five lessons and labs" (entire), "Web app → Flag submission rules" (for the derivation) |
| Effort | two weeks; content writing dominates |

## Objective

Five tier-1 challenges exist as source, lesson, manifest, oracle and solution; a deterministic build script turns each into a pinned image that passes the leak check and the solve/no-solve oracle; `challenges.json` is produced; CI does the same on GitHub; `labd pull` retrieves the images. Flag derivation is specified to the byte with shared test vectors so Django (Phase 6) matches.

## Design fixed by this document

- **Flag derivation** (ADR 0005): `flag = "LAB{" + base32_std_nopad(HMAC_SHA256(key=DEPLOY_SECRET, msg=slug))[:24] + "}"`. Base32 is RFC 4648 standard alphabet, uppercase, padding stripped, first 24 characters. `DEPLOY_SECRET` is UTF-8 bytes; `slug` is the manifest slug as UTF-8. Implemented in Go (`labd/internal/flag`) and Python (`web/progress/flag.py`), both tested against `challenges/schema/flag_vectors.json` (10 vectors with a fixed test secret).
- **Flag embedding**: `tools/flagblob` (Go, `labd/cmd/flagblob`) takes the flag and a `key_expr` description from the manifest and emits `flag_blob.h`: `static const unsigned char FLAG_BLOB[N]`, `static const unsigned FLAG_SEED_MIX` and an inline `flag_decode(unsigned key, char *out)` that runs a 32-bit xorshift PRNG seeded with `key ^ FLAG_SEED_MIX` and XORs the blob. The challenge's `report(unsigned key)` calls it and prints `out`. With the wrong key it prints 28 bytes of garbage. `key_expr` in the manifest documents what runtime value is the key (e.g. `total`), and `solve.gdb` reaches it.
- **Manifest schema** `challenges/schema/manifest.schema.json`: exactly the spec's YAML keys, plus `key_expr` (string) and `build.flags` (string) and `build.entry` (binary name). `image` must match `^ghcr\.io/[a-z0-9-]+/lab-[a-z0-9-]+@sha256:[a-f0-9]{64}$` once set; empty allowed before first build.
- **Build script** `scripts/challenge-build.sh <challenge-dir> [--push] [--secret-env DEPLOY_SECRET]`:
  1. Lint manifest with the schema (`check-jsonschema` via `uv tool run`, or a Go validator in `labd/cmd/manifestlint`; choose the Go one to avoid a Python dependency in CI images).
  2. Compute flag; generate `flag_blob.h` into a temp build dir.
  3. Build in `images/build` container with the manifest flags plus mandatory `-no-pie -fno-pie`, `SOURCE_DATE_EPOCH=0`. Build twice; hashes must match.
  4. `strings -n 4 binary | grep -c 'LAB{'` must be 0. Also grep for the flag body without braces.
  5. Oracle: run `gdb -batch -x solve.gdb binary` under `runsc` with the sandbox spec; stdout must contain the flag. Run `gdb -batch -ex run binary` (no fix); stdout must not contain the flag.
  6. Assert `&main` identical across 3 runs (`gdb -batch -ex 'print &main'`).
  7. Build image `FROM labbase` copying `src/`, binary and `README.md` into `/opt/lab/`, `WORKDIR /opt/lab`, `USER lab`. Tag `lab-<slug>:<git-sha>`. Never copy `solve.gdb`, `solution.md`, `manifest.yaml`, `flag_blob.h`.
  8. With `--push`: push to `ghcr.io/$GHCR_NAMESPACE/lab-<slug>`, read the digest, write it into `manifest.yaml` `image:`.
  9. Without `--push` (dev): import into containerd namespace `labs` and write `image:` as `local/lab-<slug>@sha256:<digest>` — accepted by the schema only when `ALLOW_LOCAL_IMAGES=1`.
- **`scripts/challenges-json.sh`** walks `challenges/tier*/*/manifest.yaml` and emits `challenges.json` `{version, generated_at, challenges:[{slug,title,tier,order,difficulty,estimated_minutes,image,limits,hints,tags,enabled:true,lesson_path,solution_path}]}` sorted by tier then order. `lesson.md` and `solution.md` are read by Django's import command from the repo checkout on the box; the JSON carries paths, not bodies.
- **CI** `.github/workflows/challenges.yml`: on push to `main` touching `challenges/**` or `images/**`, compute changed challenge dirs (`git diff --name-only HEAD~1`), build each with `--push`, commit updated manifests and `challenges.json` back on a `bot/challenges-<sha>` branch and open a PR. `labbase.yml`: on `images/labbase/**` change or weekly cron, rebuild labbase, push, then trigger `challenges.yml` for all challenges.
- **`labd pull` and `labd prune`** completed: pull by digest from GHCR using containerd's `hosts.toml` credentials; prune removes digests not referenced by any enabled challenge and older than 14 days.

## Deliverables

| File | Purpose |
| --- | --- |
| `challenges/schema/manifest.schema.json`, `flag_vectors.json` | Contract |
| `labd/internal/flag/flag.go`, `flag_test.go` | Go derivation |
| `labd/cmd/flagblob`, `labd/cmd/manifestlint` | Build tools |
| `images/build/Dockerfile` | Pinned gcc toolchain (from Phase 1, now with `strings`, `gdb` for oracle) |
| `scripts/challenge-build.sh`, `scripts/challenges-json.sh`, `scripts/challenge-new.sh <tier> <NN-slug>` | Tooling; `challenge-new.sh` scaffolds a dir from a template |
| `challenges/tier1-c-fundamentals/01-off-by-one/` … `05-stack-overwrite/` | Each: `manifest.yaml`, `lesson.md`, `src/main.c`, `README.md` (shown in the lab as `/opt/lab/README.md`), `build.sh`, `solve.gdb`, `solution.md` |
| `challenges/TEMPLATE/` | The scaffold |
| `challenges/README.md` | Authoring guide: design rules from the spec, hint escalation, how to run the oracle locally |
| `challenges.json` (repo root, generated) | Import artifact |
| `.github/workflows/challenges.yml`, `labbase.yml` | CI |
| `labd/internal/orch/pull.go`, `prune.go` and tests | Registry logic |

## Tasks

### 5.1 Flag derivation and vectors
Write `flag_vectors.json` by hand-computing with `openssl dgst -sha256 -hmac` + `base32` for 10 slugs and secret `test-secret-do-not-use`. Implement Go; test against vectors.
**Done when:** Go test passes all 10 vectors and a property test that output always matches `^LAB\{[A-Z2-7]{24}\}$`.

### 5.2 flagblob and decode header
**Done when:** a Go test compiles a tiny C program with the generated header via the build image (integration tag) and asserts correct key → flag, wrong key → not flag and no `LAB{` in `strings`.

### 5.3 Manifest schema and lint
**Done when:** `manifestlint` accepts the five manifests and rejects: missing `slug`, `slug` not matching dir, `image` with a tag instead of digest, `limits.memory_mb` > 1024, hint `cost` outside 0–3.

### 5.4 Build image
Extend `images/build/Dockerfile`: `gcc`, `binutils`, `gdb`, `make`, pinned by digest, `SOURCE_DATE_EPOCH` default.
**Done when:** building the perf program twice gives the same hash (already asserted in Phase 1; re-run).

### 5.5 Build script
**Done when:** `scripts/challenge-build.sh challenges/TEMPLATE` (a trivial passing challenge) succeeds end to end in dev mode, and deliberately putting a literal flag string in `main.c` makes step 4 fail, and deliberately breaking `solve.gdb` makes step 5 fail.

### 5.6–5.10 The five challenges
One task each, in order. For each: write `src/main.c` (< 120 lines) following the spec's table row exactly (bug, required state, `report()` key); `manifest.yaml` with limits, hints (3 escalating; lab 5 none), `key_expr`; `solve.gdb` reproducing the intended gdb path; `lesson.md` (~10 min read) covering the listed gdb concepts; `solution.md` walkthrough; `README.md` for inside the lab (what the program should do, how to run it, no hints). Run the build script.

| Task | Challenge | Key for `report()` | Oracle path |
| --- | --- | --- | --- |
| 5.6 | `01-off-by-one` | `total` (true sum) | break after loop, `set var total = <sum>`, `continue` |
| 5.7 | `02-null-deref` | `u->id` | run to crash, `up`, break on compare, `finish`, `set var u = &users[2]`, `continue` |
| 5.8 | `03-uninitialized` | final `price` | `watch found`, `set var found = 0`, `continue` |
| 5.9 | `04-unterminated` | `strlen(name)` and first 8 bytes | `x/16xb name`, `set {char}(name+8) = 0`, `continue` |
| 5.10 | `05-stack-overwrite` | `checksum` before copy | `watch checksum`, break before copy, `print checksum`, after copy `set var checksum = <saved>`, `continue` |

**Done when (each):** build script passes all steps; a human runs the lab in the Phase 3 dev page and confirms the bug is visible from a plain `run`, the hints escalate correctly, and the intended path works without the solution; recorded in STATUS.md.

### 5.11 challenges.json generator
**Done when:** `scripts/challenges-json.sh > challenges.json` validates against `challenges/schema/challenges.schema.json` (write it) and lists five enabled challenges in order.

### 5.12 `labd pull` and `prune`
**Done when:** unit tests with a fake content store: pull skips present digests, pulls missing, labels `lab.keep`; prune keeps referenced, keeps unreferenced younger than 14 days, removes older unreferenced. Integration: `labd pull` against local images succeeds.

### 5.13 CI workflows
**Done when:** a push touching one challenge triggers a build of only that challenge (visible in the Actions log) and opens a PR with the digest. Needs Q2 answered; until then `--push` is skipped in CI and the workflow builds and validates only.

### 5.14 Authoring guide
`challenges/README.md`: the five design rules from the spec, hint ladder, how to write `solve.gdb`, how to run the oracle locally, how to test in the dev page, what never goes into the image.
**Done when:** a new challenge scaffolded with `challenge-new.sh` and written by following only the README passes the build script (do this with a sixth throwaway challenge, then delete it).

### 5.15 Wire the gate
**Done when:** `./run.sh gate --phase 5` exits 0.

## Tests

Go unit: flag vectors, manifestlint cases, pull/prune. Integration: flagblob compile test, build script on TEMPLATE and on all five. Content: oracle solve/no-solve per challenge, leak check, `&main` stability, reproducible hash. Human: each lab played once.

## Gate

```
./run.sh gate --phase 5
```
1. `go test ./...` green.
2. `scripts/challenge-build.sh` on each of the five challenges exits 0 (steps 1–7, dev mode).
3. `challenges.json` validates and has 5 entries.
4. `flag_vectors.json` passes in Go (Python check added to the Phase 6 gate).
5. The x86-64 P0 run from Phase 1 exists (`docs/metrics/p0-*-<non-dev-vm host>.json`) — the oracle results on arm64 do not count for x86-specific lessons.
6. STATUS.md has five lines `Phase 5 human check <slug>: <date> <who> OK`.

## Metrics to record

Per challenge: image layer size over labbase, build time, oracle wall time under runsc. Record in `docs/metrics/challenges-<date>.md`.

## Non-goals

- Tiers 2 and 3 (Phase 8).
- Django import (Phase 6).
- Hints UI (Phase 6).

## Handoff

- STATUS.md; `challenges/README.md` is the authoring reference for Phase 8 content.
- QUESTIONS.md Q2 answered or still defaulted; note which.
