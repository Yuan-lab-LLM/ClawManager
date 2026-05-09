# Control-ui localization browser/manual E2E evidence

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Browser / Manual E2E Evidence

Gate: CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_GATE

Approval token used:

- APPROVE_CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_GATE

Dependency gates:

- CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_APPROVAL_PACKET_DONE
- CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE_DONE
- COMMANDER_READONLY_REVIEW_ACCEPTED_FOR_CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_APPROVAL_PACKET

## Verdict

CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_BLOCKED: browser-control tooling was unavailable before the ClawManager page could be observed, so no browser/manual E2E route, instance-context, or localization page evidence could be honestly recorded for instance 16.

Not `CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_DONE`: the browser/manual route and visible GTClaw localization checks were not completed.

## Target and runtime dependency

Target instance:

```text
instance_id=16
instance_name=oc2gi-loc-221427
pod=clawmanager-user-1/clawreef-16-oc2gi-loc-221427
service=clawreef-16-oc2gi-loc-221427-svc
```

Runtime evidence dependency:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-expanded-capacity-recovery-and-2gi-fresh-instance.md`

Dependency facts read from the approved runtime evidence:

```text
CONTROLUI_LOCALIZATION_EXPANDED_CAPACITY_RECOVERY_AND_2GI_FRESH_INSTANCE_DONE
instance_id=16
instance_name=oc2gi-loc-221427
control_ui_18789=HTTP 200 on loopback, PodIP, and ServiceIP
runtime_path=/usr/local/lib/node_modules/openclaw/dist/control-ui
zh-CN_hash=cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f
exactly_one_new_instance=true
```

## Browser/manual E2E attempt

Browser surface requested by task: open ClawManager UI, navigate to instance `16`, and enter the instance `16` control-ui route through the normal visible UI path.

Tooling result:

```text
browser_use_preferred_surface=node_repl browser-client with iab backend
browser_use_status=unavailable_in_this_session_after_tool_discovery
computer_use_fallback_status=blocked_by_macos_automation_permission
browser_page_observed=false
browser_manual_e2e_executed=false
```

Because the browser page could not be observed, this gate did not navigate a browser to ClawManager, did not click the instance control-ui action, did not inspect visible browser text, and did not take screenshots.

## Route shape

Expected route from the approval packet:

```text
expected_route=/api/v1/instances/16/control-ui
expected_child_route=/api/v1/instances/16/control-ui/chat?session=main
```

Observed route:

```text
route_observed=not_observed
route_instance_16_confirmed=false
stale_instance_10_excluded=not_observed
stale_instance_11_excluded=not_observed
stale_instance_15_excluded=not_observed
```

No full token-bearing access URL was recorded.

## Instance context

Expected instance context:

```text
instance_id=16
instance_name=oc2gi-loc-221427
```

Observed browser instance context:

```text
instance_context_observed=false
visible_instance_id=not_observed
visible_instance_name=not_observed
```

## Localization evidence

Required localization targets:

- `GTClaw 控制台` or approved equivalent GTClaw Chinese title/brand.
- zh-CN visible text.
- settings/connection/chat/skills/agents/sessions/errors Chinese copy where reachable.

Observed localization evidence:

```text
GTClaw 控制台=not_observed
zh-CN_visible_text=not_observed
settings_copy=blocked_before_browser_observation
connection_copy=blocked_before_browser_observation
chat_copy=blocked_before_browser_observation
skills_copy=blocked_before_browser_observation
agents_copy=blocked_before_browser_observation
sessions_copy=blocked_before_browser_observation
errors_copy=blocked_before_browser_observation
```

No screenshot or browser-visible text summary was captured because no browser page state was available.

## Known blocker handling

Known blocker from prior evidence:

```text
known_blocker=device signature invalid
known_blocker_class=auth-contract blocker
prior_manual_connection_form=true
prior_manual_connection_labels=WebSocket URL 地址, 网关 token, 密码
```

Instance `16` result:

```text
device_signature_invalid_observed_on_instance_16=not_observed
manual_connection_form_observed_on_instance_16=not_observed
deeper_chat_settings_skills_agents_sessions_checks=blocked_before_browser_observation
```

If a later approved browser/manual gate observes `device signature invalid` or a manual connection form for instance `16`, it should record it as the known auth-contract blocker. This gate did not and must not fix backend bridge behavior, trustedProxy, runtime auth contract, device signature, token flow, or password flow.

## Secret hygiene

No token value, password value, key value, cookie value, bearer value, auth header value, full access URL, registry credential, `.env`, `.codex/auth.json`, or `.codex/config.toml` content was requested, typed, captured, or recorded.

## Forbidden actions confirmation

- no trustedProxy patch
- no runtime auth patch
- no backend source edits
- no frontend source edits
- no runtime source edits
- no control-ui artifact edits
- no image edits
- no deployments edits
- no docs edits
- no longterm edits
- no AgentTeam edits
- no UnifiedFramework edits
- no kubectl mutation
- no k3d
- no Helm
- no instance mutation
- no database mutation
- no image build/tag/push/pull
- no container run
- no docker run
- no docker compose
- no browser storage/cache/cookie cleanup
- no storage cleanup
- no request for gateway token/password/key/cookie/bearer/auth header/access URL
- no plugin/skill distribution
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push

## Verification commands

```bash
sed -n '1,360p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-browser-manual-e2e.md
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-browser-manual-e2e.md
rg -n "CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_DONE|oc2gi-loc-221427|/api/v1/instances/16/control-ui|GTClaw 控制台|zh-CN|device signature invalid|manual connection|no trustedProxy|no runtime auth patch|no storage|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-browser-manual-e2e.md
rg -n -i -o "(api[_-]?key|access[_-]?token|gateway[_-]?token|password|secret|authorization|bearer|jwt|private[_-]?key)[[:space:]]*[:=][[:space:]<>]*[^[:space:]<>]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|https?://[^[:space:]]*[?&](token|access|auth|password|key)=" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-browser-manual-e2e.md | wc -l
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-browser-manual-e2e.md
```

## Verification results

| Command | Exit | Result |
| --- | ---: | --- |
| `sed -n '1,360p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-browser-manual-e2e.md` | `0` | Evidence content rendered for review. |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-browser-manual-e2e.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including the not-DONE marker, `oc2gi-loc-221427`, `/api/v1/instances/16/control-ui`, `GTClaw 控制台`, `zh-CN`, `device signature invalid`, `manual connection`, `no trustedProxy`, `no runtime auth patch`, `no storage`, `no passes:true`, `no Close`, and `no git stage/commit/push`. |
| secret/access URL shape scan piped to `wc -l` | `0` | Output was `0`. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-browser-manual-e2e.md` | `0` | Shows this allowed new browser/manual E2E evidence file as untracked. |
