[← 返回 README](../README.zh-CN.md)

# 部署指南

ClawManager 是 Kubernetes 原生平台。先选择 Kubernetes 发行版，再选择与实际存储形态匹配的 profile。

## 部署入口

| 环境 | 清单 | 存储要求 |
|---|---|---|
| k3s 单节点 | [`deployments/k3s/single-node/clawmanager.yaml`](../deployments/k3s/single-node/clawmanager.yaml) | 标记节点上的 HostPath |
| k3s 集群 | [`deployments/k3s/cluster/clawmanager.yaml`](../deployments/k3s/cluster/clawmanager.yaml) | 控制面 RWO + 工作区 RWX 的 CSI |
| Kubernetes 单节点 | [`deployments/k8s/single-node/clawmanager.yaml`](../deployments/k8s/single-node/clawmanager.yaml) | 标记节点上的 HostPath |
| Kubernetes 集群 | [`deployments/k8s/cluster/clawmanager.yaml`](../deployments/k8s/cluster/clawmanager.yaml) | 控制面 RWO + 工作区 RWX 的 CSI |

一次只使用一套完整 profile。Longhorn 的 StorageClass 名称只是示例，可以替换为访问模式等价的 CSI。不要混用清单，不要用临时 `/tmp` HostPath 修补多节点存储。

## 部署内容与流程

清单包含 ClawManager、MySQL、MinIO、Skill Scanner、Team Redis、共享工作区服务，以及 OpenClaw、Hermes、OpenCode、DeepSeek Harness Lite Runtime 池。Lite 实例在共享 Runtime Pod 中运行，不会每个实例创建独立 Pod。

1. 核对 Secret、镜像、StorageClass 和入口暴露方式。
2. 单节点先设置清单要求的存储节点标签；集群先确认 RWO/RWX StorageClass。
3. 应用一套完整清单，等待核心 Pod Ready 和 PVC Bound。
4. 打开前端，配置普通模型，验证安全防护与 AI Gateway。
5. 分别创建需要对外开放的 Runtime 测试实例。

全新 MySQL 使用 `clawmanager-mysql-init` 自动初始化；已有数据卷不会重复执行首次初始化脚本。MySQL、Redis、MinIO、工作区和对象数据都应使用持久卷，不能用 `emptyDir` 作为长期存储。

## DeepSeek Harness Runtime

- Lite 在共享 `deepseek-harness-runtime` 池中运行隔离的 `dsh web` 进程，持久化目录为 `<workspace>/home/.dsh`。
- Pro 使用专属 Webtop Deployment；桌面端口为 `3001`，内部 `dsh web` 监听回环端口 `3080`，持久化目录为 `/config/.dsh`。
- 两种模式都由 ClawManager 注入 AI Gateway 地址、实例凭据和模型列表，并支持 Skill 与工作区文件。
- Lite 浏览器必须配置独立域名模板，例如 `CLAWMANAGER_DEEPSEEK_HARNESS_PUBLIC_URL_TEMPLATE=https://deepseek-harness-{instance_id}.clawmanager.test:39443/`；`{instance_id}` 不可省略。

镜像源码位于 [AgentsRuntime 的 `deepseek-harness/`](https://github.com/Iamlovingit/AgentsRuntime/tree/main/deepseek-harness)。离线环境应为 Lite 独立域名配置通配 DNS 与证书。

## 存储边界

- 单节点 HostPath profile 只适用于固定存储节点，需保留节点标签和 node affinity。
- 多节点 profile 必须使用真正提供 RWO/RWX 能力的 CSI；`local-path` 不能伪装成跨节点 RWX。
- 集群 profile 不应启用隐式 HostPath fallback。
- 生产前检查凭据、TLS、网络策略、备份、镜像固定版本和容量规划。

## ARM64

官方 ClawManager 和 Skill Scanner 镜像支持 `linux/arm64`，但完整部署还包含 MySQL、Redis、MinIO/工作区服务及三类 Runtime。部署到 ARM 节点前检查清单中**每一个固定镜像**的 manifest；主镜像支持 ARM64 不代表自定义 Runtime 自动兼容。

混合架构集群应使用兼容标签并配置 node selector/affinity。建议使用 SSD 持久存储、足够内存和固定版本标签，完成与 amd64 相同的 PVC、实例、桌面、模型和技能验收。

## 验收与排障

确认 Pod 健康、PVC 绑定、前端可访问、普通模型可用、安全防护可打开，并验证各 Runtime。PVC Pending 时收集 StorageClass、PVC、Pod、Event 和 PVC describe 信息；不要先删除数据卷或重建存储。日常操作见 [用户手册](./use_guide_cn.md)。
