# K3S 本机部署指南（macOS）

> **目标**：在 macOS 本机用 k3d 运行 K3S，部署 GTManager，并完成 A1 core-chain 的单实例 + direct QA 最小验收链。

如果你是第一次拿到交付包，不要先看这份。先看仓库根目录的：

- `00-START-HERE.md`

它是从零开始的单文件傻瓜版入口。

## Current Release Scope

The current release path is the **A1 core-chain release**:

- default Mac/ARM deploy path targets GTManager control plane, one documented OpenClaw ARM bootstrap, and one single-instance + direct-QA acceptance chain
- `skill-scanner` / FastSkill / frozen `U3` validation are **not** part of the default release claim
- the blocked `R3-A-U3-F2-P1` investigation packet remains archived under `docs/superpowers/releases/2026-04-15-p1-internal-rc/`
- use `docs/superpowers/releases/2026-04-16-a1-core-release/README.md` as the release-scope summary
- use `docs/superpowers/releases/2026-04-16-a1-core-release/03-bootstrap-contract.md` as the canonical runtime-image + model-bootstrap contract
- use `docs/superpowers/releases/2026-04-16-a1-core-release/04-acceptance-path.md` as the canonical single-instance + direct-QA acceptance path
- use `docs/superpowers/releases/2026-04-16-a1-core-release/05-external-test-guide.md` when handing this to an external tester
- if you are packaging this for another operator, include the full `docs/superpowers/releases/2026-04-16-a1-core-release/` packet rather than this tutorial alone

---

## 背景：K3S 在 macOS 上的限制

K3S 是 Linux-only 的。macOS 上有三种方案：

| 方案 | 工具 | 特点 |
|------|------|------|
| **A（推荐）** | k3d | K3S 运行在 Docker 容器内，最快上手，mac 原生支持好 |
| B | Rancher Desktop | GUI 工具，内置 K3S，适合不熟悉命令行的用户 |
| C | Colima + K3S | 轻量 Linux VM，性能更接近原生，但配置稍复杂 |

**本文使用方案 A（k3d）**。

---

## 前置条件

```bash
# 1. Docker Desktop 必须安装并运行
open -a Docker

# 2. 安装 kubectl
brew install kubectl

# 3. 安装 k3d
brew install k3d

# 验证
k3d version
kubectl version --client
```

---

## 第一步：创建 K3S 集群

```bash
# 创建单节点 K3S 集群，将 NodePort 30443 映射到本机
k3d cluster create clawmanager \
  --port "30443:30443@loadbalancer" \
  --agents 0

# 验证集群运行
kubectl cluster-info
kubectl get nodes
```

预期输出（kubectl get nodes）：
```
NAME                      STATUS   ROLES                  AGE   VERSION
k3d-clawmanager-server-0  Ready    control-plane,master   30s   v1.31.x+k3s1
```

---

## 第二步：部署 GTManager

```bash
# 进入项目根目录
cd /Users/eduardogan/Desktop/GHJProject/ClawManager

# 部署（K3S 版配置）
kubectl apply -f deployments/k3s/clawmanager.yaml

# 查看部署状态
kubectl get pods -n clawmanager-system -w
```

当前默认发布路径中，`skill-scanner` 不参与主链 bring-up。

预期默认状态：
```
NAME                               READY   STATUS    RESTARTS
mysql-xxx                          1/1     Running   0
clawmanager-app-xxx                1/1     Running   0
```

> **说明**：首次启动需要拉取镜像（`ghcr.io/yuan-lab-llm/clawmanager:latest`），视网络情况可能需要 3–10 分钟。
> 当前 `deployments/k3s/clawmanager.yaml` 使用本地 `emptyDir` 作为对象存储回退，因此本机 K3S 验证默认不会看到 `minio` Pod。
> 当前默认发布路径将 `skill-scanner` 关闭；如果后续要做 FastSkill / `U3` 验证，再单独开启。
> `clawmanager-gateway` 是给 runtime agent 用的集群内 `ClusterIP` Service；K3S 清单需要暴露 `9001 -> 9001`，这样实例内的 agent 才能通过 `http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001` 向控制面注册。
> 除非有新证据推翻已验证的 HTTP `clawmanager-gateway:9001` 注册路径，否则不要回到 `https://...:8443` URL-builder 改法。

