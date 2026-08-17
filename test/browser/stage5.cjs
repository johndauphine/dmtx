"use strict";
// Opt-in acceptance driver. Ordinary commands travel through the shipped
// parse/jobs/ExecuteWithProgress seam; only the second run is held open by the
// Go test fixture so cancellation and reconnect are deterministic.
const { chromium } = require(process.argv[2]);
const edge = process.argv[3];
const options = JSON.parse(process.argv[4]);
const assert = (ok, message) => { if (!ok) throw new Error(message); };

(async () => {
  const browser = await chromium.launch({ executablePath: edge, headless: true });
  const errors = [];
  try {
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const page = await context.newPage();
    page.setDefaultTimeout(25000);
    page.on("console", message => { if (message.type() === "error") errors.push(message.text()); });
    page.on("pageerror", error => errors.push(String(error)));
    await page.goto(options.url, { waitUntil: "domcontentloaded" });
    const line = page.locator("#line");
    await line.waitFor();
    assert(await line.getAttribute("role") === "combobox", "console is not an ARIA combobox");

    const registry = await page.evaluate(() => fetch("/api/v1/commands").then(response => response.json()));
    for (const command of registry.filter(command => command.webui === "supported")) {
      await line.fill("/" + command.name);
      await page.waitForFunction(name => [...document.querySelectorAll("#suggestions [role=option]")].some(option => option.textContent.includes("/" + name)), command.name);
    }
    await line.fill("/health"); await line.press("Tab");
    assert((await line.inputValue()).startsWith("/health-check"), "registered alias did not complete");
    await line.fill("/cache");
    const omitted = page.locator("#suggestions [role=option]").first();
    await omitted.waitFor();
    assert(await omitted.getAttribute("aria-disabled") === "true", "omitted palette entry was enabled");
    assert((await line.inputValue()) === "/cache", "omitted palette entry mutated input");
    await line.fill("@m"); await page.waitForTimeout(50);

    const transcript = () => page.locator("#transcript").textContent();
    async function submit(value, pattern) {
      await line.fill(value); await line.press("Enter");
      if (pattern) await page.waitForFunction(text => document.querySelector("#transcript").textContent.includes(text), pattern);
    }
    async function submitStatus(value, pattern) {
      await line.fill(value); await line.press("Enter");
      await page.waitForFunction(text => document.querySelector("#console-status").textContent.includes(text), pattern);
    }
    async function finish() {
      await page.waitForFunction(() => document.querySelector("#console-status").textContent === "Command finished.");
    }
    async function setupAnswer(value, final = false) {
      await line.fill(value); await line.press("Enter");
      await page.waitForFunction(done => document.querySelector("#console-status").textContent === (done ? "Setup complete." : "Setup is waiting for input."), final);
    }

    // Browser-local commands prove their documented client seam.
    await submitStatus("/help", "Command discovery is open");
    await submit("/about", "Deterministic database migration tool");
    await submitStatus("/session", "Session defaults shown");
    await submitStatus(`/session config "${options.config}"`, "Session default set");
    await submitStatus("/session clear config", "Session default cleared");
    const downloadPromise = page.waitForEvent("download");
    await submitStatus("/logs", "Session log downloaded");
    const download = await downloadPromise;
    assert(download.suggestedFilename() === "session.log", "logs used an unexpected download name");
    await submit("/clear");
    assert((await transcript()).length < 1000, "clear did not clear transcript");

    // First run is a real SQLite migration, followed by real app commands.
    await submit(`/run --config "${options.config}" --state "${options.state}" --acknowledge-destructive`, "Started run.");
    await finish();
    for (const command of [
      `/status --state "${options.state}"`, `/history --state "${options.state}"`,
      `/validate --config "${options.config}"`, `/diagnose --state "${options.state}"`,
      `/preflight --config "${options.config}"`, `/analyze --config "${options.config}"`,
      `/config --config "${options.config}"`, `/ai config-review --config "${options.config}"`,
      "/profile list",
    ]) { await submit(command, "Started "); await finish(); }
    // These are real protected-storage commands in the temporary HOME set by
    // the Go harness, including encrypted profile save/list/delete.
    for (const command of [
      `/init --config "${options.setup}.init" --force`, "/init-secrets",
      `/profile save browser "${options.config}"`, "/profile list", "/profile delete browser",
      `/resume --config "${options.config}" --state "${options.state}"`,
    ]) { await submit(command, "Started "); await finish(); }

    // Guided setup is the documented browser-local flow. A password response
    // must never be copied into the transcript.
    await submitStatus(`/setup "${options.setup}"`, "Setup is waiting for input");
    for (const answer of [options.source, options.setupTarget, "drop_recreate", options.setup]) await setupAnswer(answer);
    await setupAnswer("yes", true);

    // A separate flow proves cancellation writes nothing.
    await submitStatus(`/setup "${options.cancelSetup}"`, "Setup is waiting for input");
    for (const answer of [options.source, options.setupTarget, "drop_recreate", options.cancelSetup]) await setupAnswer(answer);
    await setupAnswer("no", true);

    // Reusing the completed path must be refused at the application write.
    await submitStatus(`/setup "${options.setup}"`, "Setup is waiting for input");
    for (const answer of [options.source, options.setupTarget, "drop_recreate", options.setup, "yes"]) await setupAnswer(answer);
    assert(/already exists/i.test(await transcript()), "setup overwrite was not refused");
    await setupAnswer("no", true);

    // PostgreSQL reaches a masked password prompt and reports a generic,
    // bounded verification failure without retaining the password.
    await line.fill(`/setup postgres "${options.setup}.pg"`); await line.press("Enter");
    await page.waitForFunction(() => document.querySelector("#console-status").textContent === "Setup is waiting for input.");
    for (const answer of ["127.0.0.1", "1", "browser", "browser"]) await setupAnswer(answer);
    await page.waitForFunction(() => document.querySelector("#line").type === "password");
    const sentinel = "password=browser-secret-sentinel";
    await setupAnswer(sentinel);
    assert(!(await transcript()).includes(sentinel), "setup secret reached transcript");
    await page.reload({ waitUntil: "domcontentloaded" });
    await line.waitFor();

    // Browser-local recall is bounded and excludes all free-form sensitive input.
    const unsafe = [`/ai config-review --request "token=${sentinel}"`, `/resume --abandon --abandon-reason "${sentinel}"`, "/init-secrets"];
    await page.evaluate(entries => entries.forEach(entry => remember(entry)), unsafe);
    const stored = await page.evaluate(() => localStorage.getItem("dmtx-console-history-v2") || "");
    assert(!stored.includes("browser-secret-sentinel") && !stored.includes("init-secrets"), "unsafe command entered local recall");
    await line.fill(""); await line.press("ArrowUp");
    assert(!(await line.inputValue()).includes("browser-secret-sentinel"), "unsafe recall was restored");

    // The fixture's only synthetic outcome: the second run is blocked. Read
    // its exact server-owned started projection, reload, then cancel that id.
    await line.fill(`/run --config "${options.config}" --state "${options.state}" --acknowledge-destructive`); await line.press("Enter");
    let running = null;
    for (let attempt = 0; attempt < 100 && !running; attempt++) {
      const matching = await page.evaluate(async () => (await fetch("/api/v1/jobs").then(response => response.json())).filter(job => job.state === "running" && job.command === "run"));
      if (matching.length === 1) running = matching[0];
      else await page.waitForTimeout(100);
    }
    assert(running && typeof running.id === "string" && running.id, "running job projection missing id: " + JSON.stringify(running));
    const started = await page.evaluate(async id => {
      const response = await fetch("/api/v1/jobs/" + encodeURIComponent(id) + "/events");
      if (!response.ok || !response.body) throw new Error("started SSE failed: " + response.status);
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      while (!buffer.includes("event: started\n") || !buffer.includes("\n\n")) {
        const chunk = await reader.read();
        if (chunk.done) break;
        buffer += decoder.decode(chunk.value, { stream: true });
      }
      await reader.cancel();
      const match = buffer.match(/event: started\ndata: ([^\n]+)/);
      if (!match) throw new Error("started SSE frame missing");
      return JSON.parse(match[1]);
    }, running.id);
    assert(started.request && started.request.config_origin === "file", "started frame lacks safe config origin");
    assert(!JSON.stringify(started).includes(options.config), "started frame leaked raw config path");
    await page.reload({ waitUntil: "domcontentloaded" });
    await page.waitForFunction(() => document.querySelector("#transcript").textContent.includes("Reattached to run"));
    await page.locator("#cancel").click(); await page.waitForFunction(() => document.querySelector("#transcript").textContent.includes("Cancelled."));

    const worker = await page.evaluate(async () => {
      if (!("serviceWorker" in navigator)) return null;
      await new Promise(resolve => setTimeout(resolve, 3000));
      const registration = await navigator.serviceWorker.getRegistration();
      const assets = {};
      for (const path of ["/", "/static/console.css", "/static/console.js", "/static/icon.svg", "/manifest.webmanifest", "/sw.js"]) {
        try { assets[path] = (await fetch(path)).status; }
        catch (error) { assets[path] = String(error); }
      }
      return {
        active: registration && registration.active && registration.active.state,
        waiting: registration && registration.waiting && registration.waiting.state,
        installing: registration && registration.installing && registration.installing.state,
        controlled: Boolean(navigator.serviceWorker.controller),
        assets,
      };
    });
    assert(worker && worker.active === "activated", "service worker did not activate: " + JSON.stringify(worker));
    if (!worker.controlled) await page.reload({ waitUntil: "domcontentloaded" });
    await page.waitForFunction(() => Boolean(navigator.serviceWorker && navigator.serviceWorker.controller));
    const cacheControl = await page.evaluate(() => fetch("/api/v1/commands").then(response => response.headers.get("cache-control")));
    assert(/no-store/i.test(cacheControl || ""), "authenticated API response may be cached");
    const cachedShell = await page.evaluate(async () => {
      const paths = ["/", "/static/console.css", "/static/console.js", "/static/icon.svg", "/manifest.webmanifest"];
      return (await Promise.all(paths.map(path => caches.match(path)))).every(Boolean);
    });
    assert(cachedShell, "service worker did not cache the authenticated shell assets");
    assert((await transcript()).length < 200000, "transcript was unbounded");
    assert(errors.length === 0, "browser console errors: " + errors.join("; "));

    // /quit is isolated because a browser is allowed to honor window.close().
    const quitPage = await context.newPage();
    await quitPage.goto(new URL("/", options.url).href, { waitUntil: "domcontentloaded" });
    const quitLine = quitPage.locator("#line");
    await quitLine.waitFor();
    let quitClosed = false;
    quitPage.once("close", () => { quitClosed = true; });
    await quitLine.fill("/quit"); await quitLine.press("Enter");
    await page.waitForTimeout(250);
    if (!quitClosed) {
      assert(/Closing the console window|close it manually/.test(await quitPage.locator("#console-status").textContent()), "quit did not close or explain manual close");
      await quitPage.close();
    }

    // The network-first worker must return an authorization failure rather
    // than an old cached console after the session disappears.
    await context.clearCookies();
    const expiredErrorStart = errors.length;
    const expiredShell = await page.evaluate(() => fetch("/", { cache: "no-store" }).then(response => response.status));
    assert(expiredShell === 401, "expired session received a cached authenticated shell");
    await page.waitForTimeout(100);
    const expiredErrors = errors.splice(expiredErrorStart);
    assert(expiredErrors.every(message => /401|unauthorized/i.test(message)), "auth expiry produced an unexpected browser error: " + expiredErrors.join("; "));
    assert(errors.length === 0, "browser console errors after auth expiry: " + errors.join("; "));
  } finally { await browser.close(); }
})().catch(error => { console.error(error.stack || error); process.exitCode = 1; });
