# T8I E2E Rerun After Runtime Body Fix

Timestamp: 2026-05-02 Asia/Shanghai

T8I EVIDENCE RESULT: SATISFIED FOR COMMANDER REVIEW

## Evidence File Path

`specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260502-t8i-e2e-rerun-after-runtime-body-fix.md`

## Environment Preflight

This was an evidence-only T8I rerun against the current local k3d environment. It used authorized route cookies for `/proxy/` and `/control-ui/`; no token-bearing browser URL was requested or recorded, and no token/cookie value or credential is recorded in this evidence.

Required mem0 search was run first with query:

`ClawManager gtmanager-gtclaw-m1-runtime-localization T8I runtime body localization favicon evidence 2026-05-02`

Relevant mem0 results confirmed the prior T8 failure context, the T8I no-Close/no-`passes:true` gate, and the prior control-ui body/fallback blockers.

Pre-write dirty worktree status was captured before this evidence file existed. The dirty source/docs/longterm/spec tree was already present before this evidence worker; this worker did not modify those files.

```text
 M .specify/memory/constitution.md
 M AGENTS.md
 M README.md
 M backend/cmd/server/main.go
 M backend/internal/handlers/instance_handler.go
 M backend/internal/services/instance_access_service.go
 M backend/internal/services/instance_access_service_test.go
 M backend/internal/services/instance_proxy_service.go
 M backend/internal/services/k8s/client.go
 M backend/internal/services/k8s/service_service.go
 M backend/internal/services/security_scan_service.go
 M docs/k3s-local-setup.md
 M docs/manual-skill-import.md
 M docs/manual-skill-import_en.md
 M frontend/index.html
 M frontend/src/components/AdminLayout.tsx
 M frontend/src/components/InstanceAccess.tsx
 M frontend/src/components/OpenClawDesktopOverlay.tsx
 M frontend/src/components/UserLayout.tsx
 M frontend/src/contexts/I18nContext.tsx
 M frontend/src/hooks/useInstanceDesktopAccess.ts
 M frontend/src/lib/i18n.ts
 M frontend/src/pages/admin/InstanceManagementPage.tsx
 M frontend/src/pages/admin/SystemSettingsPage.tsx
 M frontend/src/pages/instances/CreateInstancePage.tsx
 M frontend/src/pages/instances/InstanceDetailPage.tsx
 M frontend/src/pages/instances/InstancePortalPage.tsx
 M frontend/src/services/instanceService.ts
 M frontend/src/stores/authStore.ts
 M frontend/src/types/instance.ts
 M longterm/CHECKLIST.md
 M longterm/workspace/app_spec.md
 M longterm/workspace/init.sh
?? backend/AGENTS.md
?? backend/internal/handlers/instance_access_contract_test.go
?? backend/internal/services/instance_proxy_service_test.go
?? backend/internal/services/k8s/service_service_test.go
?? clawmanagerArm/
?? dist/
?? frontend/AGENTS.md
?? frontend/public/gtmanager-logo.png
?? specs/
```

The approved evidence file did not exist before the write (`test -e ...after-runtime-body-fix.md` returned exit `1`).

