# Project status

Update this file at the end of every working session. Keep it factual. Newest log entry at the top.

## Current phase

Phase 3 human check: 2026-09-30 <OG> OK

**Phase 3 — Terminal gateway.** In progress on branch `phase-3-terminal-gateway`. Tasks 3.1–3.11 are built and pass on the arm64 dev VM. Two things remain, both the owner's: the human check and the x86-64 P1.

The owner's first laptop attempt (2026-09-30) failed because the steps ran out of order. The dev labd was still running when P1 started, and the dev session's lab was still there when the gate counted leftovers. labd did not hang: it exits about 0.1 s after Ctrl-C. It just logged nothing after "shutting down". Now it logs every connection and ends with `labd stopped; labs keep running`. The gate and the perf scripts check first and stop at once with what to do.

**On the laptop, in this order:**

1. **Update.** `git pull && git checkout phase-3-terminal-gateway`, then `./run.sh labs preflight`. It must say `preflight ok`. If a labd is running, stop it (Ctrl-C in its terminal). If labs are left, run `./run.sh labs clean` and answer `y`.
2. **Human check (task 3.8).** Use two terminals, A and B.
   1. A: `LABD_INTERNAL_SECRET=dev WS_TOKEN_KEY=dev ./run.sh labd`. Wait for the two `labd listening` lines (`internal API` and `terminal gateway`).
   2. B, start a session and print the page URL:
      ```
      resp=$(curl -s -H 'Authorization: Bearer dev' -d '{"user_id":1,"challenge_slug":"perf"}' http://127.0.0.1:8081/internal/sessions)
      sid=$(jq -r .session_id <<<"$resp"); tok=$(jq -r .ws_token <<<"$resp"); echo "$resp"
      echo "http://127.0.0.1:8082/dev/term?session=$sid&t=$tok"
      ```
   3. Open the URL within 60 s. The page says `running`, and A logs `ws: connected`. If the page says `closed: 1006` or A logs `ws: token rejected`, the token expired or was used: repeat step 2 (same session, fresh token).
   4. In the page's terminal: `gdb /opt/perf/perf`, `break main`, `run`, `next`, `watch counter`, `continue`, `bt`. Each should work (x86-64). On the Mac's arm64 VM, `next` and `continue` crash the program (QUESTIONS Q13); that is expected there, not a gateway fault.
   5. Paste about 100 KB of text into the terminal. For example, run `seq 20000 > /tmp/paste.txt` (109 KB), open the file in a text editor, select all, copy, and paste into the page. The orange message `input rate limit: keystrokes dropped` appears at the top.
   6. B, stop the session **before** stopping labd:
      ```
      curl -s -X DELETE -H 'Authorization: Bearer dev' -d '{"reason":"user_stop"}' http://127.0.0.1:8081/internal/sessions/$sid
      ```
      The page shows `ended (user_stop)`, then `closed: 1000 ended`. A logs `ws: detached ... closed_by=server code=1000 reason=ended`.
   7. A: Ctrl-C. The last line must be `labd stopped; labs keep running`.
   8. B: `./run.sh labs preflight` must say `preflight ok`. If it lists a lab, run `./run.sh labs clean`.
   9. Add this line to this file, on its own line, starting at the first column, with the real date and your initials (the gate greps for it): `Phase 3 human check: YYYY-MM-DD <initials> OK`.
3. **x86-64 P1**, about 11 minutes. Preflight must pass first; the script checks it.
   ```
   LAB_HOST=linux-laptop ./run.sh perf --scenario P1-lite --hold 10m
   git add docs/metrics docs/STATUS.md && git commit -m "[P3] metrics: x86-64 P1; human check" && git push
   ./run.sh gate --phase 3
   ```
   Send the gate output. The gate starts with the same preflight and stops at once if a labd is running or a lab is left.

Phase 0 task 0.9 (second developer onboarding) is still open and non-blocking.

<details><summary>Earlier checklist (done)</summary>


- **0.8 on the Linux x86-64 laptop:** clone, `./run.sh doctor`, `./run.sh vm up`, `./run.sh vm verify`, then `./run.sh gate --phase 0`. Write `docs/metrics/environment-linux-laptop.md` in the same format as `environment-dev-vm.md`. Also check whether non-terminal gVisor I/O hangs there too (ADR 0007): `sudo timeout 25 ctr -n labs run --rm --null-io --runtime io.containerd.runsc.v1 docker.io/library/alpine:3.20 t1 /bin/true; echo $?` (124 means it hangs).
- **0.9:** second developer runs `./run.sh doctor` and follows `docs/ONBOARDING.md`.
- **0.7:** removed. The owner deleted the CI workflow to save Actions minutes; do not add workflows (ADR 0008).
- **0.10:** gate output from the laptop pasted below. **Do not start Phase 1** until the owner has run Phase 0 on the Linux laptop.

