# T8I E2E Rerun After `/admin/instances` Fix

Timestamp: 2026-05-02 Asia/Shanghai

## 1. Evidence File Path

`specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260502-t8i-e2e-rerun-after-admin-instances-fix.md`

## 2. Environment Preflight

This was an evidence-only T8I rerun against the current local k3d environment. It used route cookies for `/proxy/` and `/control-ui/`; no request URL containing `token=` was used or observed.

| Check | Observed |
| --- | --- |
| App health | `GET /healthz` -> HTTP `200`, body `ok` |
| App deployment | `clawmanager-system/clawmanager-app`, `1/1` ready |
| App pod | `clawmanager-app-754cd48f95-pn7sx` |
| App image | `clawmanager:t8i-wrapper-fix-20260502172919` |
| App imageID | `sha256:29fe80505c1b74dc4a6c8082f9a93b87124aef1c56592b64dcb14176d4df6599` |
| Disposable instance | `3` |
| Runtime namespace | `clawmanager-user-1` |
| Runtime pod | `clawreef-3-gtclaw-t8-dev-20260501001159` |
| Runtime service | `clawreef-3-gtclaw-t8-dev-20260501001159-svc` |
| Runtime pod phase/ready | `Running`, `Ready=True` |
| Runtime PodIP | `10.42.0.57` |
| Runtime Service ClusterIP | `10.43.115.105` |
| Runtime service ports | `http:3001->3001/TCP`, `control-18789:18789->18789/TCP` |
| Runtime image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029` |
| Runtime imageID | `sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45` |
| Runtime package | `openclaw@2026.4.14` |
| Runtime control-ui path | `/usr/local/lib/node_modules/openclaw/dist/control-ui` exists |
| Worktree state | Dirty before this evidence worker; source/docs/longterm changes were pre-existing |

## 3. App / Bundle Evidence

`GET https://localhost:30443/` returned HTTP `200`, title `GTManager`.

| Deployed file | SHA-256 |
| --- | --- |
| `index.html` | `483830c45540eb45d97941ee89f3c97cded2c09a3f953c60153da4b7371d3cbf` |
| `/assets/index-Cce3cBw3.js` | `dd22b40310e7e2af08ac3b16cbdf1d2488db7f13588f8317fdd8142029a213a3` |
| `/assets/index-Cv71uKAX.css` | `6f70bfb0b44b60282c12521397c7768c05e48313edd1d78cc0d015543053094e` |
| `/gtmanager-logo.png` | `0d738a0371f3fac37bd71ff21d0027bc46958d98e5f5ff1c3e2bc36e93f8e7c0` |

Temp frontend build ran in `/tmp/gtclaw-t8i-rerun-20260502174831/frontend-build` with `npm ci && npm run build`. The emitted manager bundle matched the deployed bundle:

| Temp build file | SHA-256 |
| --- | --- |
| `dist/index.html` | `483830c45540eb45d97941ee89f3c97cded2c09a3f953c60153da4b7371d3cbf` |
| `dist/assets/index-Cce3cBw3.js` | `dd22b40310e7e2af08ac3b16cbdf1d2488db7f13588f8317fdd8142029a213a3` |
| `dist/assets/index-Cv71uKAX.css` | `6f70bfb0b44b60282c12521397c7768c05e48313edd1d78cc0d015543053094e` |

Browser-loaded manager assets matched the temp build hashes:

| Browser-loaded URL | Type | HTTP | SHA-256 |
| --- | --- | ---: | --- |
| `/assets/index-Cce3cBw3.js` | Script | `200` | `dd22b40310e7e2af08ac3b16cbdf1d2488db7f13588f8317fdd8142029a213a3` |
| `/assets/index-Cv71uKAX.css` | Stylesheet | `200` | `6f70bfb0b44b60282c12521397c7768c05e48313edd1d78cc0d015543053094e` |
| `/gtmanager-logo.png` | Image | `200` | `0d738a0371f3fac37bd71ff21d0027bc46958d98e5f5ff1c3e2bc36e93f8e7c0` |

## 4. Corrected Wrapper Route Matrix

All wrapper checks used a logged-in browser context with `zh-CN` locale. `ClawManager` wrapper wording was not observed in the checked manager routes.

