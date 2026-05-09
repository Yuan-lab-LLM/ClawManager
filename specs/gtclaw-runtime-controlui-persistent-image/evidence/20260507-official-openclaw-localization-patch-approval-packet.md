# Official OpenClaw localization patch approval packet

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Approval Packet

Gate: CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH_APPROVAL_PACKET

Dependency gate:

- CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PLUGIN_DISCOVERY_DONE

## Verdict

CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH_APPROVAL_PACKET_DONE

This is an approval packet only. It authorizes neither implementation nor runtime operations.

Recommended path: use the existing repo-owned static control-ui artifact overlay for GTClaw control-ui localization.

Do not continue the trustedProxy/runtime auth contract patch for this localization objective. The trustedProxy/runtime auth direction addresses mediated WebSocket auth semantics; this gate is only about GTClaw control-ui Chinese localization and static asset delivery.

## Evidence basis

The dependency discovery packet found that official OpenClaw control-ui localization is represented by built control-ui i18n assets, not by a dedicated localization plugin or skill distribution mechanism.

Relevant static artifact facts:

- Current repo-owned runtime control-ui artifact path: `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/`
- Current artifact `index.html` sets `lang="zh-CN"` and title `GTClaw 控制台`.
- Current artifact includes `assets/zh-CN-B26mMdbY.js` and `assets/i18n-B06L7jQN.js`.
- Current artifact includes `assets/index-M4TNVXB3.js`, which already carries prior control-ui persistence wiring.
- Current runtime image assembly artifact copies the static control-ui files to `/usr/local/lib/node_modules/openclaw/dist/control-ui`.

This evidence is sufficient to approve a future narrow localization patch gate over repo-owned static control-ui artifacts. It is not approval to build, tag, push, deploy, create an instance, run browser E2E, or close the feature.

## Plugin and skill decision

Plugin/skill mechanisms are not the control-ui i18n override mechanism for this gate.

- Locale files are built control-ui assets.
- `openclaw.plugin.json` package metadata and extension-bundled skills do not prove a safe static control-ui locale override path.
- Skills are agent/tooling instructions, not browser UI locale bundles.
- Control-ui skill chunks render or manage skills; they do not prove that a skill can inject or replace control-ui i18n strings.

If GTClaw needs skill distribution later, it should be split into a separate follow-up gate with its own approval packet and evidence. Skill distribution must not be bundled into this control-ui localization patch approval.

## Approved next gate target

Recommended next gate:

`CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH_GATE`

Future patch intent:

- Localize GTClaw-facing control-ui text through the repo-owned static control-ui artifact overlay.
- Preserve protected OpenClaw technical literals where they are package paths, config names, command names, runtime identifiers, or compatibility strings.
- Keep the patch limited to static control-ui output files unless evidence proves a loader or persistence wiring dependency.
- Record before and after hashes, string coverage, changed file list, and exact patch method in a new implementation evidence file.

The future gate must stop as BLOCKED if it cannot localize the target strings without expanding into backend, frontend GTManager, deployments, docs, longterm, AgentTeam, UnifiedFramework, runtime auth semantics, image build/push/deploy, or browser E2E scope.

## Candidate patch file scope

Allowed candidate static control-ui patch files:

- `index.html`
- `assets/zh-CN-*.js`
- `assets/i18n-*.js` only if default locale/loader requires
- `assets/index-*.js` only if title/default-locale/persistence wiring requires

The expected primary file is `assets/zh-CN-*.js`, with `index.html` allowed for browser language/title metadata. `assets/i18n-*.js` and `assets/index-*.js` are conditional, not default scope.

The next implementation evidence should state whether it edited:

- only the repo-owned runtime control-ui artifact, or
- a new repo-owned localization artifact copied from the current approved artifact.

Any runtime image assembly/build context update must be handled by a separate image assembly/build approval gate.

## Explicit non-goals

This packet does not approve:

- trustedProxy/runtime auth contract patch continuation
- backend WebSocket bridge changes
- runtime gateway auth/schema changes
- frontend GTManager source changes
- deployment manifest changes
- docs, longterm, AgentTeam, or UnifiedFramework changes
- plugin localization packaging
- skill localization packaging
- image build/tag/push/pull
- fresh instance creation or mutation
- browser/manual E2E
- `passes:true`
- Close
- git stage/commit/push

## Test and verification plan

Future localization patch gate file-level checks:

- artifact hash diff for each changed static control-ui file
- before/after file size and SHA-256 manifest
- string coverage scan for required GTClaw Chinese display strings
- protected literal scan for lower-case OpenClaw technical literals and command/config identifiers
- static grep/AST-safe scan to confirm no secret-bearing values are introduced
- `git diff --check` on the new evidence and changed artifact files

Separate later gates, not approved here:

- image assembly/build gate separately approved after the localization patch evidence exists
- image build/tag/push/pull only after separate approval
- fresh instance deployment only after separate approval
- fresh instance + browser/manual E2E only after separate approval
- feature close/write-back only after passing evidence and separate approval

## Approval options

APPROVE_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH_GATE: approve a future implementation gate limited to the candidate static control-ui files above, using the repo-owned static control-ui artifact overlay path and excluding trustedProxy/runtime auth work.

REJECT_OR_BLOCK: <reason>

No approval is implied by this packet. The next implementation gate must not start unless the user explicitly approves `APPROVE_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH_GATE`.

## Forbidden actions statement

Forbidden actions were not executed. Specifically: no implementation, no patch, no backend modification, no frontend modification, no deployments modification, no docs modification, no longterm modification, no AgentTeam modification, no UnifiedFramework modification, no existing evidence modification, no build/tag/push, no pull image, no browser E2E, no DevTools, no Playwright, no kubectl, no k3d, no Helm, no image build/tag/push/pull, no container run, no instance create/delete/modify, no database access or modification, no Mem0 write, no passes:true, no Close, and no git stage/commit/push.

No token, password, key, cookie, bearer material, JWT, authorization header plaintext, private key, or access URL plaintext was recorded.
