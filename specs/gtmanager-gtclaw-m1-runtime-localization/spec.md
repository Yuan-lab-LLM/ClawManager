# Feature Specification: GTManager / GTClaw M1 Runtime Localization

**Feature Branch**: `not-created`  
**Feature Short Name**: `gtmanager-gtclaw-m1-runtime-localization`  
**Created**: 2026-04-30  
**Status**: Draft - 已批准写入 spec；进入 plan、tasks、实现、E2E 或 Close 前仍需用户审核和显式批准  
**Input**: PRD "PRD-R1" 页面 `688a7d88-227f-479b-9207-091ae544d210`、Round 7 Commander 决策、G1-G7 只读 agent 报告、runtime image 只读探测结果，以及用户批准的 patch 边界。

## 范围摘要

本 feature 覆盖 PRD Round 7 中面向 runtime 的 M1 工作：

- **GTManager wrapper 层**：GTManager 里的用户可见 runtime 文案必须把盒内 runtime 用户体验称为 GTClaw；GTManager 继续作为管控面板/服务品牌。
- **Runtime control-ui 层**：真实 OpenClaw Image 内的 runtime control UI 必须默认中文，并在用户可见 UI 表面展示 GTClaw 品牌。
- **Image 与回滚层**：静态 patch 必须基于已确认的 running OpenClaw Image bundle baseline，并记录 patch 前后的精确 hash。

底层技术基座仍然是 **OpenClaw Image**。本 feature 不替换、不 fork、不重命名 OpenClaw Image，也不引入自研 OpenClaw/OpenSparrow runtime。

本 spec 只授权 Spec/Design 阶段。它不授权写入 `plan.md`、`tasks.md`、业务代码、image patch、部署文件或长期状态。

## 品牌边界

- **GTManager**：管控面板和服务品牌，显示在管理面。
- **GTClaw**：用户可见的盒内 runtime 品牌，显示在 runtime 入口和 runtime UI。
- **OpenClaw Image**：技术基座、包名、镜像来源、CLI/config/protocol 身份，以及兼容层。

用户可见 runtime 产品名默认使用 GTClaw。技术标识继续保留 OpenClaw/OpenClaw Image，除非后续有单独批准的迁移计划。

## 用户场景与测试

### User Story 1 - 管理面 runtime 入口显示 GTClaw (Priority: P1)

作为 GTManager 用户，我在管理面创建、查看、进入 runtime 时，看到的 runtime 产品名是 GTClaw，并且界面默认中文。

**优先级理由**：用户首先从 GTManager 进入 runtime。如果 wrapper 仍暴露用户可见 OpenClaw runtime 文案，就不满足 Round 7 品牌边界。

**独立测试**：使用干净浏览器 profile 登录 GTManager，检查 runtime 相关管理入口。面向用户的 runtime 标签使用 GTClaw 且默认中文；route path、API path、storage key 和技术性 OpenClaw 标识保持不变。

**验收场景**：

1. **Given** 干净浏览器 profile，**When** 用户打开管理面登录页并进入 runtime 入口流程，**Then** GTManager 仍是管控面品牌，runtime-facing 标签把盒内 runtime 描述为 GTClaw。
2. **Given** 已登录用户或管理员，**When** 用户打开 `/instances/new`、实例详情页、`/portal`、`/openclaw-configs`、`/admin/settings`、`/admin/instances`，或当前等价的 admin AI audit/gateway 入口，**Then** 用户可见 runtime 命名符合 GTClaw policy。
3. **Given** 同一组 route，**When** 检查实现和网络行为，**Then** route 名、API 调用、数据字段、storage key 和技术性 OpenClaw 标识没有被重命名。

---

### User Story 2 - Runtime Control UI 默认中文并显示 GTClaw (Priority: P1)

作为 runtime 用户，我可以进入真实 runtime control UI，包括 chat route，并在渲染后的 UI 中看到 GTClaw 品牌和默认中文文案。

**优先级理由**：只改 wrapper 不足以满足 PRD Round 7。runtime iframe / image 内页必须覆盖。

**独立测试**：通过 GTManager proxy 访问 running OpenClaw Image instance，验证真实 runtime control UI，包括 `/chat?session=main`，而不是只验证 manager shell。

**验收场景**：