| Route | Corrected criterion | Result | Evidence |
| --- | --- | --- | --- |
| `/instances/new` | After entering type-selection state, OpenClaw runtime type display is `GTClaw` | met | Filled only the local form name field, clicked `下一步`, reached `第 2 步，共 3 步：选择类型`; visible option: `GTClaw 桌面` |
| `/portal` | Shows GTClaw runtime/control-ui entry | met | Visible entry includes `打开 GTClaw 控制台` for instance 3 |
| `/openclaw-configs` | Shows GTClaw runtime config/resource wording | met | Header/content includes `GTClaw 资源管理` |
| `/admin/settings` | Runtime image/settings surface shows GTClaw | met | Runtime image card includes `实例类型 GTClaw 桌面`; technical image literal remains separately visible |
| `/admin/instances` | OpenClaw instance type user-facing label shows GTClaw; raw `openclaw` may remain in technical row | met | Filter and row show `GTClaw 桌面`; row also keeps technical `openclaw latest` |
| `/instances/3` | Instance detail/access/runtime status wording shows GTClaw | met | Access surface includes `打开 GTClaw 控制台`; runtime/status timeline is visible |
| `/admin/ai-audit` | Only deterministic trace/detail/execution-flow runtime-facing state requires GTClaw | met | Opened trace detail `?trace=trc_651816fecfe89903`; execution-flow copy includes `GTClaw 问答` |
| `/admin/ai-gateway` | AI governance overview requires GTManager manager brand and no ClawManager wrapper wording | met | Overview shows `GTManager` and AI gateway governance entries; no `ClawManager` wrapper wording observed |

## 5. Access Contract Evidence

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
| Cookie name | `instance_access_3` |
| Cookie path | `/api/v1/instances/3/proxy` |
| Cookie flags | `HttpOnly=yes`, `Secure=yes`, `Max-Age=yes`; `SameSite` not present in raw header |

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
| Cookie name | `instance_control_ui_access_3` |
| Cookie path | `/api/v1/instances/3/control-ui` |
| Cookie flags | `HttpOnly=yes`, `Secure=yes`, `Max-Age=yes`; `SameSite` not present in raw header |

Browser cookie inspection after access showed the same route-cookie names and paths; Playwright normalized `SameSite` as `Lax` in browser storage. No request URL containing `token=` was observed.

## 6. Desktop Regression Evidence

Cookie-jar route probe:

| Route | HTTP | SHA-256 | GTClaw | Desktop fallback marker in initial HTML |
| --- | ---: | --- | --- | --- |
| `/api/v1/instances/3/proxy/` | `200` | `12c54f88575ccf5c69b79bdc37900d553713499bebf7765f0f4d7f7940f18d1c` | no | no |

Browser-rendered desktop route:

| Route | Final path | Title | HTML lang | GTClaw | Desktop title |
| --- | --- | --- | --- | --- | --- |
| `/api/v1/instances/3/proxy/` | `/api/v1/instances/3/proxy/` | `ClawManager Desktop` | `en` | no | yes |

Desktop route loaded desktop assets under `/proxy/`, including `/api/v1/instances/3/proxy/assets/index-luFV63l_.js` and `/api/v1/instances/3/proxy/assets/index-C-ZxXfwb.css`. `/proxy/` remained the desktop route and did not become the control-ui route.

## 7. Control-UI Root / Chat / Assets / SPA Evidence

Cookie-jar route probes:

| Route | HTTP | Title | SHA-256 | GTClaw | Desktop fallback | 404 marker |
| --- | ---: | --- | --- | --- | --- | --- |
| `/api/v1/instances/3/control-ui/` | `200` | `GTClaw 控制台` | `a457ffd64a7ccd3219618fdee2418315ff825ef77b76d2f427f23cc135b9bed4` | yes | no | no |
| `/api/v1/instances/3/control-ui/chat?session=main` | `200` | `GTClaw 控制台` | `a457ffd64a7ccd3219618fdee2418315ff825ef77b76d2f427f23cc135b9bed4` | yes | no | no |
| `/api/v1/instances/3/control-ui/history-fallback-check` | `200` | `GTClaw 控制台` | `a457ffd64a7ccd3219618fdee2418315ff825ef77b76d2f427f23cc135b9bed4` | yes | no | no |

Browser-rendered route probes:

| Route | Final path | Title | HTML lang | Body has GTClaw | Body has required Chinese marker | Desktop fallback | 404 marker | Corrected criterion |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `/api/v1/instances/3/control-ui/` | `/api/v1/instances/3/control-ui/chat?session=main` | `GTClaw 控制台` | `en` | yes | no | no | no | not met |
| `/api/v1/instances/3/control-ui/chat?session=main` | `/api/v1/instances/3/control-ui/chat?session=main` | `GTClaw 控制台` | `en` | yes | no | no | no | not met |
| `/api/v1/instances/3/control-ui/history-fallback-check` | `/api/v1/instances/3/control-ui/history-fallback-check/chat?session=main` | `GTClaw 控制台` | `en` | yes | no | no | no | not met |

Rendered control-ui body text was the English gateway dashboard (`GTClaw Gateway Dashboard`, `WebSocket URL`, `Gateway Token`, `Connect`, origin warning). This is recorded as localization residual risk and as a blocker for the corrected control-ui body marker criterion because no required Chinese marker such as `可开始对话`, `在下方输入消息`, or `你能做什么` was visible in the rendered body.

Browser-loaded control-ui assets:

| URL | Type | HTTP | SHA-256 |
| --- | --- | ---: | --- |
| `/api/v1/instances/3/control-ui/assets/index-M4TNVXB3.js` | Script | `200` | `eb8379dce913df535806486c8a4f637ab27f069c95925ef836821fbde2cb321b` |
| `/api/v1/instances/3/control-ui/assets/index-DfTb2bb1.css` | Stylesheet | `200` | `550d3c50a88bdcfce654bd6b36b4f074ab4bfbc4f591dab65d92ea5ed387051b` |
| `/api/v1/instances/3/control-ui/assets/i18n-B06L7jQN.js` | Script | `200` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` |
| `/api/v1/instances/3/control-ui/assets/directive--_7q5XUp.js` | Script | `200` | `a6bb5d5bc5a559a879efd3b6379c783b71e487b1d20d150760c5409e3716237e` |
| `/api/v1/instances/3/control-ui/assets/format-Dg6tFpW6.js` | Script | `200` | `0a82711d6fcd7a501b4ba7ea52c2990f8bdc3f5f1e1e3dd5cf90cc70daf90c5c` |
| `/api/v1/instances/3/control-ui/assets/string-coerce-BcFtIWA_.js` | Script | `200` | `2bb1489e73ad489f215c46524c5c0bfa976422961b427b8fe6e297802cc46214` |
| `/api/v1/instances/3/control-ui/assets/open-external-url-DHNx0AO6.js` | Script | `200` | `89da25f35eb6bef40d460741ac099b6216c5981d6ca098ba6e2944a355fddb7b` |
| `/api/v1/instances/3/control-ui/assets/agents-utils-2iiM6XOJ.js` | Script | `200` | `5f380b2aedd2dce1c729263fd20e9f849821c6550925dc673e8d900b563451f1` |
| `/api/v1/instances/3/control-ui/assets/icons-CRpuKbeA.js` | Script | `200` | `a2e3760e06a3598faaf8bb2420ec28a28a873e01887031e6f95fa6835d4f5a63` |
| `/api/v1/instances/3/control-ui/assets/navigation-NToY3MGK.js` | Script | `200` | `0f0bab58e158af5ce3ba4d2206a65721ab603b3e405741c61cacc392217457bc` |
| `/api/v1/instances/3/control-ui/assets/presenter-D7XZZO4i.js` | Script | `200` | `961375ca9d0d89b1abe553469d6dd3d502897f9b5db9cf903da5115c89e9411d` |
| `/api/v1/instances/3/control-ui/assets/config-form-x_UhxUYO.js` | Script | `200` | `2f8a64769a37677f28cce9bcb63842c77a9b9f2ec7004914f81e799e25dc28f9` |
| `/api/v1/instances/3/control-ui/favicon.svg` | Image | `200` | `fa7e2ec07ebfa696bcc8c27d7e36425cbb7b1772f6f7f04ce390cf5f1c35cf0e` |
| `/api/v1/instances/3/control-ui/history-fallback-check/favicon.svg` | Image | `404` | `0019dfc4b32d63c1392aa264aed2253c1e0c2fb09216f8e2cc269bbfb8bb49b5` |

SPA/history fallback note: `/control-ui/history-fallback-check` returned the GTClaw shell and did not fall back to desktop, but the runtime router appended `/chat?session=main` under the nested path and the nested favicon request returned `404`.

## 8. Runtime Reachability Evidence

Bridge/process evidence:

| Item | Observed |
| --- | --- |
| Bridge process | `python3` process present |
| Bridge listen target | `("10.42.0.57",18789)` |
| Bridge upstream target | `("127.0.0.1",18789)` |
| OpenClaw processes | `openclaw-agent`, `openclaw`, `openclaw-gateway` present |
| Desktop browser launch | Chromium command includes `http://localhost:18789` |

