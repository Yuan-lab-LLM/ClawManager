import http from "node:http";

const owner = "owner.demo@example.com";
const now = new Date();
const instances = [
  {
    id: 101,
    owner,
    name: "市场分析助手",
    description: "持续追踪市场动态、整理竞品信息并生成结构化分析报告。",
    type: "openclaw",
    runtime_type: "gateway",
    instance_mode: "lite",
    status: "running",
    created_at: new Date(now.getTime() - 86_400_000 * 3).toISOString(),
    updated_at: now.toISOString(),
    started_at: new Date(now.getTime() - 3_600_000).toISOString(),
  },
  {
    id: 102,
    owner,
    name: "客户洞察助手",
    description: "持续整理客户反馈，识别关键诉求并生成行动建议。",
    type: "openclaw",
    runtime_type: "gateway",
    instance_mode: "lite",
    status: "running",
    created_at: new Date(now.getTime() - 86_400_000 * 2.8).toISOString(),
    updated_at: new Date(now.getTime() - 1_800_000).toISOString(),
    started_at: new Date(now.getTime() - 7_200_000).toISOString(),
  },
  {
    id: 103,
    owner,
    name: "知识库助手",
    description: "面向企业资料检索、归纳与持续知识整理的 Hermes 工作空间。",
    type: "hermes",
    runtime_type: "gateway",
    instance_mode: "lite",
    status: "running",
    created_at: new Date(now.getTime() - 86_400_000 * 2).toISOString(),
    updated_at: new Date(now.getTime() - 3_600_000).toISOString(),
    started_at: new Date(now.getTime() - 7_200_000).toISOString(),
  },
  {
    id: 104,
    owner,
    name: "深度研究助理",
    description: "使用持久会话追踪研究问题、资料线索和阶段性结论。",
    type: "hermes",
    runtime_type: "gateway",
    instance_mode: "lite",
    status: "running",
    created_at: new Date(now.getTime() - 86_400_000 * 1.8).toISOString(),
    updated_at: new Date(now.getTime() - 5_400_000).toISOString(),
    started_at: new Date(now.getTime() - 5_400_000).toISOString(),
  },
  {
    id: 105,
    owner,
    name: "代码协作助手",
    description: "面向代码生成、终端操作与仓库协作的一体化编码工作台。",
    type: "opencode",
    runtime_type: "gateway",
    instance_mode: "lite",
    status: "running",
    created_at: new Date(now.getTime() - 86_400_000).toISOString(),
    updated_at: new Date(now.getTime() - 7_200_000).toISOString(),
    started_at: new Date(now.getTime() - 7_200_000).toISOString(),
  },
  {
    id: 106,
    owner,
    name: "仓库守护助手",
    description: "用于仓库巡检、代码审查、测试修复和自动化维护。",
    type: "opencode",
    runtime_type: "gateway",
    instance_mode: "lite",
    status: "running",
    created_at: new Date(now.getTime() - 86_400_000 * 0.8).toISOString(),
    updated_at: new Date(now.getTime() - 9_000_000).toISOString(),
    started_at: new Date(now.getTime() - 9_000_000).toISOString(),
  },
  {
    id: 107,
    owner,
    name: "流程执行助手",
    description: "通过插件化能力组合执行复杂研发流程和多步骤任务。",
    type: "deepseek-harness",
    runtime_type: "gateway",
    instance_mode: "lite",
    status: "running",
    created_at: new Date(now.getTime() - 86_400_000 * 0.5).toISOString(),
    updated_at: new Date(now.getTime() - 10_800_000).toISOString(),
    started_at: new Date(now.getTime() - 10_800_000).toISOString(),
  },
  {
    id: 108,
    owner,
    name: "插件实验空间",
    description: "用于验证 DeepSeek Harness 插件、工具与子代理协作流程。",
    type: "deepseek-harness",
    runtime_type: "gateway",
    instance_mode: "lite",
    status: "running",
    created_at: new Date(now.getTime() - 86_400_000 * 0.3).toISOString(),
    updated_at: new Date(now.getTime() - 12_600_000).toISOString(),
    started_at: new Date(now.getTime() - 12_600_000).toISOString(),
  },
  {
    id: 109,
    owner,
    name: "研发协作工作台",
    description: "用于代码开发、终端操作、项目文件管理和持续研发协作。",
    type: "workbuddy",
    runtime_type: "desktop",
    runtime_variant: "linux",
    instance_mode: "pro",
    status: "running",
    created_at: new Date(now.getTime() - 86_400_000 * 0.2).toISOString(),
    updated_at: new Date(now.getTime() - 14_400_000).toISOString(),
    started_at: new Date(now.getTime() - 14_400_000).toISOString(),
  },
  {
    id: 110,
    owner,
    name: "项目维护空间",
    description: "面向仓库维护、问题排查、构建验证与技术文档整理。",
    type: "workbuddy",
    runtime_type: "desktop",
    runtime_variant: "linux",
    instance_mode: "pro",
    status: "running",
    created_at: new Date(now.getTime() - 86_400_000 * 0.1).toISOString(),
    updated_at: new Date(now.getTime() - 16_200_000).toISOString(),
    started_at: new Date(now.getTime() - 16_200_000).toISOString(),
  },
];

