# NFS 多槽位实现与验收记录（2026-11-05）

实现位于 ClawManager、AgentsRuntime 的 `mount` 分支，显式配置为 `nfs-rootless-v1`。默认配置和现有业务池未切换。部署条件、数据布局及外部迁移前提见 [NFS 部署说明](../deployments/k8s/lite-slots/nfs-rootless.md)；设计边界见 [规格](superpowers/specs/2026-11-05-nfs-multislot-isolation-design.md)。

所有编译、测试及 OCI 镜像构建都在测试节点 `10.130.81.54` 进行，本地 Mac 只编辑和传输代码。实际环境为单节点 Linux6.8、Kubernetes1.35、containerd2.4.2/runc1.5.2，测试命名空间 `clawmanager-nfs-lab`。专用 NFSv4.1 export 保持 `root_squash`；原业务命名空间及 NFS 导出策略未改。

## 实机结果

证据目录为节点 `/root/cm-nfs-multislot-20261105`，下表文件名均相对此目录。终止回执、完整容器身份及结果保存在该目录的0600证据文件中，不含在通用配置示例中。

| 检查 | 已取得结果 | 证据 |
| --- | --- | --- |
| 四类原生准备 | OpenClaw、Hermes、OpenCode、DeepSeek Harness 在真实 NFS 上重复准备配置，既有私有0600文件保留 | `storage-native-nfs.log` |
| 文件隔离与持久 tmp | A 可读写自己的 workspace/tmp，B 被拒绝；A→B→A 后 A 内容保留；客户端 UID0 负面对照被 root_squash 拒绝 | `storage-nfs-green.log` |
| 原生启动与停止 | 四类实际原生进程启动、健康检查及匹配旧容器的停止回执通过；Hermes 同时验证私有配置、HTTP 鉴权和 WebSocket | `native-deepseek-smoke.log`、`native-hermes-health-smoke.log`、下列 OpenClaw/OpenCode 记录 |
| 同 Pod 双实例 | 最终镜像 OpenCode77203/77204：A 重启并重新绑定原 workspace；B 的 ContainerID 不变，330次重新鉴权的原生服务/authority 探测全部通过；遗留后台 writer 停止。此前另有440次零失败 | `final-v2-live-opencode-rerun.log`（65.219s）、`final-v2-opencode-harness-proofs.json`；此前 `lifecycle-real-opencode-positive-buffers.log`（122.13s） |
| 未知写入阻止回收 | 注入未完成管理写入后，在改变旧 CID 前拒绝回收；单槽位停止不以整个 Pod Ready 代替精确分配验证 | 同上、`root-team-membership-green.log` |
| OpenClaw 六类维护 | 最终镜像77101实际 CM 客户端依次执行 inventory、完整 compatibility/Doctor/config/session dry-run、migrate、migration-status、restore、restore-status；六项结果不可变且均有真实 Terminated/回执/释放证明 | `final-v2-live-openclaw.log`（368.463s）、`final-v2-live-proofs.json`；此前72007亦通过365.451s |
| 维护期间源实例可用 | 最终77101源容器保持不变，1255次原生 `/readyz` 探测零失败；预检只重置空闲维护槽位。此前72007有1263次、72003有1575次零失败 | `final-v2-live-openclaw.log`、此前 `native-openclaw-six-methods-final.log`、`native-openclaw-six-methods-72003.log` |
| 迁移与恢复数据 | 1个会话、3个归档文件、5592字节；真实配置、状态及私有会话恢复检查通过，原私有文件保持0600 | 同上 |
| 跨 bind mount 恢复 | 原实现 EXDEV 已复现；改为 workspace 内暂存和原子安装，原 capsule 保留供重试，测试覆盖重复恢复、私有模式及相对符号链接 | OpenClaw `upgrade_nfs_restore_linux_test.go`、集成记录 |
| 后端私有文件接口 | 固定 Go 辅助程序完成私有0600/Team0660读写、流式上传、只读FD下载及审计确认；保留原所有者/模式；不确定写入留下持久 intent | `fileio-final-tests.log`、`fileio-lock-tests.log` |
| Team 服务端身份 | `manage-gids` 场景以临时高 UID/nologin NSS 成员验证授权和外部用户拒绝；测试身份已移除，服务端策略未放宽 | `storage-team-nss-nfs.log` |
| Team 不确定写入 | 无成员、全部停止及混合成员均保留 Team intent；阻止替代执行、维护和身份变更，普通停止 A 不要求停止正常 B | `root-team-membership-green.log`，repository1.330s/services3.193s |
| SQL 并发快照 | 显式 REPEATABLE READ、两条连接复现旧快照漏看 intent 和新增成员；锁定 JOIN/intent 当前读修复后两种顺序均拒绝未知 writer | `root-team-snapshot-red.log`、`root-team-membership-snapshot.log`、`root-team-membership-green.log` |
| 私有原生历史 | 实际后端 Alpine 镜像仅有 SETUID/GID，NFS 私有 JSONL/SQLite/recent/fingerprint 通过；外国实例硬链接拒绝；Landlock 安装后替换路径仍不能读取外国 inode | `owner-history-hardlink-nfs-green.log`（0.16s/0.04s）、`root-owner-history-final-focused.log` |
| 既有私有 inode | 真实 NFS 负面对照复现 B 所有的0644文件通过硬链接/单链接被 A 读取；准备、维护、tmp及文件/技能/私有原生接口补上身份检查后拒绝这些文件，同实例硬链接和Team共享保留 | `persisted-inode-raw-bind-red.log`、`persisted-inode-agent-nfs-final.log`、`persisted-inode-cm-final.log` |
| 隔离前 inode 检查 | 原检查在嵌套 user namespace 中将管理所有者显示为65534，实机捕获误拒绝；最终改为管理程序固定目录FD交给精确宿主实例UID，在 bind 前核对，管理祖先0700保持不变 | `persisted-bootstrap-production-red.log`、`persisted-bootstrap-production-final-green.log`（真实准备/网络/辅助入口0.37s、0700祖先0.01s） |
| 协调器重启与结果重放 | 已完成结果的三个重启窗口均从原 operation/proof 恢复，不重新选择空槽、启动子进程或释放新 owner；未知记录拒绝 | `lifecycle-replay-integration-green.log`、`lifecycle-replay-ar-green.log`；此项使用真实编排代码、确定性 worker/fake Kubernetes，不计为新的真实 Pod 故障验收 |
| 新 NFS 浏览器生命周期 | 实际 Chromium/CDP、NFS 私有资料和鉴权保留；旧执行终止对象持续占住旧名称，新 epoch 使用新控制器；SQL 重建后核对全部旧证明 | `browser-controller-tombstone-actual-green.log`（80.62s）、`browser-controller-tombstone-restart-green.log`（0.03s） |

