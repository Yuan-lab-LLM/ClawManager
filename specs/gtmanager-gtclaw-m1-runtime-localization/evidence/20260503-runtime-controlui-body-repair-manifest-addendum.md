# Runtime Control-UI Body Repair Manifest Addendum

Timestamp: 2026-05-03 Asia/Shanghai

Verdict scope: patch manifest evidence addendum only. This is not Close, not `passes:true`, and not a longterm write-back.

## Dependency Gate

This addendum is written under the Commander-provided gate that `RuntimeControlUIBodyLocalizationRepairWorker` returned `REPAIR APPLIED`.

The required T8I evidence packet exists and was reviewed as the evidence link for this addendum:

`specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260502-t8i-e2e-rerun-after-runtime-body-fix.md`

This addendum does not rerun E2E and does not change the runtime pod, runtime image, app image, registry tag, Kubernetes resources, source files, docs, specs, tasks, plan, or longterm state.

## Baseline

| Item | Value |
| --- | --- |
| App pod | `clawmanager-system/clawmanager-app-6c985497f5-2kdq8` |
| App container | `clawmanager-app` |
| App image | `clawmanager:backend-proxy-icon-rewrite-20260502231228` |
| App imageID | `sha256:4b787b460795c1fcd42f6b57716bb969bf7c9a671c66990026b5b5a916df911a` |
| Runtime pod | `clawmanager-user-1/clawreef-3-gtclaw-t8-dev-20260501001159` |
| Runtime container | `desktop` |
| Runtime pod phase | `Running` |
| Runtime container state | `ready=true`, `restartCount=0` |
| Runtime image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029` |
| Runtime imageID | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45` |
| Package | `openclaw@2026.4.14` |
| Runtime control-ui base | `/usr/local/lib/node_modules/openclaw/dist/control-ui` |
| Repair backup path | `/tmp/gtclaw-controlui-repair-backup-20260502233047` |

Baseline note: this is a disposable runtime pod patch record. It is not a persistent runtime image rebuild, not an app image rebuild, and not a registry delivery record.

## File Manifest

Read-only verification was run against the current runtime pod and the backup path. The backup directory was present.

| File | Before SHA-256 / size | Backup verified SHA-256 / size | After SHA-256 / size | Current pod verified SHA-256 / size |
| --- | ---: | ---: | ---: | ---: |
| `index.html` | `ed3560d9fa9b9156e62a405bc185c2d3495129ee3712ef8c536767f79d5778c7` / `3395` | `ed3560d9fa9b9156e62a405bc185c2d3495129ee3712ef8c536767f79d5778c7` / `3395` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` / `3398` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` / `3398` |
| `assets/i18n-B06L7jQN.js` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` / `42702` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` / `42702` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` / `42617` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` / `42617` |
| `assets/zh-CN-B26mMdbY.js` | `9a4ecc8992d00443ef59de0be41090099d5a1feb25cf062c5c02470044277f29` / `23248` | `9a4ecc8992d00443ef59de0be41090099d5a1feb25cf062c5c02470044277f29` / `23248` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` / `23255` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` / `23255` |
| `assets/index-M4TNVXB3.js` | `eb8379dce913df535806486c8a4f637ab27f069c95925ef836821fbde2cb321b` / `707543` | `eb8379dce913df535806486c8a4f637ab27f069c95925ef836821fbde2cb321b` / `707543` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` / `707959` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` / `707959` |

Manifest conclusion: current pod files match the repair worker after-hashes, and backup files match the repair worker before-hashes.

## Changed Display Strings

Summary-level old/new record only; no minified bundle snippets are copied.

