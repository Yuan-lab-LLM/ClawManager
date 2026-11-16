# 统一创建流程 · 54 发布记录

## 已部署

- 正式入口：`https://10.130.81.54:30443/instances/new`。
- 源码：`a0be77a0ba0bb4e288243c091ca0df3d2c69424c`，版本 `v2026.11.12-unified-create`。
- 镜像：`clawmanager.local/clawmanager@sha256:1238d74901e02c7a663dec70ebf80efb9a5992788fdbb1b8f60a64f9d2e8ef7a`。
- `clawmanager-system/clawmanager-app` generation 31，Ready 1/1；应用镜像、四个二进制、运行时稳定文件与候选产物核对通过。
- 2075 个源码输入已按归档清单校验；164 个实际静态资源逐字节校验通过。全部前端、Go 和镜像构建均在54执行。本地仅准备源码、文档及传输脚本。
- 未推送/合并；保留用户未提交的 `.gitignore` 和无关文档。Runtime 镜像、模板和部署配置没有更改。

## 行为与回归

- 单个/批量使用同一表单，批量入口 `/instances/new?batch=1`。真实已启用类型、自动默认镜像、负责人选填、省略空 owner、Pro 数值和配额、原有配置方案与支持范围保持有效。
- 高级配置第一项为文件导入；支持选择、更换、移除。多行环境变量和技能选择同步右侧，敏感值遮蔽。
- 部分创建失败保留已持久实例；后置安装失败通过响应 header 找回实例；未知批量结果禁止直接重建；导入失败只重试导入。
- 空/错误/加载、账户隔离、重复提交、名称/环境校验、五语言及 2048/1440/1280/768/390/320 宽度均有浏览器回归。
- 已在54观察旧向导缺少批量开关的 RED，再实现新流程；后端模板和 CORS header 回归均 RED→GREEN。
- `go test ./internal/handlers -count=1`：通过，91.954s。
- `go test ./internal/middleware -count=1`：通过，5.025s。
- 创建页及相关辅助文件 TypeScript 无产物检查、聚焦 ESLint：通过。
- `node --experimental-strip-types --test scripts/instance-creation.test.mjs`：5/5。
- `node tests/instance-create.browser.mjs`：通过，含后置安装失败、部分成功、导入重试、镜像变化后类型隔离、Pro-only 批量保护、未知结果保护、折叠编译校验。
- `node tests/instance-workspace-layout.browser.mjs`：通过。
- 管理 E2E 的旧批量弹窗断言已更新；Playwright 测试发现通过，未声称执行了完整管理 E2E。
- 审查的重要问题已处理；实际桌面及窄屏截图已检查。机械设计检查未返回问题。

## 实际页面/API验收与运行就绪阻塞

真实浏览器从正式新建页创建并保存了5个临时实例：OpenCode Lite 79、DSH Lite 80、OpenClaw Lite 批量81/82、Hermes Pro83。单个跳转新实例、空负责人、环境变量名称、批量共享负责人/描述、CPU=2/内存=4GB/存储=10GB的Pro数值均核对通过。批量逐项结果与禁止整批重复提交已验证。

**运行就绪验收未通过，不能称为完整端到端通过。** 240秒等待后5项仍在 creating。诊断证实不是页面少调用 Start：Lite 的 creating 状态已经提交调度意图；对临时79的单次人工 Start 返回409“Start is already pending”。该启动猜测对应的临时测试已撤销，正式代码及提交保持原有接口语义。

现有集群的 Runtime 运维状态阻塞了冷启动：

1. DSH 历史自动缩容 `runtime_mgmt_24a0c3c4-429f-4ded-8b52-266c8acbdbe8` 停在 `acquiring_protection / needs_attention`，全局扩容 admission 因此拒绝。
2. 该操作原证明要求原 Deployment 保持1副本和原Pod UID，当前同UID Deployment已0副本、旧Pod消失。现有官方 Cancel/Retry 无法证明“尚未 resize”，不能通过改数据库/伪造证明结束它。
3. 四池的自动休眠快照与当前模板、generation 或 sleep annotation 不匹配，解除上述项后也不能直接自动唤醒。
4. Pro83 的PVC已绑定并attach，仍在拉取既有Hermes Pro镜像 `2ade970…`，未出现imageID；未归因于本次表单。
5. Runtime scheduler lease正常续约，HOSTNAME正常，不需要调整领导者身份。

