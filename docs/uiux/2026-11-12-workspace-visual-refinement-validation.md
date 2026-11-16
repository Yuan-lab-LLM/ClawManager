# Workspace visual refinement validation

Status: released and verified on54; executable source281b96218216a074641bfbb34dbb7ee98e17e982.

The five user requests are implemented in four frontend paths. Token, resource and Agent state panels now lead the DOM and desktop grid beneath the welcome. CPU, memory and storage speedometers appear together; there is no resource selector or associated state. Their green/amber/red scales retain real assigned/quota ratios, visible amounts, no needle for unknown/zero limits and uncapped numeric oversubscription. Percentages sit below the needle to avoid overlap. Pin customization remains, and work/pin headings share the same inset and vertical geometry. The activity scale and legend use gray/light-green/green/dark-green, retaining dashed unknown cells and recorded values. The CM shell, APIs, backend/recovery and dependencies are unchanged.

Remote checks on54 in /root/cm-workspace-20261112-v3/:

- Existing model tests11/11 PASS; TypeScript noEmit and scoped ESLint PASS.
- Full actual Router/Auth browser regression PASS: three simultaneous gauges, metrics-first layout and aligned headings; pins, tasks/read acknowledgement, currency/gaps, period changes, stale/error/retry, current-account isolation, reduced motion and hidden polling; zero writes/page errors.
- Four viewports1440/1280/768/390, five languageszh/en/ja/ko/de, plus operations captures for each language at390. Desktop1440x900 and1280x800 fit the content area; mobile groups scroll internally with metrics selected initially. Empty/unavailable/error/loading and long content covered by retained tests.
- Ten review cases PASS, including full aggregate totals, CPU zero/unknown/overquota and retained history/dialog focus. Removed selector assertions were replaced by three-visible-gauge assertions; business/privacy assertions retained.
- Impeccable layout detector returned[] after source/render inspection. Parent viewed final1280 and German mobile operations images, including the corrected needle/readout placement. This is a scoped Operate refinement, not a new design system.
- Four changed source files SHA-checked against the committed local source on54. Existing remote frontend tree was used only for fixture browser/type checks; the release uses a fresh exact tracked archive.

No application/compiler/browser/container ran locally. Local work was source editing/Git/hashing/transfer and viewing54-generated screenshots. Tests used mocked API responses in the real app; no database fixture or production lifecycle/model mutation was needed. Backend Go tests were not rerun for these frontend-only changes.

Release input:2045trackedfiles, archiveSHA2569f1ed3c68fc735ebe4b333f0a030ec5156afab8e75fc77d88c25df88d0b78625; manifestSHA256ee8e921635d5a1fef0c26010d55cec9bb4b04f4bf39aa63f707b3c38ec2889a1. Buildbase3463717f, preserving runtime image configuration and base layers. All4 app binaries and frontend are built on54 from the same release source. Exact immutable image/live hashes and post-deploy checks are recorded below, after they run.


## Immutable rollout

- Built on54 at2026-11-12T03:52:23Z, versionv2026.11.12-workspace-v3. Exact source inventory/hash check, all4Go builds and frontend build passed; Vite3.27s with the existing chunk-size warning.
- Imageclawmanager.local/clawmanager@sha256:4d92c1dcb81dd69a5f2d199ee1fb00675c7440d8f0afdbafa8c6c7bc9de7fbab, based on3463717f. Base layers and OCI runtime configuration preserved.
- clawmanager-system/clawmanager-app generation13,1/1ready, Podclawmanager-app-6bc9fccd67-69gxs. Actual serving imageID and all4binary hashes match candidate; startup/nginx/runtime-helper files and other Deployment image references unchanged. Old image retained.
- URL:https://10.130.81.54:30443/dashboard
- Build/rollout/HTTP/browser evidence:/root/cm-workspace-20261112-v3/release-281b96218216/. Fixture/browser/type/lint/scan logs and screenshots are in its parent directory.


