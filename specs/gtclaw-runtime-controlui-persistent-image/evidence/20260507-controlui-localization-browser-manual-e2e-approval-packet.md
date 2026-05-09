# Control-ui localization browser/manual E2E approval packet

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Approval Packet

Gate: CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_APPROVAL_PACKET

Dependency gates:

- CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE_DONE
- COMMANDER_READONLY_REVIEW_ACCEPTED_FOR_CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE
- CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_DONE

## Verdict

CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_APPROVAL_PACKET_DONE

This document is an approval packet only. It does not run browser/manual E2E, open DevTools, run Playwright, modify browser storage/cache/cookie, mutate Kubernetes, mutate an instance, access or mutate the database, build/tag/push/pull an image, run a container, patch trustedProxy/runtime auth, close the feature, write longterm memory, stage, commit, or push git changes.

## Proposed next gate

Proposed next gate:

`CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_GATE`

Required user approval token:

`APPROVE_CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_GATE`

The next gate must not proceed without that exact explicit approval token.

## Runtime evidence dependency

Primary runtime-level dependency:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-expanded-capacity-recovery-and-2gi-fresh-instance.md`

Dependency result used by this packet:

```text
CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE_DONE
instance_id=16
instance_name=oc2gi-loc-221427
pod=clawmanager-user-1/clawreef-16-oc2gi-loc-221427
service=clawreef-16-oc2gi-loc-221427-svc
image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942
image_index_digest=sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54
linux_arm64_digest=sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e
control_ui_18789=HTTP 200 on loopback, PodIP, and ServiceIP
runtime_control_ui_path=/usr/local/lib/node_modules/openclaw/dist/control-ui
zh-CN_hash=cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
exactly_one_new_instance=true
```

Image build/tag/push dependency:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-build-tag-push.md`

Relevant image proof:

```text
CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_DONE
target_tag=gtclaw-controlui-localization-20260507211942
runtime_target=/usr/local/lib/node_modules/openclaw/dist/control-ui
index.html_hash=b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec
assets/index-M4TNVXB3.js_hash=d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648
assets/i18n-B06L7jQN.js_hash=3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63
assets/zh-CN-B26mMdbY.js_hash=cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
```

## Target instance and route expectation

Target instance:

```text
instance_id=16
instance_name=oc2gi-loc-221427
type=openclaw
memory_gb=2
expected_instance_page=/instances/16
expected_control_ui_route=/api/v1/instances/16/control-ui
accepted_child_route_example=/api/v1/instances/16/control-ui/chat?session=main
```

The later browser/manual E2E gate should open the ClawManager UI for instance `16` and use the normal visible GTManager action to open the control-ui route. Evidence must record only non-secret route shape/path evidence. It must not record any full token-bearing access URL, cookie value, credential value, bearer value, auth header value, or secret material.

Route correctness checks:

- The visible ClawManager instance context identifies `实例 ID: 16` or equivalent instance `16` evidence.
- The visible instance name is `oc2gi-loc-221427`.
- The control-ui route path is `/api/v1/instances/16/control-ui` or a child route under it.
- The route does not point to stale instance `10`, stale instance `11`, stale instance `15`, or any other instance.
- Any visible WebSocket URL shape, if shown, must reference instance `16` only and must be redacted to path/shape without access-token material.

## Proposed browser/manual E2E target

The next gate should verify GTClaw / OpenClaw control-ui localization presentation for instance `16`, using the already-created fresh runtime image.

Acceptance checks:

| Area | Required check |
| --- | --- |
| Route | Open ClawManager instance `16` control-ui route through the normal UI path; confirm it is not stale instance `10`, `11`, or `15`. |
| Brand/title | Confirm visible page brand/title includes `GTClaw 控制台` or the approved GTClaw Chinese title equivalent. |
| Locale | Confirm visible page renders zh-CN text and not an English-only control-ui shell. |
| Settings/connection | Confirm settings and connection/manual connection labels are localized where visible, including known labels such as WebSocket URL 地址, 网关 token, and 密码 if the manual connection form appears. |
| Chat | Confirm chat-facing navigation/content labels are localized if the UI reaches chat. |
| Skills | Confirm skills/tools-facing labels are localized if visible. |
| Agents | Confirm agents-facing labels are localized if visible. |
| Sessions | Confirm sessions/session-selector labels are localized if visible. |
| Errors | Confirm visible runtime/control-ui error messages or mapped client errors are localized when the localization bundle controls them; record unmapped backend/runtime text separately. |
| Regression boundary | Confirm the route is the control-ui route, not the desktop `/proxy/` route, not a 404, and not a browser cache artifact. |