Direct runtime probes used `curl -k --noproxy '*'` inside the runtime pod.

| Probe | URL | HTTP | Title | SHA-256 | GTClaw | Chinese marker | Desktop |
| --- | --- | ---: | --- | --- | --- | --- | --- |
| loopback root | `http://127.0.0.1:18789/` | `200` | `GTClaw 控制台` | `694fa7f68e930409cc641afbf3aef86a0990f7eebea28fc820179990be20ec13` | yes | yes | no |
| loopback chat | `http://127.0.0.1:18789/chat?session=main` | `200` | `GTClaw 控制台` | `694fa7f68e930409cc641afbf3aef86a0990f7eebea28fc820179990be20ec13` | yes | yes | no |
| PodIP root | `http://10.42.0.57:18789/` | `200` | `GTClaw 控制台` | `694fa7f68e930409cc641afbf3aef86a0990f7eebea28fc820179990be20ec13` | yes | yes | no |
| PodIP chat | `http://10.42.0.57:18789/chat?session=main` | `200` | `GTClaw 控制台` | `694fa7f68e930409cc641afbf3aef86a0990f7eebea28fc820179990be20ec13` | yes | yes | no |
| ClusterIP root | `http://10.43.115.105:18789/` | `200` | `GTClaw 控制台` | `694fa7f68e930409cc641afbf3aef86a0990f7eebea28fc820179990be20ec13` | yes | yes | no |
| ClusterIP chat | `http://10.43.115.105:18789/chat?session=main` | `200` | `GTClaw 控制台` | `694fa7f68e930409cc641afbf3aef86a0990f7eebea28fc820179990be20ec13` | yes | yes | no |
| desktop loopback HTTPS | `https://127.0.0.1:3001/` | `200` | `Welcome to nginx!` | `88a33b910dd633bc4e66b8c3cdea33a0bfd7b9a626ce4b274be259ab01c91b38` | no | no | no |
| desktop ClusterIP HTTPS | `https://10.43.115.105:3001/` | `200` | `Welcome to nginx!` | `88a33b910dd633bc4e66b8c3cdea33a0bfd7b9a626ce4b274be259ab01c91b38` | no | no | no |

Runtime reachability is present over loopback, PodIP, and ClusterIP for `18789`; desktop `3001` remains reachable over HTTPS.

## 9. Protected Literal Evidence

### Deployed Manager Bundle Literal Counts

| Literal | Count |
| --- | ---: |
| `GTManager` | `38` |
| `GTClaw` | `27` |
| `ClawManager` | `0` |
| `openclaw` | `124` |
| `OpenClaw` | `128` |
| `OpenClaw Image` | `0` |
| `clawmanager-openclaw-image` | `1` |
| `.openclaw` | `62` |
| `openclaw.json` | `0` |
| `openclaw dashboard --no-open` | `0` |
| `dist/control-ui` | `0` |

### Runtime Control-UI File Hashes

