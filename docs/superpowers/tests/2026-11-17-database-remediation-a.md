# 数据库治理第一批：查询与正确性修复

- 日期：2026-11-17 开始，2026-11-18（北京时间）集成验证。
- 设计：`docs/superpowers/specs/2026-11-17-database-capacity-remediation-design.md`。
- 原代码基线：`acf8665a`；开发分支：`database`。
- 状态：首批基础修复已完成集成验证，未发布；没有迁移或删除业务数据，没有更新 Runtime 镜像。

## 已实现的交付范围

| 范围 | 行为 |
| --- | --- |
| 费用统计 E1 | 使用存在性/明确调用身份过滤，避免同 trace 多次调用放大费用；核对实例、会话、状态及时间范围 |
| 模型费用明细 | 全请求窗口聚合，每条费用只统计一次；费用使用自身记录的模型身份；会话明细返回全部实际使用行，不再套用全局前五名与未使用模型填充 |
| 币种 | 当前 API 没有汇率换算语义，混合币种显式返回错误；空币种兼容为 USD，同一币种保留原标签；不修改已结算账本 |
| 审计分页 E2 | SQL 对完整 trace 集合过滤、聚合、统计和分页，不再只在最近 1,000 条记录内搜索；保留只有审计事件的 trace |
| 会话/管理员列表 | SQL 完成当前页与汇总，管理员查询按集合聚合，不再逐实例加载全量会话；保留受管类型、实例状态、搜索、时间和合规提示语义 |
| 正文读取 | 列表与会话统计不读取请求/响应正文；真正的调用详情仍保留正文读取能力 |
| 显示元数据 | 当前页用户名按 ID 分批读取，只选择 ID、用户名，不再逐用户读取完整用户记录 |
| 游标错误 E3 的安全子集 | 游标持久化失败返回失败，内存游标和更新时间保持原值；成功后再更新内存；保留原失败持久重放与去重路径 |
| Redis 读取 E5 | 初始 XREAD COUNT 100，单次响应 8 MiB、数组元素总预算 100,000、嵌套深度 16、行长 4,096 字节；解析错误丢弃连接 |
| Redis 积压恢复 | 只有预算错误才从同一游标缩小 COUNT，最多 100→50→25→12→6→3→1 七次；单条仍超限明确报错，不越过可靠消息 |
| Redis 消费可观测性 | 阻塞、原因变化、恢复时记录固定原因分类，排除原始响应、正文和凭据；不按每次重试刷日志 |

所有新 SQL 查询都有有限超时。审计及会话分页的总数和列表使用显式只读、可重复读事务；同时间记录有稳定次级排序。

## 回归证据

全部 Go 编译与测试都在 `10.130.81.54`。本地仅编辑、格式化、传输源码及检查 diff。节点证据目录：`/root/cm-database-20261117/`。

隔离命名空间 `cm-database-test-20261117` 使用临时 MySQL 8.4.8、Redis 7 Pod 和拒绝全部 Pod 网络进出的 NetworkPolicy，测试通过节点 loopback port-forward 访问。MySQL 测试须验证 `cm_database_test_marker` 并只创建/删除自己的随机命名测试库；测试资源不连接应用库。

观察到的 RED：

- E1 固定样本应得到费用 6，旧查询得到 1008；正文随列表加载；重复 trace 费用被重复分配给不同调用模型；查询错误被忽略。
- 审计 SQL 分页能力缺失，旧最近窗口不能提供完整历史；用户名补充仍逐条访问。
- 游标提交失败被当作成功，内存游标提前推进。
- Redis 巨大 bulk/array、整体预算、深度、协议边界缺少限制；固定 COUNT 无法从过大的一批消息中继续消费。
- 会话/管理员 SQL 分页能力缺失；单会话详情仍先读全部会话；混合币种没有拒绝，模型明细被截成五行。

专项结果与详细命令保留在节点证据目录。代码原始基线的 repository/services/handlers 三个包测试通过，分别耗时 0.026s / 61.209s / 106.960s。

首轮合并验证中，repository 和 handlers 通过。services 的真实 Redis fixture 暴露原有时序问题，修正后重新运行整个服务包通过；没有将第一次失败记录当作成功。最终统计如下，跳过记录包含子用例。

真实 Redis 初始失败为 `cleanup_and_crash_reaper_preserve_active_users: orphan user retained`。使用原始 `acf8665a`、相同隔离 Redis 和启用参数也能复现。fixture 客户端通过 port-forward 关闭后，Redis 可能尚未收到 FIN；维护器此时保留仍可见的连接身份是正确行为。测试改为最多 2 秒观察 CLIENT LIST 中连接实际退出后，再执行原来的孤儿回收断言，没有修改生产维护器或删除断言。修正后的完整真实 Redis 契约测试通过，耗时 11.229s；独立审查通过。

