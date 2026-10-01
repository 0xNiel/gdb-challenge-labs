// The lab page: xterm.js bridged to labd's terminal gateway, plus the side tabs.
// Protocol: labd/internal/term/README.md. Reference client: labd/testpage/index.html.
"use strict";
(() => {
  const root = document.getElementById("lab");
  if (!root) return;
  const $ = (id) => document.getElementById(id);
  const cfg = {
    session: root.dataset.session,
    tokenUrl: root.dataset.tokenUrl,
    wsBase: root.dataset.wsBase,
    challengeUrl: root.dataset.challengeUrl,
  };
  const GRACE_MS = 60000; // labd keeps a lab this long after the socket drops

  // ---- tabs
  const showTab = (name) => {
    document.querySelectorAll("[data-tab]").forEach((b) => b.setAttribute("aria-selected", b.dataset.tab === name));
    document.querySelectorAll("[data-pane]").forEach((p) => { p.hidden = p.dataset.pane !== name; });
  };
  document.querySelectorAll("[data-tab]").forEach((b) => b.addEventListener("click", () => showTab(b.dataset.tab)));

  // ---- "Commands are recorded" (S19): shown once per session
  const noticeKey = "lab-notice-" + cfg.session;
  const store = (() => { try { return window.localStorage; } catch { return null; } })();
  if (store && store.getItem(noticeKey)) $("capture-notice").hidden = true;
  $("capture-ok").addEventListener("click", () => {
    $("capture-notice").hidden = true;
    if (store) store.setItem(noticeKey, "1");
    term.focus();
  });

  // ---- hint and flag forms: submit in place, so the terminal stays connected
  document.addEventListener("submit", async (ev) => {
    const form = ev.target;
    if (!form.dataset.partial) return;
    ev.preventDefault();
    const target = document.querySelector(form.dataset.partial);
    form.querySelectorAll("button").forEach((b) => { b.disabled = true; });
    try {
      const resp = await fetch(form.action, {
        method: "POST", body: new FormData(form), credentials: "same-origin", headers: { "X-Partial": "1" },
      });
      const html = await resp.text();
      if ((resp.headers.get("Content-Type") || "").startsWith("text/html") && target) target.outerHTML = html;
    } catch {
      form.querySelectorAll("button").forEach((b) => { b.disabled = false; });
      flash("Could not reach the server; try again.");
    }
  });

  // ---- terminal
  const term = new Terminal({ cursorBlink: true, scrollback: 5000, fontSize: 14, convertEol: false });
  const fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open($("term"));
  fit.fit();
  // Copy and paste work as usual; drag-and-drop does not (spec "Client").
  ["dragover", "drop"].forEach((t) => $("term").addEventListener(t, (e) => { e.preventDefault(); e.stopPropagation(); }, true));

  let ws = null, ended = false, lostAt = 0;
  const enc = new TextEncoder();
  const send = (obj) => ws && ws.readyState === WebSocket.OPEN && ws.send(JSON.stringify(obj));
  const resize = () => { fit.fit(); send({ type: "resize", cols: term.cols, rows: term.rows }); };
  const setState = (s) => { $("lab-state").textContent = s; };
  const flash = (msg) => { $("lab-msg").textContent = msg; setTimeout(() => { $("lab-msg").textContent = ""; }, 4000); };
  const banner = (html) => { $("lab-banner").innerHTML = html; $("lab-banner").hidden = !html; };

  const REASONS = {
    idle_timeout: "it was idle too long", hard_ttl: "it reached its time limit", ws_closed: "the connection was lost",
    user_stop: "you stopped it", admin_kill: "an administrator stopped it", solved: "the challenge was solved",
    task_exited: "the shell exited", queue_timeout: "it waited too long for a slot", create_failed: "it could not be created",
  };
  const end = (reason) => {
    if (ended) return;
    ended = true;
    setState("ended");
    $("lab-ttl").textContent = "";
    $("lab-extend").hidden = true;
    term.options.disableStdin = true;
    const why = REASONS[reason] || reason || "it ended";
    banner(`The lab has ended: ${why}. <a href="${cfg.challengeUrl}">Back to the challenge</a> to start it again.`);
    if (ws) ws.close();
  };

  // Countdown between ttl frames (labd sends one every 30 s).
  let idleLeft = 0, hardLeft = 0, canExtend = false;
  const mmss = (s) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
  setInterval(() => {
    if (ended) return;
    idleLeft = Math.max(idleLeft - 1, 0); hardLeft = Math.max(hardLeft - 1, 0);
    $("lab-ttl").textContent = hardLeft ? `idle ${mmss(idleLeft)} · session ${mmss(hardLeft)}` : "";
    $("lab-extend").hidden = !(canExtend && idleLeft > 0 && idleLeft <= 120);
  }, 1000);
  setInterval(() => send({ type: "ping" }), 25000);

  const retry = () => {
    if (ended) return;
    if (!lostAt) lostAt = Date.now();
    if (Date.now() - lostAt > GRACE_MS + 15000) { end("ws_closed"); return; }
    setState("reconnecting…");
    setTimeout(connect, 2000);
  };

  async function connect() {
    if (ended) return;
    let token;
    try {
      const resp = await fetch(`${cfg.tokenUrl}?session=${encodeURIComponent(cfg.session)}`, {
        credentials: "same-origin", cache: "no-store", headers: { Accept: "application/json" },
      });
      const isJSON = (resp.headers.get("Content-Type") || "").startsWith("application/json");
      if (resp.status === 410) { const j = await resp.json(); end(j.reason || j.state); return; }
      if (!resp.ok || !isJSON) {
        // Logged out (redirect to the login page) or not ours: nothing to retry.
        banner(`The terminal could not be opened (${resp.status}). <a href="${cfg.challengeUrl}">Back to the challenge</a>.`);
        setState("closed");
        return;
      }
      token = (await resp.json()).token;
    } catch {
      retry();
      return;
    }
    const base = cfg.wsBase || `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}`;
    ws = new WebSocket(`${base}/ws/term/${cfg.session}?t=${encodeURIComponent(token)}`);
    ws.binaryType = "arraybuffer";
    ws.onopen = () => { lostAt = 0; setState("connected"); resize(); };
    ws.onmessage = (ev) => {
      if (typeof ev.data !== "string") { term.write(new Uint8Array(ev.data)); return; }
      const f = JSON.parse(ev.data);
      switch (f.type) {
        case "queued":
          setState("queued");
          banner(`Waiting for a free lab slot: you are number ${f.position} in the queue.`);
          break;
        case "state":
          if (f.state === "running") { banner(""); setState("running"); resize(); term.focus(); }
          else if (f.state === "ended") end(f.reason);
          break;
        case "ttl":
          idleLeft = f.idle_remaining_s; hardLeft = f.hard_remaining_s; canExtend = f.extend_available;
          break;
        case "warn": flash(f.message || "Typing too fast; some keys were dropped."); break;
        case "extend":
          flash(f.ok ? "Extended." : "This lab was already extended once.");
          canExtend = false;
          break;
      }
    };
    ws.onclose = (ev) => {
      if (ended) return;
      if (ev.code === 1000 && ev.reason === "ended") { end(""); return; }
      if (ev.code === 1000 && ev.reason === "replaced") {
        setState("opened elsewhere");
        banner(`This lab was opened in another tab or window. <a href="">Reload</a> to use it here.`);
        return;
      }
      retry();
    };
  }

  term.onData((d) => ws && ws.readyState === WebSocket.OPEN && ws.send(enc.encode(d)));
  window.addEventListener("resize", resize);
  $("lab-extend").addEventListener("click", () => send({ type: "extend" }));
  connect();
})();
