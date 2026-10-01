# 0014 — The lab's starting directory comes from the image's WORKDIR

Date: 2026-10-01 · Status: accepted · Phase: 5 · Refines: `labd/sandbox/sandbox-base.json` `process.cwd`

## Context

The owner played tier1-01 on the laptop and the shell started in `/home/lab`, an empty tmpfs, instead of `/opt/lab`, where the binary, `README.md` and `src/` are. Every lab README and lesson assumes the learner starts there and types `./scores`.

The cause is the base spec's `"cwd": "/home/lab"`. An OCI spec's `process.cwd` is final; the image's `WORKDIR /opt/lab` (written by `scripts/challenge-build.sh`) was never read. A fixed `/opt/lab` in the base spec would break the perf image and labbase, which have no `/opt/lab`: a cwd that does not exist fails container creation.

`cwd` is not a security setting. It decides only where the shell starts. It grants no access: the root is read-only (S3), `/opt/lab` is owned by root, and the writable paths are still only the two tmpfs mounts (S4). No invariant S1–S20 mentions it, and `CheckInvariants` does not check it.

## Decision

- The lab process's `cwd` is the image's `WorkingDir` (Docker's `WORKDIR`) when the image sets one, and the base spec's `/home/lab` otherwise.
- `SpecParams.Cwd` carries it into `BuildSpec`, which accepts only an absolute, clean path and keeps the base spec's value when it is empty. The spec is still built and checked in one place.
- The runtime reads it: `Runtime.ImageWorkingDir` in labd's session manager, and the same helper in `RunOnce` (`specrun`, the challenge build's oracle, P0). Both paths therefore start a lab in the same directory.
- `HOME` stays `/home/lab`, so shell history and anything else that writes under `$HOME` still lands on the writable tmpfs.
- Nothing else in the spec changes. The golden file's only change is `process.cwd`, because its parameters now name `/opt/lab` as a lab image would.

## Consequences

- Lab images: the shell starts in `/opt/lab` and `./<entry>` works without `cd`. No image is rebuilt: lab images already carry `WORKDIR /opt/lab`.
- labbase and perf set `WORKDIR /home/lab`, so they start where they always did.
- A learner who wants to write a file must `cd ~` or use `/tmp`; `/opt/lab` is read-only. Lab programs do not write to their working directory, and gdb does not save history unless told to.
- One extra read of the image's config per session start, from the local content store. It is small next to the 1.8 s start p95.
- A future image that sets `WORKDIR` to a path the sandbox hides or does not have would fail to start. Every image is built by our scripts, so that is a build bug, caught by the build's oracle run.