| Surface | Before / old visible behavior | After / current visible behavior |
| --- | --- | --- |
| HTML document language | `<html lang="en">` | `<html lang="zh-CN">` |
| HTML title | `GTClaw 控制台` already present in the backup body-repair baseline | `GTClaw 控制台` preserved |
| Favicon and icons | Route-safe relative hrefs were present in the backup body-repair baseline: `./favicon.svg`, `./favicon-32.png`, `./apple-touch-icon.png` | Route-safe relative hrefs remain present; T8I verified proxied `/control-ui/favicon.svg` loads and history fallback does not create nested favicon paths |
| Default locale / zh-CN route path | Static/default path could still render English dashboard body labels despite zh-CN resources | Browser-visible authorized control-ui routes render with `lang=zh-CN`, GTClaw, and Chinese body labels |
| `Gateway Dashboard` | Forbidden English label in visible dashboard body before body repair | Not present in visible DOM body on T8I root/chat/history-fallback routes; body shows `GTClaw 控制台` and Chinese gateway/control text |
| `Gateway Token` | English label | `网关 token` |
| `Password` | English label | `密码 (不存储)` where rendered in the body |
| `Connect` | English action label | `连接` |
| `origin not allowed` warning | English warning body | `来源不被允许（请从网关主机打开 Control UI，或在 gateway.corsOrigin 中允许此来源。）` |
| `How to connect` | English help heading | `如何连接` |
| `Start the gateway on your host machine` | English instruction | `在主机上启动网关` |
| `Read the docs` | English docs action label was a forbidden visible-body label for T8I | Not present in visible DOM body on the required T8I routes |

Observed body evidence in T8I included localized control text such as `GTClaw 控制台`, `网关 token`, `密码 (不存储)`, `连接`, and the localized origin warning. T8I also recorded that `Gateway Dashboard`, `Gateway Token`, `Password`, `Connect`, `origin not allowed`, `How to connect`, `Start the gateway on your host machine`, and `Read the docs` were absent from the visible DOM body on the tested authorized control-ui routes.

## Preserved Technical Literals

The repair intentionally preserved technical/product literals where they name runtime mechanics, CLI/config behavior, paths, protocol settings, or compatibility identifiers.

| Literal / category | Preservation decision |
| --- | --- |
| `openclaw` | Preserved as lowercase runtime/package/config/client literal |
| `.openclaw` | Preserved as archive/profile/status convention |
| `openclaw.json` | Preserved as config filename |
| `openclaw dashboard --no-open` | Preserved as CLI command |
| `dist/control-ui` | Preserved as runtime package path fragment |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui` | Preserved as current control-ui base path |
| `WebSocket URL` | Preserved as protocol/config literal inside Chinese UI context |
| `token` / gateway token concept | Preserved as technical credential-type literal while localized as `网关 token` where user-visible |
| `gateway.corsOrigin` / CORS-origin concept | Preserved as gateway configuration literal inside the localized warning context |
| `Control UI` | Preserved where it refers to the upstream technical control-ui product/path concept inside Chinese explanatory text |

Read-only literal counts from the current runtime files remained compatible with T8I protected-literal evidence: `openclaw`, `.openclaw`, `openclaw.json`, and `openclaw dashboard --no-open` remain present. The repair did not broad-replace OpenClaw technical identity.

## Ambiguous / Deferred Decisions

Static runtime JS dictionaries/source text still contain English strings and technical/product literals that were not rendered in the visible browser DOM body during T8I.

| Static literal | T8I visible DOM body status | Decision |
| --- | --- | --- |
| `Gateway Dashboard` | Absent from visible DOM body | Deferred static dictionary/source text; not a T8I blocker because the authorized visible body did not render it |
| `Gateway Token` | Absent from visible DOM body | Deferred static dictionary/source text; visible body uses `网关 token` |
| `Password` | Absent as English visible body label | Deferred static/source occurrence; visible body uses localized password label |
| `Connect` | Absent as English visible body label | Deferred static/source occurrence; visible body uses `连接` |
| `origin not allowed` | Absent from visible DOM body | Deferred static/source occurrence; visible body uses localized warning |
| `How to connect` | Absent from visible DOM body | Deferred static/source occurrence; visible body uses localized heading |
| `Start the gateway on your host machine` | Absent from visible DOM body | Deferred static/source occurrence; visible body uses localized instruction |
| `Read the docs` | Absent from visible DOM body | Deferred static/source occurrence; not a T8I blocker under the corrected visible-body gate |
| `OpenClaw` / `openclaw` occurrences | Some preserved static/runtime occurrences remain | Preserved when technical; otherwise deferred unless a future approved manifest classifies a specific occurrence as user-visible display copy |

Reason this is not a T8I blocker: T8I was a browser-rendered authorized route gate. It verified `/control-ui/`, `/control-ui/chat?session=main`, and history fallback visible DOM body, not a requirement to remove every static English dictionary/source string from minified bundles. The visible body did not expose the enumerated forbidden English dashboard labels.

## Rollback Target And Commands

Rollback target: restore the four changed files from:

`/tmp/gtclaw-controlui-repair-backup-20260502233047`

Expected rollback hashes are the before/backup hashes listed in the File Manifest section.

Rollback was not executed for this addendum. The commands below are an operator record only and must not be run without separate explicit rollback approval.

```bash
kubectl exec -n clawmanager-user-1 clawreef-3-gtclaw-t8-dev-20260501001159 -c desktop -- sh -lc '
set -eu
base=/usr/local/lib/node_modules/openclaw/dist/control-ui
backup=/tmp/gtclaw-controlui-repair-backup-20260502233047
for f in \
  index.html \
  assets/i18n-B06L7jQN.js \
  assets/zh-CN-B26mMdbY.js \
  assets/index-M4TNVXB3.js
