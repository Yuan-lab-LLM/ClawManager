// Run on the test node with its cached OpenClaw Chromium/Playwright. Only
// disposable local HTTP fixtures are accessed; no cluster credentials or LLMs.
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { readFile, mkdir } from "node:fs/promises";
import { createRequire } from "node:module";
import path from "node:path";
const require = createRequire("/usr/local/lib/node_modules/openclaw/package.json");
const { chromium } = require("playwright-core");
const dist = process.env.FRAME_FIXTURE_DIST;
assert.ok(dist, "FRAME_FIXTURE_DIST must point to the compiled fixture");
const output = process.env.FRAME_FIXTURE_OUTPUT || "/frame-browser-results";
await mkdir(output, { recursive: true });
let mode = "healthy", accesses = 0, navigations = 0;
const healthy = '<!doctype html><html><body><openclaw-app><main><h1>OpenClaw test session</h1><textarea aria-label="Draft"></textarea></main></openclaw-app></body></html>';
const server = createServer(async (req, res) => {
  const url = new URL(req.url, "http://fixture");
  if (url.pathname === "/api/v1/instances/42/access") {
    accesses++;
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify({ data: { access_url: "/api/v1/instances/42/proxy/", expires_at: new Date(Date.now() + 3600_000).toISOString() } }));
  } else if (url.pathname === "/api/v1/instances/42/proxy/") {
    navigations++;
    if (mode === "auth-once") {
      mode = "healthy";
      res.writeHead(401, { "Content-Type": "application/json" });
      res.end('{"error":"Access token expired or invalid"}');
    } else {
      res.setHeader("Content-Type", "text/html");
      res.end(mode === "blank" ? "<!doctype html><html><body></body></html>" : healthy);
    }
  } else if (url.pathname.startsWith("/api/")) {
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify({ data: { enabled: false, summary: {} } }));
  } else {
    try {
      const file = path.resolve(dist, "." + url.pathname);
      assert.ok(file.startsWith(path.resolve(dist) + path.sep));
      res.setHeader("Content-Type", file.endsWith(".js") ? "application/javascript" : file.endsWith(".css") ? "text/css" : "text/html");
      res.end(await readFile(file));
    } catch { res.writeHead(404); res.end("Missing fixture"); }
  }
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const browser = await chromium.launch({ executablePath: "/usr/bin/chromium", headless: true, args: ["--no-sandbox", "--disable-dev-shm-usage"] });
try {
  const page = await browser.newPage({ viewport: { width: 1200, height: 760 } });
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  // The cached test Runtime has Latin fonts; use English for readable captures.
  await page.addInitScript(() => localStorage.setItem("clawmanager_locale", "en"));
  await page.clock.install();
  const origin = `http://127.0.0.1:${server.address().port}`;
  await page.goto(origin + "/scripts/fixtures/instance-frame-recovery/index.html");
  const draft = () => page.frameLocator("iframe").getByRole("textbox", { name: "Draft" });
  await draft().waitFor();
  const tick = async (ms) => {
    // Let real fetch completions and React commits settle between clock ticks.
    // Advancing a whole minute at once can overtake an iframe navigation.
    for (let remaining = ms; remaining > 0; remaining -= 1000) {
      await page.clock.runFor(Math.min(remaining, 1000));
      await page.waitForTimeout(20);
    }
  };
  const blank = async () => { await page.locator("iframe").evaluate((frame) => { frame.contentDocument.body.innerHTML = ""; }); };

  await draft().fill("Keep this unsent draft");
  await tick(20_000);
  assert.equal(accesses, 1);
  assert.equal(await draft().inputValue(), "Keep this unsent draft");

  await blank();
  for (let i = 0; i < 5 && accesses < 2; i++) await tick(5000);
  await draft().waitFor();
  assert.equal(accesses, 2);
  assert.equal(navigations, 2, "Unchanged same-origin URL must navigate again");
  console.log("PASS established blank page renewed access and actually reloaded");

  mode = "auth-once";
  await page.locator("iframe").evaluate((frame) => { frame.src = frame.src; });
  await tick(5000);
  await draft().waitFor();
  assert.equal(accesses, 3);
  console.log("PASS established proxy 401 recovered using fresh access");

  const before = accesses;
  await blank();
  await tick(15_000);
  await page.locator("iframe").evaluate((frame) => { frame.contentDocument.body.innerHTML = '<openclaw-app><textarea aria-label="Draft">Recovered draft</textarea></openclaw-app>'; });
  await tick(5000);
  assert.equal(accesses, before);
  assert.equal(await draft().inputValue(), "Recovered draft");
  console.log("PASS self-recovery during backoff preserved the draft");

  await tick(35_000); // Reset the budget only after continuous healthy rendering.
  mode = "blank";
  const baseline = accesses;
  await blank();
  for (let i = 0; i < 30; i++) {
    await tick(5000);
    if (await page.getByRole("alert").count()) break;
  }
  await page.getByRole("alert").waitFor();
  assert.equal(accesses, baseline + 3);
  await tick(60_000);
  assert.equal(accesses, baseline + 3, "No background reloads after exhaustion");
  await page.screenshot({ path: path.join(output, "frame-error.png") });
  console.log("PASS persistent blank page stopped after three attempts and showed retry");

  mode = "healthy";
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await draft().waitFor();
  assert.equal(accesses, baseline + 4);
  assert.equal(await page.getByRole("alert").count(), 0);
  await page.screenshot({ path: path.join(output, "frame-recovered.png") });
  assert.deepEqual(errors, []);
  console.log("PASS manual retry restored the actual embedded page; no page script errors");
} finally {
  await browser.close();
  await new Promise((resolve) => server.close(resolve));
}
