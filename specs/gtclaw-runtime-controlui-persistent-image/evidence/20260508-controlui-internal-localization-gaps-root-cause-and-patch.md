# Control UI internal localization gaps root cause and patch evidence

Date/timezone: 2026-05-08, Asia/Shanghai

Gate: CONTROLUI_INTERNAL_LOCALIZATION_GAPS_ROOT_CAUSE_AND_PATCH_GATE

Approval phrase:

- APPROVE_CONTROLUI_INTERNAL_LOCALIZATION_GAPS_ROOT_CAUSE_AND_PATCH_GATE

Verdict:

- CONTROLUI_INTERNAL_LOCALIZATION_GAPS_ROOT_CAUSE_AND_PATCH_DONE

## Scope

This gate addresses GTClaw/OpenClaw internal UI localization after the user reached the internal UI in instance 22 / oc2gi-scope-r-204419.

This is not entrance connection form work. The prior blocker path is preserved: internal UI reached, device signature invalid absent, device identity required absent, and missing_scope absent.

Guardrails honored:

- no backend auth/scope
- no runtime auth predicate
- no runtime scope propagation
- no build/tag/push
- no deploy
- no browser E2E
- no passes:true
- no Close
- no git stage/commit/push

## Root Cause Summary

The observed English text was not caused by the Simplified Chinese locale chunk alone. The root cause is that several internal UI strings are compiled as direct English literals in the official OpenClaw control-ui bundle, outside the zh-CN bundle.

Two bundle surfaces were involved:

- `assets/index-M4TNVXB3.js`: chat placeholder, model/session selectors, config editor controls, section labels, and command/navigation text.
- `assets/config-form-x_UhxUYO.js`: schema form fallback and unsupported schema messages. This chunk was present in the parent runtime image but was not carried by the reviewed static artifact, so it was added to both artifact paths and copied by the assembly Dockerfile.

## Per-Residual Diagnosis And Patch