1. **Given** running OpenClaw Image instance 和授权 proxy access，**When** 用户打开 runtime proxy root，**Then** runtime HTML shell 能加载，且不依赖 manager wrapper 文案来伪装 runtime。
2. **Given** 同一个 running instance，**When** 用户通过授权 proxy path 打开 `/chat?session=main`，**Then** chat route 被单独验证，并且结果独立记录，不用 proxy root 替代。
3. **Given** runtime control UI 在干净 profile 中渲染，**When** 检查 title、login gate、sidebar、breadcrumb、logo alt/title 和 chat welcome，**Then** 用户可见品牌显示 GTClaw 或 `GTClaw 控制台`，默认可见文案为中文。

---

### User Story 3 - 保留 OpenClaw Image 技术身份 (Priority: P1)

作为运维人员，我可以确认 GTClaw 用户可见品牌修改没有破坏 OpenClaw Image 技术契约。

**优先级理由**：多个 OpenClaw literal 是真实命令、路径、配置文件名、包标识或协议标记。误改会影响 runtime 行为。

**独立测试**：对比 patch manifest 和 runtime evidence。只有 allowlist 内的 display 文件发生变化；技术 literal 和 runtime contract 保持不变。

**验收场景**：

1. **Given** patch baseline，**When** 检查 image digest、package version、control-ui path 和目标文件 hash，**Then** 它们必须全部匹配锁定 baseline，才允许继续 patch plan。
2. **Given** patch 后产物，**When** 扫描 `openclaw`、`.openclaw*`、`openclaw.json`、`openclaw dashboard --no-open`、`dist/control-ui`、package name、path、protocol marker、API identifier、DB identifier、K8S identifier 和 image tag，**Then** 这些技术 literal 保持不变。
3. **Given** 存在不确定的 `OpenClaw` occurrence，**When** 无法证明它是纯用户可见 display 文案，**Then** 默认保留，并列入 Commander decision record。

---

### User Story 4 - 静态 Patch 可审计并可回滚 (Priority: P2)

作为 reviewer 或运维人员，我可以审计到底改了什么，并在需要时恢复原始 runtime control UI 文件。

**优先级理由**：M1 当前采用静态 patch 路径，因为 running image 没有可直接使用的 runtime UI source tree 或 sourcemap。必须用可审计和可回滚来控制风险。

**独立测试**：检查 patch manifest。manifest 必须包含 baseline、allowlist、before/after hash、排除的 technical literal、不确定字符串决策和 rollback target。

**验收场景**：

1. **Given** 已锁定的 runtime baseline，**When** review patch design，**Then** 只命名 allowlist 文件和字符串分类。
2. **Given** patch 后产物，**When** 对比 before/after hash，**Then** changed files 只落在批准的 allowlist 内。
3. **Given** 需要 rollback，**When** 运维人员按照记录的 rollback path 操作，**Then** 可以恢复原 image digest 或原文件 hash。

## 边界情况

- running image digest、package version、control-ui path 或目标文件 hash 与锁定 baseline 不一致。这是停机条件；patch planning 必须暂停，并重新获取 image baseline。
- `/chat?session=main` 通过 GTManager proxy 返回 404，但 proxy root 返回 runtime HTML。proxy root 可访问不能替代 chat route evidence。
- minified bundle 中的 `OpenClaw` 可能是 display copy，也可能是 technical literal。无法确认的情况默认保留。
- zh-CN translation key 缺失时，默认中文路径可能 fallback 到英文或用户可见 OpenClaw 文案。
- 用户已有显式非中文 locale preference。M1 要求 clean/default profile 使用中文，但不应破坏用户显式偏好，除非后续 plan 和用户批准另行决定。
- repo 默认 image tag 与 running image tag 不一致。M1 patch design 必须使用 confirmed running image baseline，不能依赖 floating `latest`。
- 静态 patch 可能留下 source map 引用不匹配，因为 running image 中没有 `.map` 文件。这必须作为调试和维护风险记录。

## 需求

### 功能需求

