# ARM64 部署说明

## ARM64 平台支持

本项目支持在 ARM64 (aarch64) 架构上部署，如树莓派、ARM开发板等设备。

## 手动 Skill 导入

默认发布路径可以不启用 `skill-scanner`，但这不影响手动上传 Skill `.zip` 并挂载到实例。

操作说明见：[manual-skill-import.md](./manual-skill-import.md)

## 预构建 ARM64 镜像

以下镜像已构建并发布到 GitHub Container Registry：

| 镜像 | 地址 | 说明 |
|------|------|------|
| ClawManager 主应用 | `ghcr.io/xty00/clawmanager:latest` | ARM64版本 |
| Skill Scanner | `ghcr.io/xty00/skill-scanner:latest` | ARM64版本（官方原仓库无ARM64支持） |

### 使用预构建镜像

```bash
# 拉取ARM64镜像
docker pull ghcr.io/xty00/clawmanager:latest --platform linux/arm64
docker pull ghcr.io/xty00/skill-scanner:latest --platform linux/arm64
```

## 从源码构建 ARM64 镜像

### 后端静态编译

```bash
cd backend
CGO_ENABLED=0 go build -ldflags="-s -w -extldflags=-static" -o bin/clawreef-server ./cmd/server
```

### Docker 多平台构建

```bash
# 构建并推送多平台镜像
docker buildx build --platform linux/amd64,linux/arm64 -t ghcr.io/your-name/clawmanager:latest --push .
```

### skill-scanner ARM64 构建

skill-scanner 原仓库 (`ghcr.io/yuan-lab-llm/skill-scanner`) 仅支持 amd64 平台。

ARM64 用户需要从源码构建：

```bash
# 克隆仓库
git clone https://github.com/Yuan-lab-LLM/skill-scanner.git
cd skill-scanner

# 使用代理构建（国内网络需要）
export http_proxy="http://your-proxy:port"
export https_proxy="http://your-proxy:port"

# 构建并推送ARM64镜像
docker buildx build --platform linux/arm64 \
  -t ghcr.io/your-name/skill-scanner:latest \
  --push .
```

## Kubernetes ARM64 部署

### 修改镜像地址

在 `clawmanager.yaml` 中将镜像地址替换为 ARM64 版本：

```yaml
# 主应用
image: ghcr.io/xty00/clawmanager:latest

# skill-scanner（如果需要）
image: ghcr.io/xty00/skill-scanner:latest
```

### 数据库初始化

首次部署需要手动创建数据库用户：

```bash
kubectl exec -it -n clawmanager-system mysql-xxx -- mysql -u root

# 在MySQL中执行
CREATE USER IF NOT EXISTS 'clawmanager'@'%' IDENTIFIED BY 'your-password';
GRANT ALL PRIVILEGES ON *.* TO 'clawmanager'@'%' WITH GRANT OPTION;
CREATE DATABASE IF NOT EXISTS clawmanager CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
FLUSH PRIVILEGES;
```

## 已知问题

### skill-scanner 官方镜像无 ARM64 支持

- **问题**: `ghcr.io/yuan-lab-llm/skill-scanner:latest` 仅提供 amd64 平台
- **解决方案**: 使用第三方构建的 ARM64 镜像 `ghcr.io/xty00/skill-scanner:latest`
- **备选方案**: 从源码自行构建（见上文）

### ARM64 设备性能注意

- 建议设备内存 >= 4GB
- 建议使用 SSD 存储（项目数据目录 `/mnt/Storage1`）
- 建议配置 swap 以避免内存不足

## 测试环境

以下环境已验证可正常运行：

- **开发板**: S922X-Oes-Plus (Amlogic S922X)
- **系统**: Armbian OS 26.05.0 trixie
- **内核**: 6.12.80-ophub
- **内存**: 3.6GB RAM
- **存储**: 110GB SSD
- **集群**: k3s v1.34.6
