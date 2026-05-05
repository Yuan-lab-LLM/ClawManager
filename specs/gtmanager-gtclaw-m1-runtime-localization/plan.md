# Implementation Plan: GTManager / GTClaw M1 Runtime Localization

**Branch**: `not-created`  
**Date**: 2026-04-30  
**Spec**: `specs/gtmanager-gtclaw-m1-runtime-localization/spec.md`  
**Status**: Amended Draft - design amendment 已经内部 review + GPT Pro external review 条件通过；当前只批准进入 plan/tasks amendment，不批准 implementation、E2E rerun、Close 或 longterm write-back  
**Input**: 已获用户同意的中文 spec、PRD Round 7 决策、G1-G7 只读 agent 结果、running image bundle baseline、T8 rerun fail evidence、`design-amendment-20260501-control-ui-proxy.md`、`20260501-gpt-pro-design-review-notes.md`。

## Summary

本计划把 `gtmanager-gtclaw-m1-runtime-localization` 拆成两个必须同时满足的交付面：

1. **GTManager wrapper 面**：GTManager 管理面中用户可见 runtime 产品称呼改为 GTClaw；GTManager 继续是管控面品牌；OpenClaw 技术 literal 保持不变。
2. **OpenClaw Image runtime 面**：基于已确认的 running OpenClaw Image control-ui bundle 做受控静态 patch，使真实 runtime control UI 默认中文，并在 title、login、sidebar、breadcrumb、logo alt/title、chat welcome 等用户可见位置显示 GTClaw 或 `GTClaw 控制台`。
3. **GTManager authorized control-ui proxy 面**：T8 rerun 证明 runtime patch 已在 pod 内生效，但 GTManager `/proxy/` 仍指向 desktop shell `3001`，`/proxy/chat?session=main` 返回 404。后续实现必须保留 `/proxy/` desktop semantics，并新增独立 `/api/v1/instances/:id/control-ui/` authorized route、`access?mode=control-ui` contract、ServicePort 校验/patch、pod-network reachable control-ui target 和 deployed bundle gate。

本计划不授权实现。GPT Pro verdict 是 `APPROVE WITH CONDITIONS`，只允许进入下一轮 plan/tasks amendment。后续必须先审核 amended `tasks.md`，再由用户显式批准 implementation，并按星型拓扑分发 agent prompt 执行。E2E 仍是最终通过门槛，build/lint/unit/dry-run/curl/hash check 只能作为前置证据。

## Technical Context

**Language/Version**: Go 1.21+；React 19 + TypeScript；OpenClaw runtime bundle `openclaw@2026.4.14`  
**Primary Dependencies**: Backend Go REST API/K8S client；Frontend Vite/React/i18n；K3S/K8S；running OpenClaw Image control-ui static bundle  
**Storage**: MySQL 8.x；frontend localStorage；runtime image filesystem  
**Testing**: `go test ./...`；`npm run lint`；`npm run build`；`kubectl apply --dry-run=client`；Playwright 或人工记录 E2E  
**Target Platform**: Local K3S/K3D at `https://localhost:30443`；desktop service/proxy target `3001`；control-ui selected target port 初始设计为 `18789`，bridge fallback 可使用显式 selected pod-reachable target port  
**Project Type**: Full-stack Web control plane + Kubernetes-managed runtime image  
**Performance Goals**: 保留现有 `/api/v1/instances/:id/proxy/` desktop route 行为；新增 `/api/v1/instances/:id/control-ui/` 必须是 route-specific、mode-scoped、可验证的最小代理扩展；runtime control-ui patch 不应破坏 desktop access 或 OpenClaw technical contract  
**Constraints**:
- API/proxy/Service 变化只允许在后续 implementation approval 后按 design amendment 范围实施；不得改 DB schema、image lineage、path、command、config filename、package/protocol/storage literal。
- 不依赖 floating `latest` 作为 patch baseline。
- 不把 old M1 evidence 当作 GTClaw runtime evidence。
- 不输出 credential、token、secret、`.env`、`.codex/auth.json`、`.codex/config.toml` 内容。
- Direct `127.0.0.1:18789` / port-forward evidence 只能作为 supplementary evidence；最终必须通过 authorized GTManager proxy E2E。
- `podIP:18789` 已测试失败/currently unreachable；下一轮 E2E rerun 必须补 exact failed probe command/output，或在 bridge path 中明确旧失败被 selected bridge port 替代。
**Scale/Scope**: M1 只覆盖默认 zh-CN 用户可见路径；非中文 locale 全量翻译不作为 M1 gate。

