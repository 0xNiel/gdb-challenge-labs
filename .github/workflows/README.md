# workflows

| File | Trigger | Does | Phase |
| --- | --- | --- | --- |
| `ci.yml` | push, PR | `go vet` + `go test`, `ruff` + `pytest`, `shellcheck` | 0 |
| `challenges.yml` | push to `main` touching `challenges/**` or `images/**`; `workflow_dispatch` (rebuild all) | Build only the changed challenge dirs, push to GHCR, write digests back, regenerate `challenges.json`, open a PR | 5 |
| `labbase.yml` | `images/labbase/**` change; weekly cron | Rebuild and push `labbase`, then trigger `challenges.yml` for all | 5 |

Secrets needed: `DEPLOY_SECRET` (same value as production `web`), `GHCR_TOKEN` (write), `GHCR_NAMESPACE`.
