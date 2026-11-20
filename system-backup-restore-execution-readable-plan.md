# 系统备份/恢复执行计划易读版

> 派生自 `docs/system-backup-restore-division-plan.md`，合同版本：`system-backup-plan.v12`（2026-09-09）。
> 本文只提供阅读导航，不定义字段、枚举、API、状态跳转、错误或安全规则。与主计划或机器合同不一致时必须修改本文，禁止以本文覆盖主计划。

## 1. P0 交付结果

P0 在 K8s/k3s 的 cluster/single-node 部署中形成系统备份闭环：外部加密 artifact、签名 recovery catalog、隔离恢复演练、只读 preflight/diff、手动 prune、Console、审计和可观测性。生产覆盖恢复、backup-agent、系统级 schedule 和自动 prune 留在 P1。

验收维度及其不可合并的判定关系见主计划第 1、4、13、18 节；本文不复制状态、资格或 promotion 条件。

## 2. 开发门禁

1. **P0-0a**：完成 capability spike，随后提交 OpenAPI、JSON Schema、enum、DTO parity 和 schema registry。文件未落盘时仍是待冻结规范。
2. **P0-0b**：完成 migration、统一审计/redaction、operation/strong-auth幂等账本、dependency health与installation state、独立 controller、DB claim、maintenance gate、artifact lease、RBAC/NetworkPolicy 和 kill switch。
3. **P0a**：完成多Job capture barrier、独立staging resource、发送前冻结完整bytes的external-action账本、manifest candidate/finalizer权限分离、无持久化明文的加密artifact、catalog registration、六个必选part和backup verifier。
4. **P0b**：完成 catalog import、机器可判定的隔离 drill、独立 SELECT-only verifier、preflight/diff、精简后的权威 resource inventory 和安全 cleanup。
5. **P0c**：完成 artifact verify/health、可接管catalog scan、一次性数据库confirmation与prune retry、control/evidence GC、本地当前matrix promotion、指标/告警和CI/release四矩阵证据汇总；promotion不写recovery catalog。

后续阶段不得补交前置合同或安全门禁。

## 3. 执行路径导航

### Backup

执行顺序为任务创建、controller 接管、隔离 capture、gate 协调、seal、独立 publish、验证、清理和终态确认；权限、状态、commit/cancel、artifact 格式及 retry 规则全部见主计划第 4、7～10、14 节。

### Drill

执行顺序为 artifact 协调、兼容性检查、resource intent、provision、restore/normalize、只读验证、终态和独立 cleanup；安全删除与崩溃接管规则见主计划第 12 节。

### Preflight 与 prune

Preflight 与 prune 的执行边界、互斥和 retry 顺序见主计划第 4.5、12.3、13 节；本文不复述状态或转换条件。

## 4. Owner 导航

| Owner | 业务域 | 主要交付 |
| --- | --- | --- |
| A | users、AI Gateway | audit/redaction、strong-auth/权限配置、Secret/KEK/签名、evidence store/registry/download安全 |
| B | instances、workspace、OpenClaw | quiesce、capture Job-set/relay、CSI/HostPath staging、restore resource/normalizer |
| C | Teams、Skill Hub | Redis/object collector、artifact/finalizer/catalog、health/prune provider执行 |
| D | resources、控制面 | 机器合同、系统表、controller状态编排、API/Console、promotion、监控和集成 |

公共 collector/normalizer/verifier 接口由 P0-0a 统一冻结；各 owner 不得创造第二套顶层字段、状态或错误。

## 5. 验收导航

- 数据与 verifier：主计划第 10、11、17.3 节。
- 部署矩阵与 promotion：主计划第 4.3、17.4、18 节。
- 可观测性：主计划第 15 节。
- 测试与清理：主计划第 17 节。

## 6. P1 边界

P1 才新增 agent、schedule/自动 prune 和生产 restore run。生产恢复必须先使用不会被业务数据库覆盖的独立 control journal，并重新评审 control-evidence sidecar 的导入/冲突策略。
