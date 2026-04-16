# ARM64 Deployment Guide

## ARM64 Platform Support

This project supports deployment on ARM64 (aarch64) architecture devices such as Raspberry Pi and ARM development boards.

## Manual Skill Import

The default release path can run without `skill-scanner`, but manual Skill `.zip` upload and attach-to-instance are still supported.

See: [manual-skill-import_en.md](./manual-skill-import_en.md)

## Pre-built ARM64 Images

The following images have been built and published to GitHub Container Registry:

| Image | Address | Description |
|-------|---------|-------------|
| ClawManager Main App | `ghcr.io/xty00/clawmanager:latest` | ARM64 version |
| Skill Scanner | `ghcr.io/xty00/skill-scanner:latest` | ARM64 version (official repo has no ARM64 support) |

### Using Pre-built Images

```bash
# Pull ARM64 images
docker pull ghcr.io/xty00/clawmanager:latest --platform linux/arm64
docker pull ghcr.io/xty00/skill-scanner:latest --platform linux/arm64
```

## Building ARM64 Images from Source

### Backend Static Compilation

```bash
cd backend
CGO_ENABLED=0 go build -ldflags="-s -w -extldflags=-static" -o bin/clawreef-server ./cmd/server
```

### Docker Multi-platform Build

```bash
# Build and push multi-platform images
docker buildx build --platform linux/amd64,linux/arm64 -t ghcr.io/your-name/clawmanager:latest --push .
```

### skill-scanner ARM64 Build

The skill-scanner official repo (`ghcr.io/yuan-lab-llm/skill-scanner`) only supports amd64 platform.

ARM64 users need to build from source:

```bash
# Clone the repo
git clone https://github.com/Yuan-lab-LLM/skill-scanner.git
cd skill-scanner

# Use proxy if needed (for China networks)
export http_proxy="http://your-proxy:port"
export https_proxy="http://your-proxy:port"

# Build and push ARM64 image
docker buildx build --platform linux/arm64 \
  -t ghcr.io/your-name/skill-scanner:latest \
  --push .
```

## Kubernetes ARM64 Deployment

### Modify Image Addresses

In `clawmanager.yaml`, replace image addresses with ARM64 versions:

```yaml
# Main app
image: ghcr.io/xty00/clawmanager:latest

# skill-scanner (if needed)
image: ghcr.io/xty00/skill-scanner:latest
```

### Database Initialization

Manual database user creation is required for first deployment:

```bash
kubectl exec -it -n clawmanager-system mysql-xxx -- mysql -u root

# Execute in MySQL
CREATE USER IF NOT EXISTS 'clawmanager'@'%' IDENTIFIED BY 'your-password';
GRANT ALL PRIVILEGES ON *.* TO 'clawmanager'@'%' WITH GRANT OPTION;
CREATE DATABASE IF NOT EXISTS clawmanager CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
FLUSH PRIVILEGES;
```

## Known Issues

### skill-scanner Official Image Has No ARM64 Support

- **Problem**: `ghcr.io/yuan-lab-llm/skill-scanner:latest` only provides amd64 platform
- **Solution**: Use third-party ARM64 image `ghcr.io/xty00/skill-scanner:latest`
- **Alternative**: Build from source (see above)

### ARM64 Device Performance Notes

- Recommended device memory >= 4GB
- Recommended SSD storage (project data directory `/mnt/Storage1`)
- Recommended swap configuration to avoid OOM

## Tested Environment

The following environment has been verified to work:

- **Board**: S922X-Oes-Plus (Amlogic S922X)
- **OS**: Armbian OS 26.05.0 trixie
- **Kernel**: 6.12.80-ophub
- **Memory**: 3.6GB RAM
- **Storage**: 110GB SSD
- **Cluster**: k3s v1.34.6
