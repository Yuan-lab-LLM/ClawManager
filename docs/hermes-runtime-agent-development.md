# Hermes Runtime Image and Agent Development Guide

This guide is for Hermes image and agent developers. It explains how to build a ClawManager-managed Hermes runtime image on top of Webtop, and how the embedded Hermes agent should integrate with ClawManager Agent Control Plane so Hermes can behave like OpenClaw: report live runtime status, report health and system metrics, sync skill inventory, upload skill packages, and poll runtime commands.

## Goals

A Hermes image must satisfy two layers of requirements:

- Desktop access layer: keep the Webtop/KasmVNC runtime model. ClawManager accesses the desktop through the instance Service on port `3001`.
- Runtime agent layer: run a long-lived Hermes agent inside the image. The agent registers with ClawManager, sends heartbeats, reports state, syncs skills, and executes platform-issued commands.

Current Hermes runtime defaults in ClawManager:

- Port: `3001`
- Persistent directory: `/config/.hermes`
- Default title: `Hermes Runtime`
- Proxy path: ClawManager rewrites `SUBFOLDER` to `/api/v1/instances/{instance_id}/proxy/` when the instance is created.

Do not change the port or persistent directory in the image. If the image uses a different port or mount path, instance proxying, PVC mounting, and user data persistence will no longer match ClawManager expectations.

## Image Build Requirements

Use a LinuxServer Webtop image as the base image, for example:

```dockerfile
FROM lscr.io/linuxserver/webtop:ubuntu-xfce

USER root

# 1. Install Hermes runtime dependencies.
# Keep this as an example. The Hermes project should own the real install steps.
# RUN apt-get update && apt-get install -y ... && rm -rf /var/lib/apt/lists/*

# 2. Install Hermes itself.
# COPY hermes /opt/hermes

# 3. Install the ClawManager Hermes agent.
COPY hermes-agent /usr/local/bin/hermes-agent
RUN chmod +x /usr/local/bin/hermes-agent

# 4. Register an s6 longrun service so the agent starts with the Webtop container.
COPY root/ /

ENV TITLE="Hermes Runtime"
ENV SUBFOLDER="/"

EXPOSE 3001
```

Webtop uses s6 overlay for process supervision. The Hermes agent can run as a longrun service:

```text
root/
  etc/
    s6-overlay/
      s6-rc.d/
        hermes-agent/
          type
          run
        user/
          contents.d/
            hermes-agent
```

`type`:

```text
longrun
```

`run`:

```bash
#!/usr/bin/with-contenv bash
set -euo pipefail

if [ "${CLAWMANAGER_AGENT_ENABLED:-false}" != "true" ]; then
  echo "ClawManager Hermes agent disabled"
  sleep infinity
fi

exec /usr/local/bin/hermes-agent
```

The Hermes agent must not bind to `3001`. Port `3001` belongs to the Webtop desktop entrypoint. The agent only needs outbound HTTP access to ClawManager.

## Environment Variables Injected by ClawManager

The Hermes image must read configuration from environment variables. Do not hardcode ClawManager URLs, instance IDs, tokens, or persistent paths into the image.

Base Webtop variables:

| Variable | Description |
| --- | --- |
| `TITLE` | Desktop title. Hermes defaults to `Hermes Runtime`. |
| `SUBFOLDER` | Reverse proxy subpath. ClawManager rewrites it at runtime. |
| `HTTP_PROXY` / `HTTPS_PROXY` | Injected when platform egress proxy is enabled. |
| `NO_PROXY` | Platform-internal services and localhost are added automatically. |

Agent Control Plane variables:

| Variable | Description |
| --- | --- |
| `CLAWMANAGER_AGENT_ENABLED` | Start the Hermes agent when set to `true`. |
| `CLAWMANAGER_AGENT_BASE_URL` | ClawManager API base URL, without the `/api/v1/agent` suffix. |
| `CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN` | One-time bootstrap token used for initial registration. |
| `CLAWMANAGER_AGENT_INSTANCE_ID` | Current ClawManager instance ID. |
| `CLAWMANAGER_AGENT_PROTOCOL_VERSION` | Current protocol version, `v1`. |
| `CLAWMANAGER_AGENT_PERSISTENT_DIR` | Persistent directory. Hermes uses `/config/.hermes`. |
| `CLAWMANAGER_AGENT_DISK_LIMIT_BYTES` | Instance disk quota in bytes. |

