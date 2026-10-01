# Writing a challenge

This is the authoring guide for lab content. It is all you need to write a challenge that passes the build: the rules, the shape of the program, the files, how to test it, and what never goes into an image. The five tier-1 labs in `tier1-c-fundamentals/` are worked examples.

## The layout

One directory per challenge, `challenges/tierN-<name>/NN-<slug>/`. The manifest's `slug` is `tierN-NN-<slug>` (`tier1-01-off-by-one`): it names the image, derives the flag, and must match the directory. The build checks this.

| File | Who sees it | What it is |
| --- | --- | --- |
| `manifest.yaml` | build, labd, web | Metadata, limits, hints, the key (below). Schema: `schema/manifest.schema.json` |
| `src/main.c` | the learner, in the lab at `/opt/lab/src/main.c` | The buggy program: one bug, under 120 lines |
| `README.md` | the learner, at `/opt/lab/README.md` | What the program *should* do and how to run it. No hints |
| `build.sh` | build | The compile command; run inside `images/build` |
| `lesson.md` | web, before the lab | About a 10-minute read teaching the gdb concepts the lab needs |
| `solve.gdb` | build only | The oracle: a gdb script that reaches the right state. **Private** |
| `solution.md` | web, after a solve | The walkthrough. **Private until solved** |

Start from the scaffold: `scripts/challenge-new.sh tier2-optimized-c 01-inlined-crash` copies `TEMPLATE/` and fills in `slug`, `tier`, `order`, `title` (from the slug; fix the wording) and the binary name (`build.entry`). Everything else is the template's and must be replaced: `src/main.c`, `key_expr`, `key_value`, `hints`, `tags`, `difficulty`, `estimated_minutes`, `solve.gdb`, and the text of `README.md`, `lesson.md` and `solution.md`. The build fails while any of the three still says "Template".

**Where a new challenge goes.** A tier ends with its boss (`boss: true`), so a new challenge goes into a tier that is not finished yet, or before the boss: then renumber the boss's directory, slug and `order`. Tier 1 is complete (five labs, lab 5 the boss); new content starts at tier 2.

## The manifest, key by key

| Key | Meaning |
| --- | --- |
| `slug` | `tierN-NN-<slug>`: the tier digit, the directory's `NN`, and the directory's name after `NN-` (`tier1-c-fundamentals/01-off-by-one` gives `tier1-01-off-by-one`). Names the image and derives the flag; never rename it once published |
| `title` | Shown on the web, a few words |
| `tier`, `order` | Must match the directory: the tier digit and `NN` |
| `difficulty` | 1 to 5, relative within the curriculum (lab 1 is 1, the tier-1 boss 4) |
| `estimated_minutes` | A fair guess for a first attempt, lesson not included |
| `flag_format` | Always `"LAB{...}"` |
| `image` | Leave `""`. `--push` writes the GHCR digest here |
| `limits` | The lab's sandbox: memory, CPU, processes (at most 32), session and idle timeouts. Keep the template's values unless the program needs more; the schema caps them at what the sandbox allows |
| `hints` | Up to three, escalating (below); `[]` for a boss |
| `tags` | Lower-case topics for the web (`c`, `pointers`, `watchpoints`) |
| `boss` | `true` for the tier's last challenge, which has no hints |
| `key_expr`, `key_value` | What `report()` decodes with, and its right value (below) |
| `build.flags`, `build.entry` | Compiler flags for the tier, and the binary's name in `/opt/lab` |

## The five design rules (spec, "First five lessons and labs")

1. **One bug per lab, visible from a plain `run`**: a wrong number, a crash, a rejected input. The learner always has a symptom to start from.
2. **The fix is a runtime state change in gdb, never a source edit.** There is no compiler in the lab. `set var`, `return`, `jump`, writing memory: the learner repairs the running program, not the code.
3. **Brute force from the shell gives nothing.** `report()` decodes the flag from runtime state that only exists mid-execution. Running the binary, `strings` on it, or `call report(0)` print garbage.
4. **`solve.gdb` is the oracle and the walkthrough's source.** If the build's oracle passes, the intended path works.
5. **Hints escalate**: hint 1 names the gdb command to try (a short sequence such as `break f`, `run` is fine, and it may name the function), hint 2 the variable or value to look at (a return value, an argument), hint 3 the fix. Costs do not decrease (0, 1, 2 is usual). The tier's last challenge is the boss: `boss: true` and `hints: []`.

## The address rule (ADR 0010)