| Check | Observed |
| --- | --- |
| App health | `GET https://localhost:30443/healthz` -> HTTP `200`, body `ok` |
| App root | `GET https://localhost:30443/` -> HTTP `200`, title `GTManager`, SHA-256 `483830c45540eb45d97941ee89f3c97cded2c09a3f953c60153da4b7371d3cbf` |
| App deployment | `clawmanager-system/clawmanager-app`, ready `1/1` |
| App pod | `clawmanager-app-6c985497f5-2kdq8`, restart count `0` |
| App image | `clawmanager:backend-proxy-icon-rewrite-20260502231228` |
| App imageID | `sha256:4b787b460795c1fcd42f6b57716bb969bf7c9a671c66990026b5b5a916df911a` |
| No-cookie `/control-ui/` smoke | HTTP `400`, non-404 |
| No-cookie `/proxy/` smoke | HTTP `400`, non-404 |
| Runtime namespace | `clawmanager-user-1` |
| Runtime pod | `clawreef-3-gtclaw-t8-dev-20260501001159` |
| Runtime pod phase/ready | `Running`, `Ready=True`, restart count `0` |
| Runtime PodIP | `10.42.0.57` |
| Runtime service | `clawreef-3-gtclaw-t8-dev-20260501001159-svc` |
| Runtime Service ClusterIP | `10.43.115.105` |
| Runtime service ports | `http:3001->3001/TCP`, `control-18789:18789->18789/TCP` |
| Runtime image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029` |
| Runtime imageID | `sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45` |

Runtime repaired file hash gate matched the user-provided expected values:

| Runtime file | Size | SHA-256 |
| --- | ---: | --- |
| `index.html` | `3398` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| `assets/i18n-B06L7jQN.js` | `42617` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `assets/zh-CN-B26mMdbY.js` | `23255` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` |
| `assets/index-M4TNVXB3.js` | `707959` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` |

## App / Bundle Evidence

Temp frontend build ran under `/tmp/gtclaw-t8i-rerun-after-runtime-body-fix-20260502/frontend-build` from the current source copy using `npm ci && npm run build`.

| Temp build file | SHA-256 |
| --- | --- |
| `dist/index.html` | `483830c45540eb45d97941ee89f3c97cded2c09a3f953c60153da4b7371d3cbf` |
| `dist/assets/index-Cce3cBw3.js` | `dd22b40310e7e2af08ac3b16cbdf1d2488db7f13588f8317fdd8142029a213a3` |
| `dist/assets/index-Cv71uKAX.css` | `6f70bfb0b44b60282c12521397c7768c05e48313edd1d78cc0d015543053094e` |
| `dist/gtmanager-logo.png` | `0d738a0371f3fac37bd71ff21d0027bc46958d98e5f5ff1c3e2bc36e93f8e7c0` |

Browser-loaded deployed manager assets matched the temp build:

| Browser-loaded URL | Type | HTTP | SHA-256 |
| --- | --- | ---: | --- |
| `/assets/index-Cce3cBw3.js` | Script | `200` | `dd22b40310e7e2af08ac3b16cbdf1d2488db7f13588f8317fdd8142029a213a3` |
| `/assets/index-Cv71uKAX.css` | Stylesheet | `200` | `6f70bfb0b44b60282c12521397c7768c05e48313edd1d78cc0d015543053094e` |
| `/gtmanager-logo.png` | Image | `200` | `0d738a0371f3fac37bd71ff21d0027bc46958d98e5f5ff1c3e2bc36e93f8e7c0` |

`cd frontend && npm run lint` exited `1` with known lint debt: `126 problems (107 errors, 19 warnings)`. This is recorded as prerequisite evidence only and is not a Close waiver.

## Wrapper Route Matrix

All wrapper checks used a clean temporary Chrome profile with `zh` manager locale and API-authenticated localStorage. DOM body text was collected from the browser after route rendering, not from source scans.

| Route | Required criterion | Result | DOM evidence |
| --- | --- | --- | --- |
| `/instances/new` | Enter type-selection state; OpenClaw runtime display shows GTClaw / GTClaw desktop | met | After filling only the local name field and clicking `下一步`, DOM showed `第 2 步，共 3 步：选择类型` and `GTClaw 桌面` |
| `/portal` | Show GTClaw control-ui entry for instance 3 | met | DOM showed `打开 GTClaw 控制台` for `gtclaw-t8-dev-20260501001159` |
| `/openclaw-configs` | Show GTClaw resource/config wording | met | DOM showed `GTClaw 资源管理` |
| `/admin/settings` | Runtime image/settings surface shows GTClaw where user-facing | met | DOM showed `实例类型` and `GTClaw 桌面`; technical image fields remained separate |
| `/admin/instances` | User-facing type label shows GTClaw; raw technical `openclaw` may remain | met | DOM showed filter/row `GTClaw 桌面` and technical row text `openclaw latest` |
| `/instances/3` | Access/runtime detail shows GTClaw control-ui entry | met | DOM showed `打开 GTClaw 控制台`; technical `OpenClaw 工作区` and `.openclaw` copy remained |
| `/admin/ai-audit?trace=trc_651816fecfe89903` | Deterministic trace/detail/execution-flow runtime-facing state uses GTClaw where applicable | met | DOM showed execution-flow copy `把一次 GTClaw 问答中的模型调用...串起来查看` |
| `/admin/ai-gateway` | GTManager manager brand remains; no ClawManager wrapper wording | met | DOM showed `GTManager`; `ClawManager` wrapper wording was absent |

Manager route browser metadata:

| Route group | Title | HTML lang | `GTClaw` observed | `ClawManager` wrapper wording observed |
| --- | --- | --- | --- | --- |
| Wrapper matrix | `GTManager` | `zh` | yes on runtime-facing routes | no |
| `/admin/ai-gateway` | `GTManager` | `zh` | not required | no |

## Access Contract Evidence

Authentication was performed without printing credentials or bearer values. Access response bodies were parsed only for status, route, mode, target port, route cookie metadata, token field presence, and expiry field presence. Cookie values were not recorded.

### Desktop Access

| Field | Observed |
| --- | --- |
| Request | `POST /api/v1/instances/3/access?mode=desktop` |
| HTTP status | `200` |
| `access_mode` | `desktop` |
| `access_url` | `/api/v1/instances/3/proxy/` |
| `proxy_url` | `/api/v1/instances/3/proxy/` |
| `target_port` | `3001` |
| Token field present | yes, value redacted |
| `expires_at` present | yes |
| Route cookie name | `instance_access_3` |
| Route cookie path | `/api/v1/instances/3/proxy` |
| Cookie flags | `HttpOnly=yes`, `Secure=yes`, `Max-Age=yes`; `SameSite` absent in raw header |

### Control-UI Access

| Field | Observed |
| --- | --- |
| Request | `POST /api/v1/instances/3/access?mode=control-ui` |
| HTTP status | `200` |
| `access_mode` | `control-ui` |
| `access_url` | `/api/v1/instances/3/control-ui/` |
| `proxy_url` | `/api/v1/instances/3/control-ui/` |
| `target_port` | `18789` |
| Token field present | yes, value redacted |
| `expires_at` present | yes |
| Route cookie name | `instance_control_ui_access_3` |
| Route cookie path | `/api/v1/instances/3/control-ui` |
| Cookie flags | `HttpOnly=yes`, `Secure=yes`, `Max-Age=yes`; `SameSite` absent in raw header |

Temporary browser cookie inspection showed both route cookies with matching paths and secure/HTTP-only metadata. No requested browser URL contained `token=` (`token_url_detected=false`, `requested_url_count=146`).

## Desktop Regression Evidence

Cookie-jar route probe:

| Route | HTTP | SHA-256 | Static title | GTClaw | Desktop marker in initial HTML | 404 marker |
| --- | ---: | --- | --- | --- | --- | --- |
| `/api/v1/instances/3/proxy/` | `200` | `12c54f88575ccf5c69b79bdc37900d553713499bebf7765f0f4d7f7940f18d1c` | empty | no | no | no |

Browser-rendered desktop route:

| Route | Final path | Title | HTML lang | GTClaw | Desktop title |
| --- | --- | --- | --- | --- | --- |
| `/api/v1/instances/3/proxy/` | `/api/v1/instances/3/proxy/` | `ClawManager Desktop` | `en` | no | yes |

Browser-loaded desktop assets remained under `/proxy/`:

| Browser-loaded URL | Type | HTTP | SHA-256 |
| --- | --- | ---: | --- |
| `/api/v1/instances/3/proxy/assets/index-C-ZxXfwb.css` | Stylesheet | `200` | `f49fbf28a840a0145d3f02c6baaf7bd6e5faaa0e0c8186313d38aee9d93b5051` |
| `/api/v1/instances/3/proxy/assets/index-luFV63l_.js` | Script | `200` | `26e24061ea2c1bebd9b2ed3e64d68a903a2923bd2ab5139b38cefcd75d81c4cd` |

Runtime desktop `3001` HTTPS remained reachable:

| Probe | URL | HTTP | Title | SHA-256 | GTClaw | Desktop |
| --- | --- | ---: | --- | --- | --- | --- |
| desktop loopback | `https://127.0.0.1:3001/` | `200` | `Welcome to nginx!` | `fb47468a2cd3953c7131431991afcc6a2703f14640520102eea0a685a7e8d6de` | no | yes |
| desktop PodIP | `https://10.42.0.57:3001/` | `200` | `Welcome to nginx!` | `fb47468a2cd3953c7131431991afcc6a2703f14640520102eea0a685a7e8d6de` | no | yes |
| desktop ClusterIP | `https://10.43.115.105:3001/` | `200` | `Welcome to nginx!` | `fb47468a2cd3953c7131431991afcc6a2703f14640520102eea0a685a7e8d6de` | no | yes |