- **FR-001**: 用户可见 runtime 产品名在 M1 runtime 入口和 runtime UI 表面必须使用 GTClaw。
- **FR-002**: GTManager wrapper 中创建、列出、查看、进入、配置或审计 runtime instance 的表面必须应用 GTClaw 用户可见文案策略。
- **FR-003**: Wrapper route 覆盖必须包括 `/instances/new`、实例详情、`/portal`、`/openclaw-configs`、`/admin/settings`、`/admin/instances`，以及当前等价的 admin AI audit/gateway 入口。
- **FR-004**: Runtime control-ui patch design 必须针对 confirmed running OpenClaw Image control-ui baseline，不能针对推断的 vendor bundle 或 floating `latest`。
- **FR-005**: Baseline mismatch 必须停止 patch flow。Mismatch 包括 image digest、package version、control-ui path，或任一目标文件 hash 与 locked baseline 不一致。
- **FR-006**: Patch target files 必须限定为 `index.html`、`assets/index-M4TNVXB3.js`、`assets/i18n-B06L7jQN.js`、`assets/zh-CN-B26mMdbY.js`，除非后续有用户批准的 plan 扩展 allowlist。
- **FR-007**: Runtime 默认中文行为必须覆盖 clean/default profile 中渲染出来的 M1 runtime 路径。非中文 locale 不作为 M1 全量翻译 gate，但不得被主动破坏。
- **FR-008**: Runtime 用户可见 title、login gate、sidebar、breadcrumb、logo alt/title 和 chat welcome 必须按 display context 显示 GTClaw 或 `GTClaw 控制台`。
- **FR-009**: Lowercase `openclaw`、OpenClaw Image 标识、CLI command、config name、package name、path、protocol marker、storage key、API identifier、DB identifier、K8S identifier、runtime status field 和 image tag 必须保留。
- **FR-010**: `.openclaw*`、`openclaw.json`、`openclaw dashboard --no-open` 和 `dist/control-ui` 必须作为 technical literal 保持不变。
- **FR-011**: Patch process 不得使用 OpenClaw-to-GTClaw 的全局 search/replace。
- **FR-012**: 每个 ambiguous OpenClaw occurrence 在 implementation 前必须被分类为 `change UI display`、`preserve technical literal` 或 `defer`，并记录 Commander decision。
- **FR-013**: Patch manifest evidence 必须记录 image digest、package version、target path、file size，以及 patch 前/后的 SHA-256 hash。
- **FR-014**: 旧 `gtmanager-m1-branding-localization` evidence 只能作为 GTManager 管理面参考和 E2E 方法参考；不得作为 runtime GTClaw acceptance evidence。
- **FR-015**: 在存在 E2E evidence 之前，任何 artifact 都不得标记为 `passes:true`、accepted、passed、complete、closed，或写回 long-term feature state。
- **FR-016**: E2E evidence 和 patch record 不得输出 credential、token、secret、`.env`、`.codex/auth.json` 或 `.codex/config.toml` 内容。

### 关键实体

- **GTManager Wrapper Surface**：管理面中向用户暴露 runtime 创建、访问、配置、审计和 instance 状态的页面与组件。
- **Runtime Control UI**：running image 中从 control-ui path 服务出来的 OpenClaw Image 静态 UI。
- **Runtime Patch Baseline**：patch 前必须锁定的 image digest、package version、control-ui path、目标文件名、大小和 hash。
- **Patch Manifest**：用于 review 的记录，包含 allowlist 文件、精确改动、before/after hash、保留 literal、ambiguous-string decision 和 rollback 信息。
- **E2E Evidence Packet**：最终验证记录，覆盖 GTManager wrapper、真实 runtime UI、route 行为、默认中文、GTClaw 展示和技术标识保留。

## Runtime Patch Baseline

当前 baseline 事实来自对 running instance image 的只读探测。只要与下表任一项不匹配，就必须停止 patch planning，直到生成并 review 新 baseline。

