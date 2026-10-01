# Phase 5 — Challenge pipeline and tier 1 content

| | |
| --- | --- |
| Depends on | Phase 1 (labbase, build image). Can run in parallel with Phases 2–4 |
| Unblocks | Phase 6 |
| Spec sections | "Lab images → Per-challenge layer, Flag mechanism, Build pipeline, Registry", "Curriculum ladder" tier 1 row, "First five lessons and labs" (entire), "Web app → Flag submission rules" (for the derivation) |
| Effort | two weeks; content writing dominates |

## Objective

Five tier-1 challenges exist as source, lesson, manifest, oracle and solution; a deterministic build script turns each into a pinned image that passes the leak check and the solve/no-solve oracle; `challenges.json` is produced; CI does the same on GitHub; `labd pull` retrieves the images. Flag derivation is specified to the byte with shared test vectors so Django (Phase 6) matches.

## As built (2026-10-01) — read this before the design below

- **Flag** (ADR 0005, amended): the flag is 29 bytes, so `FLAG_BLOB` is 30. `FLAG_SEED_MIX` is derived from the secret and slug (`flag.SeedMix`), not random, so two builds are byte-identical. The vectors were computed with `openssl` and coreutils `base32`.
- **Manifest**: adds `key_value` (the correct runtime key, flagblob's `--key`; never in the image) and `boss` (no hints). `manifestlint` applies `manifest.schema.json` itself, through a small validator for the keywords it uses, with no new dependency, plus the directory, boss and hint-order rules.
- **flagblob test** compiles the generated header with the host's `cc` (it skips without one) instead of an integration test in the build image. The build script repeats the leak check on every real binary.
- **Build script**:
  - Images are built for the **lab host's architecture**. On the laptop that is x86-64 with the oracle under runsc, which is authoritative. On a Mac it is arm64 in the VM with the oracle under runc (Q13), which is the authoring loop. `--push` refuses anything but amd64.
  - The **oracle runs in the lab image itself** under the sandbox spec (`specrun`), as uid 1000. `solve.gdb` is typed in through the terminal into `/tmp` and ended by ^D, so it is never in any image.
  - Each challenge compiles with its directory mounted at `/opt/lab`, the path it has in the image, so `list` finds the source in the lab.
  - The image check uses `ls -R`, because labbase has no `find`.
  - Step 6 stops at `main` before reading the addresses, because `print &main` without running is trivially constant.
- **Dev images** are recorded in `.scratch/local-images.json`, not written into `manifest.yaml`: a local digest belongs to one host, and writing it would dirty the tree on every build. Manifests carry only GHCR digests, from `--push`.
- **challenges.json** adds `boss`. Until something is pushed (Q2: no GHCR namespace yet), every entry is `enabled: false` with an empty image, and labd now accepts an empty image only for a disabled challenge. `scripts/challenges-json.sh --local` enables them with this host's dev images. `challenges/README.md` describes how to play them in the dev page.
- **Prune** removes only images labelled `lab.keep`: the spec's rule alone would also delete labbase and other unmanaged images. `labd prune` deletes; `-dry-run` reports.
- **Deterministic bugs**: labs 1, 4 and 5 put the overrun's neighbour in a struct. Lab 3's uninitialised `found` inherits `ok = 1` from `well_formed()`, a twin frame called just before. A first version using a stack array read 0 on arm64, and the build's "plain run must not print the flag" check caught it.
- **Metrics**: each build appends to `.scratch/challenge-metrics.jsonl`. `scripts/challenges-metrics.sh` writes `docs/metrics/challenges-<date>-<host>.{json,md}`.

## Design fixed by this document

- **Flag derivation** (ADR 0005): `flag = "LAB{" + base32_std_nopad(HMAC_SHA256(key=DEPLOY_SECRET, msg=slug))[:24] + "}"`. Base32 is RFC 4648 standard alphabet, uppercase, padding stripped, first 24 characters. `DEPLOY_SECRET` is UTF-8 bytes; `slug` is the manifest slug as UTF-8. Implemented in Go (`labd/internal/flag`) and Python (`web/progress/flag.py`), both tested against `challenges/schema/flag_vectors.json` (10 vectors with a fixed test secret).
- **Flag embedding**: `tools/flagblob` (Go, `labd/cmd/flagblob`) takes the flag and a `key_expr` description from the manifest and emits `flag_blob.h`: `static const unsigned char FLAG_BLOB[N]`, `static const unsigned FLAG_SEED_MIX` and an inline `flag_decode(unsigned key, char *out)` that runs a 32-bit xorshift PRNG seeded with `key ^ FLAG_SEED_MIX` and XORs the blob. The challenge's `report(unsigned key)` calls it and prints `out`. With the wrong key it prints 28 bytes of garbage. `key_expr` in the manifest documents what runtime value is the key (e.g. `total`), and `solve.gdb` reaches it.
- **Addresses** (ADR 0010): code, globals and libc are at fixed addresses; the stack and the heap move on every `run`. No flag, `key_expr`, `solve.gdb` line or hint may depend on a stack or heap address. `solve.gdb` reaches things by name (`&users[2]`, `frame 1`, `$sp + 8`). Lesson 1 says that addresses change between runs and explains the `Error disabling address space randomization` warning in one sentence.
- **Manifest schema** `challenges/schema/manifest.schema.json`: exactly the spec's YAML keys, plus `key_expr` (string) and `build.flags` (string) and `build.entry` (binary name). `image` must match `^ghcr\.io/[a-z0-9-]+/lab-[a-z0-9-]+@sha256:[a-f0-9]{64}$` once set; empty allowed before first build.
- **Build script** `scripts/challenge-build.sh <challenge-dir> [--push] [--secret-env DEPLOY_SECRET]`:
  1. Lint manifest with the schema (`check-jsonschema` via `uv tool run`, or a Go validator in `labd/cmd/manifestlint`; choose the Go one to avoid a Python dependency in CI images).
  2. Compute flag; generate `flag_blob.h` into a temp build dir.
  3. Build in `images/build` container with the manifest flags plus mandatory `-static -no-pie -fno-pie` (ADR 0010), `SOURCE_DATE_EPOCH=0`. Build twice; hashes must match. Reject the binary if `readelf -l` shows an `INTERP` program header (it is not static).
  4. `strings -n 4 binary | grep -c 'LAB{'` must be 0. Also grep for the flag body without braces.
  5. Oracle: run `gdb -batch -x solve.gdb binary` under `runsc` with the sandbox spec; stdout must contain the flag. Run `gdb -batch -ex run binary` (no fix); stdout must not contain the flag.
  6. Assert `&main` and `&printf` identical across 3 runs (`gdb -batch -ex 'print &main' -ex 'print &printf'`). Stack and heap addresses are not checked: under gVisor they move on every run (ADR 0010).
  7. Build image `FROM labbase` copying `src/`, binary and `README.md` into `/opt/lab/`, `WORKDIR /opt/lab`, `USER lab`. Tag `lab-<slug>:<git-sha>`. Never copy `solve.gdb`, `solution.md`, `manifest.yaml`, `flag_blob.h`.
  8. With `--push`: push to `ghcr.io/$GHCR_NAMESPACE/lab-<slug>`, read the digest, write it into `manifest.yaml` `image:`.
  9. Without `--push` (dev): import into containerd namespace `labs` and write `image:` as `local/lab-<slug>@sha256:<digest>` — accepted by the schema only when `ALLOW_LOCAL_IMAGES=1`.
- **`scripts/challenges-json.sh`** walks `challenges/tier*/*/manifest.yaml` and emits `challenges.json` `{version, generated_at, challenges:[{slug,title,tier,order,difficulty,estimated_minutes,image,limits,hints,tags,enabled:true,lesson_path,solution_path}]}` sorted by tier then order. `lesson.md` and `solution.md` are read by Django's import command from the repo checkout on the box; the JSON carries paths, not bodies.
- **No CI** (ADR 0008). The pipeline is the two scripts above, run by a developer. `--push` uses `GHCR_TOKEN` from the developer's environment, writes digests into the manifests, and the developer regenerates `challenges.json` and commits. The base image is rebuilt with `./run.sh images labbase` when its Dockerfile changes and monthly; a `labbase` rebuild means rebuilding every challenge.
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

### 5.13 Rebuild-changed helper (replaces CI, ADR 0008)
Add `scripts/challenges-changed.sh [<base-ref>]`: lists challenge dirs changed since `<base-ref>` (default `origin/main`), or all of them when `images/labbase/` or `images/build/` changed, and runs `challenge-build.sh` on each. Do not add a GitHub Actions workflow.
**Done when:** touching one challenge's `src/main.c` makes the helper build only that challenge, and touching `images/labbase/Dockerfile` makes it build all five.

### 5.14 Authoring guide
`challenges/README.md`: the five design rules from the spec, the address rule from ADR 0010, hint ladder, how to write `solve.gdb`, how to run the oracle locally, how to test in the dev page, what never goes into the image.
**Done when:** a new challenge scaffolded with `challenge-new.sh` and written by following only the README passes the build script (do this with a sixth throwaway challenge, then delete it).

### 5.15 Wire the gate
**Done when:** `./run.sh gate --phase 5` exits 0.

## Tests

Go unit: flag vectors, manifestlint cases, pull/prune. Integration: flagblob compile test, build script on TEMPLATE and on all five. Content: oracle solve/no-solve per challenge, leak check, static binary, `&main` and `&printf` stability, reproducible hash. Human: each lab played once.

## Gate

```
./run.sh gate --phase 5
```
0. Preflight: no labd running, namespace `labs` empty.
1. `go test ./...` green.
2. `scripts/challenge-build.sh` on each challenge exits 0 (every step, dev mode).
3. `challenges.json` regenerates, validates, has 5 entries, and the committed file matches the manifests.
4. `flag_vectors.json` passes in Go (Python check added to the Phase 6 gate).
5. The x86-64 P0 run from Phase 1 exists (`docs/metrics/p0-*-<non-dev-vm host>.json`), and `docs/metrics/challenges-*.json` records x86-64 builds of all five with the oracle under runsc (`LAB_HOST=linux-laptop scripts/challenges-metrics.sh` after building them on the laptop). The oracle results on arm64 do not count for x86-specific lessons.
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