This is expected desktop regression evidence, not wrapper wording evidence.

## Control-UI Root / Chat / Assets / SPA Evidence

Cookie-jar route probes:

| Route | HTTP | Title | SHA-256 | Bytes | GTClaw | Desktop marker | 404 marker |
| --- | ---: | --- | --- | ---: | --- | --- | --- |
| `/api/v1/instances/3/control-ui/` | `200` | `GTClaw 控制台` | `7d2e1f21fec680b43008cd16617190ae99736f489c08bf577cead977ea1431bf` | `3472` | yes | no | no |
| `/api/v1/instances/3/control-ui/chat?session=main` | `200` | `GTClaw 控制台` | `7d2e1f21fec680b43008cd16617190ae99736f489c08bf577cead977ea1431bf` | `3472` | yes | no | no |
| `/api/v1/instances/3/control-ui/history-fallback-check` | `200` | `GTClaw 控制台` | `7d2e1f21fec680b43008cd16617190ae99736f489c08bf577cead977ea1431bf` | `3472` | yes | no | no |
| `/api/v1/instances/3/control-ui/favicon.svg` | `200` | empty | `fa7e2ec07ebfa696bcc8c27d7e36425cbb7b1772f6f7f04ce390cf5f1c35cf0e` | `1194` | no | no | no |

