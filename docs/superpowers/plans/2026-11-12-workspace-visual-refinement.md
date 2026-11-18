# Workspace visual refinement

User-approved changes to the released03e7e72 dashboard. Preserve CM shell, personal data/ownership, native task/pin interactions, true quota/unknown values and desktop one-screen layout. No API, recovery, schema or dependency changes. All runtime/build/browser checks on54 only.

- [x] Move Token/capacity/Agent panels to the first row beneath the welcome; DOM and desktop visual order agree. Mobile retains grouped navigation with Operations initially selected.
- [x] Replace capacity selection with three simultaneous CPU/memory/storage speedometers, labeled assigned/quota. Color their scale green/amber/red, show real ratios and amounts, preserve unavailable/zero/overquota semantics, reduced-motion and hidden pause.
- [x] Give Continue working and pinned Agents matching panel/header geometry so titles and first content align; retain pin customization.
- [x] Apply gray/light-green/green/dark-green activity levels and matching legend; preserve missing-data dashed treatment and calendar keyboard semantics.
- [x] Adapt existing browser regressions to three gauges and unchanged behavior. Run model tests, noEmit, scopedlint, remote build and actual browser4width/5locale/empty/error/loading/long content. Inspect rendered screenshots, then exact tracked-source image build and guarded rollout on54; verify live assets/API/browser and health. Record evidence and preserve unrelated user files.

Released281b962 to54 as immutable image4d92c1dc; live4viewport/API/assets/health verified, Vite9433 cleaned. Detailed evidence: docs/uiux/2026-11-12-workspace-visual-refinement-validation.md.

User clarification: align the complete work/pin panel frames, not just headings. Fixed in76f578 and released as image6019ae8a; full outerframes verified at2048/1440/1280, narrow768/390 retained. Follow-up evidence in the same validation document.

User color follow-up:13beb8b adds brighter Token curve/segmented pale area and Agent ring/legend palette, retaining data gaps and layout. Released on54 as image25e6da84; liveassets/API/fiveviewports/health verified. Evidence in the same validation document.

User icon follow-up: both Continue working and pinned Agents reuse actual runtime type logos, enlarged to32px, with pin text layout/recent row spacing adapted. Final168b35a released on54 as image36e235ad; actual image decoding/size, five viewport/frame regressions and health verified. Evidence in the same validation document.
