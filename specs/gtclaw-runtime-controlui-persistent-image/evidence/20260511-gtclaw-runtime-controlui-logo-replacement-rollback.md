# GTClaw runtime Control UI logo replacement rollback evidence

Date: 2026-05-11

Verdict: GTCLAW_RUNTIME_CONTROLUI_LOGO_REPLACEMENT_ROLLBACK_DONE

## Scope

- Rolled back the repo-owned runtime Control UI favicon overlay added for the logo replacement.
- Kept GTManager management frontend logo files untouched.
- Did not build, tag, push, deploy, or roll out a new image.
- Did not create a fresh replacement instance.

## Artifact rollback

Changed files:

- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/MANIFEST.md`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/Dockerfile`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/MANIFEST.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260511-gtclaw-runtime-controlui-logo-replacement-rollback.md`

Removed files:

- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/favicon.svg`
- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/favicon.ico`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/favicon.svg`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/favicon.ico`

Dockerfile readback:

```text
No COPY instruction remains for control-ui/favicon.svg or control-ui/favicon.ico.
```

Manifest readback:

```text
No declaration remains for a GT logo favicon overlay, frontend logo source asset, or favicon overlay hash.
The manifests now state that Control UI root favicon assets are inherited from the parent OpenClaw runtime image.
```

File absence readback:

```text
absent specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/favicon.svg
absent specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/favicon.ico
absent specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/favicon.svg
absent specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/favicon.ico
```

## Runtime instance handling

Instance `26` was the only runtime instance mutated by this rollback gate.

Actions:

- Updated instance `26` name to `superseded-gtclaw-logo-repl-093501`.
- Updated instance `26` description to record the rollback.
- Stopped instance `26`.
- Did not delete instance `26`.

Readback:

```text
instance 25: id=25 name=oc2gi-anloc-121909 status=running pod=clawreef-25-oc2gi-anloc-121909 namespace=clawmanager-user-1 ip=10.42.0.121
instance 26: id=26 name=superseded-gtclaw-logo-repl-093501 status=stopped image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-logo-replacement-20260511093501
clawmanager-user-1 pods: only clawreef-25-oc2gi-anloc-121909 remains Running
```

Instance `17` and instance `18` were not mutated. API readback returned not found for both IDs in this environment.

## Non-actions

- No frontend GTManager logo files were modified.
- No backend/auth/scope/security predicate files were modified.
- No image build, tag, or push was executed.
- No deploy or rollout was executed.
- No OpenClaw/GTClaw/OpenSparrow global replacement was performed.
- No old evidence, session, PVC, image, or browser cache cleanup was performed.
- No `passes:true` or Close action was executed.
- No git stage, commit, or push was executed.
- No sensitive credential values or request header values are recorded in this evidence.
