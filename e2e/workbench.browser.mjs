import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import http from "node:http";
import { spawn, spawnSync } from "node:child_process";
import readline from "node:readline";
import { chromium } from "playwright";
import { createServer } from "../frontend/node_modules/vite/dist/node/index.js";

const cm = path.resolve(import.meta.dirname, ".."),
  root = path.join(cm, "frontend");
const runtime =
  process.env.WORKBENCH_RUNTIME_ROOT || path.resolve(cm, "../AgentsRuntime");
const temp = fs.mkdtempSync(path.join(os.tmpdir(), "cm-workbench-browser-"));
const workspace = path.join(temp, "workspace");
fs.mkdirSync(workspace);
const artifacts =
  process.env.WORKBENCH_SCREENSHOTS ||
  path.resolve(cm, "../artifacts/workbench-v4-browser");
fs.mkdirSync(artifacts, { recursive: true });
const env = {
  ...process.env,
  HOME: temp,
  GIT_CONFIG_NOSYSTEM: "1",
  GIT_CONFIG_GLOBAL: path.join(temp, "gitconfig"),
};
delete env.GOROOT;
fs.writeFileSync(
  env.GIT_CONFIG_GLOBAL,
  "[user]\n name = Workbench Test\n email = workbench@example.test\n[init]\n defaultBranch = main\n",
);
function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    env,
    encoding: "utf8",
    ...options,
  });
  assert.equal(
    result.status,
    0,
    `${command} failed: ${result.stderr || result.stdout}`,
  );
  return result.stdout.trim();
}
const git = (cwd, ...args) => run("git", args, { cwd });
const helper = path.join(temp, "git-helper");
run(
  "go",
  [
    "build",
    "-o",
    helper,
    path.join(cm, "e2e/fixtures/workbench-git-helper.go"),
  ],
  { cwd: path.join(runtime, "clawmanager-agent") },
);
const remote = path.join(workspace, "origin.git");
git(workspace, "init", "--bare", remote);
const seed = path.join(workspace, "seed");
fs.mkdirSync(seed);
git(seed, "init");
fs.writeFileSync(path.join(seed, "app.py"), 'print("one")\n');
fs.writeFileSync(path.join(seed, "README.md"), "# Example\n");
git(seed, "add", ".");
git(seed, "commit", "-m", "Initial project");
git(seed, "remote", "add", "origin", remote);
git(seed, "push", "-u", "origin", "main");
const second = path.join(workspace, "second");
fs.mkdirSync(second);
git(second, "init");
fs.writeFileSync(path.join(second, "other.txt"), "second repository\n");
git(second, "add", ".");
git(second, "commit", "-m", "Second initial");
let vite, browser, api;
const children = [];
const sessions = new Map();
let generation = 3;
let terminalCreates = 0;
let previewCreates = 0;
const gitOperations = [];
const errors = [];
const json = (res, data, status = 200) => {
  res.writeHead(status, { "content-type": "application/json" });
  res.end(
    JSON.stringify(
      status === 200
        ? { success: true, data }
        : { success: false, error: data },
    ),
  );
};
async function body(req) {
  let text = "";
  for await (const chunk of req) {
    text += chunk;
    if (text.length > 1 << 20) throw Error("too large");
  }
  return text ? JSON.parse(text) : {};
}
function createTerminal(cwd = "") {
  const id = `session-${++terminalCreates}`,
    child = spawn(
      "python3",
      ["-u", path.join(cm, "e2e/fixtures/workbench-pty.py")],
      { cwd: workspace, env, stdio: ["pipe", "pipe", "pipe"] },
    );
  children.push(child);
  const session = {
    id,
    name: `Terminal ${terminalCreates}`,
    cwd,
    generation,
    created_at: new Date().toISOString(),
    exited: false,
    buffer: Buffer.alloc(0),
    cursor: 0,
    child,
    cols: 100,
    rows: 30,
  };
  sessions.set(id, session);
  readline.createInterface({ input: child.stdout }).on("line", (line) => {
    const event = JSON.parse(line);
    if (event.data)
      session.buffer = Buffer.concat([
        session.buffer,
        Buffer.from(event.data, "base64"),
      ]);
    if (event.exited) session.exited = true;
  });
  child.on("exit", () => {
    session.exited = true;
  });
  return session;
}
const serialize = (session) => ({
  id: session.id,
  name: session.name,
  cwd: session.cwd,
  generation: session.generation,
  created_at: session.created_at,
  exited: session.exited,
});
try {
  api = http.createServer(async (req, res) => {
    try {
      const url = new URL(req.url, "http://test");
      if (url.pathname.startsWith("/preview-app")) {
        res.writeHead(200, { "content-type": "text/html" });
        res.end(
          "<html><body><h1>Real local preview</h1><p>Manual browser navigation</p></body></html>",
        );
        return;
      }
      const route = url.pathname.split("/workbench")[1];
      const input = await body(req);
      if (url.pathname === "/api/v1/instances/42") {
        json(res, {
          instance: {
            id: 42,
            user_id: 7,
            name: "Workbench integration",
            type: "opencode",
            instance_mode: "lite",
            runtime_type: "gateway",
            status: "running",
            runtime_generation: generation,
            workspace_path: workspace,
            cpu_cores: 2,
            memory_gb: 4,
            disk_gb: 20,
            created_at: new Date().toISOString(),
          },
        });
        return;
      }
      if (url.pathname.endsWith("/status")) {
        json(res, {
          instance_status: {
            instance_id: 42,
            status: "running",
            availability: "available",
            created_at: new Date().toISOString(),
          },
        });
        return;
      }
      if (url.pathname.endsWith("/runtime")) {
        json(res, { commands: [], skills: [] });
        return;
      }
      if (url.pathname.endsWith("/access")) {
        json(res, {
          access_url: `http://127.0.0.1:${api.address().port}/preview-app`,
          token: "test-only",
          expires_at: new Date(Date.now() + 3600000).toISOString(),
        });
        return;
      }
      if (url.pathname.endsWith("/external-access")) {
        json(res, { external_access: null });
        return;
      }
      if (url.pathname.endsWith("/skills")) {
        json(res, { skills: [] });
        return;
      }

      if (route === "/capabilities") {
        json(res, {
          generation,
          workspace,
          git: true,
          terminal: true,
          preview: true,
          processes: true,
        });
        return;
      }
      if (Number(url.searchParams.get("generation")) !== generation) {
        json(res, "instance environment changed", 409);
        return;
      }
      if (route === "/git") {
        gitOperations.push(input.operation);
        const result = spawnSync(helper, [], {
          env,
          input: JSON.stringify({ root: workspace, request: input }),
          encoding: "utf8",
        });
        const output = JSON.parse(result.stdout);
        json(
          res,
          result.status === 0 ? output : output.error,
          result.status === 0 ? 200 : 400,
        );
        return;
      }
      if (route === "/previews") {
        previewCreates++;
        json(res, {
          id: `preview-${previewCreates}`,
          url: `http://127.0.0.1:${api.address().port}/preview-app`,
          generation,
        });
        return;
      }
      if (route === "/terminals" && req.method === "POST") {
        json(res, serialize(createTerminal(input.cwd)));
        return;
      }
      if (route === "/terminals") {
        json(res, { sessions: [...sessions.values()].map(serialize) });
        return;
      }
      if (route === "/processes") {
        json(res, { processes: [] });
        return;
      }
      const match = route?.match(/^\/terminals\/([^/]+)(?:\/(.*))?$/);
      if (match) {
        const session = sessions.get(match[1]);
        if (!session) {
          json(res, "missing session", 404);
          return;
        }
        if (match[2] === "output") {
          const cursor = Number(url.searchParams.get("cursor"));
          json(res, {
            data: session.buffer.subarray(cursor).toString("base64"),
            cursor: session.buffer.length,
            truncated: false,
            exited: session.exited,
          });
          return;
        }
        if (match[2] === "input") {
          session.child.stdin.write(
            JSON.stringify({ input: input.input }) + "\n",
          );
          json(res, {});
          return;
        }
        if (match[2] === "resize") {
          session.cols = input.cols;
          session.rows = input.rows;
          session.child.stdin.write(JSON.stringify(input) + "\n");
          json(res, {});
          return;
        }
        if (req.method === "DELETE") {
          session.child.stdin.write('{"close":true}\n');
          session.exited = true;
          json(res, serialize(session));
          return;
        }
      }
      if (url.pathname.includes("/workspace/")) {
        json(res, { entries: [] });
        return;
      }
      json(res, {});
    } catch (error) {
      errors.push(String(error));
      json(res, "test server error", 500);
    }
  });
  await new Promise((resolve) => api.listen(0, "127.0.0.1", resolve));
  const fixture = path.join(temp, "fixture.tsx");
  const component =
    "/@fs/" + path.join(root, "src/components/workbench/InstanceWorkbench.tsx");
  const i18n = "/@fs/" + path.join(root, "src/contexts/I18nContext.tsx");
  fs.writeFileSync(
    fixture,
    `import React,{useState}from'react';import{createRoot}from'react-dom/client';import{QueryClient,QueryClientProvider}from'@tanstack/react-query';import{I18nProvider}from ${JSON.stringify(i18n)};import{InstanceWorkbench}from ${JSON.stringify(component)};import '/src/index.css';const query=new QueryClient({defaultOptions:{queries:{retry:false}}});function App(){const[message,setMessage]=useState('');const[gen,setGen]=useState(3);return <QueryClientProvider client={query}><I18nProvider><main style={{display:'grid',gridTemplateColumns:'minmax(0,.9fr) minmax(0,1.1fr)',gap:16,height:'calc(100vh - 32px)',padding:16}}><aside style={{border:'1px solid #e2e8f0',borderRadius:10,padding:20,background:'white'}}><h2>Agent</h2><textarea aria-label="任务草稿" defaultValue="保留草稿"/><button onClick={()=>setMessage('npm run dev — http://localhost:3000')}>运行 Agent 命令</button><p>{message}</p><button onClick={()=>setGen(4)}>模拟迁移</button></aside><InstanceWorkbench instanceId={42} runtimeGeneration={gen} isRunning={true}/></main><style>{'@media(max-width:760px){main{display:flex!important;flex-direction:column;height:auto!important}aside{min-height:180px}.wb-root{height:680px;flex:none}}'}</style></I18nProvider></QueryClientProvider>};(window.__workbenchTestRoot??=createRoot(document.getElementById('root'))).render(<App/>);`,
  );
  const pageFixture = path.join(temp, "page-fixture.tsx");
  fs.writeFileSync(
    pageFixture,
    `import React from'react';import{createRoot}from'react-dom/client';import{MemoryRouter,Routes,Route}from'react-router-dom';import{QueryClient,QueryClientProvider}from'@tanstack/react-query';import{I18nProvider}from ${JSON.stringify(i18n)};import{AuthProvider}from '/@fs/${path.join(root, "src/contexts/AuthContext.tsx")}';import{useAuthStore}from '/@fs/${path.join(root, "src/stores/authStore.ts")}';import Page from '/@fs/${path.join(root, "src/pages/instances/InstanceDetailPage.tsx")}';import '/src/index.css';useAuthStore.setState({user:{id:7,username:'tester',role:'user'},isAuthenticated:true,isLoading:false,fetchCurrentUser:async()=>{}});createRoot(document.getElementById('root')).render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><I18nProvider><AuthProvider><MemoryRouter initialEntries={['/instances/42']}><Routes><Route path="/instances/:id" element={<Page/>}/></Routes></MemoryRouter></AuthProvider></I18nProvider></QueryClientProvider>);`,
  );
  process.chdir(root);
  vite = await createServer({
    root,
    configFile: path.join(root, "vite.config.ts"),
    resolve: { dedupe: ["react", "react-dom", "@tanstack/react-query"] },
    server: { host: "127.0.0.1", port: 0, fs: { allow: [root, temp] } },
    plugins: [
      {
        name: "workbench-test-page",
        configureServer(server) {
          server.middlewares.use("/__workbench_page__", async (_req, res) => {
            res.setHeader("content-type", "text/html");
            res.end(
              await server.transformIndexHtml(
                "/__workbench_page__",
                `<html><body><div id="root"></div><script type="module" src="/@fs/${pageFixture}"></script></body></html>`,
              ),
            );
          });
          server.middlewares.use("/__workbench__", async (_req, res) => {
            res.setHeader("content-type", "text/html");
            res.end(
              await server.transformIndexHtml(
                "/__workbench__",
                `<html><body><div id="root"></div><script type="module" src="/@fs/${fixture}"></script></body></html>`,
              ),
            );
          });
        },
      },
    ],
  });
  await vite.listen();
  browser = await chromium.launch({ channel: "chrome", headless: true });
  const page = await browser.newPage({
    viewport: { width: 1440, height: 1000 },
  });
  page.on("pageerror", (error) => errors.push(error.message));
  await page.routeWebSocket(/\/api\/v1\/ws(?:\?|$)/, () => {});
  await page.addInitScript(() => {
    localStorage.setItem("clawmanager_locale", "zh");
    localStorage.setItem("access_token", "test-token");
  });
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const target = `http://127.0.0.1:${api.address().port}${new URL(request.url()).pathname}${new URL(request.url()).search}`;
    const response = await route.fetch({ url: target });
    await route.fulfill({ response });
  });
  await page.goto(
    `http://127.0.0.1:${vite.httpServer.address().port}/__workbench__`,
  );
  await page.getByRole("tab", { name: "文件", exact: true }).focus();
  await page.keyboard.press("ArrowRight");
  assert.equal(
    await page
      .getByRole("tab", { name: "Git 项目", exact: true })
      .getAttribute("aria-selected"),
    "true",
  );
  await page.keyboard.press("End");
  assert.equal(
    await page
      .getByRole("tab", { name: "预览与终端", exact: true })
      .getAttribute("aria-selected"),
    "true",
  );
  await page.getByRole("button", { name: "新建标签", exact: true }).waitFor();
  await page.getByRole("button", { name: "运行 Agent 命令" }).click();
  assert.equal(await page.locator(".wb-program-tab").count(), 0);
  assert.equal(terminalCreates, 0);
  assert.equal(previewCreates, 0);
  await page.screenshot({
    path: path.join(artifacts, "01-manual-entry.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "新建标签", exact: true }).click();
  await page.getByRole("button", { name: "浏览器", exact: true }).click();
  await page
    .getByRole("textbox", { name: "服务地址" })
    .fill("http://localhost:3000");
  await page.getByRole("button", { name: "打开", exact: true }).click();
  await page
    .frameLocator(".wb-preview-frame")
    .getByRole("heading", { name: "Real local preview" })
    .waitFor();
  assert.equal(previewCreates, 1);
  await page.screenshot({
    path: path.join(artifacts, "02-browser-preview.png"),
    fullPage: true,
  });
  console.log("PASS manual browser and isolated iframe");
  await page.getByRole("button", { name: "新建标签", exact: true }).click();
  await page.getByRole("button", { name: "终端", exact: true }).click();
  await page.locator(".xterm-helper-textarea").waitFor();
  await page.locator(".xterm-helper-textarea").fill("");
  await page.locator(".xterm-helper-textarea").focus();
  await page.keyboard.type("printf 'UI_PTY_OK\\n'");
  await page.keyboard.press("Enter");
  await assertWait(
    () => sessions.get("session-1").buffer.toString().includes("UI_PTY_OK\r\n"),
    "real PTY command output",
  );
  await page.getByRole("tab", { name: "文件", exact: true }).click();
  assert.equal(await page.locator(".xterm").count(), 1);
  await page.getByRole("tab", { name: "预览与终端", exact: true }).click();
  assert.equal(terminalCreates, 1);
  console.log("PASS real PTY and hidden-session recovery");
  await page
    .getByRole("button", { name: "隐藏标签 Terminal 1", exact: true })
    .click();
  assert.equal(await page.locator(".xterm").count(), 1);
  await page.getByRole("button", { name: "会话与进程", exact: false }).click();
  await page
    .locator(".wb-session-row")
    .filter({ hasText: "Terminal 1" })
    .getByRole("button", { name: "显示" })
    .click();
  assert.equal(terminalCreates, 1);
  await page.getByRole("button", { name: "新建标签", exact: true }).click();
  await page.getByRole("button", { name: "终端", exact: true }).click();
  await assertWait(() => terminalCreates === 2, "second terminal");
  await page.screenshot({
    path: path.join(artifacts, "03-multiple-sessions.png"),
    fullPage: true,
  });
  await page.getByRole("tab", { name: "Git 项目", exact: true }).click();
  await page
    .getByRole("button", { name: "添加项目", exact: true })
    .first()
    .click();
  await page.getByRole("textbox", { name: "仓库地址" }).fill(remote);
  await page
    .getByRole("textbox", { name: "项目路径", exact: true })
    .fill("projects/demo");
  await page
    .getByRole("button", { name: "克隆仓库", exact: true })
    .last()
    .click();
  await page
    .getByRole("combobox", { name: "选择仓库" })
    .selectOption("projects/demo");
  const repo = path.join(workspace, "projects/demo");
  await assertWait(() => fs.existsSync(path.join(repo, ".git")), "real clone");
  fs.writeFileSync(path.join(repo, "app.py"), 'print("changed")\n');
  fs.writeFileSync(path.join(repo, "README.md"), "# Changed\n");
  await page
    .locator(".wb-project-bar")
    .getByRole("button", { name: "刷新" })
    .click();
  await page
    .getByRole("button", { name: "暂存 app.py", exact: true })
    .waitFor();
  await page.locator(".wb-file-choice").filter({ hasText: "app.py" }).click();
  await page.getByText('+print("changed")', { exact: false }).waitFor();
  await page.screenshot({
    path: path.join(artifacts, "04-git-diff.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "暂存 app.py", exact: true }).click();
  await page.getByRole("textbox", { name: "提交说明" }).fill("Commit only app");
  await page.getByRole("button", { name: "提交 (1)", exact: true }).click();
  await assertWait(
    () => git(repo, "log", "-1", "--format=%s") === "Commit only app",
    "real local commit",
  );
  assert.match(git(repo, "status", "--porcelain"), /README.md/);
  assert.notEqual(
    git(repo, "rev-parse", "HEAD"),
    git(workspace, "--git-dir", remote, "rev-parse", "HEAD"),
  );
  await page.getByRole("button", { name: "推送", exact: true }).click();
  await assertWait(
    () =>
      git(repo, "rev-parse", "HEAD") ===
      git(workspace, "--git-dir", remote, "rev-parse", "HEAD"),
    "explicit real push",
  );
  await page.getByRole("button", { name: "添加项目", exact: true }).click();
  await page
    .getByRole("button", { name: "导入已有项目", exact: true })
    .first()
    .click();
  await page
    .getByRole("textbox", { name: "项目路径", exact: true })
    .fill("second");
  await page
    .getByRole("button", { name: "导入已有项目", exact: true })
    .last()
    .click();
  await page.getByRole("combobox", { name: "选择仓库" }).selectOption("second");
  await page.getByRole("button", { name: "历史", exact: true }).click();
  await page.getByText("Second initial", { exact: true }).waitFor();
  await page.screenshot({
    path: path.join(artifacts, "05-git-projects.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({
    path: path.join(artifacts, "06-mobile-git.png"),
    fullPage: true,
  });
  assert.ok(
    await page
      .locator(".wb-root")
      .evaluate((node) => node.scrollWidth <= node.clientWidth + 1),
    "mobile workspace overflows",
  );
  await page.getByRole("tab", { name: "预览与终端", exact: true }).click();
  await page.screenshot({
    path: path.join(artifacts, "07-mobile-tabs.png"),
    fullPage: true,
  });
  assert.equal(
    await page.getByRole("textbox", { name: "任务草稿" }).inputValue(),
    "保留草稿",
  );
  generation = 4;
  await page.getByRole("button", { name: "模拟迁移" }).click();
  await page
    .locator(".wb-terminal-message:visible")
    .filter({ hasText: "实例运行环境已变更" })
    .waitFor();
  assert.equal(await page.locator(".wb-preview-frame").count(), 0);
  generation = 3;
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(
    `http://127.0.0.1:${vite.httpServer.address().port}/__workbench_page__`,
  );
  await page.locator(".wb-root:visible").waitFor();
  await page
    .locator(".wb-root:visible")
    .getByRole("tab", { name: "预览与终端", exact: true })
    .click();
  await page
    .locator(".wb-root:visible")
    .getByRole("button", { name: "新建浏览器", exact: true })
    .click();
  await page.screenshot({
    path: path.join(artifacts, "08-instance-page-desktop.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page
    .locator(".wb-root:visible")
    .getByRole("tab", { name: "预览与终端", exact: true })
    .click();
  await page.screenshot({
    path: path.join(artifacts, "09-instance-page-mobile.png"),
    fullPage: true,
  });
  assert.equal(
    await page.locator(".wb-root:visible .wb-program-tab").count(),
    1,
    "responsive layout discarded manual browser tab",
  );
  const duplicateIDs = await page
    .locator(".wb-root [id]")
    .evaluateAll((nodes) => {
      const ids = nodes.map((node) => node.id);
      return ids.filter((id, index) => ids.indexOf(id) !== index);
    });
  assert.deepEqual(
    duplicateIDs,
    [],
    "responsive workbench copies reuse DOM IDs",
  );
  const layout = await page.locator(".wb-root:visible").evaluate((node) => {
    const rect = node.getBoundingClientRect(),
      parent = node.parentElement.closest("section").getBoundingClientRect();
    return {
      top: rect.top,
      bottom: rect.bottom,
      parentBottom: parent.bottom,
      height: rect.height,
      agentHeight: node.parentElement
        .closest("section")
        .children[0].getBoundingClientRect().height,
    };
  });
  assert.ok(
    layout.agentHeight >= 260,
    `mobile Agent view is too short: ${JSON.stringify(layout)}`,
  );
  assert.ok(
    layout.height >= 500 && layout.bottom <= layout.parentBottom + 1,
    `instance mobile workspace is clipped: ${JSON.stringify(layout)}`,
  );
  assert.equal(errors.length, 0, errors.join("\n"));
  assert.ok(
    gitOperations.includes("clone") &&
      gitOperations.includes("commit") &&
      gitOperations.includes("push"),
  );
  console.log(
    "PASS manual-only tabs, real PTY I/O and retention, two sessions, real Git clone/stage/commit/push/import, isolated iframe, desktop/mobile and generation fencing",
  );
} finally {
  for (const child of children) {
    if (child.exitCode === null) {
      child.stdin.write('{"close":true}\n');
      child.stdin.end();
    }
  }
  await browser?.close();
  await vite?.close();
  if (api) await new Promise((resolve) => api.close(resolve));
  await Promise.all(
    children.map((child) =>
      child.exitCode !== null
        ? Promise.resolve()
        : new Promise((resolve) => child.once("exit", resolve)),
    ),
  );
  fs.rmSync(temp, { recursive: true, force: true });
}
async function assertWait(check, label) {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    if (await check()) return;
    await new Promise((resolve) => setTimeout(resolve, 80));
  }
  throw Error(`Timed out: ${label}`);
}
