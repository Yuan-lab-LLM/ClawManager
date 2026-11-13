# Runtime 升级闭环开发验收 · 2026-11-12

## 交付状态

- 开发源码：`ed6ea86cdd2340423167070bad563a0946d7bcd3`，候选版本 `v2026.11.12-runtime-upgrade`。
- 仅在54构建候选：`clawmanager.local/clawmanager@sha256:41fd24e4bc16bc64562223e0915d8db7ec3a5888ea430d54bcadf248dfe31dcf`。
- 2092个Git归档输入逐项校验；四个Go二进制、前端和OCI镜像均在54构建。OCI基础层和运行配置保留。候选已导入54的containerd，没有修改任何业务Deployment。
- 按用户“先不着急恢复”的要求，**未部署候选、未进行真实Runtime升级或恢复**。当前应用仍为 `df78f764d0520632ad81ab94822ea414fbe88773` / `v2026.11.12-create-return`。
- 15:01:15UTC只读检查：18项实例均creating；四类Runtime为0副本；历史DSH操作仍`needs_attention / acquiring_protection`。没有改旧journal、镜像设置、工作区或实例。

## 行为

- 新增现有升级入口的准备阶段：管理员POST`/api/v1/admin/runtime-rollouts/prepare`，冻结完整运行/休眠池范围，持久化`rollout_wake`操作；只按completed自动休眠journal和实际Deployment UID、模板、generation、PVC、操作标记恢复旧镜像容量，再等待原生Pod/注册就绪。
- 普通调度仍排除零副本。手工零副本、旧证明、换PVC、未知模板或控制器身份明确阻塞。全局admission、删除意图和leader校验保持有效。
- 准备就绪保留15分钟预约；预检与最终提交携带同一`preparation_id`，提交和消费预约在同一SQL事务内完成。最终准入再次核对完整原生范围和所有删除意图，防止混合池漏升级及人审期间再次休眠。
- 缩容中断按同操作副本CAS标记、原生身份/存储/Pod集合及源、目标SQL绑定/代数证明收敛；不能证明时保留保护。`needs_attention`准备不能被自动清理或取消。没有伪造C1、直接改副本或清除历史记录。
- 只用已完成、原生验证的业务升级作为默认镜像；新实例镜像卡与后端一致，Pro和额外自定义卡不覆盖。CM重启保留可证明的退休源池及隔离实例旧源池，未知绑定、UID/digest/代数漂移或新升级覆盖会拒绝。历史tag兼容不使整套CM启动失败；新DSH提交必须使用digest。
- 管理页面保留OpenClaw预检/确认；准备轮询与取消按账户/参数/卸载隔离。未知POST结果持久保留并阻止重提，只读核对历史后显式选择；时间截断、时钟偏差、tag转digest及仍受保护的操作有回归。
- API发布CLI及说明位于`deployments/runtime-upgrade/`，仅用CM管理员API，保存操作ID与提交阶段；`--preparation-id`不能重置未知的后续提交阶段。未来发布要求写入仓库及上级`AGENTS.md`，禁止外部直接改Runtime镜像/副本或SQL。
- OpenCode不安全滚动替换限制保留；工具在任何唤醒写入前停止，不声称已新增OpenCode原生数据安全迁移能力。

## 实际校验

所有执行均在10.130.81.54。本地只做源码、文档、静态检查和传输。

- 普通Go回归：services52.085s、repository0.028s、handlers91.951s、k8s8.864s；server编译通过。数据库opt-in另行执行。services原生脚本契约需要相邻AgentsRuntime的已跟踪`plugins/openclaw-redis-team/dist/index.js`，测试环境补齐该依赖后通过，未在本地生成它。
- 相关MySQL回归：`go test ./internal/services ./internal/repository ./internal/services/k8s -run 'Test(Runtime|Pool|Committed|SystemImageSetting|IsImmutable|OpenClaw|Upgrade)' -skip '^TestRuntimeFailureMySQLContract$' -count=1`通过，分别128.050s/1.361s/2.248s。升级通用fixture补齐生产111工作区观测迁移，避免旧schema使实际路径提前失败。
- 最后准备及混合池/删除意图/不确定发布/保护取消回归通过6.131s。最终Git归档准备相关race通过：services7.891s、k8s1.129s。
- 中断缩容矩阵与取消、原生身份、目标持久绑定校验通过；相关服务race61.866s、k8s完整包8.848s及聚焦race1.259s。
- CLI HTTP隔离fixture19项通过，含准备processing、旧状态恢复、丢失响应、OpenClaw确认及禁止重提。前端最终构建、21项服务回归、隔离浏览器与现有恢复/分组契约通过；1440/1280/768/390和五语言检查，查看了中英文宽/窄屏及受保护状态实际截图。
- 后端及交付/UI审查的重要问题均修复并复核：完整范围发布/接续、维护入口绕过、删除意图、历史tag、CLI阶段重置、准备ID丢失、未知POST重提、时钟精度和误取消保护。

### 未宣称通过的检查

全量可选数据库校验仍有既有fixture问题：`TestAutoStopResumesFromReleasedFaultRecoveryMySQL`构造空lifecycle后解引用nil instanceRepo，已在原开发基线独立复现；`TestRuntimeFailureMySQLContract`固定使用无密码root，不读取测试密码，与本轮随机密码隔离MySQL不兼容。未修改业务逻辑来迁就这些fixture，未将其报为通过。相关范围及普通回归的实际命令/结果如上。

真实镜像升级、Native Gateway端到端恢复、业务池重新就绪及当前历史状态修复均未执行；不能把fake Kubernetes/MySQL、CLI HTTP fixture或浏览器mock称为已完成真实业务升级。

## 证据与复现

54目录：`/root/cm-runtime-upgrade-20261112/`。

- `release-ed6ea86cdd23/`：最终候选、构建日志、输入校验与OCI元数据。
- `related-mysql-tests-final.log`、`services-unit-final.log`、`merged-unit-tests-final.log`、`final-preparation-race.log`：校验记录；merged-unit初次缺少原生脚本的环境问题由services-unit最终记录覆盖。
- `recovery/REPORT.md`、`delivery/`、`ui-build.log`及`/tmp/cm-upgrade-{uncertain,protected,preparing}-*.png`：子任务验证与截图。
- `baseline-auto-stop.log`：旧基线同一nil fixture失败；`business-state-readonly.json`：业务现场只读状态。

测试使用独立namespace及临时数据库，没有使用应用数据库进行测试DDL。私有凭据与诊断不进入仓库。没有推送或合并，保留用户`.gitignore`和无关文档修改。
