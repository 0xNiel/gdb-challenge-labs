"""Task 6.12: sign up, start lab 1, solve it in the browser's terminal, submit the flag, see the
lab stop itself (ADR 0016) and lab 2 unlock. Then time start and click-to-prompt over a few
more runs, each stopped with the Stop button (plan 6, "Metrics to record"), written to
.scratch/e2e/web-metrics.json.
"""

import json
import os
import platform
import re
import statistics
import time
from pathlib import Path

from tests.e2e.conftest import BASE_URL

REPO = Path(__file__).resolve().parents[3]
SLUG = "tier1-01-off-by-one"
SOLVE = REPO / "challenges" / "tier1-c-fundamentals" / "01-off-by-one" / "solve.gdb"
FLAG = re.compile(r"report: (LAB\{[A-Z2-7]{24}\})")
PROMPT = "lab$ "
PW = "e2e-correct-horse-42"


def screen(page) -> str:
    """The terminal's visible text (xterm's DOM renderer)."""
    return page.locator("#term .xterm-rows").inner_text()


def wait_screen(page, pred, timeout_s=60.0, what="text"):
    end = time.monotonic() + timeout_s
    while time.monotonic() < end:
        text = screen(page)
        if pred(text):
            return text
        time.sleep(0.1)
    raise AssertionError(f"terminal never showed {what}; it shows:\n{screen(page)}")


def type_line(page, line: str):
    page.locator("#term").click()
    page.keyboard.type(line, delay=15)  # paced well under the 2 KiB/s input limit (S13)
    page.keyboard.press("Enter")


def wait_gdb(page, count: int, timeout_s=60.0):
    """Wait until the screen holds `count` gdb prompts, i.e. the last command finished."""
    return wait_screen(page, lambda t: t.count("(gdb)") >= count, timeout_s, f"{count} gdb prompts")


def signup(page) -> str:
    email = f"e2e-{int(time.time() * 1000)}@example.com"
    page.goto("/signup")
    page.fill("input[name=email]", email)
    page.fill("input[name=password1]", PW)
    page.fill("input[name=password2]", PW)
    page.click("main button[type=submit]")
    page.wait_for_url("**/learn")
    return email


def start_lab(page) -> tuple[str, float, float]:
    """Click Start; returns (session id, seconds to the terminal page, seconds to the prompt)."""
    page.goto(f"/lab/{SLUG}")
    t0 = time.monotonic()
    page.click("text=Start the lab")
    page.wait_for_url(f"**/lab/{SLUG}/session")
    t_page = time.monotonic() - t0
    wait_screen(page, lambda t: PROMPT in t, 30, "the shell prompt")
    t_prompt = time.monotonic() - t0
    return page.locator("#lab").get_attribute("data-session"), t_page, t_prompt


def shot(page, name: str):
    """Screenshots for whoever reviews the run (.scratch/e2e/, never committed)."""
    out = REPO / ".scratch" / "e2e"
    out.mkdir(parents=True, exist_ok=True)
    page.screenshot(path=str(out / f"{name}.png"))


def session_row(db, sid: str):
    return db.execute("SELECT state, end_reason FROM sessions WHERE id = %s", (sid,)).fetchone()


def wait_ended(db, sid: str):
    end = time.monotonic() + 30
    while time.monotonic() < end:
        row = session_row(db, sid)
        if row and row[0] == "ended":
            return row
        time.sleep(0.2)
    raise AssertionError(f"session {sid} did not end: {session_row(db, sid)}")


def stop_lab(page, db, sid: str):
    page.click("text=Stop lab")
    page.wait_for_url(f"**/lab/{SLUG}")
    return wait_ended(db, sid)


def test_tier1_lab1_end_to_end(page, db):
    signup(page)
    states = page.locator("li.rung").evaluate_all("els => els.map(e => e.className)")
    assert "unlocked" in states[0] and "locked" in states[1]

    sid, t_page, t_prompt = start_lab(page)
    assert page.locator("#capture-notice").is_visible()  # S19
    text = screen(page)
    assert "/opt/lab" in text or PROMPT in text

    # The bug shows on a plain run, from /opt/lab without cd (ADR 0014).
    type_line(page, "./scores")
    wait_screen(page, lambda t: "437" in t, 30, "the buggy total 437")

    # The oracle's commands, typed as a learner would.
    type_line(page, "gdb -q ./scores")
    prompts = 1
    wait_gdb(page, prompts)
    for line in SOLVE.read_text().splitlines():
        if not line.strip() or line.startswith("#"):
            continue
        type_line(page, line)
        if line.strip() == "continue":
            break
        prompts += 1
        wait_gdb(page, prompts)
    text = wait_screen(page, lambda t: FLAG.search(t) is not None, 60, "report: LAB{...}")
    flag = FLAG.search(text).group(1)
    shot(page, "1-solved-in-terminal")

    page.click("[data-tab=flag]")
    page.fill("#flag-input", flag)
    page.click("#flag-panel button.primary")
    page.wait_for_selector("#flag-panel >> text=Correct!")
    assert page.locator("#lab").get_attribute("data-session") == sid  # no reload
    # ADR 0016: the solve stops the lab, and the page says why.
    assert wait_ended(db, sid) == ("ended", "solved")
    page.wait_for_selector("#lab-banner >> text=the challenge was solved")
    assert page.locator("#lab-state").inner_text() == "ended"
    assert not page.locator("#lab-stop").is_visible()
    shot(page, "2-flag-accepted")

    page.goto("/learn")
    states = page.locator("li.rung").evaluate_all("els => els.map(e => e.className)")
    assert "solved" in states[0] and "unlocked" in states[1]

    # Start latency: a few more start → prompt → stop cycles.
    runs = [{"page_s": t_page, "prompt_s": t_prompt}]
    for _ in range(int(os.environ.get("E2E_LATENCY_RUNS", "9"))):
        sid, t_page, t_prompt = start_lab(page)
        runs.append({"page_s": t_page, "prompt_s": t_prompt})
        assert stop_lab(page, db, sid) == ("ended", "user_stop")
    write_metrics(runs)


def p95(xs):
    xs = sorted(xs)
    return xs[min(len(xs) - 1, round(0.95 * (len(xs) - 1)))]


def lab_runtime() -> str:
    """runsc or runc, from the config scripts/web-stack.sh gave labd."""
    cfg = REPO / ".scratch" / "web-stack" / "labd.yaml"
    for line in cfg.read_text().splitlines() if cfg.exists() else []:
        if line.startswith("runtime:"):
            return "runsc" if "runsc" in line else "runc"
    return "unknown"


def write_metrics(runs):
    out = REPO / ".scratch" / "e2e" / "web-metrics.json"
    out.parent.mkdir(parents=True, exist_ok=True)
    pages = [r["page_s"] * 1000 for r in runs]
    prompts = [r["prompt_s"] * 1000 for r in runs]
    out.write_text(
        json.dumps(
            {
                "base_url": BASE_URL,
                "arch": platform.machine(),
                "runtime": lab_runtime(),
                "challenge": SLUG,
                "runs": len(runs),
                "start_to_terminal_page_ms": {
                    "p50": round(statistics.median(pages)),
                    "p95": round(p95(pages)),
                    "max": round(max(pages)),
                },
                "click_to_prompt_ms": {
                    "p50": round(statistics.median(prompts)),
                    "p95": round(p95(prompts)),
                    "max": round(max(prompts)),
                },
                "samples": runs,
            },
            indent=2,
        )
    )