| Baseline Item | Expected Value |
| --- | --- |
| Runtime image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434` |
| Image ID digest | `sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8` |
| Package | `openclaw@2026.4.14` |
| Package path | `/usr/local/lib/node_modules/openclaw/package.json` |
| Control-ui path | `/usr/local/lib/node_modules/openclaw/dist/control-ui` |
| Service port | `3001` |

| Target File | Size | Baseline SHA-256 | M1 Role |
| --- | ---: | --- | --- |
| `index.html` | 3,395 | `f313071437a1b8c432024d3f83af4056fb672a4fe15b93be8b2291dcaac0115c` | Browser title / HTML shell display |
| `assets/index-M4TNVXB3.js` | 707,545 | `e89d5e55d89aaae7bc64598b949335425df7626f57b12a8780426a1911315882` | Main runtime UI display bundle |
| `assets/i18n-B06L7jQN.js` | 42,702 | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` | Default locale and i18n loader behavior |
| `assets/zh-CN-B26mMdbY.js` | 23,247 | `2afe4858d80c81247f01e21198011a78180de12e72f567b5606fe9355dbfd2c1` | zh-CN visible copy |

已观察到但不属于默认 M1 patch target 的相关文件：

- `assets/index-DfTb2bb1.css`，size 225,499，SHA-256 `550d3c50a88bdcfce654bd6b36b4f074ab4bfbc4f591dab65d92ea5ed387051b`
- `assets/zh-TW-BIOFMTXl.js`，size 23,855，SHA-256 `7ee96ef6ee77651f1b4e51df9116587d92b8665d024f807f8430a9718f2d0020`

## Patch Policy

### 允许修改的 display 文案

- 用户可见 browser title 语境中的 `OpenClaw Control` 可改为 `GTClaw 控制台`。
- Login gate 的 brand alt/title text 可改为 GTClaw display text。
- Sidebar brand title/alt text 可改为 GTClaw display text。
- Breadcrumb/header display text 可改为 GTClaw display text。
- Chat welcome 和 logo alt/title display text 可改为 GTClaw display text。
- zh-CN default-path 中真实渲染的功能标签、提示、按钮、菜单、空状态、warning 和用户可见 error text 可以翻译或修正；前提是它们是 rendered UI，而不是 technical literal。
- 用户可见 `Control UI` product-copy 语境可改为 `控制台 UI` 或 `GTClaw 控制台`；如果它指向 OpenClaw docs、CLI 或 config，则保留。

### 必须保留的 technical literal

- `openclaw` lowercase identifier
- `OpenClaw Image`
- `clawmanager-openclaw-image`
- `/usr/local/lib/node_modules/openclaw/dist/control-ui`
- `dist/control-ui`
- `.openclaw*`
- `openclaw.json`
- `openclaw dashboard --no-open`
- package name 和 package version
- CLI、config、path、protocol、storage、localStorage、module、client identifier
- API path、DB schema/table/field name、K8S object name、runtime status field、image name、image tag、image digest
- 历史 docs 和 evidence；单独批准的产品文档更新除外

### Ambiguous Occurrences

不确定字符串默认不改。每个 ambiguous occurrence 必须记录为以下三类之一：

- `change UI display`
- `preserve technical literal`
- `defer`

不允许 broad OpenClaw-to-GTClaw replacement。

## OpenClaw 技术标识保留清单

本清单是 acceptance criteria 的一部分。任何对这些项目的变更都需要单独用户批准，并且不在本 spec 范围内：

- OpenClaw Image identity 和 image lineage
- `openclaw` runtime type 和 lowercase literal
- `clawmanager-openclaw-image`
- `.openclaw*` archive/profile/status convention
- `openclaw.json`
- `openclaw dashboard --no-open`
- `/usr/local/lib/node_modules/openclaw/dist/control-ui`
- `dist/control-ui`
- package name/version 和 Node module path
- runtime CLI/config/protocol/storage/client marker
- API、DB、K8S、runtime status、image tag、image digest 和 compatibility identifier

## 成功标准

### 可观测结果

- **SC-001**: GTManager 管理面 login 和 shell surface 仍显示 GTManager 作为 control-plane brand。
- **SC-002**: `/instances/new`、实例详情、`/portal`、`/openclaw-configs`、`/admin/settings`、`/admin/instances` 和当前等价的 admin AI audit/gateway 入口，用户可见 runtime product naming 使用 GTClaw。
- **SC-003**: confirmed running instance proxy root 可访问，并且结果单独记录。
- **SC-004**: Runtime `/chat?session=main` 通过授权 GTManager proxy 独立验证并记录；proxy root 成功不能替代它。
- **SC-005**: Runtime title、login、sidebar、breadcrumb、logo alt/title 和 chat welcome 显示 GTClaw 或 `GTClaw 控制台`。
- **SC-006**: Runtime default path 在 clean/default profile 中为 M1 用户可见路径渲染中文。
- **SC-007**: OpenClaw Image 和受保护 technical literal 在 evidence scan 与 patch manifest 中保持不变。
- **SC-008**: Patch before/after hash 差异只落在批准的 allowlist 文件中。
- **SC-009**: 在 E2E evidence 和用户显式批准前，不写 long-term feature state，也不设置任何 `passes:true`。

