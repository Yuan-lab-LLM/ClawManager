# Official OpenClaw localization/plugin discovery

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Localization Mechanism Discovery

Gate: CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PLUGIN_DISCOVERY

## Verdict

CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PLUGIN_DISCOVERY_DONE

This gate stops the trustedProxy/runtime auth contract patch direction. The immediate product goal is GTClaw control-ui localization, not runtime auth semantics.

## Scope

This was read-only discovery over ClawManager feature evidence/artifacts and locally available official OpenClaw package metadata for `openclaw@2026.4.14` / `v2026.4.14`.

OpenSparrow was not used as target source. It is only referenced as an excluded source path from prior evidence: OpenSparrow remote/local did not prove the presence of editable OpenClaw runtime control-ui TypeScript source.

## Findings: official OpenClaw i18n

Official OpenClaw control-ui already has an i18n resource structure in the distributed package.

Package metadata for `openclaw@2026.4.14` lists these control-ui locale/runtime assets:

- `dist/control-ui/assets/i18n-B06L7jQN.js`
- `dist/control-ui/assets/zh-CN-B26mMdbY.js`
- `dist/control-ui/assets/zh-TW-BIOFMTXl.js`
- `dist/control-ui/assets/de-f2soUk3u.js`
- `dist/control-ui/assets/es-DcaKxMRk.js`
- `dist/control-ui/assets/fr-DeC3E59l.js`
- `dist/control-ui/assets/id-DcPPhLHG.js`
- `dist/control-ui/assets/ja-JP-CzUriv4Z.js`
- `dist/control-ui/assets/ko-CeYCKpF4.js`
- `dist/control-ui/assets/pl-EqV8GjcE.js`
- `dist/control-ui/assets/pt-BR-BtdPUq-2.js`
- `dist/control-ui/assets/tr-D2kcSSP8.js`
- `dist/control-ui/assets/uk-DbYJ892B.js`

The current ClawManager control-ui runtime artifact confirms the app can be localized through static control-ui assets:

- `control-ui-runtime-artifact/20260506-persistence-fix-source/index.html` sets `lang="zh-CN"` and title `GTClaw 控制台`.
- `control-ui-runtime-artifact/20260506-persistence-fix-source/assets/i18n-B06L7jQN.js` is the i18n runtime chunk.
- `control-ui-runtime-artifact/20260506-persistence-fix-source/assets/zh-CN-B26mMdbY.js` contains the Simplified Chinese locale object and GTClaw-facing strings.
- `control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js` contains app-level behavior and settings wiring; prior persistence work modified this bundle.

The existing runtime image assembly artifact confirms the deployed copy target:

- destination root: `/usr/local/lib/node_modules/openclaw/dist/control-ui`
- copied files: `index.html`, `assets/index-M4TNVXB3.js`, `assets/i18n-B06L7jQN.js`, `assets/zh-CN-B26mMdbY.js`

## Findings: plugin/localization/skill mechanisms

Official OpenClaw package metadata shows a substantial plugin/extension/skill distribution mechanism:

- many `dist/extensions/*/openclaw.plugin.json` files
- extension-bundled skills such as `dist/extensions/acpx/skills/acp-router/SKILL.md`, `dist/extensions/feishu/skills/*/SKILL.md`, `dist/extensions/memory-wiki/skills/*/SKILL.md`, `dist/extensions/open-prose/skills/prose/SKILL.md`, and others
- plugin runtime files such as `dist/bundled-plugin-metadata-BJMD48hn.js`, `dist/channel-plugin-common-CGcK6wnc.js`, `dist/channel-plugin-ids-BMbnTCrg.js`, `dist/channel-plugin-resolution-BeLSBRHb.js`, and `dist/channel-plugin-runtime-CZ7dKD51.js`
- control-ui chunks for skills UI, including `dist/control-ui/assets/skills-BRWdbtpV.js` and `dist/control-ui/assets/skills-shared-D6eRDyeb.js`

No evidence was found that this plugin/extension/skill mechanism is a localization package mechanism for the control-ui static bundle.

Specifically:

- Locale files are emitted as built control-ui assets, not as plugin resources.
- The discovered `openclaw.plugin.json` files appear to package providers, channels, tools, runtime capabilities, or extension-bundled skills.
- Skills are agent/tooling instructions, not browser UI locale bundles.
- The control-ui skill chunks render/manage skills; they do not prove that a skill can inject or override control-ui i18n strings.
- No discovered package metadata showed a locale plugin manifest, localization extension point, or runtime materialization hook that can safely replace `zh-CN` strings without rebuilding or overlaying control-ui assets.

