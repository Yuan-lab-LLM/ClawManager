# Runtime 类型隔离与镜像可变唤醒验收

## 发布

- 源码：`cc781a9a9383d3650978bb3dca288f772ca6f952`；版本：`v2026.11.12-runtime-isolation`。
- 已发布到 `https://10.130.81.54:30443`。
- CM 镜像：`clawmanager.local/clawmanager@sha256:89303b708eb38d5ea5bd0a1ceb8f055261c092c461ea255eb556d6a15a290cc2`。
- 2096 个 Git 归档输入逐项校验；二进制、前端及镜像均在54构建。实际服务 Pod 的镜像 digest、四个二进制 hash 和稳定启动文件均核验一致。
- CM 发布仅修改应用镜像；没有外部修改 Runtime 镜像、副本、身份字段或数据库。后续容量唤醒及扩容均由 CM 自身执行。

## 变更

- 调度、缩容、容量创建、池删除、升级准备、升级确认和孤儿恢复按 Runtime 类型检查冲突。保留短事务准入锁及原有同类型/逐实例规则，允许恢复所需的容量创建。
- 历史操作从明确请求类型、逐项冻结证据、Pod、绑定及请求实例列表识别范围。缺失范围、未知类型和部分列表证据保持阻止并指出操作 ID；某个已识别类型的保护不会阻止其他类型。
- 普通自动休眠恢复接受镜像、镜像身份环境变量及 generation 增长；继续校验 UID、角色、PVC/PV/挂载、命令、安全配置、操作标记、残留 Pod 和写入 CAS。
- 新升级准备复制当前有效配置到新的冻结记录，原休眠日志不改写。准备后的配置/副本变化仍拒绝；已正常唤醒并扩到多个副本的普通池保留其容量处理路径。

## 54 上的校验

最终精确归档对应上述源码，所有测试和构建均在54执行：

- 普通 Go 回归：services 51.479s、repository 0.029s、handlers 91.932s、k8s 8.865s；server 编译通过。
- 相关 MySQL：`go test ./internal/services ./internal/repository ./internal/services/k8s -run 'Test(Runtime|Pool|Committed|SystemImageSetting|IsImmutable|OpenClaw|Upgrade)' -skip '^TestRuntimeFailureMySQLContract$' -count=1 -timeout=12m`：105.328s / 2.846s / 2.268s，通过。
- 聚焦竞态检查覆盖类型/部分历史范围、准备/取消、普通休眠、扩容后历史池、冻结准备和中断恢复：services 34.135s、repository 2.734s、k8s 1.183s，通过。
- RED→GREEN 复现镜像/环境变化、旧镜像候选选择、跨类型保护、部分历史范围误放行、已唤醒扩容池误拒绝。两轮审查的 Important 已修复，最终规范及质量复审通过。
- 未宣称全量可选 MySQL 套件通过；原有固定无密码 fixture 的 `TestRuntimeFailureMySQLContract` 未纳入本轮矩阵，既有 AutoStop 空依赖 fixture 问题没有用业务逻辑修改掩盖。
- HTTPS 健康检查200；164个静态文件逐字节核对；页面路由、128个浏览器 NodePort、8444目标端口及就绪 EndpointSlice 核验通过。

## 真实实例验收

- 发布前9个 OpenClaw 实例（102—110）均为 creating；发布后 Runtime 由 CM 自动唤醒并扩到2个就绪 Pod，Hermes 恢复到1个就绪 Pod。原来的 DSH 保护记录仍保持 needs_attention。
- 新建验证实例111（OpenClaw）、112/113（Hermes）均进入 running。实际浏览器打开 OpenClaw 主会话输入框；Hermes 等待宿主 data-ready=true 后，确认 Backend ready、输入框可点击，并查看实际截图。未发送模型消息。
- 浏览器功能检查使用 ignoreHTTPSErrors=true，因此不把它宣称为新的证书信任链验收。首轮脚本使用了不适用的 Hermes iframe 名称及 OpenClaw Shadow DOM 文本断言，已修正；最终检查使用实际组件及连接就绪状态。
- 用户9个原实例均保留且不再 creating。最后16:14UTC检查均为 stopped；平台现有“空闲5分钟”自动停止策略保持启用。**后续确认108的停止未完成，不能仅凭 stopped 判断为正常休眠，详见下方更正。**本次类型隔离发布验收期间没有对用户原实例提交 start、stop、delete 或修改自动停止策略。
- 验证实例111/112/113均已通过 CM API 删除，逐项404确认。仅验证实例临时禁用自动停止，随实例删除清理。隔离 MySQL namespace 与端口转发已删除。