## Live verification and cleanup

HTTP checks passed:161regular dist/public/lazy/preview files byte-identical to the candidate, exact versioncommit,401without auth,400for invalid period, today/7d/30d accounting/coverage/history and real recent ordering. All56live instances retained measured-positive Workspace values; sampled list/detail/status agree. No stopped/sleeping sample existed during this check; those semantics are covered by existing isolated regressions.

Production browser checks passed1440/1280/768/390: metrics-first geometry, aligned work/pin headings, three resource gauges without a selector, period change retaining all three, real recent order, native dialog histories/focus, calendar keyboard, Portal/Console account behavior and measured Workspace column. Zero page errors, HTTP5xx or unexpected API writes. Parent directly inspected the final actual1280Chinese production rendering.

Final health at2026-11-12T03:54:51Z returned200 with source281b962. Dashboard reports56running and no failed instance; previously recovered65 remainsrunning. Exact owned Vite9433 PID2225989 was stopped after validating command/CWD, and health rechecked. No fixture namespace was created this round; no model call, lifecycle mutation or Runtime image change was performed. Evidence/source/cache retained, unrelated local.gitignore/design/docs inputs preserved, uiux kept without merge/push. Later documentation commits do not change executable release source.

## Follow-up: complete work/pin panel frames

The user clarified that the mismatch was the visible outer panel, not the title baseline. Source76f57830022ee9008dfc7a93f962c748b54d02d8 gives the entire pinned area the shared white bordered panel, removes its transparent border and removes the redundant empty-child frame. Titles and six links sit inside that complete frame. The regression now compares actual top/bottom edges, visible border color and surface color; the previous heading-only assertion did not catch the absent frame.

On54, noEmit/scopedlint/full Router/Auth browser regression passed, three owned source hashes matched and layout scan returned[]. Parent viewed the final1280fixture and2048production renders. Exact2045file archiveSHA986c44bbf1446351f0ae6352d106dd3a193816ec3ffcded12701dc9484498fbe, manifestSHA23d2d2f0f5ce13c63668a7977bc382d31a0cc754528032b30d35054d328975d9. All4binaries/frontend built on54 at2026-11-12T04:25:13Z, versionv2026.11.12-workspace-v4.

Immutable imageclawmanager.local/clawmanager@sha256:6019ae8afd53e5d12c6c72f224bfa2447818b34f6eae3d25c24b2d0208ed4a1a was deployed as generation14/1ready Podclawmanager-app-585ff9fccc-lh5s4. Serving imageID/all4binary hashes match; base layers/runtime configuration/stable files/other Deployment images preserved. Live161asset bytes, auth/ranges/real data/56positive Workspace observations and browser checks passed. Complete frames explicitly match at2048/1440/1280, alongside narrow768/390 behavior. Zero page errors/HTTP5xx/unexpected API writes. Evidence:/root/cm-workspace-20261112-v4/release-76f57830022e/.

Final04:27:34Z health200/exact76f578;56running/no failed, instance65stillrunning. Exact owned Vite9434 PID2361748 stopped and health rechecked; no fixture namespace, backend/schema/Runtime behavior change, local application execution, merge or push. User inputs/evidence retained. Subsequent commit records documentation only.

## Follow-up: vivid Token and Agent state data colors

Source13beb8b6aa43a66b8311022388988f57567767fd changes three workspace files only. Token uses a brighter blue line/markers/rank and blue main metric, with a solid pale-blue area drawn separately for each existing contiguous known segment. Null gaps are never joined; isolated points do not invent an area. Other observation charts retain their default presentation. Agent ring and its legend share richer teal/green/blue/cyan/amber/red colors, with neutral offline/unknown colors. Essential marks have at least3:1 contrast against white; Token main metric5.09:1. Layout/complete work frames/resource gauges/activity/data semantics are preserved.