Runtime resource bootstrap variables:

| Variable | Description |
| --- | --- |
| `CLAWMANAGER_HERMES_CHANNELS_JSON` | Channel configuration injected at instance creation time. |
| `CLAWMANAGER_HERMES_SKILLS_JSON` | Skill configuration injected at instance creation time. |
| `CLAWMANAGER_HERMES_BOOTSTRAP_MANIFEST_JSON` | Manifest for the current bootstrap payload. |
| `CLAWMANAGER_RUNTIME_CHANNELS_JSON` | Generic runtime alias for channel configuration. |
| `CLAWMANAGER_RUNTIME_SKILLS_JSON` | Generic runtime alias for skill configuration. |
| `CLAWMANAGER_RUNTIME_BOOTSTRAP_MANIFEST_JSON` | Generic runtime alias for the bootstrap manifest. |

For compatibility with the existing OpenClaw resource center, ClawManager also keeps the original `CLAWMANAGER_OPENCLAW_*` variables. Hermes agents should prefer `CLAWMANAGER_HERMES_*`, then fall back to `CLAWMANAGER_RUNTIME_*`, and finally to `CLAWMANAGER_OPENCLAW_*`.

Platform-side note: some Agent Control Plane payload fields still use historical OpenClaw names. Until those fields are renamed into generic runtime fields, Hermes agents should reuse compatible fields such as `openclaw_status`, `openclaw_pid`, and `openclaw_version` to describe Hermes runtime state.

## Channel and Skill Bootstrap Consumption

Hermes agents must handle two kinds of injection:

- Runtime bootstrap injection: channels, configuration skills, session templates, agents, scheduled tasks, and related resources selected at instance creation time are injected through environment variables.
- Platform skill installation: reusable platform skills selected at instance creation time are first attached to the instance. ClawManager then sends an `install_skill` command. The agent must download and install the skill package.

### Read Order

At startup, read bootstrap payloads in this priority order. Use the first non-empty value:

| Resource | Preferred variable | Fallback variables |
| --- | --- | --- |
| Manifest | `CLAWMANAGER_HERMES_BOOTSTRAP_MANIFEST_JSON` | `CLAWMANAGER_RUNTIME_BOOTSTRAP_MANIFEST_JSON`, `CLAWMANAGER_OPENCLAW_BOOTSTRAP_MANIFEST_JSON` |
| Channels | `CLAWMANAGER_HERMES_CHANNELS_JSON` | `CLAWMANAGER_RUNTIME_CHANNELS_JSON`, `CLAWMANAGER_OPENCLAW_CHANNELS_JSON` |
| Config Skills | `CLAWMANAGER_HERMES_SKILLS_JSON` | `CLAWMANAGER_RUNTIME_SKILLS_JSON`, `CLAWMANAGER_OPENCLAW_SKILLS_JSON` |
| Session Templates | `CLAWMANAGER_HERMES_SESSION_TEMPLATES_JSON` | `CLAWMANAGER_RUNTIME_SESSION_TEMPLATES_JSON`, `CLAWMANAGER_OPENCLAW_SESSION_TEMPLATES_JSON` |
| Agents | `CLAWMANAGER_HERMES_AGENTS_JSON` | `CLAWMANAGER_RUNTIME_AGENTS_JSON`, `CLAWMANAGER_OPENCLAW_AGENTS_JSON` |
| Scheduled Tasks | `CLAWMANAGER_HERMES_SCHEDULED_TASKS_JSON` | `CLAWMANAGER_RUNTIME_SCHEDULED_TASKS_JSON`, `CLAWMANAGER_OPENCLAW_SCHEDULED_TASKS_JSON` |

