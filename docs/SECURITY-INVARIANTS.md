# Security invariants

A shell is exposed to untrusted users. These invariants are what make that acceptable. Each one names the test that enforces it. If you change code and a test here fails, the test is right and the code is wrong. Do not edit the test to make it pass; write an ADR and get the owner's agreement first.

Source: spec section "Sandbox security profile" and "Terminal gateway".

| # | Invariant | Where enforced | Test |
| --- | --- | --- | --- |
| S1 | Lab containers run under gVisor (`io.containerd.runsc.v1`), never `runc`, in production | `labd` config, containerd config | `labd/internal/orch` spec golden test; `deploy/scripts/provision.sh --role prod` registers only runsc |
| S2 | No network: the container joins a fresh, empty network namespace with no interfaces up | `labd/sandbox/sandbox-base.json` | spec golden test; P0 `nc`/socket probe returns no route |
| S3 | Root filesystem is read-only | `sandbox-base.json` `root.readonly: true` | spec golden test; P0 `touch /x` fails |
| S4 | Only writable paths are tmpfs `/tmp` and `/home/lab`, 16 MB, `noexec,nosuid,nodev` | `sandbox-base.json` mounts | spec golden test; P0 write a 20 MB file fails, `chmod +x` then exec fails |
| S5 | Process runs as uid/gid 1000 with `noNewPrivileges: true` | `sandbox-base.json` process | spec golden test; P0 `id` prints 1000 |
| S6 | All capabilities dropped: bounding, effective, permitted, inheritable, ambient are empty | `sandbox-base.json` | spec golden test |
| S7 | Resource limits from manifest applied: memory (no swap), CPU quota, lab processes ≤ 32 (gVisor: `RLIMIT_NPROC` with cgroup pids = lab limit + headroom; runc dev runs: cgroup pids only; ADR 0009), `RLIMIT_FSIZE` 32 MB, `RLIMIT_NOFILE` 256 | `orch` spec builder | unit test per limit; P3 abuser profile |
| S8 | No host bind mounts, no host `/proc`, no devices beyond the PTY | `sandbox-base.json` | spec golden test asserts mount list exactly |
| S9 | Image has no `apk`, `wget`, `nc`, `ftpget`, `telnet`, `httpd`, `udhcpc`, `ifconfig`, `route`, and no compiler | `images/labbase/Dockerfile` | `images/labbase/test.sh` |
| S10 | Only `labd` has access to the containerd socket; `web` never does | unix users/groups on the box | `provision.sh` sets socket group; Phase 8 checklist |
| S11 | Internal `labd` HTTP API listens on loopback only and requires the shared bearer secret | `labd/internal/api` | unit test: missing/wrong bearer → 401; config test: non-loopback listen refused |
| S12 | WebSocket token is HMAC-SHA256 over `session_id\|user_id\|exp`, 60 s expiry, single use; Origin must match site host | `labd/internal/term` | unit tests: expired, reused, wrong session, wrong origin all rejected |
| S13 | Terminal input rate limited to 2 KB/s sustained, 16 KB burst; output 256 KB/s with 10 s stall disconnect | `labd/internal/term` | unit tests; P3 abuser paste |
| S14 | One active session per user; concurrency cap enforced by semaphore; queue bounded | `labd/internal/orch` | unit tests; P6 |
| S15 | Flags are never stored per challenge; derived as `LAB{base32(HMAC-SHA256(DEPLOY_SECRET, slug))[:24]}`; compared in constant time | `web/progress`, build tooling | shared test vectors `challenges/schema/flag_vectors.json` pass in both Go and Python |
| S16 | Flag never appears in cleartext in a challenge image (`strings` check) and `report()` prints garbage for wrong state | challenge build script | per-challenge leak check and solve/no-solve oracle |
| S17 | Hard TTL and idle timeout always terminate a session; extension is once per session | `labd/internal/orch` | unit tests with fake clock |
| S18 | On `labd` restart, every container in namespace `labs` without an open session row is killed and deleted | `orch.Reconciler` | unit tests with fake runtime; P5 |
| S19 | Every input line typed into a lab is recorded (`command_entered`); users are told in the UI and Terms | `term` capture, `web` templates | unit test on splitter; template test asserts notice present |
| S20 | Nothing is built or pulled from the internet on the VPS at request time; images are pre-pulled and pinned by digest | `labd pull`, manifests | manifest schema requires `@sha256:`; P7 confirms pre-pull |

## Things that look like security controls but are not

- An extra OCI seccomp profile on top of gVisor. Not added in MVP; gVisor's own filter is the effective one. Revisit only with measurements.
- Host `kernel.yama.ptrace_scope`. Irrelevant inside gVisor; leave host default.
- Disabling gdb's `shell`/`python` commands. Not possible without a custom gdb build; mitigation is the image and the sandbox. `--without-python` build is a stretch goal, tracked in Phase 1.