| residual text | source file path | source region shape | classification | chosen patch target | patch summary | remaining hits |
| --- | --- | --- | --- | --- | --- | --- |
| Message Assistant (Enter to send) | `control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/index-M4TNVXB3.js`; mirrored to runtime assembly | `GE` chat composer placeholder builds `Message ${...} (Enter to send)` and default name fallback | chat input placeholder copy | compiled control-ui `index-M4TNVXB3.js` in both artifact paths | Replaced placeholder with Chinese copy and mapped default Assistant display to Chinese in the placeholder/fallback path. | Exact residual is zero. `Assistant` may still appear as user data or source literals outside this residual. |
| Default (Auto) | same `index-M4TNVXB3.js` paths | `Pk` model selector builds `Default (${display})` | model-session selector copy | compiled control-ui `index-M4TNVXB3.js` in both artifact paths | Replaced default label with Chinese copy and maps `Auto` to Chinese when it is the default display. | Exact residual is zero. |
| Default (off) | same `index-M4TNVXB3.js` paths | `Jk` thinking selector builds `Default (${... : off})` | model-session selector copy | compiled control-ui `index-M4TNVXB3.js` in both artifact paths | Replaced selector default label with Chinese copy and replaced fallback off display with Chinese. | Exact residual is zero. |
| Form | same `index-M4TNVXB3.js` paths | `qA` config mode toggle visible label | schema-config editor independent copy | compiled control-ui `index-M4TNVXB3.js` in both artifact paths | Replaced the rendered config mode toggle label and related form-mode warning copy with Chinese. | Remaining `Form` hits are a source comment, fallback/default i18n source, or code/tool literals, not the rendered config mode toggle. |
| Raw | same `index-M4TNVXB3.js` paths plus `config-form-x_UhxUYO.js` | `qA` raw mode toggle, raw editor label, raw fallback errors | schema-config editor independent copy | compiled control-ui `index-M4TNVXB3.js` and `config-form-x_UhxUYO.js` in both artifact paths | Replaced rendered raw mode labels, raw-mode warnings, raw text buttons, and schema fallback raw guidance with Chinese. | Remaining `Raw` hits are fallback/default i18n source or code/tool literals. |
| No changes | same `index-M4TNVXB3.js` paths | `qA` config editor status span | schema-config editor independent copy | compiled control-ui `index-M4TNVXB3.js` in both artifact paths | Replaced the no-change status with Chinese. | Exact residual is zero. |
| Raw mode disabled (snapshot cannot safely round-trip raw text). | same `index-M4TNVXB3.js` paths | `qA` config editor unavailable raw mode status | schema-config editor independent copy | compiled control-ui `index-M4TNVXB3.js` in both artifact paths | Replaced the full disabled raw mode sentence with Chinese. | Exact residual is zero. |
| Open | same `index-M4TNVXB3.js` paths | `qA` open config file button and nearby internal buttons/tooltips | schema-config editor independent copy | compiled control-ui `index-M4TNVXB3.js` in both artifact paths | Replaced the rendered config open button and nearby internal open labels/tooltips with Chinese. | Remaining `Open` hits are fallback/default i18n source or code/tool literals. |
| Save | same `index-M4TNVXB3.js` paths | `qA` config save button | schema-config editor independent copy | compiled control-ui `index-M4TNVXB3.js` in both artifact paths | Replaced save and saving button states with Chinese. | Remaining `Save` hits are fallback/default i18n source. |
| Apply | same `index-M4TNVXB3.js` paths | `qA` config apply button | schema-config editor independent copy | compiled control-ui `index-M4TNVXB3.js` in both artifact paths | Replaced apply and applying button states with Chinese. | Remaining `Apply` hits are code/tool literals. |
| Update | same `index-M4TNVXB3.js` paths | `qA` config update button and update banner | schema-config editor independent copy | compiled control-ui `index-M4TNVXB3.js` in both artifact paths | Replaced update and updating button states plus nearby update banner copy with Chinese. | Remaining `Update` hits are fallback/default i18n source or code/tool literals. |
| Search settings... | same `index-M4TNVXB3.js` paths | `qA` config search input placeholder and label | schema-config editor independent copy | compiled control-ui `index-M4TNVXB3.js` in both artifact paths | Replaced the search placeholder, label, and clear control label with Chinese. | Exact residual is zero. |
| Settings | same `index-M4TNVXB3.js` paths | config root nav label and command palette navigation label | hardcoded English string | compiled control-ui `index-M4TNVXB3.js` in both artifact paths | Replaced config root label and command palette navigation label with Chinese. | Remaining `Settings` hits are fallback/default i18n source. |
| Communication | same `index-M4TNVXB3.js` paths; `config-form-x_UhxUYO.js` has a protocol name literal | config section group label and communications tab root label | schema-config editor independent copy | compiled control-ui `index-M4TNVXB3.js` in both artifact paths | Replaced the rendered section group label and communications root label with Chinese. | Remaining `Communication` hits are product/protocol literals in source. |
| Schema unavailable. | `control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/config-form-x_UhxUYO.js`; mirrored to runtime assembly | `re` schema form renderer fallback when schema is absent | schema-config editor independent copy | compiled control-ui `config-form-x_UhxUYO.js` in both artifact paths; assembly Dockerfile now copies it | Added the missing chunk to the reviewed artifact path and replaced the schema unavailable fallback with Chinese. | Exact residual is zero. |

## Changed Artifact Hashes

new zh-CN hash:

- `assets/zh-CN-B26mMdbY.js`: `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` unchanged.

New control-ui artifact hashes:

- `control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/index-M4TNVXB3.js`: `ca31f0ff8127140abba3c61d1fd44d0fac923177bb54180480aff9c8a51a5d6b`
- `control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/config-form-x_UhxUYO.js`: `8e6ab9a3a394485eff7670cb79204d52a3c973c3febdb83eeb9c9d528518c245`
- `runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/index-M4TNVXB3.js`: `ca31f0ff8127140abba3c61d1fd44d0fac923177bb54180480aff9c8a51a5d6b`
- `runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/control-ui/assets/config-form-x_UhxUYO.js`: `8e6ab9a3a394485eff7670cb79204d52a3c973c3febdb83eeb9c9d528518c245`

Assembly change:

- `runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/Dockerfile` now copies `control-ui/assets/config-form-x_UhxUYO.js` into the runtime image.

## Verification Summary

Syntax checks:

- `node --check` passed for both changed `index-M4TNVXB3.js` copies.
- `node --check` passed for both changed `config-form-x_UhxUYO.js` copies.

Residual scan:

- Exact observed residual strings are zero for `Message Assistant (Enter to send)`, `Default (Auto)`, `Default (off)`, `No changes`, `Raw mode disabled (snapshot cannot safely round-trip raw text).`, `Search settings...`, and `Schema unavailable.`
- remaining hits exist only for broad short terms: `Form`, `Raw`, `Open`, `Save`, `Apply`, `Update`, `Settings`, and `Communication`.
- remaining hits are classified as fallback/default i18n source, product/protocol literal, code/tool literal, or non-rendered source only. The observed rendered config/chat/settings locations were patched.

No build, deploy, instance change, browser E2E, or final manual localization acceptance was performed in this gate.