Browser-rendered route probes:

| Route | Final path | Title | HTML lang | Body chars | GTClaw | Required Chinese marker | Desktop fallback | 404 marker |
| --- | --- | --- | --- | ---: | --- | --- | --- | --- |
| `/api/v1/instances/3/control-ui/` | `/api/v1/instances/3/control-ui/chat?session=main` | `GTClaw 控制台` | `zh-CN` | `243` | yes | `控制台`, `网关`, `连接` | no | no |
| `/api/v1/instances/3/control-ui/chat?session=main` | `/api/v1/instances/3/control-ui/chat?session=main` | `GTClaw 控制台` | `zh-CN` | `243` | yes | `控制台`, `网关`, `连接` | no | no |
| `/api/v1/instances/3/control-ui/history-fallback-check` | `/api/v1/instances/3/control-ui/chat?session=main` | `GTClaw 控制台` | `zh-CN` | `243` | yes | `控制台`, `网关`, `连接` | no | no |

Visible DOM body snippets included localized gateway/control text such as:

```text
GTClaw GTClaw 控制台 WebSocket URL 地址 网关 token 密码 (不存储) 连接 ...
... 来源不被允许（请从网关主机打开 Control UI，或在 gateway.corsOrigin 中允许此来源。）
```

Forbidden English dashboard labels were absent from visible DOM body text on all three tested routes:

