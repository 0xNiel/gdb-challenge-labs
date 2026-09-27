# workflows

Intentionally empty. Do not add GitHub Actions workflows here until the owner asks: Actions minutes are limited (ADR 0008).

Checks run locally. Before pushing:

```
./run.sh check && ./run.sh test --all && ./run.sh lint
```

Challenge images are built with `scripts/challenge-build.sh` (Phase 5). The base image is rebuilt with `./run.sh images labbase`.