All runtime checks/builds on54: noEmit/scopedlint/full Router/Auth browser regression PASS, three owned hashes match, layout scan[]. Parent viewed1280fixture and actual production renders. Exact2045file archiveSHA081e50b8c6c89d8d17b143462446eda93cac54ecb4a294cc9f4dff864818df4b, manifestSHA4fb4b312b0ac988bbdf221144a5b039c3c84101e03399b76ff39982b021fb52d. All4binaries/frontend built04:33:42Z, versionv2026.11.12-workspace-v5, immutable imageclawmanager.local/clawmanager@sha256:25e6da84754b7210a21c7e90e5a5a729befd9f0ae31ac7f6da7ad9f49844678d.

Deployment generation15/1ready Podclawmanager-app-58dc57cff7-tsrrv, serving imageID/all4binary hashes verified, base/runtime configuration/stable files/other images preserved. Live161assets/auth/three periods/real recent data/56positive Workspace observations and browser2048/1440/1280/768/390 checks passed, including complete frame alignment, three resource gauges, history/focus/calendar/menu behavior. Zero page errors/HTTP5xx/unexpected writes. Final04:36:17Z health200/exact13beb8b,56running0failed/65running; exact owned Vite9435 PID2395811 stopped and health rechecked, no fixture namespace. Evidence:/root/cm-workspace-20261112-v5/release-13beb8b6aa43/. No local application/build, backend/schema/dependency/Runtime behavior change, merge or push; unrelated user files preserved.

## Follow-up: real type icons in both work regions, enlarged

Final source168b35a7fcd47c90cccb65da2f2af6c73f627f4f (following937f443's type asset mapping) uses one local AgentTypeIcon for both Continue working and pinned Agents. The allowlist reuses existing OpenClaw/Hermes/OpenCode/DeepSeek Harness/WorkBuddy/Codex/Claude Code assets, with normalized type and generic fallback for unsupported values. Images are decorative beside the existing type text; no external assets/dependencies were added. Both regions use32px contained icons. Pin icons occupy the left side of all text rows, with compact internal spacing; recent rows are40px and bounded to whole rows. Links, captions, status semantics, data, colors and complete outer-frame alignment remain.

On54, final noEmit/scopedlint/full Router/Auth browser regression passed, both owned source hashes matched, layout scan returned[]. Parent viewed1280 and390fixture renders and actual1280production rendering with real red OpenClaw, Hermes and OpenCode logos. Live checks explicitly require recent and pin images to decode with nonzero natural dimensions and displayed icons at least28px; final source renders32px. Existing five viewport2048/1440/1280/768/390, frame/metrics/gauges/history/dialog/calendar/menu/Workspace checks passed with zero page errors/HTTP5xx/unexpected writes. All161candidate assets served byte-identically; auth/period/current-account data and56positive Workspace observations verified.

Exact2045file archiveSHAcfd7ab95222d05a36a85cd94fe742766baf583c96fec27a30884523860440301, manifestSHA219aee0d751635b9d1f7bee40a497c18d6bc9976ff704ebd6617d3060a2ffa24. All4binaries/frontend built on54 at2026-11-12T04:52:49Z, versionv2026.11.12-workspace-v7, immutable imageclawmanager.local/clawmanager@sha256:36e235ad05f400b3e08a87da60ca539f23d8387ae7d7b1140251d6dbbeb6da1f. Deploymentgeneration17/1ready Podclawmanager-app-59d6f9fcd-xlrtm; actual imageID/all4binary hashes/stable files/base layers/runtime configuration/other Deployment images verified.

Final04:56:59Z health200/exact168b35a,56running/no failed/65running. Owned Vite9437PID2479424 stopped and health rechecked. Prior icon iteration Vite9436 was also cleaned. No fixture namespace, backend/schema/dependency/Runtime behavior change, local application/build, merge or push. Evidence:/root/cm-workspace-20261112-v7/release-168b35a7fcd4/; prior pin-only evidence remains in-v6. User inputs and evidence preserved; later commits document the release only.
