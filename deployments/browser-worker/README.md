# Browser Worker

Browser Worker is an optional, per-instance visible Chromium component for
OpenClaw Lite. It lets an operator complete interactive login or verification
in ClawManager while OpenClaw controls the same browser over CDP.

## Architecture

When the option is selected during instance creation, ClawManager stores the
desired state and reconciles one Kubernetes Deployment and Service named
`clawbrowser-<instance-id>`. The Deployment contains:

- a visible Chromium/noVNC container;
- a small CDP proxy sidecar restricted to the local pod;
- a persistent browser profile on the instance workspace PVC;
- an in-memory `/dev/shm` volume sized for Chromium.

The backend patches only the `browser` section of `openclaw.json`, preserving a
restorable snapshot of the previous value. Browser access is authenticated by
ClawManager and proxied through the existing public origin; no Kubernetes
Service is exposed outside the cluster.

## Build inputs

- `Dockerfile` builds the visible browser image from a digest-pinned Chromium
  base image.
- `Dockerfile.cdp-proxy` builds the CDP proxy from source in a multi-stage image.

The default image tags are `clawmanager/browser-worker:0.1.0` and
`clawmanager/cdp-proxy:0.1.0`. Deployments can override them with
`CLAWMANAGER_BROWSER_WORKER_IMAGE` and `CLAWMANAGER_CDP_PROXY_IMAGE`.

The first release intentionally supports create-time opt-in only. It contains
no instance-specific allow-list, pilot manifest, or mutation control for
existing instances.