const workspaceEntries = {
  "": [
    { name: "projects", path: "projects", is_dir: true, size: 0, modified_at: now.toISOString(), previewable: false, downloadable: false },
    { name: "reports", path: "reports", is_dir: true, size: 0, modified_at: now.toISOString(), previewable: false, downloadable: false },
    { name: "README.md", path: "README.md", is_dir: false, size: 1864, modified_at: now.toISOString(), previewable: true, downloadable: true },
    { name: "runtime-config.json", path: "runtime-config.json", is_dir: false, size: 742, modified_at: now.toISOString(), previewable: true, downloadable: true },
  ],
  projects: [
    { name: "customer-portal", path: "projects/customer-portal", is_dir: true, size: 0, modified_at: now.toISOString(), previewable: false, downloadable: false },
    { name: "notes.md", path: "projects/notes.md", is_dir: false, size: 934, modified_at: now.toISOString(), previewable: true, downloadable: true },
  ],
  reports: [
    { name: "weekly-summary.md", path: "reports/weekly-summary.md", is_dir: false, size: 2480, modified_at: now.toISOString(), previewable: true, downloadable: true },
  ],
};

function json(response, status, data) {
  response.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
  });
  response.end(JSON.stringify({ data }));
}

function instanceForPath(pathname) {
  const match = pathname.match(/\/instances\/(\d+)/);
  return instances.find((item) => item.id === Number(match?.[1]));
}

function runtimeHTML(instance) {
  const runtimeName = instance?.type === "hermes"
    ? "Hermes"
    : instance?.type === "opencode"
      ? "OpenCode"
      : instance?.type === "deepseek-harness"
        ? "DeepSeek Harness"
        : "OpenClaw";
  return `<!doctype html>
  <html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
  <style>
    *{box-sizing:border-box}body{margin:0;background:#f7f8fa;color:#172033;font-family:Inter,"Microsoft YaHei",sans-serif}
    .shell{min-height:100vh;padding:32px}.bar{display:flex;align-items:center;justify-content:space-between;margin-bottom:28px}
    .brand{font-size:18px;font-weight:700}.status{border:1px solid #a7f3d0;background:#ecfdf5;color:#047857;border-radius:999px;padding:6px 12px;font-size:12px}
    .hero{border:1px solid #e2e8f0;background:white;border-radius:20px;padding:32px;box-shadow:0 14px 40px rgba(15,23,42,.06)}
    .eyebrow{color:#0284c7;font-size:12px;font-weight:700;letter-spacing:.16em;text-transform:uppercase}.hero h1{font-size:32px;margin:12px 0}
    .hero p{color:#64748b;line-height:1.8}.cards{display:grid;grid-template-columns:repeat(3,1fr);gap:14px;margin-top:26px}
    .card{border:1px solid #e2e8f0;border-radius:14px;padding:16px}.label{font-size:12px;color:#94a3b8}.value{margin-top:8px;font-weight:650}
  </style></head><body><main class="shell"><div class="bar"><div class="brand">${runtimeName} Runtime</div><div class="status">运行中</div></div>
  <section class="hero"><div class="eyebrow">IEI OWNER MOCK</div><h1>${instance?.name ?? "模拟实例"}</h1><p>这是本地模拟的实例服务界面，用于检查 IEI owner 门户的布局、比例和右侧文件管理器，不连接真实 Runtime。</p>
  <div class="cards"><div class="card"><div class="label">Runtime</div><div class="value">${runtimeName}</div></div><div class="card"><div class="label">Owner</div><div class="value">${owner}</div></div><div class="card"><div class="label">Instance ID</div><div class="value">#${instance?.id ?? "-"}</div></div></div></section></main></body></html>`;
}

