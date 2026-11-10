# Runtime 无绑定残留恢复：测试环境验收（2026-11-10）

## 发布与范围

- 分支：`preview`，不创建 PR。功能提交 `208321c`；存储检查修正 `366b04c`；控制器归零等待修正 `bdf21a587d35ef846809a3828c592e3b5a1fdb16`。
- 最终管理应用镜像：`clawmanager.local/clawmanager@sha256:a1e067e01aaf77c7babde55d636c7610c7d56972609f5ff29dae12558e7ecf75`。五个 Linux 二进制的构建信息一致，运行中 API version 与 SHA256 均已核验；完整哈希在节点验收目录 `deployment-image.json`。
- 主环境：`10.130.81.54:30443` / `clawmanager-system`。仅退役已核对的无绑定旧源，不启动、删除或强改现有118/166实例的业务意图。未清空业务 SQL、PVC 或项目数据，未制造 Agent teardown ACK。

## 五个旧源的产品 API 恢复

通过管理员 `preflight` 和 `evict(mode=orphan-recovery, source_pod_uid=...)`，一次一个源；失败保留原任务，使用同一操作 Retry。

| Runtime ID | 固定 Pod UID | 持久操作 ID | 结果 |
| --- | --- | --- | --- |
| 132 | `7ab50b09-bfcc-4068-89cf-7671fcffd385` | `runtime_mgmt_832a9491-85f1-40b1-bead-fbf9a2233b2c` | completed / confirmed |
| 147 | `0556ab2a-b84b-4c82-85c5-302eb4e571d2` | `runtime_mgmt_cc129191-d355-4f0c-aaee-81579a9d6a96` | completed / confirmed |
| 150 | `fb1b5391-b520-4aaa-b02f-afbea187d91f` | `runtime_mgmt_151a953a-13eb-4a9d-8bdf-3a05f7504b6e` | completed / confirmed |
| 155 | `dd903a0a-68c0-4b0d-872d-4b2335b1ecbc` | `runtime_mgmt_fdbdeb01-f5d8-43f2-87c3-abbac8985469` | completed / confirmed |
| 135 | `a46388e5-fb58-4a3e-8542-3b1ad24ca3dc` | `runtime_mgmt_4197a657-4e28-4a36-befb-d78bc32210d0` | completed / confirmed |

- 五个旧 Pod 均已实际退出；每个任务保存精确 UID/Node UID/boot/存储范围的肯定终态及独立 CRI 退出凭据。
- 五个原 controller 均保留模板、replicas=0、scheduling-enabled=false。对应 SQL Runtime 行保留审计，state=deleted、scheduling_paused=1、used_slots=0；这些源的有效 binding=0。旧心跳不能复活这些已退役记录。
- 现场 Runtime inventory=0；节点独立核验旧源无 RUNNING/UNKNOWN 容器，主命名空间也没有仍运行但已无 Kubernetes Pod 归属的容器。本次创建的5个辅助 Job及Pod均已按操作ID/UID清理，凭据仍保留在管理任务中。
- 132任务在阻塞期间经历应用版本更新，仍使用原固定镜像/源UID继续并完成，验证了持久恢复进度。
- 实机暴露的相同容器挂载目录误判已修复：比较实际 PVC/PV/NFS/hostPath，独立 `/ltt-test-20261106` 存储不再误报；共享后端、别名和未核实后端仍阻断。
- 155在旧版本中遇到 ReplicaSet 异步补建并被邻居保护拦下。事件记录确认临时 `...-kg4dv` 在07:38:33 UTC创建/删除，随后原任务重新核验继续完成。最终版本等待Deployment新generation及所有owned RS确认desired0后才显式删除旧UID；135在该版本上一轮自动完成，现场无临时补建事件。

## 历史 NFS 测试执行资源清理

仓库 `docs/nfs-multislot-acceptance.md` 和现场完整快照确认 `clawmanager-nfs-lab` 是11月5日独立验收资源，主CM DB中的Runtime记录/绑定均为0。

按精确UID退役18个测试Deployment、9个standalone测试Pod及2个零副本测试StatefulSet。29个原Pod UID已独立CRI核验无RUNNING/UNKNOWN容器；HPA/Job/Cron/DaemonSet均为0，未删除namespace、PVC/PV、NFS export、数据或历史writer ledger。

保留以下两个已终态、正在删除的原Pod对象及原finalizer，作为退出凭据/名称屏障，不占运行槽位；没有通过删除finalizer换取空列表：

