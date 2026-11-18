# CM Fluent 2 UI validation — 2026-11-12

Release source: `685030618de1c9b71c51b0fb375ae3a2d4241beb` (`uiux`), following the primary design migration `ee7349f`.
Target: `https://10.130.81.54:30443`. All application execution, tests, frontend/binary/image builds and browser captures ran on54. Local work was limited to source preparation, static checks, transfer and image inspection.

## Changes

Shared Fluent Web semantic tokens, blue interaction states,14/20 body,12/16 captions,16/22 section titles,4/8/12 radii,48px header,40px navigation,44px table rows, functional motion and reduced-motion/focus behavior. Security, ordinary administrative forms, team/chat/workbench, auth and owner surfaces consume the shared system. Real runtime brand assets remain32px in recent/pinned Agent entries.

Dashboard retains independent Token/cost, three simultaneous assigned-capacity gauges, Agent-state ring with counts, aligned recents/pins and green personal-activity heatmap. Removed legacy warm themes, ornamental gradients and redundant management heroes. Administrator instance table is single-line; complete identity/binding/update information and auto-stop policy are available in an accessible native popover. Stable lists refresh on30s cadence, transient states on5s plus focus/reconnect; errors have retry. An in-flight post-action read queues a fresh follow-up instead of retaining a stale snapshot.

## Verification

- Exact final UI inventory:84 frontend files; source hashes verified against remote test tree. Final source archive covers2046 tracked build-input files.
- Remote TypeScript, source-bound production build, UserLayout single-tree/iframe rendering, Runtime management, team workspace contract/i18n/chat/continuous presentation, owner navigation, native Runtime dialog and instance-refresh/browser regressions passed.
- Dashboard model11/11, full browser suite and10 focused regression groups passed. Includes period controls/focus, quota zero/null/overrun, unavailable activity, native details focus return, chart keyboard navigation, pinning, recents, empty/error/stale/loading, reduced motion and five locales.
- New `frontend/scripts/fluent-admin.browser.mjs` covers collapsed search/filtering, row44, keyboard/native popover focus return and resize containment, full operational details, unchanged auto-stop PUT, stable/empty focus+online refresh and error retry. A held stale GET plus Stop mutation reproduces the post-action read race before the queue fix and passes after it.
- Real read-only API captures: initial104 route/locale/viewport combinations, no JavaScript errors. One mobile security overflow was found and fixed; affected Console/admin/security views were recaptured. Dashboard real-data matrix covers EN/ZH1440/1280/768/390; broader dashboard fixtures also cover JA/KO/DE.
- Independent code review identified and verified repairs to contrast, automatic sync/retry, accessible details, popup resize and post-action stale-read handling. Independent visual review verified four material corrections: real212,808 total on one line; full1280 chart/Agent usage context; Type/Mode separation; readable desktop security identities and coverage. Verdict: ship within reviewed screenshot scope.
- Changed-source lint reports164 errors already present in the same files at baseline, with no new error counts; this is not a clean lint pass. Production build retains the existing large-chunk advisory.

## Evidence and limits

Remote evidence root: `/root/cm-fluent2-20261112/`. Functional logs `*-final.log`, dashboard logs/screenshots under `dashboard/`, real captures under `screenshots/`, lint comparison `lint-comparison.json`, source manifest `source-685030618de1c9b71c51b0fb375ae3a2d4241beb.files.sha256`. Code-review and visual-review reports are retained in controller scratch and copied into release evidence.

Existing Lucide system icons and platform-native selects remain where their migration is explicitly allowed by DESIGN.md; runtime brand icons use actual assets. No new theme component dependency was required. No paid inference or team task execution was used as UI validation. Still-image acceptance does not certify every workflow, OS-native select popup, forced-colors assistive stack or every zoom combination. Existing small display issues retained by visual review include compact pin-name truncation, a Skill Hub eyebrow, some English/Chinese mixing and security-service error wording.

## Deployed result

Final image: `clawmanager.local/clawmanager@sha256:eb8102549c2a9a75d8aab7f7771ac1d94d24a07b521bc40fa4588904e17272e6`. Deployment `clawmanager-system/clawmanager-app`, generation19, ready1/1. Pod actual imageID and all rebuilt binary hashes match the candidate. Runtime configuration, stable helper files and other deployment images are unchanged.

All161 frontend regular assets match final build bytes. Version API reports the exact release commit; auth rejection and invalid-range handling remain intact. Real deployed Chromium checks passed1440×900,1280×800 and390px, including shared theme, three top panels/three gauges, actual32px logos, one-screen desktop framing, aligned work panels and administrator native Details. No JavaScript errors.

Final health HTTP200; all56 instances running, including previously recovered OpenClaw65. Measured Workspace sizes remain available for all56 instances. The owned test Vite process was stopped. Detailed immutable release evidence: `/root/cm-fluent2-20261112/release-685030618de1`; public summary: `2026-11-12-cm-fluent2-release.json`.

A final static-check follow-up replaced recursive refresh coalescing with an explicit loop; the held-GET/Stop race regression, complete-scope lint baseline comparison164→164 with zero new error counts, production build and independent bounded code review passed again. No visual styles changed in that follow-up. No merge or push was performed; unrelated user inputs remain unchanged.
