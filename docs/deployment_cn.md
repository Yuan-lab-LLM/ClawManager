[← 返回 README](../README.zh-CN.md)

# 部署指南

ClawManager 是 Kubernetes 原生平台。先选择 Kubernetes 发行版，再选择与实际存储形态匹配的 profile。

## 部署入口

| 环境 | 清单 | 安装说明 |
|---|---|---|
| k3s 单节点 | [clawmanager.yaml](../deployments/k3s/single-node/clawmanager.yaml) | [HostPath](./install/k3s/single-node.md) |
| k3s 集群 | [clawmanager.yaml](../deployments/k3s/cluster/clawmanager.yaml) | [CSI](./install/k3s/cluster.md) |
| Kubernetes 单节点 | [clawmanager.yaml](../deployments/k8s/single-node/clawmanager.yaml) | [HostPath](./install/k8s/single-node.md) |
| Kubernetes 集群 | [clawmanager.yaml](../deployments/k8s/cluster/clawmanager.yaml) | [CSI](./install/k8s/cluster.md) |

一次只使用一套完整 profile。Longhorn 的 StorageClass 名称只是示例，可以替换为访问模式等价的 CSI。不要混用清单，不要用临时 `/tmp` HostPath 修补多节点存储。

## 部署内容与流程

清单包含 ClawManager、MySQL、MinIO、Skill Scanner、Team Redis、共享工作区服务，以及 OpenClaw、Hermes、OpenCode、DeepSeek Harness Lite Runtime 池。Lite 实例在共享 Runtime Pod 中运行，不会每个实例创建独立 Pod。

1. 核对镜像、StorageClass 和入口暴露方式，准备[审计与历史恢复凭据](#审计与历史恢复凭据)。
2. 单节点先设置清单要求的存储节点标签；集群先确认 RWO/RWX StorageClass。
3. 单节点首装使用 [HostPath 安装入口](./install/k8s/single-node.md#start-a-fresh-installation)，集群模式按对应说明准备 CSI 卷。检查核心 Pod Ready 和 PVC Bound。
4. 打开前端，配置普通模型，验证安全防护与 AI Gateway。
5. 分别创建需要对外开放的 Runtime 测试实例。

全新 MySQL 使用 `clawmanager-mysql-init` 自动初始化；已有数据卷不会重复执行首次初始化脚本。MySQL、Redis、MinIO、工作区和对象数据都应使用持久卷，不能用 `emptyDir` 作为长期存储。

## 审计与历史恢复凭据

四份清单各自包含业务资源、审计 PVC 和凭据引用。单节点的审计目录位于标记节点的 `/data/clawmanager/system/observability`，集群使用独立的 RWX 审计卷。

1. 用[凭据准备脚本](../tools/k8s/observability/README.md)为新环境生成审计密钥和恢复账号。创建 `clawmanager-system` 命名空间后，应用生成的两个 Secret。
2. 单节点安装入口自动准备凭据，并在 CM 首次迁移期间临时开启 `log_bin_trust_function_creators`。手动部署时由数据库管理员等待 MySQL 就绪、执行恢复账号的私有 `bootstrap.sql`，并在 CM 迁移完成或失败后恢复 MySQL 参数原值。
3. 升级已有环境时，复用原来的审计密钥、审计卷和恢复账号。已绑定的审计卷按现场 StorageClass 保留。

## 发布验收

安装文档、CI 和发布流程共用 [GHCR 镜像配置](../tools/k8s/single-node/ghcr-images.json)。候选 CM 镜像以 GHCR digest 为准。发布流程在两个隔离集群分别验证空库 HostPath 首装和上一版本升级。两轮均验证四种 Lite 实例的停启删与工作区文件、网关密钥按创建返回 ID 撤销，以及 OpenCode、DeepSeek Harness 的实际 Chrome 页面。任一轮失败时暂停发布标签晋升。

## 可选北向接口

基础部署 profile 默认不对外发布北向接口。需要启用时，先升级 ClawManager Core，再应用独立的 [北向接口 Kubernetes 附加包](../tools/k8s/northbound/README.md)。只有北向 Gateway 可以作为外部 API 入口；Core 内部服务必须保持集群内可见，并保留文档要求的 mTLS、凭据隔离和网络策略边界。外部端口可以通过 NodePort、负载均衡器或 Ingress 映射，不属于 API 协议本身。

已有环境请先阅读[北向接口版本升级说明](./northbound-upgrade-guide.md)，调用方以[北向接口使用说明](./northbound-api-guide.md)和 [OpenAPI 3.1 规范](./northbound-openapi.yaml)为准。

## DeepSeek Harness Runtime

- Lite 在共享 `deepseek-harness-runtime` 池中运行隔离的 `dsh web` 进程，持久化目录为 `<workspace>/home/.dsh`。
- Pro 使用专属 Webtop Deployment；桌面端口为 `3001`，内部 `dsh web` 监听回环端口 `3080`，持久化目录为 `/config/.dsh`。
- 两种模式都由 ClawManager 注入 AI Gateway 地址、实例凭据和模型列表，并支持 Skill 与工作区文件。
- Lite 浏览器需要独立的 HTTPS 源。Chrome 可使用[同主机 HTTPS 端口池](./browser-origin-ports.md)，也可配置包含 `{instance_id}` 的独立域名模板，例如 `CLAWMANAGER_DEEPSEEK_HARNESS_PUBLIC_URL_TEMPLATE=https://deepseek-harness-{instance_id}.clawmanager.test:39443/`。

镜像源码位于 [AgentsRuntime 的 `deepseek-harness/`](https://github.com/Iamlovingit/AgentsRuntime/tree/main/deepseek-harness)。离线环境应为 Lite 独立域名配置通配 DNS 与证书。

## 存储边界

- 单节点 HostPath profile 只适用于固定存储节点，需保留节点标签和 node affinity。
- 多节点 profile 必须使用真正提供 RWO/RWX 能力的 CSI；`local-path` 不能伪装成跨节点 RWX。
- 集群 profile 不应启用隐式 HostPath fallback。
- 生产前检查凭据、TLS、网络策略、备份、镜像固定版本和容量规划。

## ARM64

官方 ClawManager 和 Skill Scanner 镜像支持 `linux/arm64`，但完整部署还包含 MySQL、Redis、MinIO/工作区服务及四类 Runtime。部署到 ARM 节点前检查清单中**每一个固定镜像**的 manifest；主镜像支持 ARM64 不代表自定义 Runtime 自动兼容。

混合架构集群应使用兼容标签并配置 node selector/affinity。建议使用 SSD 持久存储、足够内存和固定版本标签，完成与 amd64 相同的 PVC、实例、桌面、模型和技能验收。

## 验收与排障

确认 Pod 健康、PVC 绑定、前端可访问、普通模型可用、安全防护可打开，并验证各 Runtime。PVC Pending 时收集 StorageClass、PVC、Pod、Event 和 PVC describe 信息；不要先删除数据卷或重建存储。日常操作见 [用户手册](./use_guide_cn.md)。