If a variable is missing or empty, treat it as an empty config. Do not fail agent startup for missing optional bootstrap payloads. If a variable exists but contains invalid JSON, log a clear error and report `health.bootstrap_config` or `health.config_loader` as `error` in the next state report.

Recommended local bootstrap state:

```text
/config/.hermes/hermes-agent/bootstrap/
  manifest.json
  channels.json
  skills.json
  applied-state.json
```

Do not print channel tokens, secrets, webhooks, bootstrap tokens, session tokens, or AI Gateway API keys in logs.

### Channel Injection

`CLAWMANAGER_HERMES_CHANNELS_JSON` is a JSON object keyed by resource key. Example:

```json
{
  "feishu": {
    "enabled": true,
    "domain": "feishu",
    "defaultAccount": "main",
    "accounts": {
      "main": {
        "appId": "cli_xxx",
        "appSecret": "secret",
        "enabled": true
      }
    },
    "requireMention": true
  },
  "telegram": {
    "enabled": true,
    "botToken": "123456:xxx",
    "dmPolicy": "open",
    "allowFrom": ["*"]
  }
}
```

Hermes agent behavior:

1. Use the top-level key as the channel ID, for example `feishu`, `telegram`, `slack`, or `dingtalk-connector`.
2. Skip channels with `enabled=false`, but keep their config on disk so future updates can re-enable them.
3. Convert channel config into Hermes-native notification or messaging configuration. A reasonable default path is `/config/.hermes/channels.json`, unless Hermes has a native config location.
4. Preserve unknown fields so future ClawManager extensions are not lost.
5. If Hermes does not support a channel type yet, mark that channel as unsupported in `health.channels` and keep registration and heartbeats running.

### Config Skill Injection

`CLAWMANAGER_HERMES_SKILLS_JSON` is a list of configuration resources. It is not a zip package. It is used to inject resource-center configuration skills into the runtime at instance creation time. Example:

```json
{
  "schemaVersion": 1,
  "items": [
    {
      "id": 5,
      "type": "skill",
      "key": "support-bot",
      "name": "Support Bot",
      "version": 1,
      "tags": ["skill"],
      "content": {
        "schemaVersion": 1,
        "kind": "skill",
        "format": "skill/custom@v1",
        "dependsOn": [],
        "config": {
          "prompt": "help"
        }
      }
    }
  ]
}
```

Hermes agent behavior:

1. Iterate over `items` and use `key` as the stable skill identifier.
2. Read `content.config` and translate it into executable Hermes skill configuration.
3. If Hermes stores skills as directories, write the generated config to `/config/.hermes/skills/{key}/skill.json`. Keep the raw `content` as well for debugging.
4. Calculate `content_md5` for the written skill directory and include it in the next `skills/inventory` report.
5. For skills generated from bootstrap config, inventory `source` should be `injected_by_clawmanager` or `bootstrap_config`. If `source` is `injected_by_clawmanager` and `skill_id` uses a platform external ID, use the format `skill-{id}`, for example `skill-5`.

Config skills and platform skill package installation are separate paths:

- `CLAWMANAGER_HERMES_SKILLS_JSON`: read and apply during startup. Do not wait for a command.
- `install_skill` command: download and install a platform-uploaded zip skill package at runtime.

### Bootstrap Manifest

`CLAWMANAGER_HERMES_BOOTSTRAP_MANIFEST_JSON` describes the payloads injected for the current bootstrap. Example:

```json
{
  "schemaVersion": 1,
  "mode": "manual",
  "resources": [
    { "id": 5, "type": "skill", "key": "support-bot", "name": "Support Bot", "version": 1 }
  ],
  "payloads": [
    { "env": "CLAWMANAGER_HERMES_CHANNELS_JSON", "count": 1 },
    { "env": "CLAWMANAGER_HERMES_SKILLS_JSON", "count": 1 }
  ]
}
```

The agent can use the manifest as an idempotency key. If the manifest hash has not changed, skip reapplying the same bootstrap payload. If it changes, reapply channel and config skill payloads, then send one state report and one full skill inventory report.