## Constitution Check

| Gate | Plan 判定 | 说明 |
| --- | --- | --- |
| 文档驱动 | PASS | 已有 `spec.md`，本文件为 amended `plan.md`；`tasks.md` 已按 design amendment / GPT Pro 条件修订为 amended draft。 |
| 用户批准 | PASS for plan/tasks amendment | 用户已批准进入 plan/tasks amendment；implementation、E2E rerun、Close 仍未批准。 |
| 不改业务代码 | PASS | 本计划只写 `plan.md`；不改 frontend/backend/deployment/runtime bundle。 |
| E2E 最终门槛 | PASS | 本计划明确 E2E 是最终 gate；前置 checks 不能替代 E2E。 |
| 技术标识保留 | PASS | 保留 OpenClaw Image、`openclaw`、`.openclaw*`、`openclaw.json`、`dist/control-ui`、API/DB/K8S/image tag 等 literal。 |
| 安全与凭证 | PASS | 所有 probe/evidence 都必须脱敏，不读取或输出 secret。 |
| 星型拓扑 | PASS | Commander 只给 prompt；agent 之间不通信；用户负责分发与回传。 |
| 外部设计 review | PASS with conditions | GPT Pro verdict: `APPROVE WITH CONDITIONS`，无 blocking findings；条件必须进入 amended tasks。 |

Re-check after design: `tasks.md` 生成前必须再次检查以上 gate，尤其是 baseline mismatch、allowed files 和 E2E evidence 路径。

## Project Structure

### Documentation for this feature

```text
specs/gtmanager-gtclaw-m1-runtime-localization/
├── spec.md              # 已写入，中文主文案
├── plan.md              # 本文件，已按 T8 failure + design amendment 更新
├── tasks.md             # 后续 worker gate 拆解；仍不授权 implementation
├── design-amendment-20260501-control-ui-proxy.md
└── evidence/
    ├── 20260501-t8-e2e-rerun.md
    ├── 20260501001159-test-env-mutation.md
    └── 20260501-gpt-pro-design-review-notes.md
```

本 plan 不创建 `research.md`、`data-model.md`、`quickstart.md` 或 `contracts/`。本 feature 不涉及新的 DB data model；API/proxy contract amendment 记录在 `design-amendment-20260501-control-ui-proxy.md` 和本 plan/tasks 中。

### Source Code / Runtime Surfaces

```text
frontend/
├── index.html
└── src/
    ├── lib/i18n.ts
    ├── contexts/I18nContext.tsx
    ├── components/
    │   ├── AdminLayout.tsx
    │   ├── UserLayout.tsx
    │   ├── InstanceAccess.tsx
    │   ├── OpenClawDesktopOverlay.tsx
    │   └── OpenClawConfigPlanSection.tsx
    └── pages/
        ├── instances/
        │   ├── CreateInstancePage.tsx
        │   ├── InstanceDetailPage.tsx
        │   ├── InstanceListPage.tsx
        │   └── InstancePortalPage.tsx
        ├── openclaw/OpenClawConfigCenterPage.tsx
        └── admin/
            ├── SystemSettingsPage.tsx
            ├── InstanceManagementPage.tsx
            ├── AIAuditPage.tsx
            └── AIGatewayPage.tsx

runtime image, read-only baseline:
/usr/local/lib/node_modules/openclaw/dist/control-ui/
├── index.html
└── assets/
    ├── index-M4TNVXB3.js
    ├── i18n-B06L7jQN.js
    ├── zh-CN-B26mMdbY.js
    └── index-DfTb2bb1.css        # observed; not default patch target
```

