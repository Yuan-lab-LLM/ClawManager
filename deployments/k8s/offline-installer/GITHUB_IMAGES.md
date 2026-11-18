# 可选：按发布清单批量准备镜像

不自行构建镜像时，在联网制包机使用此辅助方案。它不会被正常构建或服务器安装流程
自动调用。准备 Bash、Python 3.10+、Docker（Linux 容器模式）及各镜像仓库的网络访问。

## 1. 统一维护发布清单

编辑 `images/release-images.conf`。ClawManager、八个公开 Runtime、内部 Workbuddy 槽位、
MySQL、MinIO、Redis、Skill Scanner 均已列入；不包含 Kubernetes、Calico、Longhorn：

```text
platform.clawmanager|ghcr.io/yuan-lab-llm/clawmanager:latest
runtime.openclaw.pro|ghcr.io/yuan-lab-llm/agentsruntime/openclaw:latest
runtime.openclaw.lite|ghcr.io/yuan-lab-llm/agentsruntime/openclaw-lite:latest
platform.mysql|mysql:8.4.8
platform.skill-scanner|ghcr.io/yuan-lab-llm/skill-scanner:v2026.4.23
```

公开 tag 在 2026-09-17 对照 GitHub Packages 页面填写：
[ClawManager](https://github.com/Yuan-lab-LLM/ClawManager/pkgs/container/clawmanager)、
[Runtime（以 Hermes Lite 为例）](https://github.com/Yuan-lab-LLM/AgentsRuntime/pkgs/container/agentsruntime%2Fhermes-lite)、
[Skill Scanner](https://github.com/Yuan-lab-LLM/skill-scanner/pkgs/container/skill-scanner)。
ClawManager 和八个公开 Runtime 默认使用 latest 作为下载入口，也可填写明确 tag。
基础组件继续使用固定 tag。镜像发布存在不等于本地已完成部署兼容性验收。
基础设施版本沿用原 images.lock，修改时仍需与 versions.env、基础设施 YAML 配套。

对于业务镜像 latest，脚本读取 GHCR manifest 摘要和版本列表，寻找摘要相同的日期版本
（支持 v2026.9.13、2026.9.13、v2026.9.13.1 等）。若多个日期版本指向同一摘要，选日期
和修订号最新的那个。只看日期最新但摘要不同的版本不会被选中。
随后按摘要拉取镜像，再赋予已确认的版本 tag 导出；images.lock 和 tar 不会写入 latest。
例如 openclaw-lite:latest 在本次只读联网校验中解析为 openclaw-lite:v2026.9.13。
若找不到匹配的日期 tag，批量下载开始前就停止，提示手工指定实际版本；不自行编造 tag。
自动解析目前适用于公开 GHCR 的 ClawManager/Runtime，私有仓库使用明确 tag 和 Docker 登录。

这里的应用依赖使用 GHCR 或 Docker Hub，并非全部位于 GitHub。
Workbuddy 属内部版本，公开来源留空，不猜测下载地址。

两个文件用途不同：

- `release-images.conf`：此可选脚本的下载计划。非空项会下载并更新对应 lock 行；
  空项只跳过下载、保留 lock 原值，便于复用本地镜像。
- `images.lock`：最终制包的权威映射。这里的 Runtime 空值才表示不打入最终包。

因此，完全不需要某个 Runtime 时，将两个文件中的对应值都清空；只想保留本地版本时，
只清空下载计划中的值。特别是纯公开版不使用 Workbuddy 时，应清空 lock 中原有 Workbuddy 值。
脚本不修改 is_enabled。

## 2. 一次执行

在仓库根目录执行：

```bash
cd deployments/k8s/offline-installer
bash pull-github-image.sh --dry-run
bash pull-github-image.sh
```

无需逐个指定 --image 或 --key。默认读取上述清单并处理全部非空项。
dry-run 仅显示下载计划，不调用 Docker、不下载、不修改文件。

基础镜像已准备好时，可只更新业务镜像：

```bash
bash pull-github-image.sh --group clawmanager,runtime
```

也可以使用 `--group platform-base` 仅准备 MySQL、MinIO、Redis、Skill Scanner 应用依赖。
默认输出公共 `images/local/`，允许 `--catalog FILE`、`--lock FILE`、`--output DIR`
指定自定义路径。自定义输出需在制包前整理到构建器读取的公共目录。

## 3. 脚本处理结果

先检查所选清单并解析全部业务 latest，再逐项 pull linux/amd64、检查架构、save 单镜像 tar、验证 RepoTags、
生成 SHA256，最后备份并更新该键的 images.lock 值。文件名前缀自动分类：

```text
clawmanager-images-<键>-<镜像tag>-<引用哈希>.tar
runtime-images-<键>-<镜像tag>-<引用哈希>.tar
platform-base-images-<键>-<镜像tag>-<引用哈希>.tar
```

这些单镜像文件可由现有制包器直接加载，原有基础总 tar 无需删除或重做。
每项成功后保存进度；中途失败时已完成项保留，失败项不会更新 lock，脚本立即退出。
重新执行时，lock 版本一致且 tar 的 SHA256、RepoTag 校验通过的项自动跳过 Docker pull/save。
latest 入口仍需联网解析；还必须与 tar 的 `.source.json` 中记录的来源摘要一致才跳过。
旧 tar 没有来源记录时，latest 项首次重新导出以建立记录；之后可跳过。
显式版本按固定标签处理，不额外查询远端是否覆盖了同名标签。
校验不通过或版本/摘要变化则重新准备，Docker 可复用已下载层。
备份位于 lock 同级 images.lock.backup-*；不要并发修改同一个 lock。

不自动清理旧 tar。不同版本会保留各自文件，可以手动归档旧业务输入，减少重复加载。
尤其不要在多个输入包中保留同名同 tag、但内容不同的镜像。
私有仓库先通过 docker login 完成登录，不把密码/token 写入此清单。

## 4. 继续构建离线包

仍在 offline-installer 目录执行：

```bash
bash ubuntu24.04/build-offline-bundle.sh --release-tag 2026.9.17
```

无需再次执行 save-images-from-lock.sh。最终制包按 images.lock 导出总镜像 tar，
自动渲染 single-node/cluster YAML。未下载而仍在 lock 中非空的应用镜像，须由本机 Docker
或已有输入 tar 提供。此脚本不操作服务器。

K8s 的 deb 由主构建入口自动调用 scripts/build-offline-packages.sh 准备，已有缓存时复用。
这个脚本只下载 deb，不负责 kube-apiserver/Calico/Longhorn 容器镜像。
主构建入口优先复用本机 Docker 或输入 tar 中的基础设施镜像；缺失时按 images.lock
自动拉取 Kubernetes、Calico、Longhorn 的固定版本，失败重试最多三次后停止。
不需要单独准备基础设施压缩包，也不需要把这些镜像加到本辅助脚本的下载清单。
最终只生成一个自包含的安装压缩包，不增加必须单独交付的 K8s 基础压缩包。