### Skills Selected During Instance Creation

When a user selects existing platform skills while creating a Hermes instance, ClawManager attaches those skills to the instance and creates `install_skill` commands. The Hermes agent must implement this command. Otherwise the UI selection only exists in platform records and the skill will not be installed inside the instance.

Example `install_skill` payload:

```json
{
  "skill_id": "skill-12",
  "skill_version": "skill-version-34",
  "target_name": "weather-tool",
  "content_md5": "d41d8cd98f00b204e9800998ecf8427e"
}
```

Processing steps:

1. Download the version package:

```http
GET {CLAWMANAGER_AGENT_BASE_URL}/api/v1/agent/skills/versions/{skill_version}/download
Authorization: Bearer {session_token}
```

2. Validate the zip. Reject absolute paths, `../`, multiple top-level directories, and any path that would extract outside `/config/.hermes/skills`.
3. Extract to `/config/.hermes/skills/{target_name}`. Prefer extracting into a temp directory and then atomically replacing the target directory.
4. Recalculate directory `content_md5` and compare it with the command payload. Finish the command as `failed` if it does not match.
5. Send one `skills/inventory` report. For the installed skill, set `source` to `injected_by_clawmanager` and `skill_id` to the command payload `skill_id`.
6. Finish the command with `install_path`, `skill_id`, `skill_version`, and `content_md5` in `result`.

## Agent Lifecycle

Hermes agent startup flow:

1. Read `CLAWMANAGER_AGENT_*` environment variables.
2. Read and apply runtime bootstrap payloads if present.
3. If no local session token is available, register with the bootstrap token.
4. Store the returned session token in `/config/.hermes/hermes-agent/session.json`.
5. Send heartbeats using the interval returned by the server.
6. Poll commands using the command poll interval returned by the server. If heartbeat returns `has_pending_command=true`, poll once immediately.
7. Periodically send full state reports and skill inventory reports.
8. If the session token expires or an API returns HTTP 401, register again with the bootstrap token.

Recommended local agent state directory:

```text
/config/.hermes/hermes-agent/
  session.json
  state.json
  logs/
  cache/
  bootstrap/
```

## Registration

Request:

```http
POST {CLAWMANAGER_AGENT_BASE_URL}/api/v1/agent/register
Authorization: Bearer {CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN}
Content-Type: application/json
```

Body example:

```json
{
  "instance_id": 123,
  "agent_id": "hermes-123-main",
  "agent_version": "0.1.0",
  "protocol_version": "v1",
  "capabilities": [
    "runtime.status",
    "runtime.health",
    "metrics.report",
    "skills.inventory",
    "skills.upload",
    "commands.poll"
  ],
  "host_info": {
    "runtime": "hermes",
    "desktop_base": "webtop",
    "persistent_dir": "/config/.hermes",
    "port": 3001,
    "arch": "amd64"
  }
}
```

The response field `data.session_token` is the token for subsequent Agent API calls. Cache it locally, but never write it to logs.

## Heartbeat

Request:

```http
POST {CLAWMANAGER_AGENT_BASE_URL}/api/v1/agent/heartbeat
Authorization: Bearer {session_token}
Content-Type: application/json
```

Body example:

```json
{
  "agent_id": "hermes-123-main",
  "timestamp": "2026-04-27T14:30:00Z",
  "openclaw_status": "running",
  "summary": {
    "runtime": "hermes",
    "hermes_status": "running",
    "hermes_pid": 245,
    "skill_count": 8,
    "active_skill_count": 8,
    "disk_used_bytes": 2147483648,
    "disk_limit_bytes": 10737418240
  }
}
```

Compatibility requirements:

- `openclaw_status` is still the platform compatibility field. Fill it with the Hermes main process status.
- Recommended status values: `starting`, `running`, `stopped`, `error`, `unknown`.
- Default heartbeat interval is roughly 15 seconds, but use `heartbeat_interval_seconds` from the registration response.
- ClawManager considers the agent online when heartbeat is received within 45 seconds, stale between 45 and 120 seconds, and offline after 120 seconds.