**Structure Decision**: wrapper 文案走现有 frontend i18n；runtime 内页走 OpenClaw Image control-ui allowlist static patch。两条线分别验证，最终 E2E 必须同时覆盖 wrapper 和真实 runtime UI。

## Runtime Baseline Gate

后续任何 patch 设计或执行前，必须重新确认 running image baseline。以下任一项不匹配，立即停止后续 patch flow，重新基准化：

| Item | Locked Baseline |
| --- | --- |
| Runtime image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434` |
| Image ID digest | `sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8` |
| Package | `openclaw@2026.4.14` |
| Package path | `/usr/local/lib/node_modules/openclaw/package.json` |
| Control-ui path | `/usr/local/lib/node_modules/openclaw/dist/control-ui` |
| Service port | `3001` |

| Target File | Size | Baseline SHA-256 |
| --- | ---: | --- |
| `index.html` | 3,395 | `f313071437a1b8c432024d3f83af4056fb672a4fe15b93be8b2291dcaac0115c` |
| `assets/index-M4TNVXB3.js` | 707,545 | `e89d5e55d89aaae7bc64598b949335425df7626f57b12a8780426a1911315882` |
| `assets/i18n-B06L7jQN.js` | 42,702 | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` |
| `assets/zh-CN-B26mMdbY.js` | 23,247 | `2afe4858d80c81247f01e21198011a78180de12e72f567b5606fe9355dbfd2c1` |

Baseline probe 必须只读，不输出 token/env/secret。允许的后续证据形式包括脱敏 `kubectl exec` 观察、image tar inspection，或两者互相印证。

## Architecture / Approach

### 1. Wrapper layer: i18n-first, no route/API rename

- 通过 `frontend/src/lib/i18n.ts` 和现有 i18n 使用点修正用户可见 runtime 文案。
- 覆盖 `/instances/new`、实例详情、`/portal`、`/openclaw-configs`、`/admin/settings`、`/admin/instances`、当前等价 admin AI audit/gateway surface。
- 保留 `openclaw` type、service/API path、storage key、`.openclaw` 归档、OpenClaw Image、image name/tag、DB/K8S/API literal。
- 不新增第二套 localization framework。
- 默认中文策略只针对 clean/default profile；显式用户 locale preference 不在 M1 中强制覆盖。

### 2. Runtime layer: OpenClaw Image + GTClaw resources static patch

- 主路径是基于 confirmed running image 的 control-ui allowlist static patch。
- Patch 目标默认只允许：
  - `index.html`
  - `assets/index-M4TNVXB3.js`
  - `assets/i18n-B06L7jQN.js`
  - `assets/zh-CN-B26mMdbY.js`
- Patch 内容只允许 display 文案与默认 zh-CN 用户可见路径，不改 path/command/config/protocol/package literal。
- `OpenClaw Control` 用户可见 display 语境映射为 `GTClaw 控制台`；短 alt/title 语境可用 `GTClaw`。
- `Control UI` 如指 UI 产品表面，可改为 `控制台 UI` 或 `GTClaw 控制台`；如指 OpenClaw docs/CLI/config，则保留。
- 所有 ambiguous occurrence 默认保留，并进入 Commander decision list。

### 3. Runtime image delivery strategy

M1 不直接把 running pod 内文件原地修改作为最终交付。后续 tasks 必须选择一个可审计 delivery strategy：

