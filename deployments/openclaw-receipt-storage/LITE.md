# OpenClaw Lite 单 Pod 池回执存储（试验配置）

本版接入持久卷，但**没有完成多 Pod 协调、跨池/跨运行代次对账或原生整轮取消能力验收**。新任务协议仍默认关闭，不应凭此开启正式治理。Pro 请使用 [Pro 配置说明](./README.md)，两种绑定格式不同。

## 前提与限制

- 限 OpenClaw Lite 单 Pod 池（副本数 0 或 1）；多个实例可由同一控制 Agent 分配 `user-<用户ID>-instance-<实例ID>` 子目录。
- 管理员预先准备专属文件系统 PVC，访问模式必须为 `ReadWriteOncePod`，实际 CSI 驱动需支持该模式。不能复用工作区、Pro 实例卷或另一个运行时池的卷。普通 manual/hostPath 不应假定兼容。
- 参考 [Lite PVC 示例](./lite-pvc.example.yaml)，修改实际 namespace、池名、StorageClass。用途标签与池归属注解都必须正确；池名使用注解，以支持超过标签值长度限制的 Deployment 名。
- 卷根需由控制 Agent 的实际 UID 拥有，建议权限 0755，不得由组或其他用户写入，路径及祖先不得为符号链接。不要设置 Pod 级 `fsGroup`。卷须支持硬链接与文件同步；首次建议使用干净卷，不要为通过检查删除未知数据。
- 镜像必须包含 Lite 回执目录分配和恢复代码。挂载配置成功不证明旧镜像会使用该功能，仍需实际 Linux 容器验收。
- 专用目录不是任意 shell 的完整安全隔离边界；运行实例的 UID/GID 需由平台正确分配，不得人为复用其他实例身份。

## ClawManager 后端配置

在 ClawManager 后端配置（不是实例环境、也不是 Lite Agent 环境）：

```yaml
- name: CLAWMANAGER_OPENCLAW_RUNTIME_RECEIPT_PVC_BINDINGS
  value: '[{"namespace":"clawmanager-system","deployment":"openclaw-runtime","claim_name":"openclaw-runtime-receipts"}]'
```

后端各副本需保持一致并重启加载环境。仅保存环境不立即修改现有池；后续普通原地镜像更新 `RolloutImage(..., upgradeID="")` 或 Deployment `Ensure` 才会校验并接入。现有静态清单创建的池走原地镜像更新入口，不依赖新增 API。**本次代码没有自动执行任何这些操作。**

对选中池：

1. 在写 Deployment 前读取 PVC，验证用途、池归属、UID、文件系统与 RWOP；Pending 可兼容 WaitForFirstConsumer，Lost/删除中拒绝。
2. 将卷挂载到 `/var/lib/clawmanager-task-receipts`，向控制 Agent 注入 `CLAWMANAGER_TASK_RECEIPT_ROOT`。原生实例子进程的目录仍由 Agent 分配，不允许池级直接设置 `CLAWMANAGER_TASK_RECEIPT_DIR`。
3. 拒绝与既有挂载重叠、复用工作区、subPath、侧车/初始化容器共享回执卷和间接来源的卷根环境。
4. 在 Pod 模板记录 PVC 名和 UID，使用单 Pod、Recreate 更新。**会有停机窗口，不是滚动无损迁移。**
5. 后续重建保留绑定；解绑、换卷或同名 PVC 重建需要受控迁移，不能静默继续。

未配置且原先无回执存储的池保持原有模板/更新策略，不查询回执 PVC。若已有手工配置，不能靠删除管理注解绕过卷/环境检测；历史自定义目录不自动迁移。不要通过 `envFrom` Secret/ConfigMap 隐藏回执配置，当前检测不解析任意间接环境来源。

## 升级与恢复边界

- 不支持复制已绑定或已计划绑定的源池来创建升级池/实验池；目标已存在也会检查源池，避免幂等分支绕过保护。
- 带 `upgradeID` 的待机/surge 更新和升级目标激活会拒绝此配置。现有多池升级流程需先完成回执迁移协议，不能通过给两个池挂同一卷来绕过。
- 普通原地镜像更新可用，但使用 Recreate。更新前应停止接收新任务并排空/对账；本配置本身不等于任务排空协议。
- 已绑定池不可扩到 2 个或更多副本。停止到 0 仍允许，即使绑定配置或 PVC 异常；再次启动会检查存储。缩放使用资源版本前置条件，冲突需重试。
- 这不解决同一实例被调度到另一个池后的回执接续；暂只用于受控单池试验。保留旧卷，跨池/代次/凭据更换必须另行对账。
- 不创建、删除或修复 PVC；控制面模板检查不替代真实挂载、权限与持久性验收。整个 Deployment/命名空间删除后也不能依赖其模板注解恢复绑定历史。
- 不注入任务协议启用开关，不主动登记治理能力，不改变 Team 配置。

## 验证

fake Kubernetes 回归覆盖默认兼容、实际原地更新/Ensure 接线、重复更新不重复挂载、PVC 归属/UID、无效配置无 Deployment 写入、解绑/扩容保护、克隆/目标激活拒绝。真实 Linux/CSI 挂载、Pod 重建后恢复和完整运行时任务链仍待验收。