## Full State Report

Heartbeat is the lightweight online signal. Complete runtime status, system metrics, and health information are reported through state reports.

Request:

```http
POST {CLAWMANAGER_AGENT_BASE_URL}/api/v1/agent/state/report
Authorization: Bearer {session_token}
Content-Type: application/json
```

Body example:

```json
{
  "agent_id": "hermes-123-main",
  "reported_at": "2026-04-27T14:30:00Z",
  "runtime": {
    "openclaw_status": "running",
    "openclaw_pid": 245,
    "openclaw_version": "hermes-0.4.0"
  },
  "system_info": {
    "runtime": "hermes",
    "os": "ubuntu",
    "desktop_base": "webtop",
    "sampled_at": "2026-04-27T14:30:00Z",
    "cpu": {
      "cores": 2,
      "load": {
        "1m": 0.64,
        "5m": 0.52,
        "15m": 0.40
      }
    },
    "memory": {
      "mem_total_bytes": 4294967296,
      "mem_available_bytes": 2147483648
    },
    "disk": {
      "mount_path": "/config/.hermes",
      "root_total_bytes": 10737418240,
      "root_free_bytes": 8589934592
    },
    "network": {
      "interfaces": [
        {
          "name": "eth0",
          "status": "up",
          "addresses": ["10.42.0.12"],
          "rx_bytes": 123456789,
          "tx_bytes": 98765432
        }
      ]
    }
  },
  "health": {
    "hermes_process": "ok",
    "desktop": "ok",
    "agent": "ok",
    "metrics_collector": "ok",
    "bootstrap_config": "ok",
    "channels": "ok",
    "metrics_sample_interval_seconds": 5,
    "last_skill_scan_at": "2026-04-27T14:29:30Z"
  }
}
```

Reporting guidance:

- Heartbeat: send at the server-provided interval.
- State report: send once immediately after startup, then every 5 to 10 seconds when possible.
- Send an extra state report after Hermes main process state changes, skill inventory changes, bootstrap config changes, or command completion.

## Metrics Reporting Contract

ClawManager does not provide a separate CPU, memory, disk, or network metrics endpoint. Hermes agent must include every sample in the `system_info` field of the state report. The backend stores this JSON as-is, and the instance detail page reads the fields below to render recent trends.

### Required Fields

| Path | Type | Unit | Description |
| --- | --- | --- | --- |
| `system_info.sampled_at` | string | ISO 8601 UTC | Agent sampling time. |
| `system_info.cpu.cores` | number | cores | CPU cores available to the container. |
| `system_info.cpu.load.1m` | number | load average | 1-minute load average. |
| `system_info.cpu.load.5m` | number | load average | 5-minute load average. |
| `system_info.cpu.load.15m` | number | load average | 15-minute load average. |
| `system_info.memory.mem_total_bytes` | number | bytes | Container memory limit or system total memory. |
| `system_info.memory.mem_available_bytes` | number | bytes | Currently available memory. |
| `system_info.disk.root_total_bytes` | number | bytes | Total capacity of the filesystem containing `/config/.hermes`. |
| `system_info.disk.root_free_bytes` | number | bytes | Free capacity of the filesystem containing `/config/.hermes`. |
| `system_info.network.interfaces[].name` | string | none | Network interface name, for example `eth0`. |
| `system_info.network.interfaces[].status` | string | none | Suggested values: `up` or `down`. |
| `system_info.network.interfaces[].rx_bytes` | number | bytes | Monotonic received byte counter. |
| `system_info.network.interfaces[].tx_bytes` | number | bytes | Monotonic transmitted byte counter. |

The frontend calculates CPU percentage as `load.1m / cores * 100`, capped to 0..100. The agent does not need to report `cpu_percent`.

The frontend calculates memory percentage as `(mem_total_bytes - mem_available_bytes) / mem_total_bytes * 100`, and disk percentage as `(root_total_bytes - root_free_bytes) / root_total_bytes * 100`.

