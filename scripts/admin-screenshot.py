"""Screenshot /admin/live as a staff user (scripts/live-run.sh, Phase 7 task 7.8).

usage: uv run --project web python scripts/admin-screenshot.py BASE_URL EMAIL PASSWORD OUT.png
"""

import sys

from playwright.sync_api import sync_playwright

base, email, password, out = sys.argv[1:5]
with sync_playwright() as p:
    b = p.chromium.launch()
    page = b.new_page(base_url=base, viewport={"width": 1400, "height": 1000})
    page.goto("/login")
    page.fill("input[name=login]", email)
    page.fill("input[name=password]", password)
    page.click("main button[type=submit]")
    page.wait_for_url("**/learn")
    page.goto("/admin/live")
    page.wait_for_selector("#sessions")
    page.screenshot(path=out, full_page=True)
    b.close()
print(f"wrote {out}")