Every binary is linked `-static -no-pie -fno-pie`: code, globals and libc sit at the same address on every run. The stack and the heap do not: gVisor cannot turn address randomisation off, so they move on every `run`. Therefore:

- No flag key, `key_value`, `solve.gdb` line or hint may depend on a stack or heap address.
- Reach things by name: `&users[2]` (a global), `frame 1`, `f.checksum`, `$saved`. Line numbers are fine.
- Key on *values* (a sum, an id, a checksum, a length), never on pointers to locals or malloc'd memory.

## The program's shape

Every lab program has a `report(unsigned key)` that decodes the flag and prints it. Copy it from `TEMPLATE/src/main.c` unchanged:

```c
#include "flag_blob.h"

static void report(unsigned key)
{
	char out[30];
	flag_decode(key, out);
	for (int i = 0; out[i]; i++)
		if (!isprint((unsigned char)out[i]))
			out[i] = '?';
	printf("report: %s\n", out);
}
```

`flag_blob.h` is generated at build time (`labd/cmd/flagblob`, ADR 0005). It holds the flag XORed with a keystream seeded by `key`, and only the right `key` decodes it; any other key prints 29 characters of garbage (unprintable ones as `?`, so the terminal is not disturbed). The program calls `report()` on its normal path, with a value that is right only once the bug has been worked around. Choose that value and record it in the manifest:

- `key_expr`: what the key is, in words: `"total in main: the true sum of the five scores"`.
- `key_value`: what it is when the state is right, as a 32-bit unsigned number: `400`. This is flagblob's `--key`. It is never in the image.

Compute `key_value` from the program's own data (by hand, or with a two-line script that mirrors the program's arithmetic) and check it: the build's oracle fails if `solve.gdb` does not reach exactly this value.

A key with a small range can be found by calling `report()` in a gdb loop. That is accepted (ADR 0005): a learner who does it has used gdb. Mixing the key with more state makes it impractical, for example an FNV-1a hash of the bytes that must be right (lab 4):

```c
unsigned h = 2166136261u;
for (int i = 0; i < 8; i++) {
	h ^= (unsigned char)name[i];
	h *= 16777619u;
}
return h ^ (unsigned)len;   /* the key */
```

## Making the bug deterministic

The plain run must show the bug on both architectures the build uses (x86-64 for production, arm64 on Mac developer VMs). Undefined behaviour that depends on stack layout needs care:

- **Reads or writes past a buffer** hit whatever the compiler put next. Put the neighbour in a `struct` (fields keep their order and, for these sizes, no padding), so the overrun always lands on a known field: lab 1 reads `book.curve`, lab 4 runs into `card.role`, lab 5 overwrites `f.checksum`.
- **Uninitialised locals** read what the previous function left at the same place. Make that deliberate: call a function with the *same parameters and first local* just before, from the same caller, so its value is what the uninitialised variable inherits (lab 3: `well_formed`'s `ok = 1` becomes `lookup`'s `found`). A bulk array is not enough: the slot may land on a zero.
- The build's step 5 runs the program plainly and **fails if it prints the flag**: if the bug did not show, the key came out right by accident.

## build.sh

`TEMPLATE/build.sh` is right for tier 1: `cc $CFLAGS -I/gen -o "/out/$ENTRY" /opt/lab/src/main.c`. It runs inside `images/build` (Alpine, musl, the same libc as the lab image) with this directory mounted at `/opt/lab`, the path it has in the lab image, so gdb's `list` finds the source there. `$CFLAGS` is the manifest's `build.flags` plus the mandatory `-static -no-pie -fno-pie`; tier 1 uses `-O0 -g -fno-stack-protector`. Change `build.sh` only if a challenge has more than one source file.

## solve.gdb

A plain gdb command list, run as `gdb -batch -x solve.gdb /opt/lab/<entry>` inside the lab image, under the real sandbox, as the learner's user. It must make the program print the flag. Rules:

- Names, never stack or heap addresses (the address rule above). Convenience variables (`set $saved = x`) are fine.
- Keep to commands that work on x86-64 and arm64: `break`, `run`, `next`, `step`, `finish`, `set var`, `continue`, `return`, `jump`. No register names (`$rax`), no `x/...` of the stack.
- The lab's gdbinit sets `confirm off` and `pagination off`; do not rely on prompts.
- Short and direct: the oracle proves the state can be reached. The longer, teaching path belongs in `solution.md`.

## lesson.md, README.md, solution.md