1. **推荐**：以 confirmed OpenClaw Image 为 base，构建显式 patch image layer，只替换 allowlist control-ui files。新产物仍描述为 “OpenClaw Image + GTClaw resources”，不得改成自研 runtime 或 GTClaw Image。
2. **可接受的测试路径**：在本地 registry 使用显式 dev tag 进行 E2E；不得覆盖或依赖 `latest`。
3. **不接受的最终路径**：只在 running container 内 `sed`/手改文件后截图；这只能作为一次性 spike，不能作为 M1 可回滚交付。

如果后续必须使用新 image tag 进行 E2E，该 tag 只能作为环境产物记录在 patch manifest/evidence 中；不得把 repo 默认 OpenClaw Image 配置静默改成 floating tag。

### 4. Patch manifest as a required artifact

Patch manifest 必须在执行前后记录：

- image digest、package version、target path
- allowlist 文件、size、before SHA-256、after SHA-256
- 每个 changed display string 的 old/new/context
- 每个 ambiguous occurrence 的分类：`change UI display`、`preserve technical literal`、`defer`
- protected literal scan 结果
- rollback target：原 image digest 或原文件 hash
- E2E 环境与 route 入口

Manifest 可以先作为 `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/` 下 evidence 的一部分设计；实际写入 evidence 必须等 E2E/evidence gate 批准。

### 5. Authorized control-ui proxy amendment

T8 rerun showed the runtime static resources and zh-CN/GTClaw patch are present inside the pod, but the authorized GTManager route still targets the desktop shell:

- `/api/v1/instances/:id/proxy/` must remain the desktop/webtop route and must continue targeting `3001` or the existing desktop runtime config.
- `/api/v1/instances/:id/control-ui/` is the new route-specific authorized proxy for OpenClaw/GTClaw control-ui.
- `POST /api/v1/instances/:id/access?mode=desktop` maps to `/proxy/`; missing `mode` defaults to desktop.
- `POST /api/v1/instances/:id/access?mode=control-ui` maps to `/control-ui/`; `mode` is query-only for this amendment.
- Invalid mode returns `400 Bad Request` with no token/cookie. `mode=control-ui` for unsupported runtime types returns a deterministic no-token/no-cookie client error.
- Signed access claims and route handlers must enforce `instance_id`, user/admin subject, `token_type`, `access_mode`, server-selected `target_port`, and route prefix. Cookie path alone is not a security boundary.
- The control-ui handler must prove the full UI route, not title-only HTML: JS/CSS/font/image assets, redirect rewriting, `<base>`, SPA fallback, and WebSocket/SSE/streaming if used must all work under `/control-ui/`.
- The handler must not silently fall back to desktop `/proxy/` when control-ui upstream is unreachable.

### 6. Service and reachability amendment

- Current `podIP:18789` reachability has failed/currently unreachable; direct `127.0.0.1:18789` is supplementary in-pod evidence only.
- Primary runtime reachability path: bind control-ui to `0.0.0.0:18789` if the OpenClaw launcher supports it, while keeping exposure limited to pod/ClusterIP scope needed by GTManager.
- Bridge fallback is allowed only if bind cannot be configured. It must use an explicit selected pod-reachable target port, avoid `18789` conflicts, and include supervision/failure evidence.
- Service validation must distinguish the Service dial port from pod `targetPort`. A Service `.port` match alone is not enough if `.targetPort` points elsewhere.
- Misleading mappings such as `port:18789,targetPort:3001` or `port:3001,targetPort:18789` must fail validation or be explicitly handled by tests.
- Patching an existing Service must preserve desktop `3001`, selector, ClusterIP, Service type, and existing desktop behavior.

### 7. Deployed wrapper bundle amendment

Wrapper acceptance requires deployed browser evidence, not just source scans:

- Browser-loaded asset URLs/hashes must match the intended build output.
- A stale deployed bundle is a failing gate even if source contains GTClaw.
- Required wrapper route checks must run after the refreshed bundle is actually served.
- Evidence must omit credentials, tokens, token-bearing URLs, cookies, and secrets.

## File Ownership Map

