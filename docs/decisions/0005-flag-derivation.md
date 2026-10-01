# 0005 — Exact flag derivation, encoding, and embedding

Date: 2026-09-26 · Status: accepted · Phase: 5

## Context

The spec fixes the idea (`LAB{base32(HMAC-SHA256(deploy_secret, slug))[:24]}`) but two implementations (Go build tooling, Python submission check) must agree byte for byte, and "base32" has variants.

## Decision

**Derivation.**
```
mac  = HMAC-SHA256(key = UTF-8(DEPLOY_SECRET), msg = UTF-8(slug))
body = RFC4648-base32(mac, alphabet A-Z2-7, uppercase, no padding)[0:24]
flag = "LAB{" + body + "}"
```
`slug` is the manifest slug exactly (`tier1-01-off-by-one`). No normalisation of the secret. Submission compares `strip(input)` against `flag` with a constant-time compare; comparison is case-sensitive.

**Test vectors.** `challenges/schema/flag_vectors.json` holds 10 `{slug, secret, flag}` entries computed with `openssl`. Go (`labd/internal/flag`) and Python (`web/progress/flag.py`) tests must pass all of them. The vectors use the secret `test-secret-do-not-use`.

**Embedding.** `flagblob` generates `flag_blob.h`:
- `FLAG_SEED_MIX`: a random 32-bit constant per build.
- `FLAG_BLOB[29]`: the flag's 28 bytes plus NUL, each XORed with successive outputs of `xorshift32(state)` where `state = key ^ FLAG_SEED_MIX` and `key` is the 32-bit runtime value named by the manifest's `key_expr`.
- `static inline void flag_decode(unsigned key, char out[29])` implements the inverse.
- The challenge's `report(unsigned key)` calls `flag_decode` and prints `out`.

**Rotation.** Changing `DEPLOY_SECRET` requires rebuilding every challenge image (`scripts/challenge-build.sh` on every challenge, ADR 0008) and redeploying `web` with the new secret in the same deploy. Old images with the old flag are pruned after 14 days as usual.

## Consequences

- Any language can reproduce the flag from the vectors; drift is caught by tests in two languages.
- The XOR scheme is obfuscation, not encryption; it only needs to defeat `strings` and `call report()` with a wrong key, which it does. A determined user with the binary can brute-force a 32-bit key offline, but that user also has the source and the bug, so the lab is already "solved" in the sense that matters. Accepted for MVP.

## Amendment (2026-10-01, Phase 5)

Two corrections found while implementing it:

- **Length.** `LAB{` + 24 + `}` is 29 bytes, not 28. `FLAG_BLOB` is 30 bytes with the NUL, and `flag_decode` writes `out[30]`. With a wrong key it prints 29 bytes of garbage.
- **Seed.** A random `FLAG_SEED_MIX` per build breaks the plan's reproducible build: two builds must give the same hash. The seed is now `HMAC-SHA256(DEPLOY_SECRET, "flagblob-seed|" + slug)`, first 4 bytes, big-endian (`flag.SeedMix`). It is deterministic per deploy and slug, and still unknowable without the secret.

The test vectors hold `{slug, secret, flag}` per entry, computed with `openssl dgst -sha256 -hmac` and coreutils `base32`; the command is in the file.
