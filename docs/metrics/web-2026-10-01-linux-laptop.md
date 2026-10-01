# Web start latency — linux-laptop, 2026-10-01

From `./run.sh test --e2e` (`web/tests/e2e/test_tier1.py`): headless Chromium on the lab host, Django and labd from `scripts/web-stack.sh`, challenge `tier1-01-off-by-one`, labs under runsc on x86_64, 10 runs. Raw data in `web-2026-10-01-linux-laptop.json`.

| Measure | p50 | p95 | max | Spec |
| --- | --- | --- | --- | --- |
| Click Start to the terminal page (Django start request, labd create call, redirect) | 293 ms | 418 ms | 418 ms | — |
| Click Start to the shell prompt in the browser | 476 ms | 569 ms | 569 ms | p95 < 2000 ms |