| Area | Files / Paths | Plan role | Later write permission |
| --- | --- | --- | --- |
| Feature docs | `specs/gtmanager-gtclaw-m1-runtime-localization/spec.md`, `plan.md` | 当前 spec/plan artifacts | 当前只允许改 plan；spec 已经用户同意 |
| Future tasks | `specs/gtmanager-gtclaw-m1-runtime-localization/tasks.md` | 后续任务拆解 | 需用户明确批准 |
| Future evidence | `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/` | E2E evidence / manifest | 需 E2E gate 批准 |
| Wrapper i18n | `frontend/src/lib/i18n.ts`, `frontend/src/contexts/I18nContext.tsx` | 默认中文与 GTClaw wrapper 文案 | tasks 批准后才可改 |
| Wrapper surfaces | `frontend/index.html`, `frontend/src/components/AdminLayout.tsx`, `frontend/src/components/UserLayout.tsx`, `frontend/src/components/InstanceAccess.tsx`, `frontend/src/components/OpenClawDesktopOverlay.tsx`, `frontend/src/components/OpenClawConfigPlanSection.tsx` | 用户可见 runtime entry/display | tasks 批准后才可改 |
| Instance/admin routes | `frontend/src/pages/instances/CreateInstancePage.tsx`, `InstanceDetailPage.tsx`, `InstanceListPage.tsx`, `InstancePortalPage.tsx`, `frontend/src/pages/openclaw/OpenClawConfigCenterPage.tsx`, `frontend/src/pages/admin/SystemSettingsPage.tsx`, `InstanceManagementPage.tsx`, `AIAuditPage.tsx`, `AIGatewayPage.tsx` | route surface 审计与 display 文案 | tasks 批准后才可改 |
| Frontend services/types | `frontend/src/services/instanceService.ts`, `frontend/src/services/openclawConfigService.ts`, `frontend/src/types/*` | 默认只读，保护 API/type literal | 不建议改；除非 tasks 明确批准 |
| Backend access/API | `backend/internal/handlers/instance_handler.go`, `backend/internal/services/instance_access_service.go`, backend route registration | Add query-only `mode`, claim/cookie scope, invalid/unsupported mode behavior | 后续 implementation approval 后才可改 |
| Backend proxy | `backend/internal/services/instance_proxy_service.go` and tests | Add `/control-ui/` route-specific proxy while preserving `/proxy/` desktop behavior | 后续 implementation approval 后才可改 |
| Backend Service behavior | `backend/internal/services/k8s/service_service.go` and tests | Validate/ensure ServicePort mapping to selected pod target port while preserving `3001` | 后续 implementation approval 后才可改 |
| Deployment YAML | `deployments/k3s/clawmanager.yaml`, `deployments/k8s/clawmanager.yaml` | dry-run 和 image/default setting 参考 | 默认不改；除非用户另行批准 test-env/deployment mutation |
| Runtime image path | `/usr/local/lib/node_modules/openclaw/dist/control-ui` plus launcher/bind/bridge path in approved image/test env | running image baseline/patch target and control-ui reachability | 不在 repo 内；只能通过批准的 image/test-env flow 处理 |
| Old M1 | `specs/gtmanager-m1-branding-localization/**` | 管理面参考和方法参考 | 不得覆盖或 Close |

## Phase Plan

### Phase 0: Amended plan/tasks review

1. Commander writes amended `plan.md` and `tasks.md` from the T8 fail root cause, internal reviewer verdict, and GPT Pro `APPROVE WITH CONDITIONS`.
2. 用户 review amended plan/tasks。
3. 只有用户显式批准 implementation 后，才允许分发 implementation worker prompt。
4. 本 phase 不授权 frontend/backend/runtime/deployment/cluster/evidence mutation beyond the amended docs.

### Phase 1: Backend contract and route tests first

目标：先用 tests 锁定 access mode、route scope、ServicePort 行为，防止安全或 desktop regression。

