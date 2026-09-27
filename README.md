# gdb Challenge Labs

A web platform where developers learn gdb by debugging real bugs in sandboxed (gVisor) containers from the browser and submitting CTF-style flags.

- Working with an AI assistant or starting a session: read [CLAUDE.md](CLAUDE.md).
- Where the project is right now: [docs/STATUS.md](docs/STATUS.md).
- The plan: [docs/plan/IMPLEMENTATION_PLAN.md](docs/plan/IMPLEMENTATION_PLAN.md).
- The spec: [docs/spec/mvp-spec.md](docs/spec/mvp-spec.md).

## Quick start

```
./run.sh doctor         # what this machine has, lacks, and how to install it
./run.sh vm up          # Linux: provision this host. macOS: Lima VM with containerd + gVisor
./run.sh vm verify      # a container runs under runsc
./run.sh test --all     # Go + Django tests
./run.sh help           # everything else
```

New here? [docs/ONBOARDING.md](docs/ONBOARDING.md).

`make help` lists the same commands as Make targets.