const server = http.createServer((request, response) => {
  const url = new URL(request.url ?? "/", "http://127.0.0.1:9001");
  const { pathname } = url;
  console.log(`${request.method} ${pathname}`);

  const requestOrigin = request.headers.origin;
  if (requestOrigin) {
    response.setHeader("Access-Control-Allow-Origin", requestOrigin);
    response.setHeader("Access-Control-Allow-Credentials", "true");
    response.setHeader("Access-Control-Allow-Headers", "Content-Type");
    response.setHeader("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS");
    response.setHeader("Vary", "Origin");
  }
  if (request.method === "OPTIONS") {
    response.writeHead(204);
    return response.end();
  }

  if (request.method === "POST" && pathname === "/api/v1/ieisystem/session") {
    response.setHeader("Set-Cookie", "iei_system_session=mock; Path=/; HttpOnly; SameSite=Lax");
    return json(response, 200, { owner, expires_at: new Date(now.getTime() + 3_600_000).toISOString() });
  }
  if (request.method === "GET" && pathname === "/api/v1/ieisystem/session") {
    return json(response, 200, { owner, expires_at: new Date(now.getTime() + 3_600_000).toISOString() });
  }
  if (request.method === "POST" && pathname === "/api/v1/ieisystem/session/refresh") {
    response.setHeader("Set-Cookie", "iei_system_session=refreshed; Path=/; HttpOnly; SameSite=Lax");
    return json(response, 200, { owner, expires_at: new Date(now.getTime() + 3_600_000).toISOString() });
  }
  if (request.method === "DELETE" && pathname === "/api/v1/ieisystem/session") {
    return json(response, 200, null);
  }
  if (request.method === "GET" && pathname === "/api/v1/ieisystem/instances") {
    return json(response, 200, { instances, total: instances.length, page: 1, limit: 100 });
  }

  const instance = instanceForPath(pathname);
  if (request.method === "GET" && /^\/api\/v1\/ieisystem\/instances\/\d+$/.test(pathname)) {
    return instance ? json(response, 200, { instance }) : json(response, 404, null);
  }
  if (request.method === "POST" && /^\/api\/v1\/ieisystem\/instances\/\d+\/access$/.test(pathname)) {
    return instance
      ? json(response, 200, {
          access_url: `/api/v1/instances/${instance.id}/proxy/`,
          expires_at: new Date(now.getTime() + 3_600_000).toISOString(),
          desktop_proxy_mode: "control-plane",
          desktop_upstream_present: false,
          workspace_available: true,
          workspace_root: "Workspace",
        })
      : json(response, 404, null);
  }
  if (request.method === "GET" && /\/workspace\/files$/.test(pathname)) {
    const path = (url.searchParams.get("path") ?? "").replace(/^\/+|\/+$/g, "");
    return json(response, 200, { entries: workspaceEntries[path] ?? [] });
  }
  if (request.method === "GET" && /\/workspace\/preview$/.test(pathname)) {
    const path = url.searchParams.get("path") ?? "README.md";
    if (url.searchParams.get("raw") === "1") {
      response.writeHead(200, { "Content-Type": "text/plain; charset=utf-8" });
      return response.end(`# ${path}\n\n这是 IEI owner 门户的模拟工作区文件。`);
    }
    return json(response, 200, { preview: { kind: "text", text: `# ${path}\n\n这是 IEI owner 门户的模拟工作区文件。` } });
  }
  if (request.method === "GET" && /\/workspace\/download$/.test(pathname)) {
    response.writeHead(200, { "Content-Type": "text/plain; charset=utf-8" });
    return response.end("IEI mock download\n");
  }
  if (["POST", "PATCH", "DELETE"].includes(request.method ?? "") && /\/workspace\//.test(pathname)) {
    return json(response, 200, null);
  }
  if (request.method === "GET" && /^\/api\/v1\/instances\/\d+\/proxy\/?$/.test(pathname)) {
    response.writeHead(200, { "Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store" });
    return response.end(runtimeHTML(instance));
  }

  return json(response, 404, { error: `No mock route for ${request.method} ${pathname}` });
});

server.listen(9001, "127.0.0.1", () => {
  console.log("IEI mock API listening on http://127.0.0.1:9001");
});