- BackendAccessContractWorker 写 access mode query contract tests：missing mode desktop、valid modes、invalid mode `400 Bad Request` no token/cookie、unsupported runtime type no token/cookie、legacy tokens desktop-only、desktop/control-ui token cross-route rejection。
- BackendControlUIProxyWorker 写 route tests：`/proxy/` desktop behavior unchanged；`/control-ui/` prefix strip；`/control-ui/chat?session=main` forwarding；asset/base/redirect/SPA fallback；WebSocket/SSE/streaming if used；no fallback to desktop on upstream failure。
- ServicePortEnsureWorker 写 Service tests：`3001`-only Service not usable for control-ui；misleading `port`/`targetPort` combos fail or are explicit；patch preserves selector/ClusterIP/Service type/desktop `3001`。

### Phase 2: Backend implementation

目标：实现最小 authorized control-ui proxy contract。

- Add query-only `mode=desktop|control-ui` handling to access API.
- Extend signed claim and handler enforcement with access mode, target port, route prefix, token type, instance/user subject.
- Add route-specific cookies and keep token-bearing URLs out of logs/evidence.
- Add `/api/v1/instances/:id/control-ui/` route while preserving `/proxy/`.
- Add idempotent Service ensure/patch for selected control-ui target port.

### Phase 3: Runtime reachability and controlled test environment

目标：让 control-ui 从 pod network / Service 可达。

- Preferred: bind control-ui to `0.0.0.0:18789` if supported.
- Fallback: bridge to explicit selected pod-reachable target port with supervision/failure evidence.
- Do not add NodePort, LoadBalancer, Ingress, hostNetwork, broad Service exposure, or repo deployment default changes unless separately approved.
- Record exact `podIP:18789` failed probe command/output as historical context or explain bridge-port replacement.

### Phase 4: Frontend control-ui entry and wrapper bundle

目标：管理面 source 和 deployed browser bundle 同时满足 GTClaw runtime-facing behavior。

- Add frontend access client support for query-only `mode=control-ui`.
- Add GTClaw control-ui entry only on intended runtime surfaces; do not replace desktop iframe unless separately approved.
- Finish wrapper runtime-facing GTClaw copy while preserving GTManager manager brand and protected OpenClaw technical literals.
- Refresh deployed bundle through approved environment path and record browser-loaded asset URLs/hashes.

### Phase 5: Review and E2E rerun

目标：在真实 GTManager + runtime 路径上证明 M1 行为。

- Read-only review backend route scope, ServicePort behavior, token/cookie scope, frontend copy, deployed bundle evidence, protected literals, runtime reachability path, and test evidence.
- Run prerequisite checks: backend tests, frontend lint/build, deployment dry-runs, health check.
- Final E2E must prove desktop route regression, `access?mode=control-ui`, `/control-ui/`, `/control-ui/chat?session=main`, assets under `/control-ui/`, podIP/Service reachability, deployed wrapper GTClaw copy, default zh-CN runtime rendering, protected literal preservation.
- Any 404 or desktop fallback on control-ui route keeps E2E failed. No Close/write-back without E2E evidence plus explicit user approval.

## Worker Breakdown for Future Tasks

以下是后续 `tasks.md` 批准后才可分发的 worker 形态。Commander 不直接派 subagent；只给用户 prompt。