| Label | Visible DOM result |
| --- | --- |
| `Gateway Dashboard` | absent |
| `Gateway Token` | absent |
| `Password` | absent |
| `Connect` | absent |
| `origin not allowed` | absent |
| `How to connect` | absent |
| `Start the gateway on your host machine` | absent |
| `Read the docs` | absent |

Static runtime files still contain some English dictionary/source strings. This is distinguished from the browser DOM gate above; the visible DOM body did not render the forbidden English labels.

Browser-loaded control-ui assets loaded under `/api/v1/instances/3/control-ui/`:

| Browser-loaded URL | Type | HTTP | SHA-256 |
| --- | --- | ---: | --- |
| `/api/v1/instances/3/control-ui/assets/agents-utils-2iiM6XOJ.js` | Script | `200` | `5f380b2aedd2dce1c729263fd20e9f849821c6550925dc673e8d900b563451f1` |
| `/api/v1/instances/3/control-ui/assets/config-form-x_UhxUYO.js` | Script | `200` | `2f8a64769a37677f28cce9bcb63842c77a9b9f2ec7004914f81e799e25dc28f9` |
| `/api/v1/instances/3/control-ui/assets/directive--_7q5XUp.js` | Script | `200` | `a6bb5d5bc5a559a879efd3b6379c783b71e487b1d20d150760c5409e3716237e` |
| `/api/v1/instances/3/control-ui/assets/format-Dg6tFpW6.js` | Script | `200` | `0a82711d6fcd7a501b4ba7ea52c2990f8bdc3f5f1e1e3dd5cf90cc70daf90c5c` |
| `/api/v1/instances/3/control-ui/assets/i18n-B06L7jQN.js` | Script | `200` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `/api/v1/instances/3/control-ui/assets/icons-CRpuKbeA.js` | Script | `200` | `a2e3760e06a3598faaf8bb2420ec28a28a873e01887031e6f95fa6835d4f5a63` |
| `/api/v1/instances/3/control-ui/assets/index-DfTb2bb1.css` | Stylesheet | `200` | `550d3c50a88bdcfce654bd6b36b4f074ab4bfbc4f591dab65d92ea5ed387051b` |
| `/api/v1/instances/3/control-ui/assets/index-M4TNVXB3.js` | Script | `200` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` |
| `/api/v1/instances/3/control-ui/assets/navigation-NToY3MGK.js` | Script | `200` | `0f0bab58e158af5ce3ba4d2206a65721ab603b3e405741c61cacc392217457bc` |
| `/api/v1/instances/3/control-ui/assets/open-external-url-DHNx0AO6.js` | Script | `200` | `89da25f35eb6bef40d460741ac099b6216c5981d6ca098ba6e2944a355fddb7b` |
| `/api/v1/instances/3/control-ui/assets/presenter-D7XZZO4i.js` | Script | `200` | `961375ca9d0d89b1abe553469d6dd3d502897f9b5db9cf903da5115c89e9411d` |
| `/api/v1/instances/3/control-ui/assets/string-coerce-BcFtIWA_.js` | Script | `200` | `2bb1489e73ad489f215c46524c5c0bfa976422961b427b8fe6e297802cc46214` |
| `/api/v1/instances/3/control-ui/assets/zh-CN-B26mMdbY.js` | Script | `200` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` |
| `/api/v1/instances/3/control-ui/favicon.svg` | Image | `200` | `fa7e2ec07ebfa696bcc8c27d7e36425cbb7b1772f6f7f04ce390cf5f1c35cf0e` |

Favicon/history fallback evidence:

| Check | Observed |
| --- | --- |
| `/api/v1/instances/3/control-ui/favicon.svg` | HTTP `200` |
| History fallback HTML safe favicon ref | contains `/api/v1/instances/3/control-ui/favicon.svg` |
| History fallback HTML nested favicon ref | does not contain `/api/v1/instances/3/control-ui/history-fallback-check/favicon.svg` |
| Browser nested favicon request | none observed |
| Rendered logo image src | `/api/v1/instances/3/control-ui/favicon.svg` |