## 剩余环境限制

- 原 DSH 操作 `runtime_mgmt_24a0c3c4-429f-4ded-8b52-266c8acbdbe8` 仍为 needs_attention/acquiring_protection，未清除或伪造其证明，已不阻止 OpenClaw/Hermes 创建。
- 使用正式 CLI 发起 Hermes 当前 digest 的升级验证：准备 `runtime_mgmt_1410b7a0-adce-446f-8e0a-157866c88a81` 从 pending 到 completed/ready；最终提交返回409，没有发布任何 rollout。当前镜像名 `clawmanager.local` 只在节点本地镜像缓存中使用，CM 容器内无法解析该域名（curl退出6），镜像元数据读取无法继续。
- 已核对 rollout 历史为空，并通过 CM API 取消本次未使用的 ready 预约；没有清除 DSH 保护或外部强推。因此**没有宣称完成真实 Runtime 镜像滚动发布**。后续发布需先提供 CM 能访问的实际镜像仓库。
- OpenCode/DSH 既有原生容量问题尚未处理，本轮没有它们的真实页面验收，也没有放宽 OpenCode 原生安全升级限制。

## 证据

54目录：`/root/cm-runtime-isolation-20261112/`。

- `release-cc781a9a9383/{candidate-image.json,rollout-result.json,http-check.json}`：镜像、运行 Pod、二进制与静态文件核验。
- `unit-final.log`、`mysql-final.log`、`race-final.log`：最终源码检查结果。
- `before-final-release.private.json`、`running-check.private.json`、`final-live-result.json`：发布前后实例、DSH保护和清理结果。
- `browser-functional-result.json`、`hermes-ready-browser-result.json`、`native-*.private.png`：真实浏览器检查；Hermes以第二份就绪记录为最终依据。
- `registry-probe.json`、`hermes-native-release.json`、`hermes-unused-preparation-cleanup.json`：升级准备、镜像仓库限制和预约清理。

私有凭据、工作区数据和诊断快照不进入仓库。

## 108 停止未完成：后续调查与恢复

### 实际原因

- 用户截图中的 `instance operation already in progress` 对应实例108（openclaw-008）。旧 idle 操作 `17787f1a-46f4-4bdc-a1bd-e923a3735f57` 持续失败，绑定仍保留；每次90秒重试占据生命周期锁，启动无法继续。这不是正常休眠。
- 实例在延迟就绪后仍保留15:19的启动时间和旧活动窗口，16:03启动后不满一分钟即进入自动停止，未获得完整300秒运行窗口。
- CM 忽略 native preflight 的 busy/unknown/unsupported 状态，先提交 stopped/draining，再调用优雅停止。Agent 又在原生准入接受之前保存 StopIntent、关闭网络准入；busy拒绝后记录仍保留。主进程随后退出，重试永久处于 unknown。
- 源码还确认：stopped/draining 会阻止安全租约续期，而未接受优雅停止时，网络凭据失效可触发主进程终止。该机制能解释时序，但没有取得本次具体租约403日志，未把它宣称为已证实的进程退出原因。

### 已发布的 CM 修复

- 源码 `54ed38d26389655de9fd47597a52d27bfdfe6403`；版本 `v2026.11.13-stop-recovery`。
- 镜像 `clawmanager.local/clawmanager@sha256:7d8e8d0c90e5b45883efd97a8316b3ed812d2b93141dc4a9217ba413a45573e0`。
- 停止提交之前要求 native preflight 支持证明且状态为 idle；不再为已经明确无法接受的停止撤销运行权限。模型空闲策略仍保留，未增加强制停止回退。
- 已有活动记录的 creating/error→running 或新 generation 就绪会刷新启动时间和活动窗口；重复健康报告不延长窗口。首次就绪尚无活动记录时，仍依靠 Initialize 后建立的保守观察窗口，不宣称所有历史元数据路径已统一。
- 自动停止在独立 CM 故障恢复持有该实例预约时让出锁。先更新重试时间，再让行，避免最早10项长期占据队列而阻塞其他类型。
- 旧绑定、停止保护和写锁证明继续保留；没有用清锁、直接SQL、force-delete或伪造优雅停止回执绕过保护。

