# Project status

Update this file at the end of every working session. Keep it factual. Newest log entry at the top.

## Human Checks

Phase 3 human check: 2026-09-30 <OG> OK
Phase 5 human check tier1-01-off-by-one: 2026-10-01 <OG> OK
Phase 5 human check tier1-02-null-deref: 2026-10-01 <OG> OK
Phase 5 human check tier1-03-uninitialized: 2026-10-01 <OG> OK
Phase 5 human check tier1-04-unterminated: 2026-10-01 <OG> OK
Phase 5 human check tier1-05-stack-overwrite: 2026-10-01 <OG> OK
Phase 6 human check: 2026-10-01 <OG> OK
Phase 7 human check: 2026-10-01 <OG> OK

## Current phase

**Phase 7 — Metrics, rollups, admin live view, and the VPS capacity estimate: done** (gate passed 2026-10-02 00:29 UTC on the Mac, with the laptop's P10, live run and human check); merged to `main`.

**Capacity of the target VPS** (8 vCPU, 32 GB; `docs/metrics/vps-capacity.md`): P10 on the laptop with 8 CPUs online held 150 labs with 10 % busy loops, every criterion met; the limit was not reached. Memory caps the box at about 425 labs in typical use and 166 if every lab filled its limit. **`max_sessions` is now 120** (ADR 0019, superseding ADR 0013): 150 less a 20 % margin (*est.*) for a VPS vCPU. The owner confirmed 120 on 2026-10-02 (Q15 resolved).

**Next:** nothing is scheduled. Phase 8 is optional (ADR 0017) and is not started until the owner asks.

<details><summary>Phase 7 laptop checklist (done 2026-10-01)</summary>

**On the laptop, in this order** (about 1 hour 30 minutes, mostly waiting):

1. **Update.**
   ```
   git fetch && git checkout phase-7-metrics-admin && git pull
   ./run.sh labs preflight
   lscpu -e=CPU,CORE,MAXMHZ
   ```
   Keep the `lscpu` output for me: it shows which CPUs stay online. The tier-1 images from Phase 5 and the perf image are used. If `.scratch/local-images.json` is gone, rebuild the labs first (`for d in challenges/tier1-c-fundamentals/0*; do bash scripts/challenge-build.sh "$d" || break; done`).
2. **The capacity search (task 7.11), about an hour.** Close what uses CPU (browser, IDE indexing) first.
   ```
   LAB_HOST=linux-laptop labd/perf/capacity.sh --cpus 8
   ```
   sudo asks once. It takes CPUs 8–15 offline (through `/sys/devices/system/cpu`; `chcpu` is not on your PATH) and runs P10 at 60, 90, 120 and 150 labs (8 minutes each; 180 is refused on 15 GB of RAM). It stops at the first count that misses a criterion, brings the CPUs back, and writes `docs/metrics/capacity-search-<date>-linux-laptop.md`. If you interrupt it and `nproc` says 8, run `labd/perf/capacity.sh --restore-cpus`. Your `lscpu` shows CPUs 0–7 are four performance cores with their hyperthreads (4.6 GHz); 8–11 are the other two P-cores, 12–15 the efficiency cores.
3. **The twenty-lab live check (task 7.8), about 10 minutes.** Two terminals.
   ```
   ./run.sh web-stack up
   LAB_HOST=linux-laptop scripts/live-run.sh
   ```
   live-run prints a staff login for this dev stack. Open http://127.0.0.1:8000/admin/live with it while the run lasts (5 minutes). Watch the gauge fill to 20 and the memory column update. Then kill one lab with its Kill button. Also look at http://127.0.0.1:8000/admin/analytics/capacity.

   live-run takes the screenshot (`docs/metrics/admin-live-<date>-linux-laptop.png`), writes `docs/metrics/data-per-session-<date>-linux-laptop.md`, and ends by saying whether it saw the kill. Then `./run.sh web-stack down`, and add this line at the first column of this file: `Phase 7 human check: YYYY-MM-DD <initials> OK`. If something looked wrong, say so instead.
4. **Commit and push**, then send me the capacity-search `.md` and the `lscpu` output. I write the estimate from them (task 7.12); after that the gate can pass.
   ```
   git add docs/metrics docs/STATUS.md && git commit -m "[P7] metrics: P10 on 8 CPUs; live run; human check" && git push
   ```

</details>

<details><summary>Phase 6 laptop checklist (done 2026-10-01)</summary>

**On the laptop, in this order** (about 30 minutes):

1. **Update.**
   ```
   git fetch && git checkout phase-6-web-app && git pull
   ./run.sh labs preflight
   ```
   The tier-1 lab images you built for Phase 5 are still used. If `.scratch/local-images.json` is gone, rebuild them: `for d in challenges/tier1-c-fundamentals/0*; do bash scripts/challenge-build.sh "$d" || break; done`.
2. **Once per host: a browser for the end-to-end test** (it uses apt, so sudo asks once).
   ```
   (cd web && uv run playwright install --with-deps chromium)
   ```
3. **End-to-end run under gVisor, and its record.**
   ```
   ./run.sh test --e2e
   LAB_HOST=linux-laptop scripts/web-metrics.sh
   ```
   It brings labd and Django up, signs up in a headless browser, solves lab 1 in the page's terminal, submits the flag, checks lab 2 unlocked, stops the lab, times nine more starts, and takes everything down. Send the output if it fails.
4. **Human check (task 6.7): play lab 1 through the Django page.**
   ```
   ./run.sh web-stack up
   ```
   Open http://127.0.0.1:8000, sign up with any email (verification is optional in dev; mail goes to `.scratch/web-stack/web.log`), then:
   - `/learn` shows tier 1 with lab 1 unlocked and the rest locked; read lesson 1;
   - on the challenge page, Start the lab: the terminal opens in `/opt/lab` and `./scores` prints 437;
   - reveal a hint (they come one at a time), solve it with gdb, and submit the flag in the Flag tab: the page says Correct and the terminal stays connected;
   - `/learn` shows lab 2 unlocked; `/dashboard` shows 1 solved and your hints;
   - Stop lab, then `./run.sh web-stack down`.

   Then add this line to this file at the first column: `Phase 6 human check: YYYY-MM-DD <initials> OK`. If something is wrong or confusing, say so instead.
5. **Commit, push, gate.**
   ```
   git add docs/metrics docs/STATUS.md && git commit -m "[P6] metrics: laptop e2e; human check" && git push
   ./run.sh gate --phase 6
   ```

</details>

Phase 0 task 0.9 (second developer onboarding) is still open and non-blocking.

<details><summary>Phase 5 laptop checklist (done 2026-10-01)</summary>

**Phase 5 — Challenge pipeline and tier 1: in progress** on branch `phase-5-challenge-pipeline`. Every task is built:
- flag derivation and vectors, `flagblob`, the manifest schema and `manifestlint`;
- the build script with its oracle, the five tier-1 labs, `challenges.json`;
- `labd pull` and `prune`, the rebuild-changed helper, the authoring guide, the gate.

All five labs pass every build step in the Mac's arm64 VM. The x86-64 builds passed on the laptop with the oracle under gVisor (`docs/metrics/challenges-2026-10-01-linux-laptop.md`). Gate 5 still needs a human to play each lab.

**On the laptop, in this order** (about an hour, most of it playing):

1. **Update.**
   ```
   git checkout phase-5-challenge-pipeline && git pull
   ./run.sh labs preflight
   ```
   No rebuild is needed: the starting-directory fix (ADR 0014) is in labd, which `./run.sh labd` rebuilds, and the lab images already in containerd are unchanged. If `.scratch/local-images.json` is gone, rebuild with `for d in challenges/tier1-c-fundamentals/0*; do bash scripts/challenge-build.sh "$d" || break; done`.
2. **Play each lab once (tasks 5.6–5.10).** Use two terminals.
   ```
   scripts/challenges-json.sh --local > .scratch/challenges.local.json
   sed "s|^challenges_file:.*|challenges_file: $PWD/.scratch/challenges.local.json|" labd/labd.dev.yaml > .scratch/labd.local.yaml
   # A:
   LABD_INTERNAL_SECRET=dev WS_TOKEN_KEY=dev ./run.sh labd --config .scratch/labd.local.yaml
   # B, for each slug in turn (tier1-01-off-by-one, tier1-02-null-deref, tier1-03-uninitialized, tier1-04-unterminated, tier1-05-stack-overwrite):
   slug=tier1-01-off-by-one
   resp=$(curl -s -H 'Authorization: Bearer dev' -d "{\"user_id\":1,\"challenge_slug\":\"$slug\"}" http://127.0.0.1:8081/internal/sessions)
   sid=$(jq -r .session_id <<<"$resp"); echo "http://127.0.0.1:8082/dev/term?session=$sid&t=$(jq -r .ws_token <<<"$resp")"
   ```
   Open the URL within 60 s. The shell starts in `/opt/lab`, which holds only the binary, `README.md` and `src/` (ADR 0014; if it starts in `/home/lab`, labd is older than this branch). `cat README.md`, then check three things:
   - a plain `./<binary>` shows the bug;
   - each hint, read in order, moves you forward (lab 5 has none);
   - the intended path works without reading the solution, ending in `report: LAB{...}`.

   **The hints and the solution are not in the lab, on purpose.** They are private: the build fails if `manifest.yaml`, `solution.md` or `lesson.md` is in an image. Read them in the repo on the laptop, in a third terminal or your editor:

   | Lab | Binary | Repo directory on the laptop |
   | --- | --- | --- |
   | `tier1-01-off-by-one` | `./scores` | `challenges/tier1-c-fundamentals/01-off-by-one/` |
   | `tier1-02-null-deref` | `./greeter` | `challenges/tier1-c-fundamentals/02-null-deref/` |
   | `tier1-03-uninitialized` | `./checkout` | `challenges/tier1-c-fundamentals/03-uninitialized/` |
   | `tier1-04-unterminated` | `./badge` | `challenges/tier1-c-fundamentals/04-unterminated/` |
   | `tier1-05-stack-overwrite` | `./packet` | `challenges/tier1-c-fundamentals/05-stack-overwrite/` |

   The hints are the `hints:` list in that directory's `manifest.yaml`, one per line, cheapest first; reveal one at a time (`grep -A3 '^hints:' <dir>/manifest.yaml | sed -n 2p`, then `3p`, `4p`). `solution.md` in the same directory is the walkthrough; open it only after you have finished or are stuck. `lesson.md` there is what the learner reads before the lab. From Phase 6 the web page shows the lesson and reveals the hints one at a time; until then the repo is the only place to read them.

   Then stop the session before the next slug: `curl -s -X DELETE -H 'Authorization: Bearer dev' http://127.0.0.1:8081/internal/sessions/$sid`. After the last lab, Ctrl-C in A. Then, for each lab, add a line to this file starting at the first column: `Phase 5 human check <slug>: YYYY-MM-DD <initials> OK`. If a lab is confusing or a hint misleads, say so instead; that is a content fix, not an OK.
3. **Commit and run the gate.**
   ```
   git add docs/STATUS.md && git commit -m "[P5] docs: human checks of the five tier-1 labs" && git push
   ./run.sh gate --phase 5
   ```
   Send the gate's output. If you rebuilt any lab, also run `LAB_HOST=linux-laptop scripts/challenges-metrics.sh` and add `docs/metrics` to the commit.

</details>

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
| 3 | Terminal gateway (WebSocket ↔ PTY) | done; merged to `main` | passed on the laptop (x86-64) | 2026-09-30 |
| 4 | Perf suite and measured capacity | done; merged to `main` | passed on the laptop (x86-64) | 2026-10-01 |
| 5 | Challenge pipeline and tier 1 content | done; merged to `main` | passed on the Mac; x86-64 builds and human checks from the laptop | 2026-10-01 |
| 6 | Django web app | done; merged to `main` | passed on the laptop (x86-64) | 2026-10-01 |
| 7 | Metrics, rollups, admin live view; VPS capacity (ADR 0017) | done; merged to `main` | passed on the Mac; P10, live run and human check from the laptop (x86-64) | 2026-10-02 |
| 8 | Production on the VPS, tiers 2–3, beta | optional (deferred by the owner, ADR 0017) | — | — |

States: `not started`, `in progress`, `gate failing`, `done`, `blocked on N`, `optional`.

## Open blockers

- None yet. Decisions awaiting the owner are in [QUESTIONS.md](QUESTIONS.md); each has a default that is in force.

## Measured numbers so far

x86-64 laptop, one lab (`docs/metrics/single-lab-2026-09-29-linux-laptop.json`, rows in [metrics/capacity.md](metrics/capacity.md)):
- **Memory:** gVisor 25.8 MiB cgroup p95 at a breakpoint, against the spec's estimate of ~130 MB. runc uses 13.1 MiB.
- **Start latency:** container to gdb prompt, p95 1543 ms (target < 2 s).
- **Base image:** 28.4 MiB.

Early warning: a whole scripted gdb session takes 6.4× longer under gVisor than under runc, against a < 2× target for `step`. The per-command number comes in Phase 4.

Capacity of the target VPS (8 vCPU, 32 GB, 400 GB, 32 TB), from P10 on the laptop with 8 CPUs online ([metrics/vps-capacity.md](metrics/vps-capacity.md), `docs/metrics/capacity-search-2026-10-01-linux-laptop.md`):

| Bound | Concurrent labs | Source |
| --- | --- | --- |
| CPU, 10 % busy loops | at least 150, every criterion met; limit not reached (laptop RAM stopped the run). Echo p95 9.7 ms, `next` p95 18 ms, host CPU 98 % at 150 | `run-P10-2026-10-01-linux-laptop-n150.json` |
| Memory, typical | about 425 (50.2 MB per lab; *est.* 3.2 GB for the rest; 25 % headroom) | `run-P2-2026-10-01-linux-laptop.json` |
| Memory, every lab at 128 MiB | about 166 | `deploy/labd.prod.yaml` limits |
| Disk, bandwidth | not limits: about 98 GB of data and 0.25 TB a month at 400 labs | `data-per-session-2026-10-02-linux-laptop.json`, `run-P8-2026-10-01-linux-laptop.json` |
| **`max_sessions`** | **120**: 150 less a 20 % margin (*est.*) for a VPS vCPU (ADR 0019) | |

- A learner on the real tier-1 labs costs 0.17 to 0.19 % of a core; a busy loop 50 %. The busy-loop share and `cpu_millicores` are the dials.
- Data per session-minute: `events` 1474 B, `samples` 5324 B (20-lab live run).

## Log

### 2026-10-02 — Phase 7 gate passed; VPS capacity estimate (task 7.12)
- **Task 7.12**: `docs/metrics/vps-capacity.md` rewritten from P10. Its CPU section is measured only; the VPS vCPU margin (20 to 30 %, *est.*) is its own section. Disk recomputed with the measured 1474 B (`events`) and 5324 B (`samples`) per session-minute: about 29 GB at 120 labs around the clock, 98 GB at 400. `capacity.md` gets a Phase 7 disk table outside the generated block.
- **ADR 0019 supersedes ADR 0013**: production `max_sessions` 120, `max_queue` 50. `deploy/labd.prod.yaml` and `TestParse_ProductionFile` follow. **QUESTIONS.md Q15** asks the owner to confirm 120 (or 105, or 100); 120 is in force.
- **Found and fixed**: `test_dashboard_numbers` failed in the first 30 minutes of every UTC day. Its fixture session began 30 minutes before now, and the usage view counts starts on their start day and lengths on their end day, so just after midnight they are two rows. The view was right; the test now looks up each row by its own day.
- **Checks on the Mac**: `check`, `test --all` (138 web tests), `lint` pass (staticcheck only warns: the Mac's is built with Go 1.25); `test --integration` and `test --e2e` (runc, not authoritative) pass; `go test -race -count=5 ./...` passes; staticcheck in the VM, with and without `-tags integration`, clean.
- **Next:** none. Phase 8 is optional (ADR 0017).

Phase 7 gate on the Mac:
```
  PASS  no labd running, namespace labs empty

==> [gate 7] Go and Django suites
  PASS  run.sh test --go
  PASS  ruff check
  PASS  pytest (unit and view)
  PASS  makemigrations --check clean

==> [gate 7] integration: the sampler writes every metric from 2 real labs (task 7.1)
  PASS  run.sh test --integration
  PASS  no containers left in namespace labs

==> [gate 7] end to end: lab 1 solved in the browser; admin kills a lab, drains and resumes (task 7.7)
  PASS  run.sh test --e2e
  PASS  no containers left in namespace labs

==> [gate 7] rollups and retention on the dev Postgres, as role web (tasks 7.4, 7.5)
  PASS  rollup --minute writes rows from the e2e run's samples and events
  PASS  rollup --hour writes rows from the minute rows
  PASS  retention runs as role web

==> [gate 7] human check (task 7.8): twenty labs watched on /admin/live, one killed
  PASS  STATUS.md records 'Phase 7 human check'
  PASS  screenshot of /admin/live (admin-live-2026-10-02-linux-laptop.png)

==> [gate 7] VPS capacity (ADR 0017, tasks 7.11 and 7.12)
  PASS  P10 on x86-64 with 8 CPUs under runsc (capacity-search-2026-10-01-linux-laptop.json): largest passing 150
  PASS  vps-capacity.md: no estimate left in the CPU section

==> GATE 7 PASSED. Paste this output into docs/STATUS.md.
```

### 2026-10-01 — Phase 7 built: sampler, rollups, dashboards, admin live view
- **Sampler** (7.1, `labd/internal/metrics`):
  - every 10 s, host memory, CPU, CPU and memory pressure, disk, and active and queued counts;
  - per lab, memory, CPU, Sentry RSS, and terminal bytes in and out; one `COPY` per tick;
  - the peak memory goes to `sessions.peak_rss_mb`.
  - Integration in the VM: 2 real labs give every metric name.
- **Events** (7.2, 7.3): labd and web already wrote them all; tests now check each lifecycle and its data keys.
- **Rollups and retention** (7.4, 7.5):
  - `manage.py rollup` and `retention` work on hand-computed fixtures, and on the VM's Postgres as role web;
  - **ADR 0003 amended**: the rollup tables are web's, and web may delete old samples and events (labd migration 0002).
- **Dashboards** (7.6): Live, Usage, Learning and Capacity at `/admin/analytics/…`, staff only, as inline SVG charts.
- **Admin live** (7.7): `/admin/live` has kill, drain and resume, challenge toggles, and the pending-pull banner. **ADR 0018**: drain goes through `POST /internal/drain`, not an override file. A browser test kills a real lab and drains and resumes, in the VM.
- **Live run** (7.8): `scripts/live-run.sh` is ready for the human check. A dry run in the VM (6 labs, one killed) worked. It showed `samples` growing about 5.4 KB per session-minute, against my estimate of 1.5 KB; the laptop's 20-lab run will give the real figure.
- **Gate 7** (7.9): wired; also runs the browser tests and checks the capacity items from ADR 0017. `./run.sh manage ARGS` runs `manage.py` against the dev Postgres. systemd timers for rollup and retention are in `deploy/systemd/` for Phase 8.
- **Checks on the Mac:** `check`, `test --all` (136 web tests), `lint`, `test --integration`, `test --e2e` pass; staticcheck in the VM clean; `go test -race -count=5 ./...` passes.
- **Next:** the laptop steps at the top of this file, then task 7.12 (the estimate) from the P10 record.

Phase 7 gate on the Mac (expected failures: the laptop items):
```
==> gate for phase 7 — 2026-10-01T15:23Z — macbook.local
==> [gate 7] preflight: no labd running, namespace labs empty
==> preflight ok: no labd running, namespace labs empty
  PASS  no labd running, namespace labs empty
==> [gate 7] Go and Django suites
  PASS  run.sh test --go
  PASS  ruff check
  PASS  pytest (unit and view)
  PASS  makemigrations --check clean
==> [gate 7] integration: the sampler writes every metric from 2 real labs (task 7.1)
  PASS  run.sh test --integration
  PASS  no containers left in namespace labs
==> [gate 7] end to end: lab 1 solved in the browser; admin kills a lab, drains and resumes (task 7.7)
  PASS  run.sh test --e2e
  PASS  no containers left in namespace labs
==> [gate 7] rollups and retention on the dev Postgres, as role web (tasks 7.4, 7.5)
  PASS  rollup --minute writes rows from the e2e run's samples and events
  PASS  rollup --hour writes rows from the minute rows
  PASS  retention runs as role web
==> [gate 7] human check (task 7.8): twenty labs watched on /admin/live, one killed
  FAIL  STATUS.md lacks a line 'Phase 7 human check: YYYY-MM-DD <who> OK'
  FAIL  screenshot of /admin/live — no file matches ~/labbing-platform/docs/metrics/admin-live-*.png
==> [gate 7] VPS capacity (ADR 0017, tasks 7.11 and 7.12)
  FAIL  no P10 record from x86-64 with 8 CPUs under runsc: on the laptop LAB_HOST=linux-laptop labd/perf/capacity.sh --cpus 8, commit docs/metrics
  FAIL  vps-capacity.md: 3 *est.* left in the CPU section (task 7.12)
==> GATE 7 FAILED. Fix the FAIL lines above; do not start the next phase.
```

### 2026-10-01 — Phase 7 started: Phase 8 optional, the VPS capacity estimate
- **ADR 0017**: the owner made Phase 8 optional and asked for a measured estimate for the target VPS (8 vCPU, 32 GB, 400 GB NVMe, 32 TB). It moves into Phase 7 as tasks 7.10–7.12, measured on the laptop with 8 CPUs online. The phase board and `IMPLEMENTATION_PLAN.md` mark Phase 8 optional.
- **First estimate** (`docs/metrics/vps-capacity.md`), from Phase 4's measured costs:
  - memory: about 425 labs in typical use (50.2 MB each), 166 if every lab filled its 128 MiB limit;
  - bandwidth: about 0.25 TB a month at 400 labs; disk: under 80 GB;
  - CPU decides, on two unmeasured inputs, a real learner's CPU and the share of busy loops: about 80 to 130 labs at 75 % CPU with 10 % busy loops.
  - `max_sessions` stays 100.
- **Built first** (7.10, 7.11), because it is the priority:
  - a `learner` profile: the five real tier-1 labs, gdb started and quit again and again, `run`, software watchpoints;
  - P10 and `labd/perf/capacity.sh`, which step through counts on N CPUs and write `capacity-search-*.md`.
  - Tested: unit tests, plus 3 and 20 labs in the arm64 VM under runc (not authoritative), with no command errors.
- **Next**: the owner runs the capacity search on the laptop (steps above). Meanwhile, tasks 7.1–7.9.

### 2026-10-01 — A correct flag stops the lab (ADR 0016)
- The owner asked, after the Phase 6 gate passed, that a lab stop once its flag is accepted. **ADR 0016** overrides the spec's "kept open independently of the flag".
- web stops the user's live session for that challenge with reason `solved`; the page shows "the lab has ended: the challenge was solved" and hides Stop. A wrong flag, a resubmission, or a solve with no lab stops nothing; if the stop fails, the solve still counts and the lab idles out as before.
- Tests: five new view tests (107 web tests); the e2e test expects `ended/solved` right after the flag and still covers the Stop button in its timed runs. It passes in the arm64 VM (labs under runc).
- Optional on the laptop: `git pull` on `main`, then `./run.sh test --e2e` to see it under gVisor.
- Merged into `main` (fast-forward) after gate 6 passed again on the Mac with the change (its x86-64 record and human check are the laptop's, from before the change):
```
==> gate for phase 6 — 2026-10-01T14:31Z — macbook.local
==> [gate 6] preflight: no labd running, namespace labs empty
==> preflight ok: no labd running, namespace labs empty
  PASS  no labd running, namespace labs empty
==> [gate 6] web: lint, unit and view tests, migrations
  PASS  ruff check
  PASS  pytest (unit and view, fake labd)
  PASS  makemigrations --check clean
==> [gate 6] shared vectors (S12, S15)
  PASS  flag vectors in Python
  PASS  WebSocket token vectors in Python
  PASS  WebSocket token vectors in Go
==> [gate 6] command-recording notice on the lab page (S19)
  PASS  template test
==> [gate 6] end to end on this host (sign up, solve lab 1 in the browser, unlock lab 2, stop)
  PASS  run.sh test --e2e
  PASS  no containers left in namespace labs
==> [gate 6] authoritative end-to-end run (x86-64, labs under runsc; ADR 0001)
  PASS  x86-64 e2e record (web-2026-10-01-linux-laptop.json): click-to-prompt p95 569 ms
==> [gate 6] human check (task 6.7: lab 1 solved through the Django lab page)
  PASS  STATUS.md records 'Phase 6 human check'
==> GATE 6 PASSED. Paste this output into docs/STATUS.md.
```

### 2026-10-01 — Phase 6 gate passed on the x86-64 laptop
- The owner ran the e2e test under gVisor and played lab 1 through the Django page (human check 2026-10-01).
- **Start latency** (`docs/metrics/web-2026-10-01-linux-laptop.md`, 10 runs, runsc): click Start to the terminal page p95 418 ms; click Start to the shell prompt p95 569 ms, against the spec's 2 s.
- Phase 6 merged into `main` (fast-forward) and pushed.
- **Next:** the owner asked that a correct flag stop the lab; then Phase 7 when the owner says to continue.

Phase 6 gate on the Linux laptop:
```
==> gate for phase 6 — 2026-10-01T14:24Z — linux-laptop
==> [gate 6] preflight: no labd running, namespace labs empty
==> preflight ok: no labd running, namespace labs empty
  PASS  no labd running, namespace labs empty
==> [gate 6] web: lint, unit and view tests, migrations
  PASS  ruff check
  PASS  pytest (unit and view, fake labd)
  PASS  makemigrations --check clean
==> [gate 6] shared vectors (S12, S15)
  PASS  flag vectors in Python
  PASS  WebSocket token vectors in Python
  PASS  WebSocket token vectors in Go
==> [gate 6] command-recording notice on the lab page (S19)
  PASS  template test
==> [gate 6] end to end on this host (sign up, solve lab 1 in the browser, unlock lab 2, stop)
  PASS  run.sh test --e2e
  PASS  no containers left in namespace labs
==> [gate 6] authoritative end-to-end run (x86-64, labs under runsc; ADR 0001)
  PASS  x86-64 e2e record (web-2026-10-01-linux-laptop.json): click-to-prompt p95 569 ms
==> [gate 6] human check (task 6.7: lab 1 solved through the Django lab page)
  PASS  STATUS.md records 'Phase 6 human check'
==> GATE 6 PASSED. Paste this output into docs/STATUS.md.
```

### 2026-10-01 — Phase 6 built: the Django web app
- **Models and import** (6.1, 6.3): six apps; `tiers`, `lessons`, `challenges`, `progress`, `flag_attempts`; labd's `sessions` and `events` as unmanaged models, with a test that compares their columns to labd's SQL. `migrate` as role web on the VM's Postgres works, and the models read labd's rows. `import_challenges` upserts, disables absent ones, never deletes.
- **Flags and tokens** (6.2, 6.6): Python passes the 10 flag vectors. **ADR 0015:** web mints the browser's WebSocket tokens; labd mints only with `dev_mint_tokens` (dev and perf configs, never production), because labd-perf, replay and `/dev/term` use it. New `challenges/schema/ws_token_vectors.json`, computed with openssl, passes in Go and Python.
- **Accounts and pages** (6.4, 6.5, 6.10, 6.11): allauth, email only (Q4 default, now implemented); signup unlocks lab 1. `/learn`, lessons with sanitised markdown, `/dashboard` (solved, minutes, hints, streak), Django admin with an enable toggle; Progress and labd's rows read-only.
- **Labs** (6.6–6.9): challenge page, start (unlocked, one active session per user), stop, the terminal page (`static/js/lab.js`: token, frames, TTL, Extend, reconnect, recording notice), flags (10 per 10 minutes, then 429) and hints (in order). Hint and flag forms swap partials, so the terminal never reloads.
- **End to end** (6.12): `./run.sh test --e2e` (`scripts/web-stack.sh e2e`) solves lab 1 in headless Chromium against real labd and Postgres. In the arm64 VM, labs run under runc (Q13): click to prompt p95 346 ms over 3 runs (not an x86-64 number; the laptop run is the one that counts).
- **Gate 6** (6.13) is wired. It also needs an x86-64 e2e record under runsc (`scripts/web-metrics.sh`), as gate 5 needed the x86-64 builds.
- **Checks on the Mac:** `check`, `test --all` (102 web tests), `lint`, `test --integration` pass; staticcheck in the VM clean; `go test -race -count=5 ./...` passes.
- **Next:** the laptop steps at the top of this file.

Phase 6 gate on the Mac (expected failures: the laptop items):
```
==> gate for phase 6 — 2026-10-01T14:08Z — macbook.local
==> [gate 6] preflight: no labd running, namespace labs empty
==> preflight ok: no labd running, namespace labs empty
  PASS  no labd running, namespace labs empty
==> [gate 6] web: lint, unit and view tests, migrations
  PASS  ruff check
  PASS  pytest (unit and view, fake labd)
  PASS  makemigrations --check clean
==> [gate 6] shared vectors (S12, S15)
  PASS  flag vectors in Python
  PASS  WebSocket token vectors in Python
  PASS  WebSocket token vectors in Go
==> [gate 6] command-recording notice on the lab page (S19)
  PASS  template test
==> [gate 6] end to end on this host (sign up, solve lab 1 in the browser, unlock lab 2, stop)
  PASS  run.sh test --e2e
  PASS  no containers left in namespace labs
==> [gate 6] authoritative end-to-end run (x86-64, labs under runsc; ADR 0001)
  FAIL  no x86-64 e2e record: on the laptop run the gate, then LAB_HOST=linux-laptop scripts/web-metrics.sh, commit docs/metrics
==> [gate 6] human check (task 6.7: lab 1 solved through the Django lab page)
  FAIL  STATUS.md lacks a line 'Phase 6 human check: YYYY-MM-DD <who> OK'
==> GATE 6 FAILED. Fix the FAIL lines above; do not start the next phase.
```

### 2026-10-01 — Phase 5 gate passed
- The owner played all five tier-1 labs on the laptop on 2026-10-01 and recorded one "ALL-Labs" stamp. At the owner's request it was expanded into the five per-lab lines gate 5 checks, same date and initials.
- The x86-64 builds of all five passed on the laptop with the oracle under runsc (`docs/metrics/challenges-2026-10-01-linux-laptop.md`). On the laptop the gate then failed only on the human-check lines.
- Gate 5 below was run on the Mac after the lines were added (arm64 builds, oracle under runc; the x86-64 checks read the laptop's record).
- Phase 5 merged into `main` (fast-forward) and pushed.
- **Next:** Phase 6, when the owner says to continue.

Phase 5 gate on the Mac:
```
==> gate for phase 5 — 2026-10-01T13:34Z — macbook.local
==> [gate 5] preflight: no labd running, namespace labs empty
==> preflight ok: no labd running, namespace labs empty
  PASS  no labd running, namespace labs empty
==> [gate 5] unit tests (flag vectors, manifestlint, challenges.json, pull and prune)
  PASS  run.sh test --go
  PASS  flag_vectors.json passes in Go
==> [gate 5] every challenge builds and proves itself on this host (lint, flag, build, leak, image, oracle, addresses)
  PASS  challenge-build.sh challenges/tier1-c-fundamentals/01-off-by-one
  PASS  challenge-build.sh challenges/tier1-c-fundamentals/02-null-deref
  PASS  challenge-build.sh challenges/tier1-c-fundamentals/03-uninitialized
  PASS  challenge-build.sh challenges/tier1-c-fundamentals/04-unterminated
  PASS  challenge-build.sh challenges/tier1-c-fundamentals/05-stack-overwrite
==> [gate 5] challenges.json
  PASS  challenges.json regenerates and validates
  PASS  committed challenges.json has 5 entries and matches the manifests
==> [gate 5] authoritative results (x86-64, ADR 0001)
  PASS  x86-64 P0 from Phase 1 (p0-2026-09-29-linux-laptop.json)
  PASS  x86-64 builds of all five, oracle under runsc (challenges-2026-10-01-linux-laptop.json)
==> [gate 5] human checks (task 5.6-5.10: each lab played once in the dev page)
  PASS  STATUS.md records 'Phase 5 human check tier1-01-off-by-one'
  PASS  STATUS.md records 'Phase 5 human check tier1-02-null-deref'
  PASS  STATUS.md records 'Phase 5 human check tier1-03-uninitialized'
  PASS  STATUS.md records 'Phase 5 human check tier1-04-unterminated'
  PASS  STATUS.md records 'Phase 5 human check tier1-05-stack-overwrite'
==> GATE 5 PASSED. Paste this output into docs/STATUS.md.
```

### 2026-10-01 — Lab shell starts in /opt/lab; hints and solution are repo files
The owner built all five labs on the laptop and played lab 1: the bug showed as designed (437, a garbage report line). Two problems, both fixed:
- **The shell started in `/home/lab`, not `/opt/lab`.** The base spec's `process.cwd` overrode the image's `WORKDIR`. **ADR 0014:** the lab starts in the image's `WORKDIR`, else `/home/lab`; `HOME` stays `/home/lab`, and cwd is not a security setting. labd's session manager (`Runtime.ImageWorkingDir`) and `RunOnce` (specrun, the build's oracle) read it with the same helper. The golden spec's only change is `process.cwd`.
  - Tests: `BuildSpec` cwd table and bad-cwd rejections; the manager passes the image's `WORKDIR` to `Create`; integration `TestCwd_FromImageWorkdir` types `pwd` under runsc: perf gives `/home/lab`, lab 1 gives `/opt/lab`, `HOME` is `/home/lab` in both.
  - By hand with specrun (runsc, arm64): lab 1 starts in `/opt/lab`, and `./scores` prints `total of 5 scores: 437` with no `cd`. Perf starts in `/home/lab`.
  - **No image rebuilt.** Lab images already carry `WORKDIR /opt/lab`. Rebuilding all five in the arm64 VM passed every step and gave the same digests as before.
- **`manifest.yaml` and `solution.md` are not in the lab, by design.** The laptop steps above now say they are repo files, list each lab's directory and binary, and give a command to reveal one hint at a time. `challenges/README.md`'s dev-page section says the same. Hints appear in the web page from Phase 6.
- **Checks on the Mac:** `check`, `test --all`, `lint` pass; `test --integration` passes; staticcheck in the VM clean (also with `-tags integration`); `go test -race -count=5 ./...` in labd passes. `./run.sh gate --phase 5` on the Mac passes every check except the five human-check lines; the x86-64 build record (the owner's commit `7b3e31f`) now passes.
- **Next:** the laptop steps at the top of this file. Lab 1 must be played again: it now starts in `/opt/lab`.

### 2026-10-01 — Phase 5 built: flag, pipeline, five tier-1 labs
- **Flag** (5.1, 5.2): `labd/internal/flag` derives the flag; 10 vectors computed with `openssl` and `base32`, one slug non-ASCII. `flagblob` writes `flag_blob.h` from the secret, read from the environment. A test compiles the header with a real C compiler: the right key prints the flag, wrong keys don't, and the binary holds neither `LAB{` nor the flag body. **ADR 0005 amended:** the flag is 29 bytes, not 28; the blob's seed is derived from the secret and slug, because a random seed breaks reproducible builds.
- **Manifest** (5.3): `manifest.schema.json` adds `key_value` and `boss`. `manifestlint` applies the schema file itself, plus the directory, boss and hint rules; 12 rejection cases are tested.
- **Build script** (5.4, 5.5): `scripts/challenge-build.sh` runs lint, flag, a double build with identical hashes, the static check, the leak check, the image, the oracle and the address check, then publishes.
  - The oracle runs `solve.gdb` inside the lab image under the real sandbox, typed in through the terminal, so it never enters an image.
  - Tested: TEMPLATE passes; a literal flag fails step 4; a wrong `solve.gdb` fails step 5; leftover template text fails step 1.
- **Five labs** (5.6–5.10): off-by-one (key 400), null deref (347), uninitialized (4999), unterminated (FNV-1a hash), stack overwrite (the boss, no hints). Each has source under 75 lines, a manifest, `solve.gdb`, a lesson (about 10 minutes), a walkthrough and a README. All pass every step in the arm64 VM (oracle under runc). On a plain run, each shows its bug: 437 instead of 400; a crash; `SPRING10` wrongly accepted; `Thompsonadmin`; a dropped packet.
  - Lab 3's first version read a zero from the stack on arm64, so the bug didn't show; the build's "plain run must not print the flag" check refused it. It now inherits `ok = 1` from a twin function called just before.
- **challenges.json** (5.11): generated, schema-checked, five entries. All are disabled for now: nothing is pushed until the GHCR namespace is known (QUESTIONS Q2, default unchanged). labd ignores a disabled entry that has no image. `--local` enables this host's dev images.
- **pull and prune** (5.12): moved behind an image-store interface and unit-tested. `prune` now deletes, but only `lab.keep` images unused for 14 days; the spec's rule alone would also delete labbase.
- **No CI** (5.13): `scripts/challenges-changed.sh` rebuilds what changed, and all five when labbase or the build tooling changed.
- **Authoring guide** (5.14): `challenges/README.md` and `scripts/challenge-new.sh`. A fresh agent given only the guide and TEMPLATE wrote a sixth challenge that passed on its first build, then it was deleted. The gaps it reported are fixed.
- **Gate 5** (5.15) is wired. It also requires an x86-64 build record of all five (`scripts/challenges-metrics.sh`), since challenge content counts only from x86-64 (ADR 0001).
- **Checks on the Mac:** `check`, `test --all`, `lint`, `test --integration` pass; staticcheck in the VM clean. Gate 5 on the Mac passes everything except the x86-64 build record and the five human checks.
- **Next:** the owner's laptop steps at the top of this file.

### 2026-10-01 — Phase 4 gate passed on the x86-64 laptop
- **Final laptop P2** (disk in KiB): disk per lab 0.0078 MB (8 KiB, two empty overlay directories); start p95 1832 ms; echo p95 6.1 ms; host 9.8 GB peak, 4.8 GB before any lab.
- **Headline numbers** (`docs/metrics/perf-report-2026-10-01-linux-laptop.json`, `capacity.md`):
  - per-lab memory: 25.5 MiB cgroup p95, 50.2 MB whole host cost;
  - host at 100: 9.8 GB used (desktop included); CPU 33 % of 16 vCPUs with 10 abusers;
  - start p95 1832 ms; echo p95 2.5 ms at 100 mixed;
  - gVisor: +14 MiB per lab, `step` 3.4× (7 ms; accepted), about +1.4 s per start;
  - derived `max_sessions` 394; production 100 (ADR 0013, updated to the final numbers).
- Phase 4 merged into `main` (fast-forward) and pushed.

Phase 4 gate on the Linux laptop:
```
==> gate for phase 4 — 2026-10-01T04:27Z — linux-laptop
==> [gate 4] unit tests (includes internal/perf)
  PASS  run.sh test --go
==> [gate 4] perf report from an x86-64 host (ADR 0001)
  PASS  perf report perf-report-2026-10-01-linux-laptop.json (host linux-laptop)
  PASS  every spec value measured
  PASS  run-P1..P9 recorded for linux-laptop
  PASS  every missed criterion has a decision line
==> [gate 4] capacity table and max_sessions
  PASS  capacity.md has no est. value
  PASS  max_sessions ADR (0013-max-sessions.md)
==> GATE 4 PASSED. Paste this output into docs/STATUS.md.
```

### 2026-10-01 — start latency passes after the fix; disk per lab measured in KiB
- **Laptop rerun with the gateway fix** (P1, P2, P3, P8, the KVM and runc runs; `docs/metrics/run-P*-2026-10-01-linux-laptop*.json`):
  - start p95: P2 2484 → 1839 ms, P8 2507 → 1855 ms, P1 2133 → 1496 ms, P2 on KVM 1777 ms; all under 2 s;
  - runc: P2 458 ms, P1 412 ms, so gVisor adds about 1.4 s per start;
  - nothing else moved: P3 echo p95 2.5 ms, abusers bounded, no OOM, every user completed.
- **Gate 4** then failed on `per_lab.disk_mb = 0`. Two bugs in `disk.sh`, both fixed:
  - It measured in whole MB, and 100 labs add under 1 MB. It now uses KiB, sampled before the first session and once all are running.
  - A `du` racing teardown produced invalid JSON (P2-kvm's "exit status 2").
  - Dev VM: 8 KiB per lab.
- **Next:** the owner reruns P2 and the report (steps above), then the gate.

### 2026-10-01 — Phase 4 on the laptop: everything measured; start latency was a gateway bug
- **The owner ran the full suite on the laptop** (14 runs, about 6 h; `docs/metrics/run-P*-2026-09-30-linux-laptop*.json`, `perf-report-2026-09-30-linux-laptop.json`). Headline numbers at 100 labs:
  - **Memory:** lab cgroup p95 25.5 MiB; whole host cost 43.8 MB per lab, including the shim; host 8.5 GB peak with 5.0 GB before any lab; no OOM kill.
  - **Echo:** p95 1.6 ms at 100 mixed labs, 6.2 ms at 100 readers. Abusers did not move it.
  - **CPU:** readers 0.03 % and steppers 0.39 % of a core per lab; each abuser held to its 0.5-core quota (51 %); 100 mixed labs use 32 % of 16 vCPUs, nearly all of it the 10 abusers.
  - **Churn and recovery:** P4 (30 min) and P9 (2 h, 1540 sessions) had no leak, containers equal to active, and stable starts. P5 had all 100 labs running again 3.6 s after SIGKILL, with 100 of 100 users back on their own lab. P6 queued 50 in order and drained them FIFO. P7: a start without the image fails, so pre-pull is required; the image comes back in 1.3 s.
  - **Disk and bandwidth:** 0.01 MB of snapshot per lab; 1.0 KB of `events` per session-minute; 61 B/s out per stepper, 0.05 Mbps for 100.
  - **gVisor against runc:** +14 MiB per lab (cgroup); `step` 9.05 ms against 3.09 ms, 2.9× but 6 ms absolute, accepted. The KVM platform made no difference against systrap.
- **Start latency missed** (p95 2.4–2.5 s at N, 2.1 s for one lab; target < 2 s). The runs showed why: request-to-running was 1006–1008 ms for every session under both runtimes, though a lab is created in about 250 ms. The gateway polled a session being created at the 1 s queue interval. **Fixed** (`11fb540`): it now checks every 50 ms while creating. On the dev VM, request to running fell from 1006 to 104 ms and start p95 from 1283 to 546 ms. The laptop re-measures (steps above). No decision line is written for the old misses, so the gate enforces the rerun.
- **`max_sessions` (ADR 0013):** 100 in production, `max_queue` 50, in the new `deploy/labd.prod.yaml`; a config test pins runsc only, no dev switches, and 100/50. Memory alone would allow 446, but under abuse each lab burns half a core. At the spec's 10 % abuser mix, 200 labs would need about 10 cores on the 8-vCPU VPS.
- **capacity.md:** no estimates left. Every spec row is measured in the generated block, with decision lines for the step overhead and the start-latency root cause. The runc memory row now compares cgroups: host per-lab memory on a desktop moves by ±15 MB between runs, too much to subtract one run from another.
- **QUESTIONS Q10** resolved: the laptop runs the 100-lab scenarios.
- **Test hygiene:** one combined `-race -count=5` run failed once in `internal/term` and did not reproduce in 65 more. The likely cause, the new running-frame test racing the fake clock, now steps the clock under a real deadline. Two more `-race -count=5` rounds over the whole module pass.
- **Checks on the Mac:** `check`, `test --all`, `lint` pass; staticcheck in the VM clean. `./run.sh gate --phase 4`: every check passes except the decision lines for the five pre-fix start-latency misses (P1, P1-kvm, P2, P2-kvm, P8).

### 2026-09-30 — Phase 4 driver built; dev VM smoke passes
- **Built:** `labd-perf run|report|profiles` and `internal/perf` (percentiles, the spec-schema report, profiles from `session.gdb` with a jittered pacer, virtual users over the real API and WebSocket, the host collector, the scenario runner, criteria, the report builder). Also `labd/perf/scenario.sh` (private labd from `labd.perf.yaml`; P5 kill, P7 image flush, disk sampling, runc and KVM switches), `disk.sh`, and `runall.sh`, reached as `./run.sh perf --scenario all`. Gate 4 is wired. What differs from the plan is in the phase document's "As built" section.
- **Findings while building:**
  - Each lab's containerd shim (about 24 MiB RSS) runs outside the lab's cgroup, so the cgroup understates a lab's cost. `max_sessions` uses the larger of the cgroup p95 and the host's (used − idle) / labs. On the dev VM, one lab is 25.3 MiB cgroup but 39.4 MB on the host.
  - labd reconciles before it listens, so P5 measures from the SIGKILL, not from labd answering.
  - A reader can sit idle longer than the 60 s reconnect grace, so users now reattach as soon as the socket drops.
  - The dev VM has nine orphaned gVisor sandboxes from Phase 0/1 experiments (`verify-5994`, `dbg1`, `c-*`), invisible to containerd. `./run.sh labs ls` and `preflight` now warn about such orphans. They were left alone; the warning prints the command that removes one.
- **Dev VM smoke** (task 4.6, arm64, cap 20; validates the driver, not the capacity): P1, P6, P4 and P5 pass every criterion. P6: 20 admitted, 10 queued in order, FIFO drain. P4: 140 sessions, containers equal to active, no leak. P5: all 20 labs running again 305 ms after SIGKILL, 20 of 20 users back on their own lab. Files: `docs/metrics/run-P{1,4,5,6}-2026-09-30-dev-vm.json`.
- **capacity.md** is restructured: the Phases 1 and 3 single-lab rows are hand-written; a block generated by `labd-perf report` sits between markers; the remaining estimates are listed under "Still estimated" until the laptop runs replace them.
- **Checks on the Mac:** `./run.sh check`, `test --all` and `lint` pass (`internal/perf` included, `-race`); staticcheck in the VM is clean. `./run.sh gate --phase 4` fails only on the laptop results: no x86-64 perf report, estimates left, no `max_sessions` ADR yet.
- **Next:** the owner runs the laptop steps at the top of this file.

### 2026-09-30 — Phase 3 gate passed on the x86-64 laptop
- **Laptop P1** (`docs/metrics/p1-2026-09-30-linux-laptop.md`, runsc, 10 min at 20 commands/min):
  - request to `(gdb)` over the socket 2296 ms (one start);
  - echo p50 3.2 ms, p95 5.5 ms, max 9.6 ms;
  - lab cgroup 28.0 MiB p50, 29.4 MiB max; Sentry RSS 47.8 MiB p50;
  - CPU 0.53 % of a core; WebSocket 3.6 B/s in, 57 B/s out; 201 commands, 0 errors.
- **capacity.md:** the "CPU: during step loops" and "Bandwidth per active terminal" rows now carry these numbers. Both are far below the spec's estimates (5–20 % of a core; 0.5–5 KB/s). At the owner's request, the echo latency row also takes its number from the same file (p95 5.5 ms, target < 100 ms).
- **Start latency miss, recorded:** 2296 ms against a p95 < 2 s target, from a single start that includes the client starting gdb. The container-to-prompt p95 is 1543 ms. Recorded under "Failures and decisions" in capacity.md; Phase 4 (P2) measures it properly. Not a gate failure (the phase document requires the number).
- The owner's human check passed on the laptop (the line at the top of "Current phase"). Phase 3 is merged into `main` (fast-forward) and pushed.

Phase 3 gate on the Linux laptop:
```
==> gate for phase 3 — 2026-09-30T15:48Z — linux-laptop
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
  PASS  x86-64 P1 with echo latency p95 recorded (p1-2026-09-30-linux-laptop.json)
  PASS  STATUS.md records 'Phase 3 human check'
==> GATE 3 PASSED. Paste this output into docs/STATUS.md.
```

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