## E2E Gate

E2E 是最终 acceptance gate。Build、lint、unit test、dry-run、kubectl 只读探测、curl check 和 hash check 都只能算前置 evidence。

最小 E2E 覆盖：

1. `https://localhost:30443/healthz` 可访问。
2. GTManager login 可用，且不暴露 credential 或 token。
3. 选择 running OpenClaw Image instance，并在不输出 secret 的前提下获取 proxy access。
4. Runtime proxy root 返回 runtime control UI shell。
5. Runtime `/chat?session=main` 被测试，并作为独立 route evidence 记录。
6. Runtime 默认中文行为在 clean/default profile 中可见。
7. Runtime title、login gate、sidebar、breadcrumb、logo alt/title 和 chat welcome 显示 GTClaw 或 `GTClaw 控制台`。
8. GTManager wrapper runtime surface 显示 GTClaw，同时 GTManager 仍是 manager brand。
9. OpenClaw Image 和受保护 technical literal 被证明保持不变。
10. Patch manifest 包含 before/after file size 和 SHA-256。

如果 `/chat?session=main` 仍然返回 404，M1 runtime E2E 不得关闭。结果必须记录为 open risk，或者由用户显式批准基于真实 runtime route 行为调整 route target。

Close/write-back 只允许在 E2E evidence 存在并且用户显式批准 Close 之后进行。

## 旧 M1 Evidence 边界

现有 feature `specs/gtmanager-m1-branding-localization` 及其 evidence 只能作为以下参考：

- GTManager management-plane branding 和 Chinese default behavior
- 先前 asset 和 route verification method
- K3S health 和 access method reference
- OpenClaw technical identifier preservation examples

它不能作为以下证据：

- GTClaw runtime branding
- OpenClaw Image internal UI localization
- runtime iframe/control-ui acceptance
- 任何“GTClaw 应该从 runtime 用户可见 UI 中缺席”的结论

旧 M1 feature 不得作为本 feature 的一部分被 Close 或覆盖。

## 不在范围内

- 在用户批准前写入 `plan.md` 或 `tasks.md`。
- 从本 spec gate 修改 frontend、backend、deployment、image、runtime bundle 或业务代码。
- 重命名 path、command、config filename、package name、protocol identifier、API path、DB schema、K8S name、image name、image tag、`.openclaw*`、`openclaw.json`、`openclaw dashboard --no-open` 或 `dist/control-ui`。
- 全局 search/replace OpenClaw 为 GTClaw。
- 完成所有非中文 locale 翻译。
- 替换、fork 或重命名 OpenClaw Image。
- 依赖 floating `latest` 作为可复现 patch baseline。
- Close 或写回旧 M1 状态。
- 读取、写入或暴露 credential、token、`.env`、`.codex/auth.json` 或 `.codex/config.toml`。

## 外部输入 / 阻塞项

- 后续 plan gate 必须决定 allowlist runtime 文件的精确 patch mechanics。
- 后续 evidence gate 必须重新测试 `/chat?session=main`，因为之前 proxy 探测显示该 route 返回 404，即使 proxy root 返回 runtime HTML。
- 如果 running image baseline 发生变化，必须先重复 image mapping，再进入 patch design。
- 如果 ambiguous string 无法可靠分类，则保持不变，直到 Commander/user 决策。

## Open Questions

当前 Commander 默认决策后，没有 spec-blocking open question：

- 用户可见 `OpenClaw Control` display copy 在中文默认路径上映射为 `GTClaw 控制台`。
- 短 alt/title 语境中，如果 `GTClaw 控制台` 过长，可以使用 `GTClaw`。
- `/chat?session=main` 保持独立 E2E gate；如果它仍返回 404，除非用户显式批准替换为真实可达 chat route，否则 M1 runtime E2E 不得关闭。
