# Browser E2E Approval Packet - 2026-05-04

## Verdict

BROWSER_E2E_APPROVAL_PACKET_DONE

This is an approval packet only. It is not Browser E2E execution authorization, and no browser E2E executed under this packet.

`BLOCKED` is not the current packet verdict. If later evidence shows the fresh instance is missing, deployed pod hashes do not match, previous setting was not restored, or this packet cannot be used without executing E2E first, the next worker must stop with `BLOCKED` and must not guess or repair.

## Gate Statement

Approval packet only, no browser E2E executed, no browser opened, no Playwright run, no DOM route check, no desktop route check, no token-bearing URL access, no runtime pod/container read or write, no `kubectl cp`, no runtime/K8S/database mutation, no registry mutation, no image/tag mutation, no backend/frontend/deployments/docs/longterm/AgentTeam/spec/plan/tasks/existing evidence modification, no Mem0 write, no longterm write, no passes:true, no Close.

Only this repository file was written:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-approval-packet.md`

## Dependency Evidence

| Dependency | Evidence file | Commander-reviewed result |
| --- | --- | --- |
| T017-T021 image delivery loop | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-image-delivery-loop-rerun-after-baseline-decision.md` | `IMAGE_DELIVERY_LOOP_RERUN_DONE`; source artifact hash = built image hash |
| T022 fresh instance approval packet | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-approval-packet.md` | `FRESH_INSTANCE_APPROVAL_PACKET_DONE`; fresh instance gate prepared, browser E2E excluded |
| T023-T026 fresh disposable instance and pod hash | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-mutation-and-pod-hash.md` | `FRESH_INSTANCE_POD_HASH_DONE`; fresh deployed pod hash matched all four files |
| Historical F-006 browser criteria baseline | `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260502-t8i-e2e-rerun-after-runtime-body-fix.md` | Criteria baseline only, not persistence proof |
| Historical body repair addendum | `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260503-runtime-controlui-body-repair-manifest-addendum.md` | Display-string and protected-literal baseline only, not persistence proof |

## Fresh Instance Identity For Future E2E

Future Browser E2E, if explicitly approved by the user, must target the already-created fresh disposable instance below. It must not create another instance or mutate this instance unless separately approved.

| Field | Value |
| --- | --- |
| Instance | `5` / `gtclaw-fresh-20260504095843` |
| Namespace | `clawmanager-user-1` |
| Pod | `clawreef-5-gtclaw-fresh-20260504095843` |
| Container | `desktop` |
| Pod image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| imageID | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| Image index digest | `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| linux/arm64 manifest digest | `sha256:d1fbcb65e2a576ffe32396d61931d86931b1e87c829051001bd5be67e067261e` |
| Restart count | `0` |
| Package | `openclaw@2026.4.14` |
| Runtime path | `/usr/local/lib/node_modules/openclaw/dist/control-ui` |
| Previous setting restore | restored image matched previous setting |
| Manual pod patch status | no manual pod patch; no pod file copy-in or repair in place |

## Deployed Pod Hash Dependency

Fresh pod hash evidence recorded all four allowlist files as matched under `/usr/local/lib/node_modules/openclaw/dist/control-ui`.

| File | Required deployed pod SHA-256 | Size |
| --- | --- | ---: |
| `index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` |
| `assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | `42617` |
| `assets/zh-CN-B26mMdbY.js` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` | `23255` |
| `assets/index-M4TNVXB3.js` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` | `707959` |

## Proposed Future E2E Scope

If and only if the user explicitly approves the next gate, Browser E2E may cover only these checks:

| Future check | Scope |
| --- | --- |
| Authorized GTManager `/control-ui/` root DOM check | Use authorized access for instance `5`; record redacted route metadata and DOM outcome only |
| Authorized GTManager `/control-ui/chat?session=main` DOM check | Confirm chat route renders the same localized control-ui body |
| Authorized GTManager history fallback DOM check | Confirm a history fallback path under `/control-ui/` routes back to the localized control-ui body |
| Desktop regression | Confirm `access?mode=desktop` and `/proxy/` remain desktop behavior and are not replaced by control-ui |

No other browser checks are approved by this packet. No backend proxy redesign, frontend code change, runtime file read/write, pod patch, fresh instance creation, registry mutation, image mutation, database mutation, or deployment default change is included.

## Future E2E Criteria

Future Browser E2E evidence must include these criteria:

| Route class | Required criteria |
| --- | --- |
| `/control-ui/` root | Visible DOM body renders `GTClaw 控制台`; `html lang=zh-CN` or equivalent DOM evidence; required Chinese body markers present; no desktop fallback; no 404 marker |
| `/control-ui/chat?session=main` | Visible DOM body renders `GTClaw 控制台`; `html lang=zh-CN` or equivalent DOM evidence; required Chinese body markers present; no desktop fallback; no 404 marker |
| history fallback | Visible DOM body renders `GTClaw 控制台`; `html lang=zh-CN` or equivalent DOM evidence; required Chinese body markers present; no desktop fallback; no 404 marker |
| desktop regression | `access?mode=desktop` and `/proxy/` preserve desktop regression behavior and remain desktop behavior |