Network rates are calculated by the frontend from adjacent `rx_bytes` and `tx_bytes` samples. Report monotonic counters, not instantaneous rates. If counters reset after a container restart, the frontend will resume calculation from the next valid sample.

### Sampling Sources

- CPU load: read the first three values from `/proc/loadavg`.
- CPU cores: prefer cgroup quota. For cgroup v2, read `/sys/fs/cgroup/cpu.max`. If there is no quota, fall back to `/proc/cpuinfo` or the language runtime.
- Memory: prefer cgroup memory limit and current usage. For cgroup v2, read `/sys/fs/cgroup/memory.max` and `/sys/fs/cgroup/memory.current`. Compute `mem_available_bytes = memory.max - memory.current`, floored at 0. If no cgroup limit exists, use `/proc/meminfo` `MemTotal` and `MemAvailable`.
- Disk: call `statvfs` on `/config/.hermes`. Keep the field names `root_total_bytes` and `root_free_bytes`, but interpret them as the filesystem containing the Hermes persistent directory.
- Network: read `/proc/net/dev`. Exclude `lo` by default and keep business interfaces such as `eth0`.

### Reporting Frequency

- Send one state report with complete `system_info` immediately after successful startup.
- During normal operation, sample and report every 5 seconds to match the instance detail page polling cadence.
- If overhead is a concern, 10 seconds is acceptable. Do not sample faster than every 2 seconds.
- When receiving `collect_system_info` or `health_check`, sample immediately, send a state report, then finish the command.

### Command Result Guidance

`collect_system_info` finish `result` can reuse the same snapshot:

```json
{
  "agent_id": "hermes-123-main",
  "status": "succeeded",
  "finished_at": "2026-04-27T14:31:05Z",
  "result": {
    "sampled_at": "2026-04-27T14:31:05Z",
    "system_info": {
      "cpu": {
        "cores": 2,
        "load": {
          "1m": 0.70,
          "5m": 0.55,
          "15m": 0.42
        }
      },
      "memory": {
        "mem_total_bytes": 4294967296,
        "mem_available_bytes": 2013265920
      },
      "disk": {
        "mount_path": "/config/.hermes",
        "root_total_bytes": 10737418240,
        "root_free_bytes": 8589934592
      },
      "network": {
        "interfaces": [
          {
            "name": "eth0",
            "status": "up",
            "rx_bytes": 124000000,
            "tx_bytes": 99000000
          }
        ]
      }
    }
  },
  "error_message": ""
}
```

`health_check` finish `result` should include `health` and `system_info.sampled_at`. If sampling fails, still send available fields in the state report and set `health.metrics_collector` to `error` with a short `health.metrics_error` message.

### Metrics Acceptance

1. Hermes agent sends two consecutive state reports roughly 5 seconds apart.
2. `GET /api/v1/instances/{instance_id}/runtime` returns `data.runtime.system_info.cpu`, `memory`, `disk`, and `network`.
3. The ClawManager instance detail page starts showing CPU, Memory, Disk, and Network metrics within 10 seconds.
4. Creating network traffic or disk writes changes the corresponding trend in later samples.

## Skill Inventory Sync

Hermes agent must discover skills installed inside the instance and report inventory to ClawManager.

Recommended skill root:

```text
/config/.hermes/skills
```

Optional environment override:

```text
HERMES_SKILL_DIRS=/config/.hermes/skills
```

Each skill should be managed as a directory. For every skill, calculate `content_md5` and report it:

```http
POST {CLAWMANAGER_AGENT_BASE_URL}/api/v1/agent/skills/inventory
Authorization: Bearer {session_token}
Content-Type: application/json
```

Body example:

```json
{
  "agent_id": "hermes-123-main",
  "reported_at": "2026-04-27T14:30:00Z",
  "mode": "full",
  "trigger": "startup",
  "skills": [
    {
      "skill_id": "hermes-weather",
      "skill_version": "1.2.0",
      "identifier": "hermes-weather",
      "install_path": "/config/.hermes/skills/hermes-weather",
      "content_md5": "d41d8cd98f00b204e9800998ecf8427e",
      "source": "discovered_in_instance",
      "type": "hermes-skill",
      "size_bytes": 20480,
      "file_count": 12,
      "metadata": {
        "runtime": "hermes",
        "manifest": "skill.json"
      }
    }
  ]
}
```