## Runtime Reachability Evidence

Runtime probes used `kubectl exec` inside the runtime pod with `curl -k --noproxy '*'`. They did not mutate pod processes or files.

| Probe | URL | HTTP | Title | Lang | SHA-256 | Bytes | GTClaw | Chinese marker | Desktop |
| --- | --- | ---: | --- | --- | --- | ---: | --- | --- | --- |
| loopback root | `http://127.0.0.1:18789/` | `200` | `GTClaw 控制台` | `zh-CN` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` | yes | yes | no |
| loopback chat | `http://127.0.0.1:18789/chat?session=main` | `200` | `GTClaw 控制台` | `zh-CN` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` | yes | yes | no |
| PodIP root | `http://10.42.0.57:18789/` | `200` | `GTClaw 控制台` | `zh-CN` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` | yes | yes | no |
| PodIP chat | `http://10.42.0.57:18789/chat?session=main` | `200` | `GTClaw 控制台` | `zh-CN` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` | yes | yes | no |
| ClusterIP root | `http://10.43.115.105:18789/` | `200` | `GTClaw 控制台` | `zh-CN` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` | yes | yes | no |
| ClusterIP chat | `http://10.43.115.105:18789/chat?session=main` | `200` | `GTClaw 控制台` | `zh-CN` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` | yes | yes | no |

Process and listener evidence, read-only:

| Check | Observed |
| --- | --- |
| Process names | `nginx`, `openclaw`, `openclaw-agent`, `openclaw-gatewa` |
| OpenClaw-related args, redacted | `/usr/local/bin/openclaw-agent`, `openclaw`, `openclaw-gateway`; nginx master/worker processes present |
| `/proc/net/tcp` listeners | `0100007F:4965` (`127.0.0.1:18789`), `39002A0A:4965` (`10.42.0.57:18789`), `00000000:0BB9` (`0.0.0.0:3001`) |
| `/proc/net/tcp6` listeners | loopback `:4965` and wildcard `:0BB9` present |

## Protected Literal Evidence

### Deployed Manager Bundle Literal Counts

Scanned deployed manager `index.html`, JS, and CSS assets.

| Literal | Count |
| --- | ---: |
| `GTManager` | `38` |
| `GTClaw` | `27` |
| `ClawManager` | `0` |
| `openclaw` | `124` |
| `OpenClaw` | `128` |
| `.openclaw` | `62` |
| `openclaw.json` | `0` |
| `openclaw dashboard --no-open` | `0` |
| `dist/control-ui` | `0` |

### Runtime Control-UI Literal Counts

Scanned `index.html`, `assets/index-M4TNVXB3.js`, `assets/i18n-B06L7jQN.js`, `assets/zh-CN-B26mMdbY.js`, and `assets/index-DfTb2bb1.css` in the runtime pod.

| Literal | Count |
| --- | ---: |
| `GTManager` | `0` |
| `GTClaw` | `8` |
| `ClawManager` | `0` |
| `openclaw` | `73` |
| `OpenClaw` | `2` |
| `.openclaw` | `9` |
| `openclaw.json` | `3` |
| `openclaw dashboard --no-open` | `4` |
| `dist/control-ui` | `0` |

Static English string counts in runtime files:

| Literal | Static count |
| --- | ---: |
| `Gateway Dashboard` | `1` |
| `Gateway Token` | `1` |
| `Password` | `31` |
| `Connect` | `66` |
| `origin not allowed` | `2` |
| `How to connect` | `1` |
| `Start the gateway on your host machine` | `1` |
| `Read the docs` | `1` |
| `Control UI` | `7` |

Static English strings remain in JS/CSS dictionaries or source text, but the browser-rendered visible DOM body did not expose the listed forbidden English dashboard labels on the three required authorized control-ui routes.