This E2E is scoped to localization presentation and route correctness only. It is not authorized to fix or change trustedProxy, runtime auth, backend WebSocket auth bridge behavior, browser storage, runtime startup config, image content, Kubernetes resources, instance records, sessions, assets, registry tags, or database rows.

## Known blocker handling

Known blocker source:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-browser-manual-e2e-controlui-persistence.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-device-signature-invalid-root-cause.md`

Known blocker summary:

```text
prior_instance=15
prior_route=/api/v1/instances/15/control-ui/chat?session=main
prior_route_stale_instance_10_11_excluded=true
prior_origin_blocker_excluded=true
prior_1006_blocker_excluded=true
known_error=device signature invalid
root_cause=auth-contract mismatch between browser-generated device signature and backend rewritten upstream auth
```

If the next gate observes `device signature invalid`, a manual connection form, or gateway auth/device-signature failure on instance `16`, it must record that as a known auth-contract blocker. It must not fix trustedProxy, must not apply a runtime auth patch, must not alter backend/frontend/runtime code, must not mutate storage/cache/cookie to force a pass, and must not ask the user to provide or type gateway token, password, cookie, bearer, auth header, access URL, key, or other secret material.

The next gate may still record localization evidence visible before or alongside the blocker, such as `GTClaw 控制台`, zh-CN connection labels, visible route shape, and non-secret error text. If the blocker prevents validating deeper settings/chat/skills/agents/sessions views, those checks must be marked blocked by the known auth-contract issue, not passed.

## Proposed evidence requirements for next gate

The later browser/manual E2E evidence should include:

- approval token used: `APPROVE_CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_GATE`.
- target instance id/name: `16 / oc2gi-loc-221427`.
- route path/shape only, with no token-bearing full URL.
- confirmation that stale routes for instance `10`, `11`, and `15` were not used.
- visible title/brand evidence for `GTClaw 控制台`.
- visible zh-CN evidence for settings/connection/chat/skills/agents/sessions/errors where reachable.
- explicit handling of `device signature invalid` if observed.
- explicit statement that trustedProxy/runtime auth contract was not changed.
- explicit forbidden-actions confirmation.

## Secret hygiene

The next gate must not record token values, password values, key values, cookie values, bearer values, auth header values, full access URLs, registry credentials, `.env`, `.codex/auth.json`, or `.codex/config.toml` content. Route evidence should be limited to safe path shapes such as `/api/v1/instances/16/control-ui/chat?session=main`.

## Forbidden actions confirmation for this approval packet

- no browser/manual E2E
- no DevTools
- no Playwright
- no browser storage/cache/cookie cleanup
- no kubectl mutation
- no k3d
- no Helm
- no instance mutation
- no database access or mutation
- no image build/tag/push/pull
- no container run
- no docker run
- no docker compose
- no trustedProxy patch
- no runtime auth patch
- no plugin/skill distribution
- no backend modification
- no frontend modification
- no deployments modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no UnifiedFramework modification
- no existing artifact modification
- no existing evidence modification
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push

## Verification commands

```bash
sed -n '1,320p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-expanded-capacity-recovery-and-2gi-fresh-instance.md
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-browser-manual-e2e-approval-packet.md
rg -n "CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_APPROVAL_PACKET_DONE|oc2gi-loc-221427|GTClaw 控制台|zh-CN|18789|device signature invalid|no trustedProxy|no runtime auth patch|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-browser-manual-e2e-approval-packet.md
rg -n -i -o "(api[_-]?key|access[_-]?token|gateway[_-]?token|password|secret|authorization|bearer|jwt|private[_-]?key)[[:space:]]*[:=][[:space:]<>]*[^[:space:]<>]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-browser-manual-e2e-approval-packet.md | wc -l
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-browser-manual-e2e-approval-packet.md
```

## Verification results

| Command | Exit | Result |
| --- | ---: | --- |
| `sed -n '1,320p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-expanded-capacity-recovery-and-2gi-fresh-instance.md` | `0` | Runtime-level dependency evidence rendered for review. |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-browser-manual-e2e-approval-packet.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including approval-packet verdict, `oc2gi-loc-221427`, `GTClaw 控制台`, `zh-CN`, `18789`, `device signature invalid`, `no trustedProxy`, `no runtime auth patch`, `no passes:true`, `no Close`, and `no git stage/commit/push`. |
| secret-shape scan piped to `wc -l` | `0` | Output was `0`. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-browser-manual-e2e-approval-packet.md` | `0` | Shows this allowed new approval-packet evidence file as untracked. |