</details>

## Phase board

| Phase | Name | State | Gate result | Date |
| --- | --- | --- | --- | --- |
| 0 | Bootstrap: repo, toolchain, dev VM | done (0.9 open, non-blocking) | passed on Mac and laptop | 2026-09-28 |
| 1 | gVisor + gdb spike (labbase, sandbox spec, P0) | done; merged to `main` | passed on the laptop (x86-64) | 2026-09-29 |
| 2 | labd core (sessions, semaphore, reconciler) | done; merged to `main` | passed on the laptop (x86-64) | 2026-09-30 |
| 3 | Terminal gateway (WebSocket ↔ PTY) | in progress: needs human check and x86-64 P1 | Mac: all but the two laptop items | 2026-09-30 |
| 4 | Perf suite and measured capacity | blocked on 3 | — | — |
| 5 | Challenge pipeline and tier 1 content | not started (owner review of Phase 1 first) | — | — |
| 6 | Django web app | blocked on 3, 5 | — | — |
| 7 | Metrics, rollups, admin live view | blocked on 6 | — | — |
| 8 | Production on the VPS, tiers 2–3, beta | blocked on 4, 7 | — | — |

States: `not started`, `in progress`, `gate failing`, `done`, `blocked on N`.

## Open blockers

- None yet. Decisions awaiting the owner are in [QUESTIONS.md](QUESTIONS.md); each has a default that is in force.

## Measured numbers so far

x86-64 laptop, one lab (`docs/metrics/single-lab-2026-09-29-linux-laptop.json`, rows in [metrics/capacity.md](metrics/capacity.md)):
- **Memory:** gVisor 25.8 MiB cgroup p95 at a breakpoint, against the spec's estimate of ~130 MB. runc uses 13.1 MiB.
- **Start latency:** container to gdb prompt, p95 1543 ms (target < 2 s).
- **Base image:** 28.4 MiB.

Early warning: a whole scripted gdb session takes 6.4× longer under gVisor than under runc, against a < 2× target for `step`. The per-command number comes in Phase 4.

## Log

### 2026-09-30 — labd shutdown checked, gateway logging, preflight for gate and perf
- **The owner's laptop run.** Both P1 runs refused to start because the dev labd was still running. The gate then found the dev session's lab, which labs outliving labd leaves behind by design. The log could not show whether labd had exited.
- **Shutdown did not hang.** Reproduced on the dev VM (`.scratch/shutdown-repro.sh`, not committed): labd exits 102–109 ms after SIGINT or SIGTERM, with a WebSocket client attached and with only a lab running. The lab keeps running, and the next labd adopts it. One real defect: the attached client saw a bare EOF, not the 1000 close the README promised, because `http.Server.Shutdown` does not track hijacked connections. Fixed: `term.Server.Shutdown` closes every socket with 1000 and later handshakes get 503. Tests: `TestWS_ShutdownClosesConnectionsAndKeepsLab` and `TestServeAll_ClosesWebSocketsOnCancel`. After the fix, exit takes 104–106 ms and the client gets 1000.
- **Logging.** The gateway now logs `ws: connected`, `ws: detached` (who closed it, codes, duration), `ws: replaced`, slow consumers and every refused handshake, each with the remote address. Each listener's shutdown line names it. `labd stopped; labs keep running` is the last line; if it is missing, labd hung. The list is in `labd/internal/term/README.md` ("Log lines").
- **Fail fast.** `./run.sh labs ls|clean|preflight` (`scripts/labs.sh`). The gate and the perf scripts run `preflight` first and stop within a second if a labd is running or a lab is left. `clean` refuses while a labd runs. The human-check steps above now stop the session before Ctrl-C and end with a preflight.
- **Checks on the Mac:** `./run.sh check`, `test --all`, `lint` and `test --integration` pass; `go test -race -count=5 ./...` passes. The Mac's staticcheck was built with Go 1.25 and cannot load this module, so it only warns. staticcheck in the VM is clean.
- **Gate on the Mac:** everything passes except the two laptop items:
```
==> [gate 3] preflight: no labd running, namespace labs empty
  PASS  no labd running, namespace labs empty
==> [gate 3] unit tests (go vet, go test -race)
  PASS  run.sh test --go
==> [gate 3] integration tests (includes real gdb over the WebSocket)
  PASS  run.sh test --integration
==> [gate 3] P1 runs on this host (2 min, scratch output)
  PASS  P1-lite: one session replaying session.gdb over the socket
  PASS  no containers left in namespace labs
==> [gate 3] recorded P1 (x86-64, ADR 0001) and the human check
  FAIL  no 10-minute P1 from an x86-64 host yet: on the laptop run LAB_HOST=linux-laptop ./run.sh perf --scenario P1-lite --hold 10m, commit docs/metrics
  FAIL  STATUS.md lacks a line 'Phase 3 human check: YYYY-MM-DD <who> OK'
==> GATE 3 FAILED. Fix the FAIL lines above; do not start the next phase.
```
- **Next:** the owner runs the laptop steps at the top of this file.

