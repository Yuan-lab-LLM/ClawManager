# Evidence Index

Feature: gtclaw-runtime-controlui-persistent-image
Date: 2026-05-07
Status: non-authoritative index

## Purpose

This index makes the evidence corpus navigable. It does not replace individual
evidence files, does not rewrite historical evidence, and does not mark feature
acceptance. If this index conflicts with a specific gate evidence file, inspect
the gate evidence file and add a future correction note instead of rewriting
history.

## Current Active Chain

Latest accepted gate:
FRESH_INSTANCE_RUNTIME_DEPLOYMENT_APPROVAL_PACKET_CONTROLUI_PERSISTENCE_DONE

Latest built runtime image:

- Host tag: localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712
- In-cluster tag: k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712
- Image index digest: sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908
- Linux arm64 digest: sha256:0616ee761cee6ed35addd9becfe7ec0cbc6df4821c67c9f8a0607490f6e33d0a

Current blocking fact:

- Fresh instance/runtime deployment for the latest image has approval-packet
  evidence but has not been approved for execution in this chain.
- Browser/manual E2E has not passed for the latest image.
- Therefore there is no passes:true, no Close, and no longterm write-back.

Current next gate:

- Fresh Instance / Runtime Deployment Gate for the latest control-ui persistence
  image, only after explicit user approval.

## Evidence Groups

### 20260503 baseline and runtime delivery

This group covers early runtime image, source inspection, image delivery,
generated artifact handling, and first persistent runtime delivery preparation.

Representative evidence names include files with:

- `20260503-`
- `persistent-image`
- `runtime-delivery`
- `image-delivery`
- `source-inspection`

### 20260504 runtime and browser investigation

This group covers browser checks, 18789 listener checks, runtime reachability,
fresh instance loops, and pre-close runtime validation.

Representative evidence names include files with:

- `20260504-`
- `18789`
- `browser`
- `fresh-instance`
- `runtime`

### 20260505 F-007 close-era work

This group covers runtime startup artifact changes, backend persistent runtime
checks, final close-era evidence, governance updates, historical archive, and
generated cleanup.

Representative evidence names include files with:

- `20260505-`
- `runtime-startup-artifact`
- `backend-persistent-runtime`
- `close`
- `governance`
- `generated`

### 20260506 post-close follow-up

This group is the active follow-up chain created after manual E2E exposed new
GTClaw control-ui usability issues.

Subchains:

- WS challenge root cause, bridge implementation, hardening, review, backend
  build/deploy.
- Origin allowlist root cause, runtime config materialization investigation,
  runtime startup artifact implementation, runtime image build/tag/push, fresh
  instance deployment, and manual E2E block.
- Stale route root cause, persistence-fix approval, source artifact recovery,
  patched control-ui bundle implementation, image assembly artifact, latest
  runtime image build/tag/push, and fresh-instance approval packet.

Current important evidence:

- `20260506-runtime-image-assembly-artifact.md`
- `20260506-runtime-image-build-tag-push-controlui-persistence.md`
- `20260506-fresh-instance-runtime-deployment-approval-packet-controlui-persistence.md`

## Worker Name Normalization

Historical evidence may mention specific worker names. Future prompts should
normalize these to the canonical team model:

- Commander
- Worker
- Verifier
- Reviewer
- Closer

Specializations such as Design, Research, Implementation, Evidence Review,
Architecture Review, and Closeout are task types, not persistent agent roles.

## Handling Rules

- no delete of historical evidence.
- no rewrite historical evidence.
- no old session cleanup from this index.
- no old asset cleanup from this index.
- no build/tag/push/pull from this index.
- no deploy/restart from this index.
- no fresh instance mutation from this index.
- no browser E2E from this index.
- no K8S, runtime, database, or registry mutation from this index.
- no token, password, or key input from this index.
- no passes:true from this index.
- no Close from this index.
- no git stage, commit, or push from this index.

## Next Read Order

For the active GTClaw chain, read in this order:

1. `20260506-runtime-image-assembly-artifact.md`
2. `20260506-runtime-image-build-tag-push-controlui-persistence.md`
3. `20260506-fresh-instance-runtime-deployment-approval-packet-controlui-persistence.md`
4. The future fresh-instance deployment evidence, once approved and created.
5. The future browser/manual E2E evidence, once approved and created.
