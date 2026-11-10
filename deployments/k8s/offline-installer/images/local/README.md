# 本地镜像输入包约定

`images/local/` 中的 tar 是制包阶段的输入缓存。只有 ClawManager 和 Runtime 属于每次
交付时可替换的业务输入；基础依赖包由通用安装方案维护，默认不动。用户只需修改上一级
`images.lock`，不要再手工同步两份离线 YAML。

## 三个镜像分组

1. `clawmanager-images-<发布标签>.tar`：只放 `platform.clawmanager` 控制面镜像。
2. `runtime-images-<发布标签>.tar`：放全部非空 `runtime.*`，可以包含一个或多个 Runtime。
3. `platform-base-images-<发布标签>.tar`：保存 Kubernetes、Calico、Longhorn、MySQL、
   MinIO、Redis、Skill Scanner 等基础依赖，普通业务发版默认复用。

Ubuntu 22.04 和 24.04 共用本目录，不要复制两套大镜像文件。

## 从本机 Docker 一键导出

先把 `images.lock` 中所有非空镜像以完全相同的名称和 tag 拉取或构建到本机。导出脚本
只读取本机 Docker，不会自动 pull：

```bash
cd deployments/k8s/offline-installer
bash save-images-from-lock.sh --release-tag 2026.9.15 --group all
```

也可以只重做变化的分组：

```bash
bash save-images-from-lock.sh --release-tag 2026.9.15 --group clawmanager
bash save-images-from-lock.sh --release-tag 2026.9.15 --group runtime
bash save-images-from-lock.sh --release-tag base-v1 --group platform-base
bash save-images-from-lock.sh --release-tag 2026.9.15 --group clawmanager,runtime
```

脚本会检查本地镜像是否存在、是否为 `linux/amd64`，并检查生成 tar 的 `RepoTags` 与
该分组的 lock 值完全一致。缺镜像时会列出准确镜像名并停止；一个镜像即使在本机还有
其他 tag，也只导出 lock 指定的 tag。

## images.lock 规则

- `images/images.lock` 是唯一需要维护的镜像映射。
- 所有非空镜像必须使用明确 tag；生产交付不应使用可变的 `latest`。
- 制作总离线包时，两份成品 YAML 及 MySQL Runtime 映射 SQL 会自动按 lock 渲染。
- 空 `runtime.*` 键仍作为支持的固定槽位保留，但不进入 tar、不写入成品 YAML 的 Runtime
  映射，也不改变数据库已有行的 `is_enabled`。
- OpenClaw/Hermes 的 Lite 值为空时，其预热 Runtime Deployment/Service 不进入成品 YAML。

当前固定 Runtime 键：

```text
runtime.openclaw.pro
runtime.openclaw.lite
runtime.hermes.pro
runtime.hermes.lite
runtime.dsh.pro
runtime.dsh.lite
runtime.opencode.pro
runtime.opencode.lite
runtime.workbuddy.internal
```

Pro 对应 desktop backend，Lite 最终对应 gateway backend。MySQL 引导阶段先用旧 schema
兼容的 `shell`，再由 ClawManager 现有迁移转换为 `gateway`。值是否为空只决定本次离线包
是否携带和配置该镜像，不作为 `is_enabled` 开关。

## GitHub Release 的单镜像 tar

如果 GitHub 提供的是 GHCR 容器镜像而非 tar 下载文件，可以用
[pull-github-image.sh 辅助方案](../../GITHUB_IMAGES.md) 自动拉取、导出到本目录并更新 lock。

允许一个镜像一个 tar，不要求先合并。下载后只需将文件名调整为以下任一前缀：

```text
clawmanager-images-*.tar
runtime-images-*.tar
platform-base-images*.tar
```

制包脚本会加载全部匹配文件，然后按 `images.lock` 重新导出一个共享层去重的总镜像 tar。
文件名只负责分组，真正的镜像名和 tag 取自 tar 内 RepoTag；若 RepoTag 与 lock 不一致，
应用镜像缺少锁定 tag 时制包会停止，不会自动换 tag。只有 Kubernetes、Calico、Longhorn
基础设施镜像缺失时，主制包脚本会按 lock 的固定版本联网补拉；服务器安装不会下载。