### 2026-09-30 — Phase 3 built: WebSocket gateway, capture, client, dev page, P1
- **Built** (tasks 3.1–3.11):
  - HMAC WebSocket tokens (single use);
  - the gateway (`GET /ws/term/{id}`) with Origin check, input and output rate limits, a backpressured output with slow-consumer close (1008), connection replacement (1000 `replaced`), reconnect grace (60 s), TTL frames, extend, and resize;
  - command capture into `command_entered` events;
  - the scripted client and the replay command;
  - the dev test page with vendored xterm.js 5.5.0;
  - `POST /internal/sessions` now returns a real `ws_token`;
  - the P1 script and the gate.
- The protocol as implemented is in `labd/internal/term/README.md`: the contract for web in Phase 6.
- **ADR 0012:** tokens carry a random nonce. The plan's format made two tokens minted in the same second identical, so the second failed as "reused" (two tabs, fast reconnect). The tests caught it.
- **Plan correction:** task 3.5's example `"ne\x7fxt\n" → next` contradicts its own backspace rule; the code follows the rule.
- **Real gdb over the socket** (`integration/term_test.go`): start gdb, break, run, print, quit; stop closes 1000; commands captured. Passes on the arm64 VM.
- **Dev VM P1** (arm64, `docs/metrics/p1-2026-09-30-dev-vm.md`, 10 min, 20 commands/min):
  - request to `(gdb)` 1321 ms; echo p50 2.9 ms, p95 4.9 ms;
  - lab cgroup 25.7 MiB p50; Sentry RSS 42 MiB; CPU 0.22 % of a core;
  - WebSocket 3 B/s in, 34 B/s out; 201 commands, 0 errors.
- `capacity.md`'s CPU-per-lab and bandwidth rows wait for the laptop's P1 (x86-64 only replaces an *est.*).

### 2026-09-30 — Phase 2 gate passed on the x86-64 laptop
- **Laptop P4-lite** (`docs/metrics/create-latency-2026-09-30-linux-laptop.md`, runsc, 10 min at cap 20):
  - 300 sessions; create to running p50 223 ms, p95 277 ms, max 312 ms;
  - containers never above 20 and equal to active once quiet; 0 left at the end;
  - labd: 24.9 MiB and 13 goroutines idle, 37.5 MiB and 133 goroutines at 20 sessions, back to 13 after.
- Create is about 3× slower on the laptop than on the arm64 VM (p50 70 ms), the same direction as Phase 1's start-to-prompt (1463 ms against about 430 ms). Phase 4 should look at runsc boot cost on x86-64 (systrap there, KVM available on the laptop).
- Integration tests, P5-lite and the full gate pass on the laptop. Phase 2 is merged into `main` and pushed; Phase 3 starts.

Phase 2 gate on the Linux laptop:
```
==> gate for phase 2 — 2026-09-30T01:14Z — linux-laptop
==> [gate 2] unit tests (go vet, go test -race)
  PASS  run.sh test --go
==> [gate 2] integration tests (real containerd, runsc, Postgres)
  PASS  run.sh test --integration
==> [gate 2] P4-lite and P5-lite on this host
  PASS  P4-lite: 10 min churn at cap 20, no leak
  PASS  P5-lite: kill -9 labd at 20 labs, all adopted within 15 s
  PASS  no containers left in namespace labs
==> [gate 2] recorded create latency (x86-64, ADR 0001)
  PASS  create-latency report (create-latency-2026-09-30-linux-laptop.md)
  PASS  x86-64 create latency recorded (create-latency-2026-09-30-linux-laptop.json)
==> GATE 2 PASSED. Paste this output into docs/STATUS.md.
```

