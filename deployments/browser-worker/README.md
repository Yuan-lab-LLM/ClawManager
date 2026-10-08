# Browser Worker

Browser Worker is an optional, per-instance visible Chromium component for
OpenClaw Lite. It lets an operator complete interactive login or verification
in ClawManager while OpenClaw controls the same browser over CDP.

## Architecture

When the option is selected during instance creation, ClawManager stores the
desired state and reconciles one Kubernetes Deployment and Service named
`clawbrowser-<instance-id>`. The Deployment contains:

- a visible Chromium/noVNC container;
- an authenticated CDP/GUI relay sidecar;
- a persistent browser profile on the instance workspace PVC;
- an in-memory `/dev/shm` volume sized for Chromium.

The backend patches only the `browser` section of `openclaw.json`, preserving a
restorable snapshot of the previous value. Browser access is authenticated by
ClawManager and proxied through the existing public origin; no Kubernetes
Service is exposed outside the cluster. This is manual completion of verification,
not a CAPTCHA bypass or a guarantee that a website will accept a login.

## Isolation and lifecycle

Each instance gets a cryptographically random 256-bit credential in a Kubernetes
Secret. Chromium's GUI, VNC and raw CDP listen only on loopback. The relay requires
instance-specific Basic authentication for **both** HTTP and WebSocket requests:
the backend authenticates GUI requests after checking the user's scoped cookie;
OpenClaw uses a credential-bearing CDP URL in its private `openclaw.json`. Never
copy that URL into PRs, logs, screenshots or shared configuration exports.

A per-worker ingress NetworkPolicy admits only the backend's `app=clawmanager-app`
pods on relay port 5801 and OpenClaw runtime pods on relay port 9222. CDP still
requires its instance credential, because multiple Lite instances share runtime
pods. A NetworkPolicy-capable CNI is required for the network-isolation layer.
Custom installations must preserve these pod labels. Policy source namespaces
come from `POD_NAMESPACE` and `RUNTIME_NAMESPACE`, falling back to the Kubernetes
client's system namespace; a namespace selector and pod selector are both required.
The Service exposes 5800
(mapped to authenticated 5801) and 9222, not the raw loopback ports.

The elected control-plane leader reconciles desired state. Creating instances
remain pending until their runtime writes `openclaw.json`; a failed patch never
acknowledges the generation and is retried. GUI health reads do not mutate desired
or observed generations. Browser access cookies renew one minute before expiry;
manual refresh also renews before loading the iframe.

The common instance deletion path cleans the Deployment, Service, NetworkPolicy
and Secret before removing the instance record, so single, batch and Team deletion
share the same behavior. Failed cleanup preserves deletion intent for retry.
Orphan cleanup reclaims resources whose instance no longer exists. Deletion does
not erase the persisted browser profile; normal workspace retention applies.

## Build inputs

- `Dockerfile` builds the visible browser image from a digest-pinned Chromium
  base image.
- `Dockerfile.cdp-proxy` builds the CDP proxy from source in a multi-stage image.

There are deliberately **no default image tags**: this PR does not claim that an
upstream registry has published the images. Both image environment variables must
be configured before create-time opt-in is accepted. Missing images or invalid
options are rejected before creating an instance record.

### Build, publish, and configure (operator workflow)

From the repository root, substitute a registry path you control:

```sh
docker build -t <registry>/<owner>/browser-worker:0.2.0 deployments/browser-worker
docker build -f deployments/browser-worker/Dockerfile.cdp-proxy -t <registry>/<owner>/browser-worker-relay:0.2.0 deployments/browser-worker
docker push <registry>/<owner>/browser-worker:0.2.0
docker push <registry>/<owner>/browser-worker-relay:0.2.0
```

Configure the ClawManager **backend** Deployment with:

```yaml
- name: CLAWMANAGER_BROWSER_WORKER_IMAGE
  value: <registry>/<owner>/browser-worker:0.2.0
- name: CLAWMANAGER_CDP_PROXY_IMAGE
  value: <registry>/<owner>/browser-worker-relay:0.2.0
```

For offline clusters, export both built images and import them into the containerd
image store on every eligible Kubernetes node; use exactly the same image names
in these variables. `IfNotPresent` permits using those preloaded images. For a
private registry, provision node-level pull credentials before enabling opt-in.
Do not store a registry password in this repository.

Workers default to the same system namespace used by the Kubernetes client;
`CLAWMANAGER_BROWSER_WORKER_NAMESPACE`, when set, is used consistently by resource
creation and GUI proxying. The chosen namespace must contain the shared workspace
PVC (`RUNTIME_WORKSPACE_PVC_CLAIM`). For this first version, keep the workers,
backend and runtime pools in the same system namespace. PVC access mode and
scheduling must support mounting the profile alongside the runtime pool.

The CI Browser Worker job runs relay tests on Linux and builds both Dockerfiles
for `linux/amd64`, without publishing or deploying them. Multi-architecture
releases are not claimed by this first version. A release operator must publish
and verify pull access separately. Existing v0.1 experimental relay images are
not compatible with the authenticated v0.2 relay contract.

## Verification

Before release, run the backend suite, frontend build, frontend Browser Worker
renewal tests and relay suite. In a disposable Kubernetes environment additionally
verify: create-time opt-in, delayed config generation and retry, manual login in
the same CDP browser, HTTP/WebSocket authentication and cross-instance rejection,
cookie expiry/refresh, custom namespace, stop/start, batch deletion and orphan
cleanup. Fake-client unit tests do not validate CNI enforcement or Chromium
startup. Never use a production server to perform these release checks implicitly.

The first release intentionally supports create-time opt-in only. It contains
no instance-specific allow-list, pilot manifest, or mutation control for
existing instances.