`mode` semantics:

- `full`: complete inventory. ClawManager marks instance skills missing from this report as removed.
- `incremental`: partial update. Only skills in this report are updated.

Recommended behavior:

- Send one `full` inventory after startup.
- Use file watching or periodic scanning for later `incremental` updates.
- When the platform sends `sync_skill_inventory` or `refresh_skill_inventory`, run a `full` scan.

### content_md5 Calculation

ClawManager `content_md5` is a skill directory content fingerprint, not a zip file MD5. The full algorithm is defined in [Skill Content MD5 Calculation Spec](skill-content-md5-spec.md).

The most common mistake is top-level directory handling:

- During inventory, calculate against the contents of `/config/.hermes/skills/{skill_name}`.
- During upload, the zip must contain one top-level directory named `{skill_name}/`.
- ClawManager strips the zip top-level `{skill_name}/` once before validation.
- Do not strip internal directories such as `src/`, `lib/`, or `dist/`.

For example, if the local file is `/config/.hermes/skills/weather/src/main.py`, the relative path used for MD5 must be `src/main.py`, not `weather/src/main.py` and not `main.py`.

The agent must use the same directory content and the same algorithm for inventory and `collect_skill_package` upload. Otherwise ClawManager will return `skill package md5 mismatch`.

## Skill Package Upload

When ClawManager finds a skill blob without object content, it sends a `collect_skill_package` command. Hermes agent should zip the corresponding skill directory and upload it.

Request:

```http
POST {CLAWMANAGER_AGENT_BASE_URL}/api/v1/agent/skills/upload
Authorization: Bearer {session_token}
Content-Type: multipart/form-data
```

Form fields:

| Field | Description |
| --- | --- |
| `file` | Zip package. It must contain exactly one top-level skill directory. |
| `agent_id` | Current agent ID. |
| `skill_id` | Skill ID from inventory. |
| `skill_version` | Skill version from inventory. |
| `identifier` | Skill name or key. |
| `content_md5` | Directory fingerprint reported in inventory. |
| `source` | Usually `discovered_in_instance` or `injected_by_clawmanager`. |

Zip structure example:

```text
hermes-weather/
  skill.json
  main.py
  README.md
```

Do not upload multiple top-level directories. Do not put loose files at the zip root. ClawManager rejects both formats.

## Command Polling and Execution

Hermes agent polls commands with the session token:

```http
GET {CLAWMANAGER_AGENT_BASE_URL}/api/v1/agent/commands/next
Authorization: Bearer {session_token}
```

If `data.command` is `null`, no command is pending. If a command exists, the agent must:

1. Call the start endpoint.
2. Execute the command.
3. Call the finish endpoint with the result.
4. Always finish failed commands with `status=failed`.

Start:

```http
POST {CLAWMANAGER_AGENT_BASE_URL}/api/v1/agent/commands/{id}/start
Authorization: Bearer {session_token}
Content-Type: application/json
```

```json
{
  "agent_id": "hermes-123-main",
  "started_at": "2026-04-27T14:31:00Z"
}
```

Finish:

```http
POST {CLAWMANAGER_AGENT_BASE_URL}/api/v1/agent/commands/{id}/finish
Authorization: Bearer {session_token}
Content-Type: application/json
```

```json
{
  "agent_id": "hermes-123-main",
  "status": "succeeded",
  "finished_at": "2026-04-27T14:31:05Z",
  "result": {
    "message": "skill inventory refreshed",
    "skill_count": 8
  },
  "error_message": ""
}
```

Current platform command types:

- `collect_system_info`
- `health_check`
- `sync_skill_inventory`
- `refresh_skill_inventory`
- `collect_skill_package`
- `install_skill`
- `update_skill`
- `uninstall_skill`
- `remove_skill`
- `disable_skill`
- `quarantine_skill`
- `handle_skill_risk`
- `start_openclaw`
- `stop_openclaw`
- `restart_openclaw`
- `apply_config_revision`
- `reload_config`