### 2026-09-29 — Phase 2 built: sessions, queue, timers, reconciler, store, internal API
- **Phase 1** fast-forwarded into `main` locally at the owner's request. Nothing pushed.
- **Built** (tasks 2.1–2.13): clock with fake; containerd `Runtime`; the session manager (slot cap, per-user cap, FIFO queue with a 2-minute timeout, idle and hard timers, extend once, runtime cap changes); the Postgres store with embedded migrations and `labd migrate`; the reconciler; the internal API with SIGHUP and `/internal/reload`; `labd pull` and a listing-only `labd prune`; the P4-lite and P5-lite scripts; the gate. Deviations are listed in the plan's new "As built" section.
- **ADR 0011:** `sessions` stores `challenge_slug` instead of web's `challenge_id`, because labd never knows that id. It also adds `image` and `extended`.
- **Finding: labd does not need root.** With its own FIFO directory, a user in the `containerd` group can create, run and delete gVisor labs (S10). Tests and the perf scripts run that way (`scripts/with-containerd-group.sh`).
- **Code review** (a separate agent) found a real bug: a failed containerd `Wait` was read as the lab exiting. A containerd restart or a graceful labd stop would therefore have torn down, or later removed, every live lab. Fixed, along with the shutdown ordering, a user left pointing at a stopped-while-creating lab, and pipe leaks on error paths. After the fix, a containerd restart with 3 labs running leaves all 3 running.
- **Dev VM numbers** (arm64, runsc; `docs/metrics/create-latency-2026-09-29-dev-vm.md`):
  - P4-lite, 10 min at cap 20: 302 sessions; create to running p50 70 ms, p95 115 ms, max 175 ms; containers never above 20 and equal to active once quiet; 0 left at the end.
  - labd: 22.5 MiB and 13 goroutines idle; 33.2 MiB and 133 goroutines at 20 sessions; back to 13 after.
  - P5-lite: all 20 labs adopted 462 ms after `kill -9` and 464 ms after a SIGTERM restart (deadline 15 s).
  - A terminal re-attached after its creator died still carries keystrokes and output (`integration/attach_test.go`).
- **API fields left for later phases:** `ws_token` is always `""` until Phase 3. There is no extend route yet (`Manager.Extend` exists). Web's button for it comes with Phase 3 or 6.
- **Next:** the owner runs the laptop steps above; then the gate output goes here and Phase 2 is marked done.

Phase 2 gate on the Mac (everything passes except the x86-64 result):
```
==> gate for phase 2 — 2026-09-29T19:14Z — macbook.local
==> [gate 2] unit tests (go vet, go test -race)
  PASS  run.sh test --go
==> [gate 2] integration tests (real containerd, runsc, Postgres)
  PASS  run.sh test --integration
==> [gate 2] P4-lite and P5-lite on this host
  PASS  P4-lite: 10 min churn at cap 20, no leak
  PASS  P5-lite: kill -9 labd at 20 labs, all adopted within 15 s
  PASS  no containers left in namespace labs
==> [gate 2] recorded create latency (x86-64, ADR 0001)
  PASS  create-latency report (create-latency-2026-09-29-dev-vm.md)
  FAIL  no create latency from an x86-64 host yet: on the laptop run LAB_HOST=linux-laptop ./run.sh perf --scenario P4-lite --hold 10m, commit docs/metrics
==> GATE 2 FAILED. Fix the FAIL lines above; do not start the next phase.
```

### 2026-09-29 — Phase 1 gate passed on the x86-64 laptop
- **Laptop P0** (`docs/metrics/p0-2026-09-29-linux-laptop.*`, static perf image):
  - runsc: 27 PASS, 3 FALLBACK, 0 FAIL. The FALLBACKs are `hw-watchpoint`, `disable-randomization` and `aslr-gdb-stack`, all documented.
  - runc: 30 PASS, 0 FAIL.
  - Every ADR 0010 row passes on x86-64: `&main` 0x401342 and `&printf` 0x403aae are fixed under gdb and run directly, and no shared library is mapped.