因此 OpenCode/DSH 原生composer/子origin证书的运行态验收未完成。128个NodePort（31600–31727）、8444目标端口、App selector和Ready EndpointSlice均核对通过。全程保留上述保护，没有手工扩容绕过、清数据库、改历史journal或替换Runtime镜像。

临时实例79/80/81/82/83均通过正常删除API清理，未动其他实例。恢复后的健康、版本、Ready副本及库存另存 final-health.json。运行池恢复需要作为独立运维恢复处理。

## 证据

54目录：`/root/cm-unified-create-20261112/`。

- `release-a0be77a0ba0b/`：构建日志、源码/产物hash、rollout-result.json、http-check.json、正式页面截图。
- `e2e-instances.private.json`：实际创建及字段验收，包含所有临时ID；运行等待断言失败保留为未通过。
- `cleanup-result.json`、`final-health.json`：临时实例清理和最终健康。
- `diagnostics/`：只读运行池、运维记录及休眠快照对比。
- `/tmp/cm-instance-create-boundary.log`、`/tmp/cm-instance-create-browser/`：创建页完整浏览器回归与截图。

私有诊断与凭据留在54，不入仓库。

## 创建完成导航与 Runtime 只读复盘补充

- 导航修复源码 `df78f764d0520632ad81ab94822ea414fbe88773`，版本 `v2026.11.12-create-return`，镜像 `clawmanager.local/clawmanager@sha256:2c434ed1e10cb996ad41640b3f8b62d75eb8cc6c7ebf043cc0efd159958d1daf`，generation32，Ready1/1。
- 单个/批量全部成功后通过 `/instances` 返回列表工作台并选中首个新实例；最后一项创建或导入重试成功同样返回。替换创建页的历史记录，部分失败继续保留处理入口。
- 浏览器回归先在原实现观察“successful creation returns through My Instances”断言失败，再验证修改后通过；覆盖9项全部成功、部分失败、最后一项创建/导入重试及既有六宽度/五语言场景。TypeScript无产物检查、创建页ESLint通过。测试API全部拦截。
- 全部构建和测试在54执行，2075输入校验、164实际静态文件逐字节核对、应用四个二进制与稳定文件核对通过。健康/创建页200。14:13:58UTC库存18项，原18项ID全部保留；四个Runtime仍为0副本，未执行恢复。证据：`release-df78f764d052/`、`/tmp/cm-create-return-red.log`、`/tmp/cm-create-return-green.log`。

用户要求暂不恢复、先复盘。只读确认18项（Hermes9、OpenClaw9）均creating，Runtime列表为空，历史DSH操作仍needs_attention。此前发布脚本 `/root/cm-force-delete-20261112/upgrade-idle-runtimes.py` 只检查绑定及running/creating实例数量，直接修改休眠Deployment镜像/模板和镜像设置，未同步休眠journal，也未检查未完成调度操作；这解释了Hermes/OpenClaw快照与当前版本不符。此前发布记录确认测试池恢复原0副本（11:41:17UTC），与DSH自动缩容开始（11:41:15UTC）时间重叠，支持并发修改假设；缺少操作审计，不能断言最后一次副本写入来自哪个命令。

当前服务对needs_attention在全局admission中阻塞，提前中断的缩容只尝试证明原副本/Pod集合未变后清理；原集合已消失，无法收敛。待分配异常被归类为control-plane pending，不展示具体阻塞，因而用户持续看到creating。建议后续依次补生命周期中断状态收敛、统一发布/维护入口及休眠版本迁移、基于共享资源冲突范围隔离阻塞、显式零副本引导和阻塞状态、冷启动/中断/休眠升级验收；本轮没有实现这些Runtime改动。