| Worker | 串并行 | 依赖 gate | Allowed files / paths | Forbidden |
| --- | --- | --- | --- | --- |
| DesignApprovalRecorder | 串行，当前 | GPT Pro `APPROVE WITH CONDITIONS` + user says OK | `plan.md`, `tasks.md` | 禁止 implementation、cluster、runtime、longterm |
| BackendAccessContractWorker | 串行，backend first | Amended tasks approved + implementation approval | `backend/internal/handlers/instance_handler.go`, `backend/internal/services/instance_access_service.go`, tests | 禁止 frontend/deployments/runtime/longterm |
| BackendControlUIProxyWorker | 可与 ServicePortEnsureWorker 并行 after contract shape | Backend access contract tests drafted | backend route registration, `instance_proxy_service.go`, proxy tests | 禁止改 `/proxy/` semantics; 禁止 frontend/runtime |
| ServicePortEnsureWorker | 可与 BackendControlUIProxyWorker 并行，write scope separated | Backend access contract shape stable | `backend/internal/services/k8s/service_service.go`, Service tests | 禁止 deployment YAML; 必须 preserve `3001` |
| RuntimeReachabilityWorker | 串行，after backend selected target port defined | user approves bind/bridge path | approved image/test-env path only | 禁止 repo defaults, NodePort/Ingress/hostNetwork unless separate approval |
| FrontendControlUIAccessWorker | 可在 backend API shape stable 后执行 | access contract stable | approved frontend service/hook/pages/components | 禁止 backend/deployments/runtime/longterm |
| WrapperBundleWorker | 串行 after frontend source changes | frontend source ready + test env approved | approved bundle refresh/test env path; evidence path only if approved | 禁止 source scope generated artifacts; 禁止 token-bearing logs |
| ReviewWorker | 串行 after implementation artifacts | backend/frontend/runtime/test-env outputs ready | read-only diffs, tests, evidence drafts, scans | 禁止 writes |
| EvidenceRerunWorker | 串行最后 | Review cleared + test env ready + user approves evidence collection | approved evidence file under `specs/.../evidence/*.md` | 禁止 source/deployment/longterm writes; 无 E2E 不得 Close |

## Verification Plan

### 前置 evidence

```bash
cd frontend && npm run lint
cd frontend && npm run build
cd backend && go test ./...
kubectl apply --dry-run=client -f deployments/k3s/clawmanager.yaml
kubectl apply --dry-run=client -f deployments/k8s/clawmanager.yaml
curl -sk https://localhost:30443/healthz
```

这些命令只能作为前置 evidence。它们不能证明 GTClaw runtime UI 已满足 PRD。

### Runtime baseline checks

后续任务可使用只读等价命令确认：

```bash
kubectl get pod -n clawmanager-user-1 clawreef-2-a1-local-registry-004259 -o jsonpath='{.status.containerStatuses[0].imageID}'
kubectl exec -n clawmanager-user-1 clawreef-2-a1-local-registry-004259 -c desktop -- sh -lc 'node -p "require(\"/usr/local/lib/node_modules/openclaw/package.json\").version"'
kubectl exec -n clawmanager-user-1 clawreef-2-a1-local-registry-004259 -c desktop -- sh -lc 'sha256sum /usr/local/lib/node_modules/openclaw/dist/control-ui/index.html /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js'
```

执行时不得输出 env、token、secret。若 pod/name/image 已变，先重新定位 running instance，不得硬套旧 baseline。

### Final E2E gate

E2E 必须覆盖：

1. `https://localhost:30443/healthz`
2. GTManager login
3. desktop route regression: `access?mode=desktop` and `/api/v1/instances/:id/proxy/` still render the desktop shell
4. control-ui access contract: `POST /api/v1/instances/:id/access?mode=control-ui` returns the expected route without exposing token values
5. authorized control-ui root: `/api/v1/instances/:id/control-ui/` renders `GTClaw 控制台`
6. authorized control-ui chat: `/api/v1/instances/:id/control-ui/chat?session=main` renders the chat route and visible Chinese text
7. control-ui JS/CSS/font/image assets and SPA/history fallback load under `/control-ui/`
8. runtime reachability probes: in-pod loopback as supplementary, same-namespace `podIP:<selected-control-ui-target-port>`, and Service `ClusterIP:<selected-service-port>`
9. exact command/output for the historical failed `podIP:18789` probe, or explicit bridge-port replacement note
10. runtime default zh-CN and GTClaw title/login/sidebar/breadcrumb/logo alt/chat welcome
11. wrapper runtime surfaces show GTClaw while GTManager remains the manager brand
12. browser-loaded deployed asset URLs/hashes match intended frontend build
13. OpenClaw Image / technical literal preservation
14. patch manifest before/after hash

