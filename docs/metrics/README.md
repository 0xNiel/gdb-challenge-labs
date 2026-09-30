# Metrics

Every measured number in the project lands here. Files carry the date and the host, e.g. `p1-2026-10-12-dev-vm.json`. Nothing here is edited by hand except this index and the parts of `capacity.md` outside the block `labd-perf report` generates.

## Hosts

| Label | What it is | Authoritative? |
| --- | --- | --- |
| `dev-vm` | Lima VM on the Apple Silicon Mac, arm64, 4 vCPU / 8 GB | No. Correctness and trends only |
| `linux-laptop` | Owner's Linux x86-64 laptop (specs in `environment-linux-laptop.md`, QUESTIONS Q10) | Yes, for Phases 1 and 4; 100-lab runs only if RAM allows |
| `hostinger` | The production VPS, 8 vCPU / 32 GB | Yes, final |

## Files (index; keep updated)

| File | Phase | What |
| --- | --- | --- |
| `capacity.md` | 1, 3, 4, 7, 8 | The capacity table from the spec, estimates replaced by measurements as they arrive |
| `environment-dev-vm.md`, `environment-linux-laptop.md`, `environment-hostinger.md` | 0, 4, 8 | Kernel, containerd, runsc, cgroup mode, CPU, RAM, KVM presence |
| `p0-<date>-<host>.{json,md}` | 1 | gdb feature matrix under runsc and runc |
| `single-lab-<date>-<host>.{json,md}` | 1 | One idle lab: RSS, Sentry RSS, start latency, image size |
| `create-latency-<date>-<host>.md` | 2 | Container create-to-running from P4-lite |
| `p1-<date>-<host>.{json,md}` | 3 | Single-session 10-minute stepper profile |
| `run-P<n>-<date>-<host>[-runc\|-kvm].json` | 4 | Raw scenario runs from `labd-perf run`: meta, summary, criteria evaluated, timeline, every simulated user, host samples every 5 s. `-runc` is the gVisor reference, `-kvm` the runsc KVM platform |
| `perf-report-<date>-<host>.json` | 4, 8 | The spec-schema report from `labd-perf report` (`meta.sources` names the run behind each value); it also rewrites the generated block of `capacity.md` |
| `challenges-<date>.md` | 5 | Per-challenge layer size, build and oracle time |
| `web-<date>-<host>.md` | 6 | Click-to-prompt and Django request latency |
| `admin-live-<date>.png` | 7 | Screenshot of the live view during the 20-lab run |
| `beta-week1.md` | 8 | First week of real users |

## Conventions

- JSON is the source; Markdown is a rendering for humans. Both are committed.
- Percentiles are `p50`, `p95`, `p99`, `max` unless the spec's schema says otherwise.
- Memory in MB (10^6 bytes) unless the field name says `mib`. Latency in ms. Bandwidth in bytes per second.
- Every Markdown table has a "Source" column or a line under it naming the JSON file and the command that produced it.