---

## 第三步：确认数据库初始化

GTManager 首次启动时会通过 MySQL 容器环境变量和 `clawmanager-mysql-init` ConfigMap 自动创建数据库、应用用户和基础表结构。
只有在 **复用旧 PVC** 或 **初始化脚本未执行成功** 时，才需要人工进入 MySQL 复核。

```bash
# 找到 MySQL pod 名称
MYSQL_POD=$(kubectl get pods -n clawmanager-system -l app=mysql -o name | head -1)
echo "MySQL pod: $MYSQL_POD"

# 查看当前 Secret 中的实际密码
MYSQL_ROOT_PASSWORD=$(kubectl get secret -n clawmanager-system clawmanager-secrets -o jsonpath='{.data.mysql-root-password}' | base64 -d)
MYSQL_APP_PASSWORD=$(kubectl get secret -n clawmanager-system clawmanager-secrets -o jsonpath='{.data.mysql-password}' | base64 -d)

echo "mysql-root-password=${MYSQL_ROOT_PASSWORD}"
echo "mysql-password=${MYSQL_APP_PASSWORD}"

# 进入 MySQL，确认数据库和应用用户已经存在
kubectl exec -it -n clawmanager-system $MYSQL_POD -- \
  mysql -uroot -p"${MYSQL_ROOT_PASSWORD}" -e "SHOW DATABASES; SELECT User,Host FROM mysql.user;"
```

> **注意**：K3S 清单里的 Secret 名称是 `clawmanager-secrets`，应用用户密码默认来自 `mysql-password` 字段，而不是旧文档里的 `mysql-secret` / `clawmanager123`。

---

## 第四步：访问 GTManager

```bash
# 访问管理员门户
open https://localhost:30443

# 或用 curl 验证
curl -k https://localhost:30443/healthz
```

浏览器会提示证书不受信任（自签名）—— 选择"继续访问"即可。

---

## 第五步：创建一个 OpenClaw 实例（A1 单实例主链）

在进入这一步之前，先完成 operator 侧 model bootstrap：

- 至少存在一个 active normal model
- 至少存在一个 active secure model
- 如果 `/api/v1/admin/models` 仍为空，停止在这里，不要假装实例链已可验证

当前 A1 正式发布路径只冻结一条最小成功链：

- deploy
- admin login
- ARM runtime image bootstrap
- normal + secure model bootstrap
- create one OpenClaw instance
- wait until runtime ready
- capture one direct-QA proof

可以通过 GTManager UI 创建实例，也可以直接按 `docs/superpowers/releases/2026-04-16-a1-core-release/04-acceptance-path.md` 中的 API 命令创建。

如果你只是照着本教程操作，至少遵守这两个边界：

- 第一条验证实例按已验证 profile 走：`type=openclaw`、`memory_gb=3`
- 当前 A1 不要求多实例并发，不要把 multi-instance 当默认签收链

1. 登录管理员账户
2. 完成 `03-bootstrap-contract.md` 里的 ARM runtime image bootstrap 与 model bootstrap
3. 进入「实例管理」→「创建实例」
4. 创建 1 个 OpenClaw 实例
5. 验证 K3S 中的 Pod：

```bash
# 查看用户命名空间中的 OpenClaw 实例 Pod
kubectl get pods -A | grep clawreef
```

然后继续按 `04-acceptance-path.md` 完成 runtime ready 与 direct QA proof，不要停在 admin ready 或 model boundary。

---

## 常用管理命令