没有 E2E evidence 和用户显式批准，不得写 `passes:true`、Close、complete、passed、accepted 或 long-term write-back。

## Risks / Blockers

1. **Baseline drift**: running image 不是 repo default `latest`，而是 local registry dev-arm64 tag；任何 digest/hash drift 都会阻断 patch。
2. **No sourcemap**: running image 无 `.map` 文件，minified JS patch 审计难度高；必须靠 allowlist、hash 和浏览器 E2E 控制风险。
3. **Route risk**: `/proxy/chat?session=main` 曾返回 404；必须通过新的 `/control-ui/chat?session=main` 独立验证，不能用 root/title 200 代替。
4. **Literal mispatch risk**: `openclaw dashboard --no-open`、`openclaw.json`、`.openclaw*`、path/protocol/package literal 误改可能破坏 runtime。
5. **Image delivery risk**: 直接 patch running container 不可复现；最终必须用可审计 image layer 或等价可回滚 artifact。
6. **Wrapper scope creep**: “普通功能文案”不得扩大成全 locale 翻译；M1 只要求 default zh-CN rendered UI path。
7. **Old evidence misuse**: 旧 M1 evidence 可作管理面参考，但不能证明 GTClaw runtime。
8. **Dirty worktree**: 当前仓库有大量非本轮改动；后续 worker 必须只处理自己授权的 files，不能 revert 用户改动。
9. **ServicePort mapping risk**: `service.Spec.Ports[].port` 与 `.targetPort` 混淆会导致 Service falsely usable；后续 tests 必须覆盖 misleading mappings。
10. **Token/cookie scope risk**: cookie path 不是安全边界；handler 必须强制校验 mode、target port、route prefix、subject 和 token type。
11. **Control-ui asset risk**: root HTML/title 可渲染但 JS/CSS/assets/history fallback 失败；E2E 必须证明完整 UI load。
12. **Stale deployed bundle risk**: source string scan 不能证明 deployed wrapper；browser-loaded asset URL/hash mismatch 必须 fail。

## Approval Gates Before Next Stage

1. 用户 review amended `plan.md` 与 `tasks.md`。
2. 用户必须显式批准 implementation，才能分发 backend/frontend/runtime/test-env worker prompt。
3. 用户必须显式选择或授权 runtime reachability path：preferred bind `0.0.0.0:18789` 或 bridge fallback with selected target port。
4. 用户必须批准任何 test environment mutation、frontend bundle refresh、runtime image/tag selection 或 Service exposure assumption。
5. 实现后必须先 Review，再 E2E rerun。
6. Close/write-back 必须在 E2E evidence 存在且用户显式批准后进行。

## Notes for Task Review / Future Workers

后续 review `tasks.md` 或分发 worker prompt 时，必须保留以下硬约束：

- 每个任务写明 allowed files 和 forbidden files。
- Backend access contract tests 必须先于 backend implementation。
- `/proxy/` desktop semantics 必须保留，不能改成 control-ui。
- `/control-ui/` route 必须覆盖 asset/base/redirect/SPA fallback 和 streaming/WebSocket/SSE if used。
- ServicePortEnsure 必须区别 Service dial port 与 pod targetPort，并保留 desktop `3001`。
- Runtime reachability 必须包含 bind/bridge 选择、podIP/Service probe、historical `podIP:18789` failed evidence。
- WrapperBundleWorker 必须证明 deployed browser asset URL/hash，而不是 source-only scan。
- EvidenceRerunWorker 必须最后执行。
- 任一任务不得输出 token/secret。
- 无 E2E evidence 不得标记 `passes:true`，不得说 Close。
