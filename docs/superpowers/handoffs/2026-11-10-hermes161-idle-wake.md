# Hermes161 休眠唤醒故障

## 根因与修复

实例161（Hermes，abc）的 Start 请求已成功提交 Creating 意图，但原正式 Web 运行池被自动回收到0副本。旧 Hermes Pod132 心跳仍报告可用容量，而物理槽位检查无法证明有可分配的槽位。调度器尝试旧 Pod 后返回 pending；恢复休眠池的分支只在活跃候选列表为空时运行，因此一直等待，直到120秒启动期限到期退回 Stopped。

共享 `assignInstance` 现会在所有活跃候选均无法接单后尝试现有 `restoreIdleRuntimeCapacity`，覆盖四种 Agent。唤醒仍依赖已完成的自动回收日志、Deployment UID/模板/代数、原 PVC 身份及调度互斥校验。已经提交且结果不确定的 Gateway create 会在此分支之前返回并保留原预留，不能换池重复创建。

## 现场恢复

通过现有 `WakeIdleRuntimePool` 方法，使用已完成的自动休眠日志 `runtime_mgmt_76eb3d0d-d1b2-4b18-94e1-035416a402fe` 恢复 `hermes-runtime-eight-web-20261109`，由0副本唤醒为1。未绕过其身份和存储检查。

恢复后实例161为 Running/G15，绑定 Runtime167、Gateway `gw-161-15`；Hermes 后端启动就绪，桌面 bootstrap 能力接口返回 `available: true`。

## 发布记录

- `preview` 源码修复：`f1b5a35`。
- 独立热修复分支：`fix/runtime-idle-wake-20261110`，提交 `138fab1`，基于线上源版本 `c4fe848` 回移同一修复。
- 热修复基于线上镜像 `sha256:5dbe2a45fe1153e0a0d3c700138d925c70fdcc64699cbf9a2133cb2bc56fb00d`，仅替换 manager 二进制。数据库观察一致性改动（migration105）仍待单独完成集成验证与发布。
- 本地回归：修复前8个四类型/不可用槽位用例失败；修复后包含不确定创建保留预留的12个用例通过，相关既有槽位与休眠回归及 race 检查通过。
- 独立审查通过，确认不确定创建不会进入换池唤醒分支。
- Linux 后端回归通过：3530 项通过、0 失败，350 项按原有条件跳过；相关 race 检查40项通过、0失败，14项未启用隔离 MySQL 而跳过。本热修复不增加 SQL 或 schema 变更。
- 已部署镜像：`clawmanager.local/clawmanager@sha256:14b7212d0bbeed632f95d9610960caf97b09e6de70b1d6e0ad63dace03f3e6f4`。镜像检查确认只替换 manager 二进制，基础配置、静态页面和备份工具保留；现有用户 Runtime Pod UID/重启计数及实例代数在 manager 发布期间保持一致。
- 最终验收：实例161原为 Stopped/G15，正式 Hermes Web 池为0副本；通过 Start API 后，调度器自动恢复到1副本，实例在15.93秒内进入 Running/G16，桌面 bootstrap 可用，激活桌面会话后 session API 返回 HTTP200。
- 现有休眠策略为每60秒检查、无模型活动300秒休眠。首次恢复G15后观察到的再次停止符合该策略，完整冷启动验收使用其自然休眠后的状态，无须人为停止用户服务。
- 远端证据目录：`/root/cm-hermes161-wake-20261110/`，包含源文件校验、Linux测试日志、镜像检查、发布前后快照及 `cold-start-acceptance.json`。其中私有自动休眠日志快照不应提交Git。

上一份交接中的 containerd 隔离 MySQL `cm-runtime-observation-mysql-20261109` 已核对所有权 label 后终止并删除，其私有密码文件已删除。它不是 Kubernetes Pod，也不是生产 MySQL。