```bash
# 查看所有 GTManager 相关 Pod
kubectl get pods -n clawmanager-system

# 查看日志
kubectl logs -n clawmanager-system deployment/clawmanager-app -f

# 重启 GTManager app（不影响数据库）
kubectl rollout restart -n clawmanager-system deployment/clawmanager-app

# 停止集群（保留数据）
k3d cluster stop clawmanager

# 再次启动
k3d cluster start clawmanager

# 完全删除集群（清空所有数据）
k3d cluster delete clawmanager

# 查看集群资源使用
kubectl top nodes
kubectl top pods -n clawmanager-system
```

---

## ARM64 用户（Apple Silicon / Mac mini M 系列）

官方镜像 `ghcr.io/yuan-lab-llm/clawmanager:latest` 支持 ARM64。  
当前默认发布路径中，不需要先解决 `skill-scanner` ARM64 问题，因为 scanner 默认关闭。

ARM 本地主链需要补的，是 OpenClaw runtime bootstrap image。
先按 `docs/superpowers/releases/2026-04-16-a1-core-release/03-bootstrap-contract.md` 获取 `CM_API` 与 `CM_ADMIN_TOKEN`，再执行下面的精确 bootstrap：

```bash
CM_BASE_URL=https://localhost:30443

curl -sk -X PUT "$CM_BASE_URL/api/v1/system-settings/images" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"instance_type":"openclaw","display_name":"OpenClaw ARM Dev Bootstrap","image":"ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:dev-arm64-pkt09-20260414170434"}'
```

完成上面的 runtime bootstrap 后，当前默认发布路径可以到达：

- `mysql` / `clawmanager-app` 全部 `Running`
- `https://localhost:30443/healthz` 返回 `200`
- admin 登录成功
- `/api/v1/system-settings/images` 可见 OpenClaw ARM dev bootstrap image
- 然后继续完成 `03-bootstrap-contract.md` 里的 normal + secure model bootstrap
- 最后按 `04-acceptance-path.md` 完成单实例 runtime ready 与 direct QA proof

如果你后续要单独验证 FastSkill / `skill-scanner`，再参考 `docs/arm64-deployment_en.md` 自行构建或单独开启 scanner 路径。

---

## 故障排查

| 问题 | 检查命令 | 可能原因 |
|------|----------|----------|
| Pod 一直 Pending | `kubectl describe pod -n clawmanager-system <pod>` | 镜像拉取失败 / 资源不足 |
| clawmanager-app CrashLoopBackOff | `kubectl logs -n clawmanager-system deployment/clawmanager-app` | 数据库未就绪 / 配置错误 |
| 无法访问 30443 | `k3d cluster list` | 集群未运行 / 端口映射问题 |
| MySQL 连接失败 | `kubectl logs -n clawmanager-system deployment/mysql` | 初始化脚本失败 |
| 实例 `running` 但 agent 仍是 `offline` | `kubectl get svc,endpoints -n clawmanager-system clawmanager-gateway && kubectl exec -n <user-namespace> <runtime-pod> -- curl -sv http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001/healthz` | `clawmanager-gateway` 未暴露 `9001` / endpoint 未就绪 / app 未监听 `9001` |
| admin 已可登录，但不应继续建验证实例 | `curl -sk https://localhost:30443/api/v1/admin/models -H "Authorization: Bearer $CM_ADMIN_TOKEN"` | operator 还没有创建 active normal model + active secure model；这是文档化前置条件，不是隐藏技术故障 |

## 可选：后续启用 skill-scanner

如果后续要单独推进 FastSkill / `U3`：

- 把 `deployments/k3s/clawmanager.yaml` 中 `SKILL_SCANNER_ENABLED` 改回 `true`
- 把 `skill-scanner` Deployment `replicas` 改回 `1`
- 在 Mac/ARM 上单独解决 `skill-scanner` 镜像来源问题

---

## 后续：迁移到 Mac mini

当本机测试通过后，将集群迁移到 Mac mini 的步骤：

1. 在 Mac mini 上安装 Docker Desktop + k3d + kubectl
2. 复制整个 ClawManager 项目到 Mac mini
3. 重复本文档步骤
4. 修改本机的 `~/.kube/config` 以连接远程 Mac mini（可选，用于远程管理）

---

*Last updated: 2026-04-16*