- **Laptop single-lab** (`docs/metrics/single-lab-2026-09-29-linux-laptop.*`):

  | Metric | runsc | runc |
  | --- | --- | --- |
  | Start to gdb prompt p50 / p95 | 1463 / 1543 ms | 312 / 466 ms |
  | cgroup memory p95, at a breakpoint | 25.8 MiB | 13.1 MiB |
  | Sentry RSS p95 | 45.5 MiB (includes shared pages) | n/a |
  | session.gdb wall | 2723 ms | 425 ms |
- **capacity.md:** the rows Phase 1 measures now carry these numbers and cite their source files. They cover memory per lab, Sentry RSS, total per lab, start latency, base image size, and gVisor memory and start overhead. Still *est.*:
  - `step` latency, which Phase 4 measures per command. The whole session is 6.4× slower under gVisor, which is an early warning against the < 2× target.
  - The 200-challenge image total (Phase 5).
  - `max_sessions` (Phase 4).
- The single-lab JSON fields end in `_mb`, but the values are MiB; capacity.md says so. Renaming the fields can wait until Phase 4 rewrites the report schema.
- **Next:** the owner reviews the branch and decides whether to merge it and when to start Phase 2 or Phase 5.

Phase 1 gate on the Linux laptop:
```
==> gate for phase 1 — 2026-09-29T17:14Z — linux-laptop
==> [gate 1] images
  PASS  labbase builds
  PASS  labbase contents (images/labbase/test.sh)
  PASS  perf image builds; binaries reproducible
==> [gate 1] unit tests (spec golden file, invariants, cgroup parsing)
  PASS  run.sh test --all
==> [gate 1] P0 and single-lab run on this host
  PASS  P0 completes (runsc and runc)
==> [gate 1] authoritative results (x86-64, ADR 0001)
  PASS  x86-64 P0 has no runsc FAIL rows (p0-2026-09-29-linux-laptop.json)
  PASS  x86-64 single-lab has runsc start latency and at-breakpoint memory (single-lab-2026-09-29-linux-laptop.json)
==> GATE 1 PASSED. Paste this output into docs/STATUS.md.
```

### 2026-09-29 — static lab binaries (ADR 0010)
- **Decision (owner):** gVisor cannot turn ASLR off: `personality(ADDR_NO_RANDOMIZE)` returns EINVAL, and host sysctls do not reach the sandbox. Every lab binary is now linked `-static -no-pie -fno-pie`, so code, globals and libc are fixed. Stack and heap still move; no exercise may depend on them. ADR 0010 lists the rejected options. CONVENTIONS and the Phase 5 plan now require `-static`. The spec is unchanged; the ADR overrides it.
- **perf image:** `perf` and `probe` are static. `build.sh` rejects a binary with an `INTERP` or `DYNAMIC` segment (tested against a dynamic build). Both binaries still build byte-identical twice. Measured size cost on arm64: text 4.8 KB → 48.7 KB, file 78 KB → 366 KB.
- **P0:** 3 new rows. `aslr-gdb-libc` and `aslr-direct-libc` check that `&printf` is identical over 3 runs, under gdb and run directly. `no-shared-libs` checks that `/proc/self/maps` has no `ld-musl` or `.so` mapping. All three pass under runsc and runc on the dev VM. `disable-randomization` and `aslr-gdb-stack` stay FALLBACK and cite ADR 0010.
- **Bug found by the static build:** `session.gdb` never stepped the `sum_scores` loop. In a dynamic binary, `display i` in `main` bound silently to a global `i` in `/lib/ld-musl-aarch64.so.1`, so the session passed by accident. It now `continue`s into `sum_scores` first and ends with `report key=42`.
- **Dev VM P0** (arm64, explicit run, committed as `docs/metrics/p0-2026-09-29-dev-vm.*`): runc 30 PASS, 0 FAIL. runsc 20 PASS, 3 FALLBACK, 7 FAIL, up from 14 PASS and 10 FAIL.
- **arm64 gdb under gVisor:** static binaries removed the loader crash, but 7 checks still fail. Three gVisor arm64 ptrace gaps were hiding behind the loader crash: stepping over a breakpoint (displaced stepping) crashes the program, the FP/SIMD register read returns EINVAL, and with displaced stepping off `next` runs to the end. Q13 updated; its default stands (Mac developers use runc for gdb work).
- **Gate on the Mac:** everything passes except the two x86-64 checks (output below). The gate's own P0 went to `.scratch/` and was not committed. `./run.sh check`, `./run.sh lint`: pass.
- Not re-run: the dev-VM single-lab. Its "at a breakpoint" memory under runsc may now be reachable on arm64, but only the laptop's numbers count.
- **Next:** the owner runs the laptop steps above. Then `capacity.md` gets the measured rows and Phase 1 is marked done.