Protected technical `openclaw`, `.openclaw`, `openclaw.json`, and `openclaw dashboard --no-open` literals remain present where expected. Raw technical `openclaw` also remains visible in technical rows/contexts such as `/admin/instances`, which is allowed by the corrected criteria.

## Verification Command Results

| Command | Exit | Sanitized result |
| --- | ---: | --- |
| `cd backend && go test -count=1 ./...` | `0` | Backend package tests returned exit `0`; tested packages included `internal/handlers`, `internal/services`, and `internal/services/k8s` |
| `cd frontend && npm run lint` | `1` | Known lint debt remains: `126 problems (107 errors, 19 warnings)` |
| Temp frontend `npm ci && npm run build` under `/tmp/gtclaw-t8i-rerun-after-runtime-body-fix-20260502/frontend-build` | `0` | `npm ci` exit `0`; `npm run build` exit `0`; emitted hashes matched deployed browser-loaded manager assets |
| `kubectl apply --dry-run=client -f deployments/k3s/clawmanager.yaml` | `0` | K3S manifest dry-run returned configured/created resources without client validation error |
| `kubectl apply --dry-run=client -f deployments/k8s/clawmanager.yaml` | `0` | K8S manifest dry-run returned configured/created resources without client validation error |
| `git diff --check` | `0` | No whitespace errors reported |
| Runtime repaired hash gate | `0` | Four expected file hashes and sizes matched |
| Deployed manager bundle hash check | `0` | Browser-loaded JS/CSS/logo hashes matched temp build |
| Control-ui browser DOM route check | `0` | Root, chat, and history fallback rendered `GTClaw 控制台`, `lang=zh-CN`, GTClaw, Chinese markers, no desktop fallback, no 404 marker |
| Runtime reachability checks | `0` | `18789` loopback/PodIP/ClusterIP root/chat returned HTTP `200`; desktop `3001` loopback/PodIP/ClusterIP HTTPS returned HTTP `200` |
| Temp cleanup check | `0` | `/tmp/gtclaw-t8i-rerun-after-runtime-body-fix-20260502` and `/tmp/t8i-chrome-profile-after-body-fix` were removed after evidence writing |

Frontend lint statement: lint remains failing and is recorded as known debt under this T8I evidence-only gate. This is not a Close waiver.

## Failures / Blockers

No T8I evidence blocker was observed under the user-provided criteria.

Known residual notes:

- `cd frontend && npm run lint` still exits `1` with existing lint debt. This evidence does not waive lint for Close.
- Static runtime JS/CSS text still contains English strings and protected technical literals. The corrected gate is visible browser DOM body, and the forbidden English dashboard labels were not rendered as visible DOM text in the required routes.
- Visible control-ui body still includes technical/product terms such as `WebSocket URL`, lowercase `token`, and `Control UI` inside Chinese text. These are not the forbidden labels enumerated for this T8I gate.

## Scope Statement

This worker only collected T8I evidence and wrote this single approved evidence file:

`specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260502-t8i-e2e-rerun-after-runtime-body-fix.md`

No backend, frontend, deployment, docs, longterm, spec, plan, tasks, design amendment, old evidence, app image, runtime image, registry tag, Kubernetes Deployment, Service, Secret, ConfigMap, NodePort, Ingress, hostNetwork, namespace, or database mutation was performed by this worker. The existing dirty worktree items listed in Environment Preflight were pre-existing.

Temporary files were limited to `/tmp/gtclaw-t8i-rerun-after-runtime-body-fix-20260502`, `/tmp/t8i-chrome-profile-after-body-fix`, and `/tmp/t8i-*` probe/auth/cookie files. They were removed after evidence writing.

No token value, cookie value, credential, secret, `.env`, `.codex/auth.json`, `.codex/config.toml`, full environment dump, or token-bearing URL is recorded here.

## Gate Statement

This is T8I evidence collection only, not Close, no `passes:true`.