Minimum Hermes implementation:

- `collect_system_info`
- `health_check`
- `sync_skill_inventory`
- `refresh_skill_inventory`
- `collect_skill_package`
- `install_skill`

Commands containing `openclaw` are currently compatibility names. Hermes may ignore `start_openclaw`, `stop_openclaw`, and `restart_openclaw` until Hermes-specific or generic runtime commands are added.

## Skill Installation and Version Download

If Hermes supports platform-managed skill installation, command payloads may include a skill version identifier. The agent can download the zip package through:

```http
GET {CLAWMANAGER_AGENT_BASE_URL}/api/v1/agent/skills/versions/{external_version_id}/download
Authorization: Bearer {session_token}
```

After downloading:

1. Validate the zip path boundaries.
2. Extract it under `/config/.hermes/skills`.
3. Recalculate `content_md5`.
4. Send `skills/inventory`.
5. Finish the command with install path, skill ID, version, and `content_md5`.

## Local Development

Use the following variables to run the agent locally:

```bash
export CLAWMANAGER_AGENT_ENABLED=true
export CLAWMANAGER_AGENT_BASE_URL=http://127.0.0.1:8080
export CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN=agt_boot_xxx
export CLAWMANAGER_AGENT_INSTANCE_ID=123
export CLAWMANAGER_AGENT_PROTOCOL_VERSION=v1
export CLAWMANAGER_AGENT_PERSISTENT_DIR=/config/.hermes
export CLAWMANAGER_AGENT_DISK_LIMIT_BYTES=10737418240
```

For local bootstrap testing, also set:

```bash
export CLAWMANAGER_HERMES_CHANNELS_JSON='{}'
export CLAWMANAGER_HERMES_SKILLS_JSON='{"schemaVersion":1,"items":[]}'
export CLAWMANAGER_HERMES_BOOTSTRAP_MANIFEST_JSON='{"schemaVersion":1,"mode":"manual","payloads":[]}'
```

Never commit real tokens, channel secrets, Gateway API keys, or downloaded session tokens into images, repositories, or logs. At most, log a short token prefix and suffix for debugging.

## Acceptance Checklist

Before delivering a Hermes image, verify:

- The Webtop desktop is reachable through the ClawManager instance proxy.
- `/config/.hermes` is mounted and persists across restarts.
- The agent registers and starts heartbeat within 30 seconds.
- The instance detail page shows agent online, runtime running, and an updated last report time.
- CPU, memory, disk, and network metrics are visible and refresh continuously.
- Channel bootstrap payloads are applied or clearly reported as unsupported.
- Config skill bootstrap payloads are applied and included in inventory.
- Skill inventory syncs after changes under the Hermes skill directory.
- For discovered skills without stored object content, `collect_skill_package` causes the agent to upload a valid zip package.
- `content_md5` in inventory and package upload match the ClawManager specification.
- Command execution calls start and finish, including clear `error_message` on failure.
- Network interruption, ClawManager restart, or session expiration causes retry and re-registration.

## Platform-Side Companion Checklist

ClawManager must keep the following capabilities for Hermes to work end to end:

- Inject `CLAWMANAGER_AGENT_*` variables for `hermes` during instance creation and start.
- Allow `hermes` instances to register with the Agent Control Plane.
- Inject `CLAWMANAGER_LLM_*` and OpenAI-compatible variables so Hermes can access models through ClawManager AI Gateway.
- Inject Hermes and generic runtime bootstrap variables for channels, skills, and related resources.
- Mount persistent storage at `/config/.hermes`.
- Support `.hermes` import and export.
- Keep compatibility fields such as `openclaw_status`, `openclaw_pid`, and `openclaw_version` until generic runtime fields are introduced.
- Add Hermes-specific runtime control commands, or generic `start_runtime`, `stop_runtime`, and `restart_runtime`, if runtime process control becomes required.
