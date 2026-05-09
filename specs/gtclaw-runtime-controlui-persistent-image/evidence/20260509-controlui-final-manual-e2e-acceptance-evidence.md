# Control UI Final Manual E2E Acceptance Evidence

Date/timezone: 2026-05-09, Asia/Shanghai

Role/task: Verifier, serial topology

Gate type: CONTROLUI_FINAL_MANUAL_E2E_ACCEPTANCE_EVIDENCE_GATE

## Verdict

```text
CONTROLUI_FINAL_MANUAL_E2E_ACCEPTANCE_EVIDENCE_DONE
```

This evidence records the user's manual E2E acceptance result only. Verifier did not run browser/manual E2E, did not mutate the instance, did not build or deploy, and did not perform Close or `passes:true` write-back.

## Dependency Gates

```text
CONTROLUI_AGENT_NODES_LOCALIZATION_RUNTIME_DELIVERY_DONE
user_manual_e2e_instance=25 / oc2gi-anloc-121909
user_conclusion=基本汉化没问题
```

Dependency evidence read:

```text
specs/gtclaw-runtime-controlui-persistent-image/evidence/20260509-controlui-agent-nodes-localization-runtime-delivery-and-manual-e2e.md
```

Relevant dependency facts:

```text
created_id=25
created_name=oc2gi-anloc-121909
api_instance_status=running
created_pod=clawreef-25-oc2gi-anloc-121909
service=clawreef-25-oc2gi-anloc-121909-svc
runtime_18789_status=HTTP 200 on loopback, PodIP, and ServiceIP
```

## User Manual E2E Target

```text
instance_id=25
instance_name=oc2gi-anloc-121909
manual_e2e_url=https://localhost:30443/api/v1/instances/25/control-ui/chat?session=main
```

The URL above is recorded because the user provided it as the manual E2E target. It contains no token-bearing query parameter.

## User Manual E2E Acceptance Result

The user reported manual E2E on instance `25 / oc2gi-anloc-121909` with the following acceptance result:

```text
connected_chat_ready_reached=true
internal_ui_reached=true
agents_page_basic_acceptance=基本通过
nodes_page_basic_acceptance=基本通过
overall_user_acceptance=基本汉化没问题
```

Interpretation boundary:

- `connected/chat ready reached` is accepted from the user's manual E2E observation.
- `internal UI reached` is accepted from the user's manual E2E observation.
- `代理页基本通过` is accepted from the user's manual E2E observation.
- `节点页基本通过` is accepted from the user's manual E2E observation.
- The user's final acceptance wording is recorded as `基本汉化没问题`.

## Accepted Residual Boundary

The user accepted the following residual boundary for this manual E2E acceptance record:

```text
code_file_literals_may_remain=true
examples=main, AGENTS, SOUL
dynamic_metadata_residuals_if_any=deferred
```

Code or file literals such as `main`, `AGENTS`, and `SOUL` may remain. Dynamic metadata residuals, if any, are deferred and are not blockers for this acceptance evidence gate.

## Verifier Non-Actions

Verifier actions in this gate were limited to reading prior evidence and writing this final manual acceptance evidence file.

Explicit non-actions:

```text
no build/deploy
no instance mutation
no browser/manual E2E by Verifier
no cleanup
no passes:true
no Close
no longterm write-back
no git stage/commit/push
```

No backend, frontend, runtime, deployment, Kubernetes, database, image, registry, browser storage, cache, cookie, session, or old evidence mutation was performed.

## Secret Hygiene

No token value, password value, key value, cookie value, bearer value, auth header value, JWT value, registry credential, `.env`, `.codex/auth.json`, `.codex/config.toml`, or token-bearing full access URL is recorded in this evidence.

The recorded URL is:

```text
https://localhost:30443/api/v1/instances/25/control-ui/chat?session=main
```

It contains only `session=main`.

## Verification Commands

```bash
sed -n '1,240p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260509-controlui-final-manual-e2e-acceptance-evidence.md
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260509-controlui-final-manual-e2e-acceptance-evidence.md
for marker in 'CONTROLUI_FINAL_MANUAL_E2E_ACCEPTANCE_EVIDENCE_DONE' '25 / oc2gi-anloc-121909' 'https://localhost:30443/api/v1/instances/25/control-ui/chat?session=main' 'connected_chat_ready_reached=true' 'internal_ui_reached=true' 'agents_page_basic_acceptance=基本通过' 'nodes_page_basic_acceptance=基本通过' 'overall_user_acceptance=基本汉化没问题' 'main, AGENTS, SOUL' 'dynamic_metadata_residuals_if_any=deferred' 'no build/deploy' 'no instance mutation' 'no browser/manual E2E by Verifier' 'no cleanup' 'no passes:true' 'no Close' 'no longterm write-back' 'no git stage/commit/push'; do grep -Fq "$marker" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260509-controlui-final-manual-e2e-acceptance-evidence.md || exit 1; done
/Users/eduardogan/.npm-global/lib/node_modules/@openai/codex/node_modules/@openai/codex-darwin-arm64/vendor/aarch64-apple-darwin/path/rg -n -i -o '(api[_-]?key|access[_-]?token|gateway[_-]?token|password|secret|authorization|bearer|jwt|private[_-]?key)[[:space:]]*[:=][[:space:]<>]*[^[:space:]<>]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|https?://[^[:space:]]*[?&](token|access|auth|password|key)=' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260509-controlui-final-manual-e2e-acceptance-evidence.md | wc -l
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260509-controlui-final-manual-e2e-acceptance-evidence.md
```

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `sed -n '1,240p' ...` | `0` | Evidence content rendered for review. |
| `git diff --check -- ...` | `0` | No whitespace errors. |
| required marker loop | `0` | Output: `all required markers present`. |
| secret/access URL shape scan piped to `wc -l` | `0` | Output was `0`. |
| `git status --short -- ...` | `0` | Shows this allowed new final manual acceptance evidence file as untracked. |
