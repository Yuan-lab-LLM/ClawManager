# mount 分支提交整理（2026-11-05）

两个仓库均保留在 `mount` 分支。本次整理只创建本地提交，不推送、合并或更新集群；本地不编译、不构建镜像。测试、Go 格式检查及已有镜像构建均在测试节点 `10.130.81.54` 完成。

## 提交范围

| 仓库 | 提交 | 内容 |
| --- | --- | --- |
| AgentsRuntime | `82bdd6e` | root-squash NFS 多槽位隔离、实例容器生命周期、原生维护及文件所有者检查 |
| ClawManager | `82017f6` | NFS 多槽位设计与验收计划 |
| ClawManager | `5592e9d` | NFS 配置、槽位终止证明、文件与 Team 写入协调、浏览器生命周期及验收记录 |
| ClawManager | `fe37fd7` | 空闲超时停止显示为“自动休眠 / Auto Sleep”，普通停止保持原显示 |
| ClawManager | `889b4d4` | 状态菜单按生命周期筛选，自动休眠与普通停止分别查询；团队版类型菜单限定四类 Runtime |

本轮收尾整理合并筛选页导入，去掉多余常量别名、类型断言及非空断言，并将请求状态类型收紧为 `Instance["status"]`。保留原有 URL 兼容、默认值和筛选行为，不调整 NFS 或运行时协议。

## 本轮核对

- 两仓库相对各自 `main` 的本轮变更中，252 个非 `docs/` 文件与测试节点快照逐文件 SHA-256 一致，差异为 0。前端快照包含本轮整理后的代码。
- 226 个 Go 文件经测试节点 `gofmt -l` 检查，无格式差异；两个仓库的提交差异通过 `git diff --check`。
- 前端 16 项自动休眠、状态筛选及自动停止相关测试通过；`tsc -b --force` 通过。
- 后端调用者范围、查询参数、自动休眠/普通停止计数与分页、无活动记录的旧实例，以及 IEI 停止原因投影相关测试通过。MySQL 用例使用独立测试库。
- 本轮验证日志位于节点 `/root/cm-commit-prep-20261105/`：`source-check.json`、`format-check.json`、`frontend-tests.log`、`frontend-types.log`、`backend-tests.log`。

本轮前端复验命令：

```sh
node --experimental-strip-types --test \
  scripts/instance-list-filters.test.mjs \
  scripts/instance-lifecycle-status.test.mjs \
  scripts/runtime-auto-stop-ui.test.mjs
node node_modules/typescript/bin/tsc -b --force
```

本轮后端复验命令（测试节点预先配置独立 MySQL 端口和密码）：

```sh
go test ./internal/handlers ./internal/repository \
  -run 'TestListInstances|TestGetInstanceSummaryUsesCurrentUser|TestInstanceListAutoSleepFilter|TestIEIInstanceViewPreservesStopReasonForDisplay' \
  -count=1
```

NFS 功能、Runtime 五模块完整回归、并发检测及真实集群验收沿用已完成记录，本轮未重跑这些未修改的功能。具体结果及发布边界见 [NFS 验收记录](../../nfs-multislot-acceptance.md)、[镜像记录](../../nfs-multislot-images-20261105.json)和 [部署说明](../../../deployments/k8s/lite-slots/nfs-rootless.md)。

## 已部署版本与边界

NFS 验收使用独立命名空间 `clawmanager-nfs-lab`；完整 NFS 分支尚未整体部署到原业务池。

主测试入口 `https://10.130.81.54:30443` 已发布自动休眠显示及筛选 API 的限定修改，镜像为 `clawmanager.local/clawmanager@sha256:18655aad4d654452725a839cd84d9fc7f58e59ab0cde829ce55b884779781955`，版本提交标记为 `889b4d4-filters-only`。该镜像基于当时已部署后端，只加入对应修改，不代表完整 `mount` 分支镜像。

该次发布已通过线上中英文 Chromium 验证、URL 刷新保留筛选、自动休眠 84 个与普通停止 3 个的独立查询和分页检查，以及全部 119 个 JavaScript 静态资源校验。实例 Pod 身份、容器身份和自动停止配置保持不变。发布证据位于节点 `/root/cm-instance-filters-20261105/`。本轮收尾代码未再次发布。

历史设计、评审草案和 UI 预演资产继续保留为未跟踪文件，不纳入本次提交；`.deploy`、访问配置、私有凭据、构建产物和故障隔离账本均未加入提交。两仓库 `main` 分支保持不变。
