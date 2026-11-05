# 推送前部署一致性核对

2026-11-04。本轮只读核对仓库构建/部署路径及测试节点 `10.130.81.54` 的 `clawmanager-system`，没有发布镜像、重启实例或更改池模板。另补充了 [Lite 部署说明](../../../deployments/k8s/lite-slots/README.md)。

## 已对应

- CM Dockerfile 复制完整前端及后端源码；新增页面、公共访问 hook 和 Go 源文件进入正常构建。普通构建未指定分钟测试标签。
- 数据库使用 `go:embed migrations/*.sql`，新增 `096`、`097`、`098` 不需要手工追加镜像 COPY；测试数据库已记录三条迁移完成。
- Runtime 官方 Lite Dockerfile 编译各自 Agent 和共享核心；Hermes 完整构建包含新的配置生成器、Smoke 文件与源码锁。现有 Hermes Agent overlay 也会使用原镜像 verifier 重新生成制品清单，不继承其他载荷的 acceptance。
- 测试 CM 当前镜像为 `sha256:f52d375a8ee742e4c864c21c93a9dfa1d6560120a4369da61f72dd1a0dd2cc1a`，包含本轮后端功能及嵌入页恢复前端。自动停止已开启，两项配置均为 1 天，下一次检查为北京时间 2026-11-05 04:00。
- 默认池和当前承载实例的 OpenClaw 池、OpenCode、DSH 的模板使用本轮已验证镜像。工作空间与 PVC 保留。
- 测试集群的缩容 ValidatingAdmissionPolicy 与 Binding 均存在；CM ServiceAccount 的 Pod patch/delete、Deployment patch 和 ReplicaSet get 权限检查通过。仅核对鉴权结果，没有执行 Pod 删除操作。

## 尚未完成的部署

- Hermes 的默认池与自定义池仍使用前一版可用镜像 `sha256:52c63fdd0908d638aa0a5ea5bfbef7b2fa4486907ce49a1036a904f04a2e4de3`。独立供应商分组候选 `sha256:a72360b2646059c239ec8a2ccd61487e44f10111ed0fb1ce7fdfc415b49bb4ab` 已构建、未部署；实际桌面列表验收也尚未完成。
- `openclaw-runtime-u5`、`openclaw-runtime-u5-u6` 两个历史自定义池目前为 0 副本，仍保留旧镜像 `sha256:28524c52a76cc14abed91f80c66f9cc8c4c5bfca379068767f3a07eddd3ccd48`。不能把当前活跃池已更新扩大为所有历史池都已更新；后续需受控升级与兼容性确认。
- 通用部署清单不写入测试节点本地构建的镜像引用。正式部署需要两个仓库的匹配发行镜像及相应模板/默认镜像配置，不应仅拉取旧的浮动标签就认为 Runtime 协议已升级。

## 推送与发布的区别

当前分支为 `feat/lite-idle-stop`。CM Build 工作流的 push 过滤包含 `main`、`feature/**`、`fix/**`，不包含该 `feat/**` 分支；PR 或 workflow_dispatch 可以启动验证。Runtime 镜像发布工作流只允许默认分支发布。本次不修改这些触发/发布权限，也不额外触发工作流。因此推送这两个功能分支本身不意味着重新构建、发布或部署。

节点只读记录：`/root/cm-pod-elastic-20261104/push-deployment-audit.json`。代码提交及发布待办还见[提交准备记录](2026-11-04-commit-preparation.md)和[发布准备记录](2026-11-04-lite-elastic-release-preparation.md)。
