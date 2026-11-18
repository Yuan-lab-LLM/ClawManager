# Runtime 实时通知流 10,000 条上限

2026-11-04，测试节点 10.130.81.54；用户确认将 `clawmanager:runtime-events` 限制为最多 10,000 条。所有测试、构建和部署在测试节点执行，本地只编辑、格式化及检查差异。

## 实现

`runtime_events.go` 定义固定上限 10,000；`redisBus.XAdd` 仅对该精确键名追加 `MAXLEN = 10000`，通过 Redis 原生 XADD 在同一操作中追加新事件并裁剪最旧事件。使用精确限制，未使用近似限制。其他流保持原写入方式；没有增加新队列、定时任务或管理配置项。

已确认该流用于管理页面实时通知；业务事实和调度恢复另有持久化记录。该变更不裁剪 Team inbox、可靠投递、去重标记或会话凭证，也不修改通知内容格式。限制的是条数，不是单条大小或 Redis 总字节数。

## 验证

- 在独立临时 Redis 中先复现失败：预置 10,003 条，旧实现新增一条后变为 10,004 条。
- 新实现验证：首次写入将积压裁剪至 10,000，保留正确的新旧边界；16 个并发写入后仍为 10,000。
- 独立 Team 流预置 10,003 条后正常新增到 10,004，验证它未被连带裁剪。
- 实机测试要求专用 localhost 端口及隔离 Redis 标记，缺少标记时拒绝删除或写入测试键。
- 带 race 检查的普通构建和分钟级测试构建各通过 5 项定向用例，0 失败、0 跳过。

## 测试集群结果

部署前分批裁剪该流的最旧积压，避免首次在线写入一次处理数百万条；随后更新单副本 CM 后端。保留前端、Runtime 镜像、当前环境配置及弹性调度测试计时规则。

| 项目 | 更新前 | 更新后 |
| --- | ---: | ---: |
| Runtime 事件条数 | 6,559,528 | 10,000 |
| Redis 数据内存 | 15.88 GiB | 24.90 MiB |
| Redis RSS | 15.97 GiB | 163.36 MiB |

更新后的 6 次采样均为 10,000。CM Ready，版本接口可访问；临时命名空间 `cm-redis-retention-test-20261104`、Redis、端口转发已清理，构建服务已停止。

Redis 64 GiB 容器上限保持不变。本次未手动重写 AOF 或删除持久化文件；当前内存下降不代表旧 AOF 已完成压缩，后续降配需另外核对重启回放峰值。

## 镜像与证据

- 已部署的测试镜像：`clawmanager.local/clawmanager@sha256:144c23a580da4d274954c1412ad3a50510cbde1a0a42e8e855cb55952159294a`。
- 已构建但未部署的普通日规则镜像：`clawmanager.local/clawmanager@sha256:b3a0a4e87e32b6dd055bbe685f03ca197022cc8e551ebc19e8f95fbfc3fc197f`，同样包含 10,000 条上限。
- 节点证据目录：`/root/cm-pod-elastic-20261104`。
- `retention-red.log`、`retention-green.log`、`retention-final-green.log`、`retention-results/`：回归记录。
- `retention-before.json`、`retention-deploy.log`、`retention-deployment-verification.json`、`retention-final-verification.json`：存量迁移、镜像更新、条数及内存校验。
- 含部署配置的更新前快照仅保留在节点权限为 0600 的文件中。