Phase 1 gate on the Mac:
```
==> gate for phase 1 — 2026-09-29T16:36Z — macbook.local
==> [gate 1] images
  PASS  labbase builds
  PASS  labbase contents (images/labbase/test.sh)
  PASS  perf image builds; binaries reproducible
==> [gate 1] unit tests (spec golden file, invariants, cgroup parsing)
  PASS  run.sh test --all
==> [gate 1] P0 and single-lab run on this host
  PASS  P0 completes (runsc and runc)
==> [gate 1] authoritative results (x86-64, ADR 0001)
  FAIL  no P0 from an x86-64 host yet: on the laptop run LAB_HOST=linux-laptop ./run.sh perf --scenario P0, commit docs/metrics
  FAIL  no single-lab measurement from an x86-64 host yet: LAB_HOST=linux-laptop ./run.sh perf --scenario single-lab
==> GATE 1 FAILED. Fix the FAIL lines above; do not start the next phase.
```

### 2026-09-29 — laptop P0: gdb works under gVisor on x86-64
- **Laptop runsc P0:** every gdb check passes, except the two with fallbacks already in force:
  - Hardware watchpoints are accepted but never trigger, which is worse than a refusal. The `.gdbinit` default of 0 matters.
  - `personality(ADDR_NO_RANDOMIZE)` fails, so the stack moves; `&main` is fixed at 0x4013d2.
- Every sandbox check passes, with the same numbers as the dev VM. Q13 is now arm64-only.
- **Laptop single-lab (runsc):**
  - start to prompt p50 1450 ms, p95 1507 ms (target < 2 s);
  - memory at the gdb prompt 23.4 MiB and at a breakpoint 28.6 MiB (cgroup);
  - Sentry RSS 42 to 46 MiB;
  - session.gdb 3.2 s.
- **Laptop runc** failed everywhere with `exec /usr/bin/gdb: resource temporarily unavailable`. `RLIMIT_NPROC` (ADR 0009) counts every host process of uid 1000, which is the owner's desktop user. Reproduced on the dev VM with 40 processes owned by uid 1000.
- **Fix:** the process limit is runtime-aware. gVisor uses `RLIMIT_NPROC`; runc uses the cgroup only and sets no `RLIMIT_NPROC`. runc now passes 27 of 27 under that load. ADR 0009 amended; the golden spec now shows cgroup pids 128 for the gVisor default.
- Also fixed: the P0 hardware-watchpoint row says "accepted but never triggered", and single-lab no longer prints an `awk` error when the cgroup vanishes at the end of a hold.
- **Next:** the owner re-runs P0 and single-lab on the laptop so the committed files have valid runc columns, then commits and pushes `docs/metrics`.

### 2026-09-29 — first Phase 1 gate run on the laptop
- The owner's gate run failed for reasons outside the Phase 1 code: no access to the Docker socket (permission denied), a transient proxy.golang.org error, and P0 failing only because no images existed.
- Fixed: every image script goes through `docker_init` in `images/lib.sh`. It uses plain `docker` when the user can reach the socket, else `sudo docker` after one visible prompt with the permanent fix printed, else it says the daemon is down. Build containers run as the invoking user, so `images/out` never gets root-owned files. `doctor` reports Docker access. `run.sh test` pre-downloads Go modules with 3 retries. The gate's own P0 run now writes to a scratch directory, not `docs/metrics`.
- Tested: the sudo fallback with a fake `docker` in the VM, and the full gate on the Mac (same result as before: everything passes except the two x86-64 result checks).