### 54 实测与限制

- 先通过 CM `mode=recovery` 提交仅覆盖Pod33/实例108的操作 `runtime_mgmt_b8537c01-60d2-45ae-b590-9b4a3e0fd815`。发布修复后该操作完成，独立节点助手确认旧容器退出和写锁账本，journal为released，旧idle操作正常完成。
- 在重新启动之前，对工作区 `/workspaces/openclaw/user-1/instance-108` 的506个文件逐项SHA256比较：数量相同，缺失/变化0，新增/变化0。
- 第一次启动进入generation2后又遇到 `PROXY_POLICY_NOT_READY`：旧容器虽已重启，持久槽位book仍有原停止隔离记录。仅恢复停止记录不等于该槽位已可复用；首次浏览器检查没有通过，未宣称恢复成功。
- 使用 CM 自带容量API按openclaw类型增加1个同镜像Pod，原Deployment副本从1到2。源配置检查需要可调度的当前证明，第一次请求在源Pod暂停时409、未扩容；随后通过CM恢复配置证明、成功扩容并再次暂停旧Pod33。没有外部修改Runtime镜像、副本或身份字段。
- 第二次启动后，108在新Pod35上进入running/generation3，仍使用原工作区。真实浏览器确认原生OpenClaw输入框可见且可编辑，未发送模型消息。首个脚本额外等待了隐藏的侧栏按钮，移除不适用断言后通过。
- 其他8个用户原实例仍为stopped/generation1，没有删除或启动它们；DSH历史needs_attention保护保持不变。旧Pod33暂停调度，保留原生隔离记录，没有强行清除。
- **剩余限制：**这次是CM侧修复与实际实例恢复。瞬时preflight不能消除随后原生接受的竞态；Agent停止意图接受顺序和独立故障回执后的槽位复用协议仍需后续Runtime修复。Runtime镜像没有变化，沿用上文镜像仓库不可访问的发布限制，不把这次同镜像容量扩展称为滚动镜像升级。

### 校验

- RED→GREEN复现：四种Runtime的busy请求被错误提交；首次延迟就绪未重置空闲窗口；自动停止和独立恢复争抢执行。新增回归覆盖运行状态/空停止记录、绑定保留、重试队列轮转、运行报告/完整Update两条就绪路径、300秒边界及重复报告幂等。
- 普通Go回归：services 51.755s、repository 0.029s、handlers 91.961s、k8s 8.875s；server编译通过。
- 相关MySQL：repository（AutoStop/Idle/ManualStartIntent/RuntimeReportedStateRecoveryFence）7.518s；services（AutoStop/RuntimeRecoverySQLReleaseLifecycle/RuntimeManagementMySQLForceEviction）11.492s，通过。
- 聚焦race+MySQL：services 4.143s、repository 1.443s，通过。
- 扩展Idle矩阵的既有 `TestIdleLegacyAgentProControlPlaneUnchanged` 因fixture缺少desired_config_revision_id字段失败；在未修改的HEAD基线上0.468s同样复现，不宣称全量可选MySQL套件通过。此前空依赖/空updated_at的恢复fixture已修正，使已有恢复断言真正进入生产分支。
- 2097个归档输入逐项核验；被测试backend与精确提交归档逐文件一致。四个二进制、前端、镜像均在54构建；实际运行镜像与二进制hash、稳定启动/恢复助手文件均核验一致。
- HTTPS健康检查200，164个静态文件逐字节一致；128个浏览器NodePort/8444和就绪EndpointSlice正常。本轮未验证OpenCode/DSH实际页面，既有容量限制仍在。
- 独立只读代码复核的队列公平性问题已修复并加入断言。隔离MySQL测试namespace和33333端口转发已清理。

证据保存在54的 `/root/cm-stop-recovery-20261113/`：发布目录 `release-54ed38d26389/`、`unit-checks.log`、`race-check.log`、`workspace-check.json`、`browser-functional-result.json` 及私有恢复/启动/容量记录。审查报告位于本机 `/tmp/cm-stop-recovery-20261113/{protocol-review.md,fix-review.md}`。
