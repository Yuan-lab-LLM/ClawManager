# Dashboard usage card refinement

User-approved annotated design: emphasize username independently from greeting; remove welcome summary; show smooth Token and cost curves together on independent axes with different colors; changing period updates only the usage card with a local loading transition.

- [x] Add lightweight authenticated `/dashboard/workspace/usage` endpoint reusing usage/attribution aggregation, without collecting instances, capacity, tasks or activity snapshots. Preserve currency separation and missing data semantics.
- [x] Add browser regression proving unrelated dashboard nodes/state persist and only the lightweight endpoint runs when selecting a period; verify simultaneous curves, independent axes, errors, rapid switching and reduced motion.
- [x] Give the usage card its own period/query state, preserve previous visible chart during loading, add card-local loading/retry and support selecting cost currency when several exist. Keep details synchronized to that card's selected period.
- [x] Extend the shared SVG chart to smooth bounded cubic curves, preserving missing-data segments, exact sampled values, keyboard navigation and a second independent axis for costs. Never combine currencies or treat missing prices as zero.
- [x] Style username separately from greeting; remove welcome task-count text and its header computation while retaining actual task handling in Continue working.
- [x] Run remote backend tests, frontend type/lint and browser regressions. Review actual screenshots and deploy together with the instance-page refinements on 10.130.81.54. No local builds.
