#!/usr/bin/env node
// ClawManager Agent Shim — 自动注册 + 心跳 + 状态上报
// 读取 CLAWMANAGER_AGENT_* 环境变量，向平台控制面注册并持续心跳
"use strict";

const os = require("os");
const { execSync } = require("child_process");

const BASE_URL  = process.env.CLAWMANAGER_AGENT_BASE_URL;
const BOOT_TOKEN = process.env.CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN;
const INSTANCE_ID = Number(process.env.CLAWMANAGER_AGENT_INSTANCE_ID || process.env.INSTANCE_ID || 0);
const PROTOCOL_VER = process.env.CLAWMANAGER_AGENT_PROTOCOL_VERSION || "v1";
const HEARTBEAT_SEC = 30;
const REPORT_SEC = 60;

if (!BASE_URL || !BOOT_TOKEN || !INSTANCE_ID) {
  console.error("[agent-shim] missing env, skipping (BASE_URL=%s BOOT=%s ID=%d)",
    !!BASE_URL, !!BOOT_TOKEN, INSTANCE_ID);
  process.exit(0);
}

const AGENT_ID = `clawmanager-shim-${INSTANCE_ID}`;
const AGENT_VER = "shim-1.0.0";
let sessionToken = null;
let heartbeatTimer = null;
let reportTimer = null;
let recovering = false;

function log(msg, ...a) { console.log(`[agent-shim] ${msg}`, ...a); }
function logErr(msg, ...a) { console.error(`[agent-shim] ${msg}`, ...a); }

function openclawVersion() {
  try { return execSync("openclaw --version 2>/dev/null | head -n1", { encoding: "utf8", timeout: 5000 }).trim(); }
  catch { return "unknown"; }
}

async function post(path, token, body) {
  const url = BASE_URL.replace(/\/+$/, "") + path;
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json", "Authorization": `Bearer ${token}` },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(15000),
  });
  const json = await res.json();
  if (!res.ok || json.success === false) {
    throw new Error(`${res.status} ${json.message || res.statusText}`);
  }
  return json;
}

async function register() {
  log("registering agent (instance=%d)...", INSTANCE_ID);
  const resp = await post("/api/v1/agent/register", BOOT_TOKEN, {
    instance_id: INSTANCE_ID,
    agent_id: AGENT_ID,
    agent_version: AGENT_VER,
    protocol_version: PROTOCOL_VER,
    capabilities: ["runtime-status", "heartbeat", "state-report"],
    host_info: { hostname: os.hostname(), platform: process.platform, arch: process.arch },
  });
  if (!resp.success || !resp.data || !resp.data.session_token) {
    throw new Error("register failed: " + JSON.stringify(resp));
  }
  sessionToken = resp.data.session_token;
  const hbInterval = resp.data.heartbeat_interval_seconds || HEARTBEAT_SEC;
  log("registered OK (heartbeat_interval=%ds)", hbInterval);
  return hbInterval;
}

async function heartbeat() {
  if (!sessionToken) return;
  try {
    await post("/api/v1/agent/heartbeat", sessionToken, {
      agent_id: AGENT_ID,
      openclaw_status: "running",
      summary: { openclaw_pid: process.ppid || 1, uptime: process.uptime() },
    });
  } catch (e) {
    logErr("heartbeat error: %s", e.message);
    await tryReRegister();
  }
}

async function reportState() {
  if (!sessionToken) return;
  try {
    await post("/api/v1/agent/state/report", sessionToken, {
      agent_id: AGENT_ID,
      runtime: { openclaw_status: "running", openclaw_version: openclawVersion() },
      system_info: { hostname: os.hostname(), mem_total: os.totalmem(), mem_free: os.freemem() },
      health: { ok: true },
    });
  } catch (e) {
    logErr("report error: %s", e.message);
  }
}

async function tryReRegister() {
  sessionToken = null;
  if (recovering) return;
  recovering = true;
  let delay = 5000;
  const MAX_DELAY = 120000;
  while (!sessionToken) {
    try {
      await register();
      log("re-registered successfully after recovery");
    } catch (e) {
      logErr("re-register failed: %s (retry in %ds)", e.message, delay / 1000);
      await new Promise(r => setTimeout(r, delay));
      delay = Math.min(delay * 2, MAX_DELAY);
    }
  }
  recovering = false;
}

async function main() {
  // 等 OpenClaw 网关先起来
  log("waiting for openclaw gateway to be ready...");
  for (let i = 0; i < 60; i++) {
    try {
      const r = await fetch("http://127.0.0.1:" + (process.env.OPENCLAW_PORT || 3001) + "/health",
        { signal: AbortSignal.timeout(3000) });
      if (r.ok) { log("gateway ready"); break; }
    } catch {}
    await new Promise(r => setTimeout(r, 2000));
  }

  let hbSec = HEARTBEAT_SEC;
  for (let attempt = 0; attempt < 10; attempt++) {
    try { hbSec = await register(); break; }
    catch (e) { logErr("register attempt %d failed: %s", attempt + 1, e.message); await new Promise(r => setTimeout(r, 5000)); }
  }
  if (!sessionToken) { logErr("giving up registration"); process.exit(1); }

  // 立即做一次上报
  await reportState();

  heartbeatTimer = setInterval(heartbeat, hbSec * 1000);
  reportTimer = setInterval(reportState, REPORT_SEC * 1000);
  log("shim running (heartbeat=%ds, report=%ds)", hbSec, REPORT_SEC);
}

process.on("SIGTERM", () => { clearInterval(heartbeatTimer); clearInterval(reportTimer); process.exit(0); });
process.on("SIGINT",  () => { clearInterval(heartbeatTimer); clearInterval(reportTimer); process.exit(0); });

main().catch(e => { logErr("fatal: %s", e.message); process.exit(1); });