- `clawbrowser-75202-153618ed6bc07c24d4b0c76605edc47b-0` — `fe426e50-96dc-4a35-a983-a5c4cff765c6`
- `clawbrowser-75202-51cad8a4c55ca5fd3438aadb6254d745-0` — `cb3e52cf-6eb4-46a4-9d1a-6e5ef59f0d54`

## 数据和邻居验证

- 118项目7个文件的SHA256与操作前一致。
- 工作区PVC UID保持 `01637c57-4ea8-414d-a0c9-4e19233ffe8a`；HTTPS证书SHA256保持 `c8631124b043a57248503b06be6eeac306b772b217d7018c373c65838c7c0cb0`，未更换证书。
- `clawmanager-ltt-system/clawmanager-app-756d7cdbdf-6z4t8` 的Pod UID、所有ContainerID及restartCount保持不变。
- 主实例记录仍为118和166，没有为了清理幽灵资源而删除它们。

## 验收时容量阻塞与随后观测

07:52 UTC验收时118为stopped/G10。验收初期是G9；07:20:46 UTC进入一次启动等待，07:22:22被启动watchdog超时退回stopped，错误为 `Start timed out waiting for capacity or readiness; retry Start manually`。没有G10 native stop，旧idle停止信息仍在；现存审计及已回收的旧应用日志不足以确认发起者/HTTP route。本轮未调用它的Start/Stop，且最后源135的恢复未再增加代次。

166保留为 `research-team-1-leader`、G1，原先creating，验收时error / `RUNTIME_POOL_UNAVAILABLE: no schedulable openclaw runtime pod`。不是仍有旧Pod占用，而是当前没有可用OpenClaw受管池。`scale-options` 也明确拒绝复用已被隔离的旧OpenClaw默认controller；其他类型没有新鲜健康serving Pod证明默认模板。受管池清单只剩一个replicas0的Hermes池。

因此本轮完成的是无绑定残留退役、数据库与现场核对及可重试手动恢复机制；未擅自选择新Runtime镜像、解除旧源fence、创建业务容量或单独启动/删除受保护Team实例。后续恢复业务运行需要通过正常池维护流程建立已验证的可调度容量。不能把“旧残留已清理”表述成“两个实例都已恢复运行”。

08:01:46 UTC最新只读回查：118已为running/G11，实际绑定新Runtime171（`opencode-runtime-eight-20261109-67859dbb59-vr68t`，UID `3658b5b9-9d1a-4f4f-a098-e115e8fc7e89`），capacity=8、used_slots=1、bound_instances=1、unknown_slots=0，状态ready。它是有实际业务绑定的新运行池，已保留，不能当成旧测试残留再清理。本轮没有发起这次Start或创建该新池，不推断外部调用者。166仍为error/G1、RUNTIME_POOL_UNAVAILABLE，未擅自启动/删除；因此不能声称两个实例全部恢复。最新证据为节点 `final-observation.json`，旧7:52验收快照保留原时点事实。

缺少历史来源的generation Secret不会被凭空补成已停止；未来实例删除若缺全历史凭据，会保留deleting并明确阻塞，不能删SQL来掩盖。

## 回归与交接证据

- 后端广回归：4912 passed、422 skipped、27包通过。Darwin沿用既有Linux-only NFS编译overlay，且显式排除在未改动HEAD实现上已复现的 `TestWorkspaceTextConcurrentSave` 偶发ENOENT，不把该项记为通过。
- 隔离SQL完整orphan流程136.588s，最终queued/reset/真实Start/editor互斥及原子回滚78.402s，既有与新增reset执行回归17.728s通过。
- 存储独立性/别名真实RED→GREEN，最终K8s包8.925s通过；控制器等待修正真实RED→GREEN，K8s包9.067s、真实SQL流程8.463s通过。
- 前端61项测试、TypeScript/Vite构建通过；真实Chrome1280/390组件验收通过。Linux amd64/arm64 helper构建已验证。
- 13319本机隔离MariaDB已关闭，进程/监听/datadir/socket/pid清理，日志保留。

节点私有证据目录：`/root/cm-orphan-20261110/`。主要文件为 `before.json`、`deployment-image.json`、`acceptance.json`、`verification-recovered.json`、`operations-latest.json`、`helper-cleanup.json`、`cleanup-verified.json`、`nfs-lab-before.json`、`nfs-lab-retirement.json`、`nfs-lab-exit.json`、`source155-controller-events.json`、`scale-options-after.json`、`final-observation.json`。完整源快照保存在受限目录，不提交含环境内容的manifest。
