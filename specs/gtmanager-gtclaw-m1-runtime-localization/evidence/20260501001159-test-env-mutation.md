# Test Environment Mutation - gtmanager-gtclaw-m1-runtime-localization

Timestamp: 2026-05-01 00:11:59 Asia/Shanghai

Gate: TestEnvironmentMutationWorker, authorized after T8 `TEST ENV NOT READY`.

Scope: test environment only. No frontend, backend, deployment, docs, longterm, spec, plan, tasks, old M1, `latest`, or Close mutation was performed.

## Result

A disposable OpenClaw runtime test instance was created for the T8 rerun:

| Item | Value |
| --- | --- |
| Instance ID | `3` |
| Instance name | `gtclaw-t8-dev-20260501001159` |
| Namespace | `clawmanager-user-1` |
| Pod | `clawreef-3-gtclaw-t8-dev-20260501001159` |
| Container | `desktop` |
| Pod phase / Ready | `Running` / `true` |
| Instance API status after restore | `running` |
| Pod restarts after restore | `0` |

## Pre-mutation State

`git status --short` before this mutation:

```text
 M .specify/memory/constitution.md
 M AGENTS.md
 M README.md
 M docs/k3s-local-setup.md
 M docs/manual-skill-import.md
 M docs/manual-skill-import_en.md
 M frontend/index.html
 M frontend/src/components/AdminLayout.tsx
 M frontend/src/components/InstanceAccess.tsx
 M frontend/src/components/OpenClawDesktopOverlay.tsx
 M frontend/src/components/UserLayout.tsx
 M frontend/src/contexts/I18nContext.tsx
 M frontend/src/lib/i18n.ts
 M frontend/src/pages/admin/SystemSettingsPage.tsx
 M frontend/src/pages/instances/CreateInstancePage.tsx
 M frontend/src/stores/authStore.ts
 M frontend/src/types/instance.ts
 M longterm/CHECKLIST.md
 M longterm/workspace/app_spec.md
 M longterm/workspace/init.sh
?? backend/AGENTS.md
?? clawmanagerArm/
?? dist/
?? frontend/AGENTS.md
?? frontend/public/gtmanager-logo.png
?? specs/
```

Running user runtime pod before the mutation:

| Namespace | Pod | Ready | Container | Image | ImageID |
| --- | --- | --- | --- | --- | --- |
| `clawmanager-user-1` | `clawreef-2-a1-local-registry-004259` | `true` | `desktop` | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434` | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8` |

Original `openclaw` system image setting, read through GTManager API:

```json
{
  "id": 1,
  "instance_type": "openclaw",
  "display_name": "OpenClaw ARM Local Registry",
  "image": "k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434",
  "is_enabled": true,
  "created_at": "2026-04-16T03:31:12Z",
  "updated_at": "2026-04-27T16:39:17Z"
}
```

## T6 Image and Alias Proof

Target T6 dev image:

`localhost:5001/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029`

Local Docker image inspection:

| Field | Value |
| --- | --- |
| Image ID / image index digest | `sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45` |
| Descriptor media type | `application/vnd.oci.image.index.v1+json` |
| Descriptor size | `856` |
| Platform | `linux/arm64` |
| Repo digest | `localhost:5001/clawmanager-openclaw/openclaw@sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45` |

Host registry `localhost:5001` manifest HEAD returned:

```text
HTTP/1.1 200 OK
Content-Type: application/vnd.oci.image.index.v1+json
Docker-Content-Digest: sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45
```

In-cluster k3d registry alias was checked from the running `clawmanager-app` pod:

`http://k3d-clawmanager-registry:5000/v2/clawmanager-openclaw/openclaw/manifests/gtclaw-resources-dev-20260430192029`

Alias manifest HEAD returned the same digest:

```text
HTTP/1.1 200 OK
Content-Type: application/vnd.oci.image.index.v1+json
Docker-Content-Digest: sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45
```

Both host registry and in-cluster alias exposed the same linux/arm64 manifest:

| Platform | Manifest digest |
| --- | --- |
| `linux/arm64` | `sha256:112421e96f10a9b45fb679ae1784f269c552680f19066e754ebff0fec11c7984` |

The in-cluster alias manifest body SHA-256 was:

`b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45`

Decision: the k3d alias is digest-equivalent to the target T6 dev image, so the temporary system setting used:

`k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029`

## Mutation Steps

1. Authenticated to GTManager API without printing credentials or tokens.
2. Temporarily changed `openclaw` system image setting to:

```json
{
  "instance_type": "openclaw",
  "display_name": "OpenClaw ARM Local Registry",
  "image": "k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029"
}
```

3. Created disposable instance:

```json
{
  "id": 3,
  "name": "gtclaw-t8-dev-20260501001159",
  "type": "openclaw",
  "image_registry": "k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029",
  "image_tag": null,
  "pod_name": "clawreef-3-gtclaw-t8-dev-20260501001159",
  "pod_namespace": "clawmanager-user-1"
}
```

4. Waited for pod Ready:

```text
pod/clawreef-3-gtclaw-t8-dev-20260501001159 condition met
```

## Running Pod Evidence

Post-create pod state:

| Namespace | Pod | Phase | Ready | Container | Image | ImageID |
| --- | --- | --- | --- | --- | --- | --- |
| `clawmanager-user-1` | `clawreef-3-gtclaw-t8-dev-20260501001159` | `Running` | `true` | `desktop` | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029` | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45` |

Package:

```text
openclaw@2026.4.14
```

Control UI path:

```text
/usr/local/lib/node_modules/openclaw/dist/control-ui
```

Hash verification inside the running pod:

| File | SHA-256 |
| --- | --- |
| `index.html` | `ed3560d9fa9b9156e62a405bc185c2d3495129ee3712ef8c536767f79d5778c7` |
| `assets/index-M4TNVXB3.js` | `eb8379dce913df535806486c8a4f637ab27f069c95925ef836821fbde2cb321b` |
| `assets/i18n-B06L7jQN.js` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` |
| `assets/zh-CN-B26mMdbY.js` | `9a4ecc8992d00443ef59de0be41090099d5a1feb25cf062c5c02470044277f29` |

## Setting Restore

After the pod was Ready and hash evidence matched, the original `openclaw` system image setting was restored through GTManager API:

```json
{
  "id": 1,
  "instance_type": "openclaw",
  "display_name": "OpenClaw ARM Local Registry",
  "image": "k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434",
  "is_enabled": true,
  "updated_at": "2026-04-30T16:12:42Z"
}
```

The new test pod remained available after restore:

| Namespace | Pod | Phase | Ready | Restarts | ImageID |
| --- | --- | --- | --- | ---: | --- |
| `clawmanager-user-1` | `clawreef-3-gtclaw-t8-dev-20260501001159` | `Running` | `true` | `0` | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45` |

Current running user runtime pods after the mutation:

| Namespace | Pod | Ready | Image | ImageID |
| --- | --- | --- | --- | --- |
| `clawmanager-user-1` | `clawreef-2-a1-local-registry-004259` | `true` | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434` | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8` |
| `clawmanager-user-1` | `clawreef-3-gtclaw-t8-dev-20260501001159` | `true` | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029` | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45` |

## T8 Rerun Inputs

Use this instance for the T8 rerun:

| Field | Value |
| --- | --- |
| Instance ID | `3` |
| Instance name | `gtclaw-t8-dev-20260501001159` |
| Pod namespace/name | `clawmanager-user-1/clawreef-3-gtclaw-t8-dev-20260501001159` |
| Container | `desktop` |
| Pod image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029` |
| Pod imageID | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45` |
| T6 image index digest | `sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45` |
| T6 linux/arm64 manifest digest | `sha256:112421e96f10a9b45fb679ae1784f269c552680f19066e754ebff0fec11c7984` |
| Base image digest | `sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8` |

Proxy access acquisition for T8:

1. Authenticate to GTManager as an authorized user.
2. Call `POST /api/v1/instances/3/access`.
3. Do not write the returned token or token-bearing `proxy_url` to evidence.
4. Use the returned cookie/session behavior or keep the token-bearing URL only in process memory while checking proxy root and `/chat?session=main`.

Do not output credential-bearing URLs in the T8 report.

## Rollback

Delete the disposable test instance created by this task:

- UI path: GTManager instances list -> delete `gtclaw-t8-dev-20260501001159`.
- API path: authenticated `DELETE /api/v1/instances/3`.

Confirm the disposable runtime is gone:

```bash
kubectl get pod -n clawmanager-user-1 clawreef-3-gtclaw-t8-dev-20260501001159
```

Expected after deletion: Kubernetes reports the pod is not found.

Confirm the system image setting is restored:

```bash
curl -sk -H "Authorization: Bearer <redacted>" https://localhost:30443/api/v1/system-settings/images
```

Expected `openclaw` item:

```json
{
  "instance_type": "openclaw",
  "display_name": "OpenClaw ARM Local Registry",
  "image": "k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434",
  "is_enabled": true
}
```

Confirm this task did not introduce source, deployment, docs, or longterm changes:

```bash
git status --short
git status --short -- backend frontend deployments docs longterm
git status --short -- specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260501001159-test-env-mutation.md
```

Expected: existing dirty worktree entries may remain from pre-existing work, and this task's only intended repository write is this evidence file.

## Non-mutation Statements

- No `frontend/**` files were edited by this task.
- No `backend/**` files were edited by this task.
- No `deployments/**` files were edited by this task.
- No `docs/**` files were edited by this task.
- No `longterm/**` files were edited by this task.
- Current feature `spec.md`, `plan.md`, and `tasks.md` were not edited by this task.
- No old M1 feature files were edited.
- No `latest` tag was overwritten.
- No old/base runtime instance was deleted or recreated.
- No credentials, cookies, tokens, secrets, `.env`, `.codex/auth.json`, or `.codex/config.toml` content is included in this report.
- No Close was performed and no `passes:true` state was written.