| 检查 | 结果 / 证据 |
| --- | --- |
| repository 全包 | 61 个顶层 PASS，207 个跳过记录，0 失败，1.882s；`evidence/integration-all.jsonl` |
| services 全包复跑 | 1547 个顶层 PASS，361 个跳过记录，0 失败，74.112s；`evidence/integration-services-final.jsonl` |
| handlers 全包 | 327 个顶层 PASS，1 个跳过记录，0 失败，106.950s；`evidence/integration-all.jsonl` |
| 独立审查 | E1、E2/SQL 列表、E3 游标安全子集、E5 和全分支交叉审查通过；重要问题均已修复或撤回有风险的实现 |
| 定向 race | repository 3.013s、services 1.931s，均通过；`evidence/integration-race.log` |
| SQL / 详情专项合并 | MySQL 和服务回归通过，含历史检索、混合币种、全部模型行、精确详情接线；`evidence/integration-reporting.log` |
| 服务端构建 | 节点执行 `go build -trimpath -buildvcs=false ... ./cmd/server` 通过；没有构建或发布镜像 |
| 服务端 SHA-256 | `3aee1efc4b9271ef5855432d0df1a58fffde7d2d084bdbdf727800e9d7646e7b` |
| 源码一致性 | 节点 `evidence/source-manifest.json` 校对本地改动源码与参与测试的 integration 目录；本地 `git diff --check` 通过 |

测试公共环境为 `GOTOOLCHAIN=local`、`GOPROXY=off`、`GOSUMDB=off`、`GOMAXPROCS=2`，复用节点 Go 1.26.8 工具链和模块/构建缓存。普通测试 `CGO_ENABLED=0`，race 测试 `CGO_ENABLED=1`。

```sh
# 在节点 integration/backend 目录，使用已校验的隔离 loopback fixture。
TEST_DATABASE_MYSQL_PORT=33317 \
TEST_RUNTIME_EVENT_REDIS_PORT=33318 \
TEST_TEAM_CONTEXT_REDIS_URL=redis://127.0.0.1:33318/0 \
TEST_MANAGED_REDIS_DISPOSABLE=1 TEST_MANAGED_REDIS_PORT=33318 \
go test -json -p 2 ./internal/repository ./internal/services ./internal/handlers -count=1
```

已有测试的显式跳过及其具体原因保留在 JSON 日志中，不能把跳过计为通过。真实 Redis fixture 修正后只重跑受影响的整个 services 包，repository/handlers 源码未发生后续变更。


临时命名空间及 MySQL/Redis Pod 已删除，两个 port-forward 进程已停止；节点源码、日志和构建产物保留用于审查，没有发布到运行中的应用。

## 审查纠正

独立审查发现，给每条事件提前写重放 intent 的方案会引入两个风险：成功投影后完成标记失败会重复执行业务更新；长时间投影可能与未加所有权锁的 outbox sweep 并发。该方案已撤下。

最终只保留经过复核的游标提交修复，不增加每条成功事件的持久历史。现有投影与失败重放仍是多次写入，完整事务、崩溃恢复和副作用幂等 **尚未完成**。不得据此启用可靠 Stream 裁剪。

费用明细的前五名截断和币种混算经独立审查发现后补回归修复。原始扣费账本没有改写；没有声称已完成现网历史对账。

## 仍待后续批次完成

- 持久会话/实例/trace 汇总与稳定游标 API。当前 SQL CTE 仍扫描所选历史元数据，执行计划可能包含临时表和 filesort；本批只证明应用侧按页物化与语义正确，不能声称达到 1 万实例或 P95 目标。
- 默认/最长查询时间窗口和长范围导出入口；现有 API 的时间范围没有被静默截短。
- Trace 详情的明细分页与正文分段读取。本批没有截断现有详情数据。
- E3 完整投影事务与恢复协议、E4 可靠日志补偿、E6 失败备份残留回收。
- 正文引用去重与生命周期、已完成活动/outbox/终态历史维护、日志归档/月表、备份恢复适配。
- 可靠 Redis 安全裁剪、容量配额/背压、删除 Team 后回收、实测资源预算及 maxmemory 发布。

因此本批不等于“四项治理全部完成”，也不等于已证明在线 MySQL 表或可靠 Redis 历史不再增长。已存在的 Runtime UI 通知 10,000 条上限保持原样。