Required Chinese body markers should include the same visible-body family used by the historical F-006 T8I criteria, such as `控制台`, `网关`, and `连接`. Future evidence may also record localized labels such as `网关 token` and `密码 (不存储)` when visible, but it must not require removing preserved technical literals like `WebSocket URL`, lowercase `token`, or `Control UI` when they appear in technical context.

Historical F-006 T8I evidence is only a criteria baseline. It must not be reused as persistence proof for this feature. The future E2E packet must record fresh-instance route results for instance `5`.

## Secret Hygiene Plan

Future Browser E2E must follow this secret hygiene plan:

- do not record token values
- do not record cookie values
- do not record credentials
- do not record secrets
- do not record `.env`
- do not record `.codex/auth.json`
- do not record `.codex/config.toml`
- do not record any token-bearing URL
- access tokens and cookies may only be used transiently if later explicitly approved
- route cookies may be used only in process memory; evidence may record cookie names, paths, and redacted presence metadata, but not values
- evidence must prefer redacted route labels and DOM findings over raw request URLs if a URL could contain credentials or tokens

Required future statement:

`No token value, cookie value, credential, secret, .env, .codex/auth.json, .codex/config.toml, or token-bearing URL was recorded.`

The required scan phrase for this approval packet is: no token-bearing URL.

## Proposed Future Evidence Output

Proposed output path for actual Browser E2E, for later approval only:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e.md`

This packet does not create that file and does not authorize writing it until the user explicitly approves the next Browser E2E gate.

## Stop Conditions For Future Browser E2E Worker

The future worker must stop with `BLOCKED` and must not run browser E2E if any of these are true at the next gate:

- fresh instance `5` / `gtclaw-fresh-20260504095843` is unavailable or no longer maps to pod `clawmanager-user-1/clawreef-5-gtclaw-fresh-20260504095843`
- pod image is not `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506`
- imageID no longer maps to `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10`
- linux/arm64 digest evidence is inconsistent with `sha256:d1fbcb65e2a576ffe32396d61931d86931b1e87c829051001bd5be67e067261e`
- fresh pod hash evidence is missing or contradicted
- previous runtime image/resource setting is not restored
- any manual pod patch, pod file copy-in, runtime file repair, or runtime process restart is detected
- the worker would need to record a token-bearing URL, token value, cookie value, credential, or secret to proceed

## Explicit User Approval Template For Next Gate

The following text is a template only. It is not approval unless the user sends it as their own instruction.

```text
I explicitly approve Browser E2E for specs/gtclaw-runtime-controlui-persistent-image using the approval packet:
specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-approval-packet.md

Authorized target:
- Instance: 5 / gtclaw-fresh-20260504095843
- Pod: clawmanager-user-1/clawreef-5-gtclaw-fresh-20260504095843 / desktop
- Image: k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506
- imageID digest: sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10
- linux/arm64 manifest digest: sha256:d1fbcb65e2a576ffe32396d61931d86931b1e87c829051001bd5be67e067261e

Allowed actions:
- Use authorized GTManager access for instance 5 with tokens/cookies only transiently.
- Run browser DOM checks for /control-ui/, /control-ui/chat?session=main, and one /control-ui/ history fallback route.
- Run desktop regression checks for access?mode=desktop and /proxy/.
- Write only:
  specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e.md

Required evidence:
- visible DOM body renders GTClaw 控制台
- html lang=zh-CN or equivalent DOM evidence
- required Chinese body markers present
- no desktop fallback in control-ui routes
- no 404 marker in control-ui routes
- /proxy/ desktop behavior preserved
- no token value, cookie value, credential, secret, .env, .codex/auth.json, .codex/config.toml, or token-bearing URL recorded

Forbidden actions:
- Do not create, delete, restart, patch, or modify any runtime instance/pod/container.
- Do not modify runtime image/resource setting.
- Do not read or write pod files.
- Do not run kubectl cp.
- Do not mutate runtime, Kubernetes, database, registry, image, or tags.
- Do not modify backend/**, frontend/**, deployments/**, docs/**, longterm/**, AgentTeam/**, spec.md, plan.md, tasks.md, existing evidence, or /tmp/gtclaw-runtime-patch/**.
- Do not write Mem0 or longterm.
- Do not write passes:true.
- Do not Close.
```

## Verification Commands

Commands to verify this approval packet:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-approval-packet.md
rg -n "BROWSER_E2E_APPROVAL_PACKET_DONE|BLOCKED|gtclaw-controlui-persistent-20260504005506|clawreef-5-gtclaw-fresh-20260504095843|b63a62a|d1fbcb65|/control-ui/|chat\\?session=main|history fallback|/proxy/|desktop regression|GTClaw 控制台|zh-CN|no browser E2E executed|no token-bearing URL|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-approval-packet.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-approval-packet.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-mutation-and-pod-hash.md
```
