# OpenClaw Remote Image Setting Swap

Date: 2026-05-11
Branch: dev

## Scope

This record backs up the previous GTClaw/OpenClaw runtime image setting and records the new remote image setting used for a pullability smoke test.

No source code, runtime artifact, deployment manifest, Kubernetes resource, instance, database schema, or registry content was modified by this file.

## Previous Setting

- instance_type: openclaw
- display_name: OpenClaw ARM Local Registry
- image: k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434
- is_enabled: true

## New Setting

- instance_type: openclaw
- display_name: GTClaw 桌面
- image: ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:latest
- is_enabled: true

## Remote Image Verification

- docker buildx imagetools inspect succeeded for ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:latest.
- The remote image index includes linux/amd64 and linux/arm64 manifests.

## API Verification

- PUT /api/v1/system-settings/images returned success for instance_type=openclaw.
- GET /api/v1/system-settings/images readback returned the new image for instance_type=openclaw.

## Not Performed

- No instance was created.
- No runtime image was built, tagged, pushed, or pulled into the cluster.
- No Kubernetes mutation was performed.
- No browser/manual E2E was performed.
- No git stage, commit, or push was performed.
- No Mem0 or longterm write was performed.