### 2026-09-29 — Phase 1 tasks 1.1–1.8 on the dev VM
- **labbase:** Alpine 3.20 + gdb 14.2 + binutils + file. Only the spec's keep-list is on PATH (326 entries removed). 95.9 MB uncompressed, 30.6 MB gzipped, against a 45 MB target; Python is most of it (task 1.9). 50 content checks pass.
- **Sandbox spec + `BuildSpec`:** 14 ways of loosening it are rejected, and the golden file is pinned. `specrun` runs one container with a terminal and reports cgroup peaks. Verified under gVisor: uid 1000, no capabilities, read-only root, 16 MiB tmpfs, no network interfaces.
- **perf image:** perf and probe build reproducibly. **P0** runs 27 checks per runtime in about 30 s.
- **Findings** (arm64 VM):
  1. gdb under gVisor crashes the traced program in the musl loader, 10 of 10 runs (Q13).
  2. `personality(ADDR_NO_RANDOMIZE)` fails. Code and globals stay fixed thanks to `-no-pie`; the stack moves.
  3. The cgroup pids limit counts gVisor's own host tasks; a 32 limit killed the sandbox. Fixed with ADR 0009.
  4. gVisor's in-sandbox CPU clock over-reports; the host cgroup confirms the quota is exact.
  5. gVisor mounts its own synthetic `/sys`.
- **From the owner's commit `d322f44`:** `ctr run` needs root even with the containerd group, and `timeout` needs `--foreground` around `script`. Docs corrected; the rule is in ADR 0007.
- Go bumped to 1.26.6 (containerd v2.4.1 client). Provisioning installs it.
- Next: the owner runs the laptop steps above.

### 2026-09-28 — Phase 0 gate passes on the Linux laptop
- The owner ran Phase 0 on the laptop: `doctor` all required present, `vm up` a no-op, `vm verify` passes on x86_64, and the gate passes (output below). Environment in `docs/metrics/environment-linux-laptop.md`.
- ADR 0007 is confirmed on x86-64: gVisor without a terminal hangs (`timeout 25` exited 124), as on the arm64 VM. Terminal mode works on both, and labs always use it.
- Open: task 0.9 (second developer onboarding) and the laptop's CPU model for the environment file.

Phase 0 gate on the Linux laptop:
```
==> gate for phase 0 — 2026-09-28T01:21Z — linux-laptop
  PASS  run.sh check
  PASS  run.sh test --all
  PASS  run.sh vm verify prints runsc ok + cgroup2fs
  PASS  working tree clean
==> GATE 0 PASSED.
```

### 2026-09-27 — `vm verify` hung silently on the laptop
- On the laptop, `./run.sh vm verify` printed `cgroup2fs` and then nothing. Likely cause: the shell predates the `containerd` group membership that `vm up` added, so verify used `sudo ctr`. That call ran inside `script` (the PTY required by ADR 0007), which is a new terminal, so sudo asked for the password again, and the prompt was swallowed by the captured output. The Mac VM never showed this because Lima has passwordless sudo.
- Fix: verify asks for sudo once, visibly, before doing anything, and runs the whole `script` under sudo, so nothing inside can prompt. It prints each step to stderr. On a timeout or bad output it dumps diagnostics (task state, gVisor processes, runsc log, containerd journal for that container) and then cleans up. `VERIFY_TIMEOUT` overrides the 60 s limit.
- Tested in the VM: the normal path passes, and a simulated hang produces the diagnostics and leaves no containers or sandboxes. The gVisor container finishes in under 0.3 s.
- Still unconfirmed on the laptop: whether the sudo prompt was the whole story, or whether gVisor also hangs there with a terminal. The new diagnostics will show which.

### 2026-09-27 — doctor crash with shellcheck installed; laptop is Ubuntu 26.04
- On the laptop, `./run.sh doctor` stopped silently after the `jq` line. The cause: `shellcheck --version` has no number on its first line, so the generic version parser's `grep` failed, and `set -euo pipefail` aborted the script. It never showed up before because no tested host had shellcheck. Fixed: shellcheck gets its own parser, and a failed version lookup can no longer abort `doctor`. Verified in a container with shellcheck and inside the fully provisioned VM, which covers every Linux branch of `doctor`.
- The laptop runs **Ubuntu 26.04.1** (kernel 7.0), whose repositories ship only Postgres 18. That is what broke `vm up`. With the previous fix, an Ubuntu 26.04 container gets Postgres 16.15 from apt.postgresql.org (`resolute-pgdg`), and every base package exists on 26.04.
- New warning in `doctor` and `provision.sh` when a Docker or distro containerd package is installed: `vm up` replaces the running containerd daemon with the pinned build.
- Laptop specs: 16 vCPU, 15 GB RAM, `/dev/kvm` present. Too little RAM for the 100-lab run (QUESTIONS Q10 updated).

