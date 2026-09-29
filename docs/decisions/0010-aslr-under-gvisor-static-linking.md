# 0010 — gVisor cannot turn off ASLR; link every lab binary statically

Date: 2026-09-29 · Status: accepted · Phase: 1 (affects 5, 8) · Overrides: spec "Sandbox security profile → gdb-specific notes" (ASLR bullet), "Build pipeline" step 3, "Curriculum ladder" flags column

## Context

The spec makes "ASLR off" a hard requirement for early tiers and relies on two mechanisms: `-no-pie` builds, and gdb's `disable-randomization on`, which calls `personality(ADDR_NO_RANDOMIZE)`. The spec says gVisor supports that call. It does not:

- gVisor's source says so directly: "Unlike Linux, address randomization can't be disabled". Its `personality()` accepts only bits that are no-ops for it and returns `EINVAL` for `ADDR_NO_RANDOMIZE`. P0 shows gdb's warning `Error disabling address space randomization` under runsc on arm64 and x86-64.
- Host sysctls (`kernel.randomize_va_space`) do not reach inside the sandbox; the Sentry picks its own layout.

Measured under gVisor with the Phase 1 perf binary (dynamically linked, `-no-pie`): `main` and the globals sit at fixed addresses. The stack, the heap (musl's malloc takes memory from `mmap`), the musl loader, every shared library and the vdso move between runs. So `print &main` is stable, but `print &printf`, a stack address or a heap pointer is not. P0 rows `disable-randomization` and `aslr-gdb-stack` record this on both architectures.

## Options considered

| Option | Why rejected |
| --- | --- |
| Patch gVisor to honour `ADDR_NO_RANDOMIZE` | A fork to maintain and re-apply on every gVisor release, in the component that is our security boundary |
| Build determinism into each binary (fixed-address stack, a malloc arena at a fixed address) | Every challenge carries unusual startup code that learners will step into and that tier 2+ optimisation can break; it teaches something that is not how real programs behave |
| Run some labs (early tiers) under runc | Breaks S1. runc is not a sandbox for untrusted shells |
| VM-based sandboxes (Firecracker, Kata) | A different platform with a different cost model; Phase 1 exists to measure gVisor, and the VPS may not offer nested KVM |
| Static linking | Chosen, below |

## Decision

- **Every lab binary is linked `-static -no-pie -fno-pie`.** Code, globals and all of libc are then at fixed addresses, and there is no loader and no shared library to move. This applies to every tier, to the Phase 1 `perf` and `probe` binaries, and to the Phase 5 build script.
- **Stack, heap and anonymous `mmap` regions still move** under gVisor, on every `run`. Nothing in this project tries to stop that.
- `.gdbinit` keeps `set disable-randomization on`. It works under runc, and under gVisor it is harmless but prints a warning on every `run` (see Consequences).
- P0 enforces the decision: `&main` and a libc function's address are identical across 3 runs, both under gdb and run directly, and `/proc/self/maps` of a lab process has no `ld-musl` or other shared-library mapping. The `disable-randomization` and `aslr-gdb-stack` rows stay FALLBACK and cite this ADR.

## Consequences

**Challenge design (Phase 5 and every later tier):**

- No exercise may depend on a stack or heap address. Flags, `key_expr` values, `solve.gdb` scripts and hints must never use a pointer value that comes from the stack, `malloc` or `mmap`. Symbols (`&global`, `&function`, `&printf`) and offsets within a frame or an object are fine.
- `solve.gdb` must find things by name (`print &users[2]`, `frame 1`, `$sp + 8`), never by a hard-coded stack or heap address.
- The build script rejects a binary that is not static (`readelf -l` shows an `INTERP` header, or `file` does not say "statically linked"), in addition to the existing `&main` check.
- Tier 2 heap bugs and tier 1's stack overwrite still work: the learner inspects the addresses in their own session. They cannot copy an address from the lesson.
- Tier 5 (C++) links libstdc++ statically; binaries grow by roughly 1 to 2 MB each (*est.*). Tier 7 (Go) is static by default. Tier 8 (Rust) needs a musl target; decide when that tier is planned.

**Lessons:**

- Tier 1 says, early and plainly, that stack and heap addresses change on every `run`, and why (the sandbox randomises them). Example output in lessons shows stack and heap addresses as illustrations, with a note, never as values to type.
- Learners will see `warning: Error disabling address space randomization: Invalid argument` on every `run`. Lesson 1 explains it in one sentence. QUESTIONS.md Q14 asks whether to drop `disable-randomization` from the gdbinit to silence it.
- `info sharedlibrary` shows nothing, and stepping into libc shows no source (musl's static library has no debug info). No tier 1–3 lesson needs either.

**Platform:**

- Static binaries do not share libc pages between labs. musl's static libc adds tens of KB per binary (*est.*; P0's perf binary size shows the real number), so memory per lab should not change measurably.
- The arm64 gdb crash in QUESTIONS Q13 happened inside the musl loader. Static binaries have no loader, so P0 re-tests it on the dev VM.
- Revisit if gVisor ever implements `ADDR_NO_RANDOMIZE`: P0's `disable-randomization` row would turn PASS.