- **lesson.md**: about a 10-minute read. Teach the gdb concepts on a *different* example program, then end with a "Try it" paragraph that states the lab's symptom and goal without the answer. Tiers 1 to 3 take their concepts from the spec's tables ("First five lessons and labs", "Curriculum ladder"); for anything else, list the concepts at the top of the lesson and teach each one the lab needs.
- **README.md**: what the program should do, its inputs, `./<entry>` and `gdb ./<entry>`. It must let the learner see the symptom ("should print 400") without saying where the bug is.
- **solution.md**: symptom, how to find it (commands with their output), the fix in gdb, and the real fix in the source. Copy real output, do not write it by hand: `scripts/challenge-build.sh <dir> --keep-work` leaves the oracle's full gdb session in `.scratch/challenge-build/<slug>/solve.out` (it contains the dev flag; never commit it), and the dev page (below) gives an interactive session.

## Building and testing

```
scripts/challenge-build.sh challenges/tier1-c-fundamentals/06-double-free
```

The steps keep the plan's numbers but run in this order: 1 lint the manifest; 2 generate `flag_blob.h`; 3 compile twice (identical hashes) and check the binary is static; 4 check it contains neither `LAB{` nor the flag body; 7 build the image and check nothing private is in it (7 runs before 5 and 6 because they run inside that image); 5 run `solve.gdb` (must print the flag) and a plain run (must not) in the lab image under the sandbox; 6 check `&main` and `&printf` do not move over 3 runs; 8 publish. Each step says what failed. Step 7 lists the image's `/opt/lab`: exactly `README.md`, `src/` with `main.c`, and the binary.

- **Where.** The build targets the lab host's architecture. On the x86-64 laptop it is authoritative: x86-64, oracle under gVisor (runsc). On a Mac it runs in the arm64 VM with the oracle under runc, because gdb under gVisor on arm64 cannot resume from a breakpoint (QUESTIONS Q13). Use the Mac for writing, the laptop for the final check: a bug that depends on memory layout is only proven on x86-64 there.
- **Secret.** The flag comes from `$DEPLOY_SECRET`. Without it the build uses a dev secret and says so: fine for testing, never for publishing.
- **Dev images.** Without `--push`, the image is imported into containerd as `local/lab-<slug>@sha256:...` and recorded in `.scratch/local-images.json`, not in the manifest. `--push` (x86-64 only) pushes to `ghcr.io/$GHCR_NAMESPACE/lab-<slug>` and writes the digest into `manifest.yaml`; commit it.
- **Rebuild what changed:** `scripts/challenges-changed.sh` (all of them when labbase or the toolchain changed).
- **challenges.json:** `scripts/challenges-json.sh > challenges.json` (pushed images only; unpushed challenges are listed disabled). For local play, `scripts/challenges-json.sh --local > .scratch/challenges.local.json`.

## Playing it in the dev page

1. Build the challenge, then `scripts/challenges-json.sh --local > .scratch/challenges.local.json`.
2. Point a labd config at it: `sed "s|^challenges_file:.*|challenges_file: $PWD/.scratch/challenges.local.json|" labd/labd.dev.yaml > .scratch/labd.local.yaml`.
3. `LABD_INTERNAL_SECRET=dev WS_TOKEN_KEY=dev ./run.sh labd --config .scratch/labd.local.yaml`.
4. Start a session for the challenge's slug (the `curl` in `docs/STATUS.md`'s Phase 3 steps, with `"challenge_slug": "<slug>"`) and open the printed `/dev/term` URL.
5. Play it as a learner: check the bug shows on a plain `./<entry>`, that each hint, read in order, moves you forward, and that the intended path works without the solution. Stop the session before stopping labd.

## Never in an image

The build checks the image for these and fails if it finds one: `solve.gdb`, `solution.md`, `manifest.yaml`, `flag_blob.h`, `lesson.md`, `build.sh`. The flag itself is never in any file; it exists only as the XORed blob in the binary. Do not commit `flag_blob.h` or anything under `.scratch/`.

## Checklist

- [ ] One bug, visible from a plain run, deterministic on both architectures.
- [ ] `report()` copied from TEMPLATE; called on the normal path with a key that is right only after the fix.
- [ ] `key_expr` and `key_value` set; no key, hint or `solve.gdb` line depends on a stack or heap address.
- [ ] Three escalating hints (none for a boss).
- [ ] `solve.gdb` by names, portable commands only.
- [ ] README with the expected behaviour and no hints; lesson with a different example; solution with the real fix.
- [ ] `scripts/challenge-build.sh` passes on the laptop.