do
  test -f "$backup/$f"
  cp "$backup/$f" "$base/$f"
done
sha256sum \
  "$base/index.html" \
  "$base/assets/i18n-B06L7jQN.js" \
  "$base/assets/zh-CN-B26mMdbY.js" \
  "$base/assets/index-M4TNVXB3.js"
wc -c \
  "$base/index.html" \
  "$base/assets/i18n-B06L7jQN.js" \
  "$base/assets/zh-CN-B26mMdbY.js" \
  "$base/assets/index-M4TNVXB3.js"
'
```

No token, cookie, secret, credential, or token-bearing URL is required by the rollback command.

## Evidence Linkage

T8I evidence:

`specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260502-t8i-e2e-rerun-after-runtime-body-fix.md`

That T8I packet recorded:

- authorized `/api/v1/instances/3/control-ui/` route evidence
- independent `/api/v1/instances/3/control-ui/chat?session=main` evidence
- visible DOM body localization for the control-ui root, chat route, and history fallback
- route-safe favicon/history fallback behavior
- control-ui static asset loading under `/control-ui/`
- desktop `/proxy/` regression evidence
- deployed manager bundle asset URL/hash match evidence
- runtime loopback, PodIP, and ClusterIP reachability on `18789`
- protected OpenClaw technical literal evidence
- no Close and no `passes:true`

## Scope / Gate Statement

Only this addendum file was written:

`specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260503-runtime-controlui-body-repair-manifest-addendum.md`

No source, deployment, docs, longterm, existing spec, existing plan, existing tasks, old evidence, runtime pod file, runtime image, app image, registry tag, Kubernetes resource, Secret, ConfigMap, NodePort, Ingress, hostNetwork, namespace, or database mutation was performed by this addendum task.

This addendum is not Close. It does not set `passes:true`. It does not update `longterm/**`. It does not waive the T8I-recorded frontend lint debt or any future Close gate requirement.

## Verification Commands Run

The following verification was run in read-only mode except for writing this addendum file:

- `git status --short`
- `test -e specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260503-runtime-controlui-body-repair-manifest-addendum.md`
- `kubectl get pods -n clawmanager-system -o jsonpath=...`
- `kubectl get pod -n clawmanager-user-1 clawreef-3-gtclaw-t8-dev-20260501001159 -o jsonpath=...`
- `kubectl exec -n clawmanager-user-1 clawreef-3-gtclaw-t8-dev-20260501001159 -c desktop -- sh -lc 'node -p ...package version...'`
- `kubectl exec -n clawmanager-user-1 clawreef-3-gtclaw-t8-dev-20260501001159 -c desktop -- sh -lc 'sha256sum/wc current control-ui files...'`
- `kubectl exec -n clawmanager-user-1 clawreef-3-gtclaw-t8-dev-20260501001159 -c desktop -- sh -lc 'sha256sum/wc backup control-ui files...'`
- read-only grep/count checks for HTML language/icon refs, changed display labels, and preserved technical literals

Post-write checks are recorded by the Commander output, not by this section.