### 2026-09-27 — Postgres 16 on non-reference distros
- On the owner's Linux laptop, `./run.sh vm up` failed with `Unable to locate package postgresql-16`. `provision.sh` assumed Ubuntu 24.04, whose repositories ship Postgres 16; the laptop's distro does not.
- Fix: `provision.sh` detects the distro, refuses non-apt distros with a clear message, and adds the official PostgreSQL apt repository (apt.postgresql.org) when the distro lacks `postgresql-16`. Derivatives map to their Ubuntu or Debian base codename.
- Tested in containers: Debian 12, Ubuntu 22.04 and a Mint-style derivative get 16.15 from the PostgreSQL repo; Ubuntu 24.04 still uses the distro package. All are idempotent. Fedora is refused. The Mac's VM is unchanged.
- `doctor` now shows the distro and reports the Postgres server separately from the optional `psql` client.

### 2026-09-27 — remote added, CI removed
- Owner added a remote and pushed. `main` now holds every Phase 0 commit (fast-forward, linear history) plus the owner's "removed CI" commit.
- No GitHub Actions until the owner asks (ADR 0008). Phase 0 task 0.7 and Phase 5 task 5.13 were rewritten to use local `run.sh` commands and a local rebuild-changed helper.
- Commits pushed so far carry the placeholder author email `your.email@example.com`. Fixing them now means rewriting pushed history and force-pushing, which only the owner should decide. Setting `git config user.email` fixes future commits.
- Next: the owner runs Phase 0 on the Linux laptop (task 0.8). Phase 1 has not started.

### 2026-09-27 — Phase 0 tasks 0.1–0.7
- Committed the scaffold, then on `phase-0-bootstrap`: Go module with config loader and `/healthz` (0.2), Django 5.2 skeleton (0.3), Lima VM + `provision.sh` + `verify-runtime.sh` (0.4–0.6), CI workflow (0.7).
- Pinned: containerd 2.4.1, gVisor release-20260921.0, runc 1.5.2, Postgres 16, Go 1.26.4, uv 0.12.3, Django 5.2.17, Python 3.13.14.
- `vm up` from nothing took 1 min 34 s. `provision.sh` second run is a no-op. Go and Django tests pass on macOS arm64 and inside the Linux arm64 VM with `-race`.
- **Finding, ADR 0007:** gVisor containers started through containerd hang in `Create` unless they have a terminal. A shim goroutine dump shows it waiting on a pipe the sandbox inherited. It reproduces on two gVisor releases; runc is fine. labs always use a terminal, so the product path is unaffected, but P0 scripts and the challenge oracle must allocate a PTY. Not yet checked on x86-64.
- **Finding:** gVisor releases since 2026-08-31 ship as one tarball with a `gvisor-bin/` sidecar directory that must sit next to `runsc`.
- Dev hosts now get gcc (needed by `go test -race`); production still gets no compiler.
- Git author email on the Mac is the placeholder `your.email@example.com`; fix with `git config user.email` and amend before the first push.

Phase 0 gate on the Mac:
```
==> gate for phase 0 — 2026-09-27T21:29Z — macbook.local
  PASS  run.sh check
  PASS  run.sh test --all
  PASS  run.sh vm verify prints runsc ok + cgroup2fs
  PASS  working tree clean
==> GATE 0 PASSED.
```

### 2026-09-27 — two hosts, two developers
- Owner has a Linux x86-64 laptop: it becomes the reference environment (ADR 0001 amended, Q1 resolved). Both arm64 (Mac via Lima) and x86-64 must stay green.
- `run.sh` now works natively on Linux (`vm up` provisions the host, `vm ssh`/`verify` run locally) and gained `doctor`, a per-host dependency report with install hints; `check` is `doctor --strict`. Verified on the Mac and in a bare x86-64 Ubuntu container.
- Added `docs/ONBOARDING.md` for the second developer; Phase 0 gained tasks 0.8 (both hosts green + environment files) and 0.9 (second developer onboarded).
- New questions Q10 (laptop specs, where the 100-lab run happens), Q11 (challenge images arm64?), Q12 (second developer's OS).

### 2026-09-26 — project scaffolded
- Folder structure, CLAUDE.md, phase plan (0–8), conventions, security invariants, ADRs 0001–0006, `run.sh`, `Makefile`, Lima template, gate dispatcher created.
- Spec moved from the repo root to `docs/spec/mvp-spec.md`.
- Git initialised on `main`; nothing committed yet.
- Next: Phase 0, task 0.1.
