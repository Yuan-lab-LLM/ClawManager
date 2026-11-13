# Hermes 模型切换与故障槽位验收

构建、Go 测试、浏览器验收均在 `10.130.81.54` 执行。本地仅编辑、校验和传输源码。

## 模型切换

- Hermes 主会话模型选择器发送不带 `--session` 的 `config.set model`。CM 接受受限的 provider/model 语法，完成目录及会话归属核验后，统一补上 `--session`，禁止全局配置写入。
- WebSocket 回归覆盖原始请求、未归属会话、非目录模型和外部 provider。
- 线上专用临时实例 141 已成功执行原始切换请求：`clawmanager-clawmanager / deepseek-v4.1-flash`，Runtime 返回对应模型及 `scope=session`。未发送用户任务或聊天消息。
- 联调发现 `/gateway/llm/models` 使用 CM 数据信封，Hermes 按 OpenAI 列表读取时触发 `str.get` 异常。SDK 列表必须返回标准对象，并遵守当前用户与网关密钥的模型权限。

## 已验证的槽位协议

- 专用临时实例 141 的旧 supervisor 被暂停后，CM 的节点助手取得独立停止回执。
- 恢复操作 `runtime_mgmt_f70820aa-4503-4c40-83ea-6d4b781d04ff` 已完成；journal 为 `released`，Runtime 确认实际释放。
- 实例以新代次在同一 Pod 43、同一槽位 slot-0 重用成功；未伪造原生优雅停止证明。
- 正常停止成功。正常启动首次被长时间迁移等待拖延，重试后以代次 4 在同一槽位运行。该调度延迟仍须单独修复。

## 尚未完成的 OpenClaw 原生升级

原生升级 3 使用不可变候选镜像 `sha256:07b15906d886b2fafe68f7e49051af2304d26701c4263231545f1bf9a7f6bab3`。所有会话已迁移，但旧 Pod 33 的历史隔离槽位不能确认 drain，升级进入回滚；随后同一槽位也不能确认 undrain，回滚停在 `rollback_restore` / `error`。

此状态不等同于升级完成。旧 Pod 的隔离、升级保护和工作区保持原状；未用 SQL 修改状态、清除保护或外部修改 Runtime Deployment 镜像。新 Runtime 的槽位释放协议已验收，旧版本残留 Pod 的安全退役仍未完成。

测试集群私有验收记录保存在 `/root/cm-fault-slot-release-20261113/`，包括 `acceptance-result.json`、`acceptance-normal-lifecycle-result.json`、`model-switch-live-result.json` 和 `openclaw-rollout-final-held.private.json`。这些文件不加入仓库。