Discovery result:

- Localization mechanism found: yes, built control-ui i18n assets and locale chunks.
- Dedicated localization plugin mechanism found: no.
- Skill distribution mechanism found: yes, but not suitable for carrying control-ui localization resources.
- General plugin/extension distribution mechanism found: yes, but not proven to support static control-ui locale override.

## Delivery options

Option A: official source i18n patch and control-ui build.

- Modify official OpenClaw control-ui i18n source for `zh-CN` and GTClaw-branded shell strings.
- Build the control-ui from official source and promote the generated static assets.
- Best long-term maintainability if an official source workspace and build/test commands are available.
- Requires source workspace recovery and a separate localization patch approval packet before any edit.

Option B: repo-owned static control-ui artifact overlay.

- Continue using the existing ClawManager artifact pattern under `control-ui-runtime-artifact/`.
- Patch only the required static output files and assemble an image layer that overwrites `/usr/local/lib/node_modules/openclaw/dist/control-ui`.
- Lowest short-term risk because the current feature already has a repo-owned artifact and an image assembly artifact with explicit destination paths and checksums.
- Must keep the overlay allowlist narrow: `index.html`, `assets/zh-CN-*.js`, and only `assets/i18n-*.js` or `assets/index-*.js` if loader/default-language or persistence behavior requires it.

Option C: runtime startup materialization.

- Inject or rewrite locale assets during runtime startup.
- Lower image-build coupling, but higher runtime risk: startup scripts become responsible for mutating static app assets, checksums are harder to reason about, and failures appear at runtime.
- Not recommended unless image-layer delivery is unavailable.

Option D: plugin/skill localization package.

- Not recommended for the current target.
- No discovered official OpenClaw mechanism proves that plugin or skill packages can replace control-ui locale bundles.
- It would likely require new OpenClaw product support, which is larger than the GTClaw localization goal.

## Recommendation

Recommended path:

1. Use official OpenClaw source only to identify clean i18n source ownership and expected build outputs.
2. For the immediate GTClaw delivery, use the existing repo-owned static control-ui artifact overlay and image assembly path.
3. Keep the localization patch focused on `zh-CN` locale strings and shell branding needed for `GTClaw 控制台`.
4. Do not continue the trustedProxy/runtime auth contract patch for this localization objective. The trustedProxy work addresses connection/auth behavior, while this gate is about control-ui Chinese localization and delivery mechanics.

Recommended next gates:

1. `CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH_APPROVAL_PACKET`
   - approve exact localization resource files and whether the source-of-truth will be official source rebuild or static overlay.
2. `CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_IMPLEMENTATION`
   - apply the approved localization patch only.
3. `CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_BUILD_GATE`
   - assemble/build/tag/push only after separate approval.
4. `CONTROLUI_LOCALIZATION_FRESH_INSTANCE_BROWSER_MANUAL_E2E_GATE`
   - verify a fresh instance and browser/manual E2E only after image delivery approval.

## Files/resources likely involved

For an official source rebuild path:

- official control-ui i18n source for the `zh-CN` locale
- official control-ui shell/index template or app title source if the GTClaw browser title and first-viewport strings are source-owned there
- generated `dist/control-ui/index.html`
- generated `dist/control-ui/assets/zh-CN-*.js`
- generated `dist/control-ui/assets/i18n-*.js` only if the i18n loader changes
- generated `dist/control-ui/assets/index-*.js` only if app behavior/default locale/title wiring changes

For the existing artifact overlay path:

- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/<new-localization-artifact>/index.html`
- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/<new-localization-artifact>/assets/zh-CN-*.js`
- optional `assets/i18n-*.js` if loader/default locale changes
- optional `assets/index-*.js` if app-level strings or persisted gateway behavior changes
- matching runtime image assembly files in a separately approved image assembly gate

## Forbidden actions statement

Forbidden actions were not executed: no implementation, no patch, no backend/frontend/runtime artifact/deployments/docs/longterm/AgentTeam/UnifiedFramework modification, no existing evidence modification, no build/tag/push/pull image action, no container run, no kubectl, no k3d, no Helm, no browser E2E, no DevTools, no Playwright, no instance create/delete/modify, no database access or modification, no Mem0 write, no passes:true, no Close, and no git stage/commit/push.

No token, password, key, cookie, bearer material, auth header plaintext, or access URL plaintext was recorded.
