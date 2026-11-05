# Hermes 冷启动重复拉起与桌面不可用：修复记录

2026-11-04，测试节点 10.130.81.54，命名空间 `clawmanager-system`。本地只编辑与传输源码，构建和测试全部在测试节点执行。

## 问题与修复

1. 单个 Hermes 实例启动时，默认池与自定义池先后各拉起一个 Pod。Kubernetes Ready 先于 CM 的注册就绪；恢复逻辑收到“已有原生容量”后跳过默认池，又恢复了下一个池。现在必须等待该池完成已验证的 CM 注册，只有其已注册容量全部占满才继续考虑其他池。新增回归先复现 `[hermes-runtime zzz-hermes]` 两次唤醒，再验证只等待第一个池。
2. 桌面接口先返回 `ticket_store_unavailable`。平台 Redis 加载既有持久化数据时反复 OOM，16 GiB 容器内存上限不足；恢复后 Redis 自报内存约 15.88 GiB、RSS 约 15.97 GiB。测试节点可用内存充足，将上限提高到 64 GiB，并采用单写者 Recreate 更新方式。持久化卷和数据文件未删除，未执行清库。现有 `maxmemory=0`、`noeviction` 未改；此项属于容量恢复，后续增长控制仍按数据库改进方案处理。
3. Redis 恢复后返回 `runtime_capability_unsupported`。此前测试镜像只替换 Agent 二进制，未更新 Hermes 制品清单；实际检查仅 `/usr/local/bin/clawmanager-agent` 哈希不匹配。使用镜像内现有 `verify_lite_release.py --generate-release --verify-release` 重新生成并完整验证清单，保留 `desktop_web_accepted=false`，未伪造签名或验收、未放宽能力校验。后续 Agent 层构建辅助脚本补入同样流程。

之前仅验证 Running 与持久化，不能据此证明桌面可进入。本次补验页面、会话 API 与 WebSocket。

## 部署与验证

- 未强制停止正在使用的实例。等待 Hermes 正常空闲停止、绑定释放后更新两个池。
- 更新时先修改 CM 镜像配置触发了现有启动一致性保护，CM 曾短暂不可用；同步默认池模板后恢复。保护条件未修改。
- 两个新模板逐个短暂恢复 1 个空 Pod，再由现有调度器自动缩至 0，生成真实的新休眠操作证明，没有伪造数据库记录或手动缩为 0。
- 从全部 Hermes Pod 为 0 开始，启动实例 25（`lite-hermes-010`），约 17.013 秒进入 Running；观察启动全过程及随后 30 秒，最多只有 1 个 Hermes Pod。
- 桌面能力 `available=true`；登录引导成功设置 HttpOnly 会话 Cookie；实际页面、会话检查及会话列表 API 均为 HTTP 200；一次性票据的 WebSocket 握手为 HTTP 101。未发送模型提示词，未输出 Cookie、票据或对话内容。
- 最新源码带 race 回归：普通构建 76 项、测试计时构建 32 项通过，0 失败 / 跳过。
- 临时 MySQL、测试命名空间、端口转发已清理；构建服务已停止。最终 CM 和 Redis 就绪，实例 25 保持 Running。继续保留实例每分钟扫描、空闲 5 分钟的测试规则，后续自然空闲仍会停止。

## 镜像与证据

- 已部署的测试 CM：`clawmanager.local/clawmanager@sha256:b9fc33926b14d33669506409881f87900bb0b82892da8e925e2d0ba54ee1823f`。
- 已构建、未切换的普通日规则 CM：`clawmanager.local/clawmanager@sha256:24161060a69a990ee6d31422cf5c8217ea8adc39a05d2c71134aa6874e7f2e3f`。
- 已部署的 Hermes 测试镜像：`clawmanager.local/hermes-lite@sha256:52c63fdd0908d638aa0a5ea5bfbef7b2fa4486907ce49a1036a904f04a2e4de3`。

节点证据目录 `/root/cm-pod-elastic-20261104`：`duplicate-wake-red.log`、`duplicate-wake-green.log`、`results/`、`hermes-fix-deployed.json`、`hermes-fix-validation.log`、`hermes-cold-start-desktop-verification.json`、`hermes-fix-final.json`。包含环境变量的更新前快照仅保留在节点权限为 0600 的文件中。
