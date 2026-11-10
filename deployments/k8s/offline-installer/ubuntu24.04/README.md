# Ubuntu 24.04（Noble）离线方案

本目录只保存 Ubuntu 24.04 专属的版本锁、deb 仓库和构建输出。镜像、ClawManager
YAML、Calico/Longhorn 资源以及安装脚本复用上一级目录，避免重复占用空间。

在联网制包机执行：

```bash
cd ubuntu24.04
bash build-offline-bundle.sh --release-tag 2026.9.15
```

完整离线包输出到 `ubuntu24.04/dist/`。生成的压缩包是自包含的，复制到 Ubuntu 24.04
control-plane 后不依赖上一级公共目录。

交付部署人员从 `DEPLOYMENT_GUIDE.md` 开始阅读，其中同时包含 `single-node`、`cluster`
的选择、配置、安装、验收和续跑流程。

`cluster` 双节点实机验收步骤见 `VALIDATION.md`，`single-node` 实机验收步骤见
`SINGLE_NODE_VALIDATION.md`。
制包机、Ubuntu 基础系统和离线 deb 准备说明见 `PREPARATION.md`。