| Runtime file | SHA-256 |
| --- | --- |
| `index.html` | `ed3560d9fa9b9156e62a405bc185c2d3495129ee3712ef8c536767f79d5778c7` |
| `assets/index-M4TNVXB3.js` | `eb8379dce913df535806486c8a4f637ab27f069c95925ef836821fbde2cb321b` |
| `assets/i18n-B06L7jQN.js` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` |
| `assets/zh-CN-B26mMdbY.js` | `9a4ecc8992d00443ef59de0be41090099d5a1feb25cf062c5c02470044277f29` |
| `assets/index-DfTb2bb1.css` | `550d3c50a88bdcfce654bd6b36b4f074ab4bfbc4f591dab65d92ea5ed387051b` |

### Runtime Control-UI Literal Counts

Scanned `index.html`, `assets/index-M4TNVXB3.js`, `assets/i18n-B06L7jQN.js`, `assets/zh-CN-B26mMdbY.js`, and `assets/index-DfTb2bb1.css` in the runtime pod.

| Literal | Count |
| --- | ---: |
| `GTClaw` | `8` |
| `GTClaw 控制台` | `2` |
| `ClawManager` | `0` |
| `openclaw` | `73` |
| `OpenClaw` | `2` |
| `OpenClaw Image` | `0` |
| `clawmanager-openclaw-image` | `0` |
| `.openclaw` | `9` |
| `openclaw.json` | `3` |
| `openclaw dashboard --no-open` | `4` |
| `dist/control-ui` | `0` |

Protected literal decision: no unintended `ClawManager` wrapper wording was found in deployed manager assets or runtime control-ui assets. Required technical `openclaw`, `.openclaw`, `openclaw.json`, and `openclaw dashboard --no-open` literals remain present where scanned.

## 10. Verification Command Results

| Command | Exit | Sanitized result |
| --- | ---: | --- |
| `cd backend && go test -count=1 ./...` | `0` | Backend package tests returned exit `0` |
| `cd frontend && npm run lint` | `1` | Known lint debt remains: `126 problems (107 errors, 19 warnings)` |
| Temp frontend `npm ci && npm run build` in `/tmp/gtclaw-t8i-rerun-20260502174831/frontend-build` | `0` | Build returned exit `0`; emitted bundle hashes match deployed manager bundle. `npm ci` also reported an engine warning and `7 vulnerabilities (4 moderate, 3 high)` |
| `git diff --check` | `0` | No whitespace errors reported |
| Scoped `git status --short -- specs/.../20260502-t8i-e2e-rerun-after-admin-instances-fix.md backend frontend deployments docs longterm` before evidence write | `0` | Source/docs/longterm changes were already dirty; the allowed evidence file did not exist yet |
| Scoped `git status --short -- specs/.../20260502-t8i-e2e-rerun-after-admin-instances-fix.md` after evidence write | `0` | Only the allowed evidence file appears as newly untracked in this scoped check |
| Temp cleanup check | `0` | `/tmp/gtclaw-t8i-rerun-20260502174831` and `/tmp/gtclaw-t8i-rerun-current-path` were removed |
| Deployed bundle hash checks | `0` | Deployed and browser-loaded manager JS/CSS hashes matched temp build |
| Runtime reachability checks | `0` | `18789` loopback/PodIP/ClusterIP root/chat returned HTTP `200`; desktop `3001` HTTPS returned HTTP `200` |

Frontend lint statement: lint remains failing and is recorded as known debt under the Commander temporary T8I-only waiver. This is not a Close waiver.

## 11. Failures / Blockers

E2E evidence status: blocked by control-ui rendering criteria.

Blocking evidence gaps:

- `/api/v1/instances/3/control-ui/` and `/api/v1/instances/3/control-ui/chat?session=main` render title `GTClaw 控制台` and body `GTClaw`, but the rendered body does not contain a required Chinese marker such as `可开始对话`, `在下方输入消息`, or `你能做什么`.
- Rendered control-ui body is still English gateway dashboard text and `html lang=en`; recorded as localization residual risk.
- SPA fallback route `/api/v1/instances/3/control-ui/history-fallback-check` returns the GTClaw shell and not desktop, but a nested favicon request under that fallback path returned HTTP `404`.
- `npm run lint` still exits `1` with known frontend lint debt. This was temporarily waived only for T8I evidence collection.

Non-blocking observations:

- `/proxy/` still renders the desktop route with title `ClawManager Desktop`; this is expected desktop regression evidence, not wrapper bundle wording.
- Access API responses still include a token field for compatibility; values were not recorded. Route cookies were used for proxy/control-ui requests.
- Raw technical `openclaw` remains visible in technical rows/details, including the `/admin/instances` row and runtime detail image path; this is allowed by the corrected criteria.

No stale deployed manager bundle was observed. No request URL containing `token=` was observed.

## 12. Scope Statement

This worker performed evidence collection only.

- No `backend/**`, `frontend/**`, `deployments/**`, `docs/**`, or `longterm/**` files were edited by this worker.
- No app image, runtime image, registry, tag, Secret, ConfigMap, NodePort, Ingress, hostNetwork, pod, service, or cluster resource was modified by this worker.
- No pod/service/app/runtime restart was performed.
- Only this evidence file was written under the approved path.
- Temporary files were limited to `/tmp/gtclaw-t8i-rerun-*`; auth/cookie/login temp files were cleaned after evidence writing.
- No token value, cookie value, credential, secret, full environment, or token-bearing URL is recorded here.

## 13. Gate Statement

This is T8I evidence collection only. It is not Close. No `passes:true` was written.