首次失败的71001/71003执行保持原隔离记录，未以清账本换取成功；72003的 EXDEV 负面证据同样保留。成功的后续验证使用新实例/受控新池，不覆盖旧未知 writer 的身份。

## 回归验证与审查

逐任务运行远端 RED→GREEN、真实凭据/NFS测试和独立审查。最终整分支审查及后续主机身份FD修正审查均无剩余阻塞项。审查修复覆盖：浏览器丢失历史/自主替代、Team缺少独立intent/事务旧快照、覆盖上传破坏私有模式、技能FIFO/取消、Team所有权、原生完成结果重放及历史文件硬链接。

最终五个测试镜像的完整 digest、基础镜像及二进制 SHA256 见 [构建记录](nfs-multislot-images-20261105.json)。四类最终 Runtime 镜像均通过实际原生启停；Hermes7.421s、DeepSeek3.473s，OpenCode/OpenClaw结果见表。

Go 回归覆盖后端全部包及 Runtime 五个模块的 `./...`。后端首轮完整快照缺少 e2e/deployments/docs 的仓库外部测试资料，补齐后对应 aigateway/db/northbound 包通过；同轮发现新增协调表未登记到系统备份策略，按既有活执行记录的 `metadata_only/reference_only` 策略登记后 systembackup 通过0.677s。其他后端包已通过该完整轮次（handlers88.771s、services44.529s、k8s8.688s）。记录为 `final-backend-all-tests.log` 及 `final-backend-affected-recheck.log`。

最终 Runtime 五模块日志为 `final-hostfd-*-tests.log`。并发检测 `final-hostfd-core-race.log`：sandbox1.074s、slots3.331s、netguard1.077s、gateway7.195s全部通过；四镜像构建合约、入口角色校验及 Hermes release 校验同样通过。外部环境专用用例在普通包测试中显式跳过，实机结果单独列出。

最终 OpenCode 用例曾把停止证明之后的单次 PodStatus 读取误当作新 Running CID 已可见；修正测试为先验证精确旧容器证明，再限时等待新 Running CID 且每次确认 B 不变。超时仍失败，未修改产品停止协议。原失败日志和后续65.219s实际通过记录均保留。

## 发布边界

- 只有单节点，尚未取得真实双节点 A→B→A、节点失联后的外部 fencing 或生产 NFS 故障重连证明。代码中的拒绝/恢复测试不等于这些实机项通过。
- 旧浏览器 Deployment/ReplicaSet 的不确定历史创建请求可能延迟落地。不能据控制器缩至0、observedGeneration、Pod 列表为空或新加 finalizer 宣布其停止；自动切换保持阻塞，需要外部可靠 quiescence/fencing。新 StatefulSet 和已离线导入的新受管资料使用正常路径。
- 新 NFS 浏览器保留终止 Pod 名称及零副本控制器；75202实测旧8个主/初始化容器CID均不在 containerd task 中、两个旧PodUID无挂载残留，不占终止后CPU/内存调度预算，但对象配额继续计数。注册记录受64KiB上限约束，达到后拒绝，不自动剪掉证明。
- 测试控制服务提供注册/时间/租约等最小协议，模型上游返回403。这些结果不证明真实供应商模型调用、外网网站登录或完整 Team 消息投递。
- 本次为 linux/amd64 实机验收；arm64、实际容量/压力/密度、其他内核/AppArmor/NFS服务端配置须分别通过发布门槛。
- 完整原生快照仍按既有策略禁用，没有把拒绝执行的快照计为通过。未关闭 root_squash、未放宽已有用户文件权限、未增加外层 SYS_ADMIN/NET_ADMIN，未部署到现有业务池。
