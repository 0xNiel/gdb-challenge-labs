"""Task 7.7 in the real stack: a staff user sees a learner's lab on /admin/live, kills it, and the
container is gone; the learner's page says an administrator stopped it. Also drain and resume.
"""

import time

from tests.e2e.conftest import BASE_URL
from tests.e2e.test_tier1 import PROMPT, SLUG, signup, start_lab, wait_ended


def test_admin_kills_a_lab_and_drains(browser, db):
    learner = browser.new_context(base_url=BASE_URL).new_page()
    learner.set_default_timeout(30_000)
    signup(learner)
    sid, _, _ = start_lab(learner)
    assert PROMPT in learner.locator("#term .xterm-rows").inner_text()

    admin = browser.new_context(base_url=BASE_URL).new_page()
    admin.set_default_timeout(30_000)
    email = signup(admin)
    db.execute("UPDATE auth_user SET is_staff = true WHERE email = %s", (email,))
    admin.goto("/admin/live")
    row = admin.locator("#sessions tr", has_text=sid[:8])
    assert row.count() == 1
    assert "running" in row.inner_text()

    admin.once("dialog", lambda d: d.accept())
    row.locator("button", has_text="Kill").click()
    admin.wait_for_selector("text=killed")
    assert wait_ended(db, sid) == ("ended", "admin_kill")
    learner.wait_for_selector("#lab-banner >> text=an administrator stopped it")
    deadline = time.monotonic() + 15
    while admin.locator("#sessions tr", has_text=sid[:8]).count() and time.monotonic() < deadline:
        admin.reload()
    assert admin.locator("#sessions tr", has_text=sid[:8]).count() == 0

    # Drain: no new starts; resume: starts again.
    admin.once("dialog", lambda d: d.accept())
    admin.click("button:has-text('Drain')")
    admin.wait_for_selector("text=Draining:")
    learner.goto(f"/lab/{SLUG}")
    learner.click("text=Start the lab")
    learner.wait_for_url(f"**/lab/{SLUG}/session")
    learner.wait_for_selector("#lab-banner >> text=Waiting for a free lab slot")
    admin.click("button:has-text('Resume')")
    admin.wait_for_selector("text=Resumed")
    start = time.monotonic()
    while PROMPT not in learner.locator("#term .xterm-rows").inner_text():
        assert time.monotonic() - start < 30, "queued lab did not start after resume"
        time.sleep(0.2)
    queued_sid = learner.locator("#lab").get_attribute("data-session")
    learner.click("text=Stop lab")
    assert wait_ended(db, queued_sid) == ("ended", "user_stop")
