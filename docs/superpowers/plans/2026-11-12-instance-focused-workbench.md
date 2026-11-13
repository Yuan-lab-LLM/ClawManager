# Focused instance workspace refinement

User-approved annotated design: remove duplicated page header, move editable name/state and icon-only management controls into Agent frame header, remove model-reply hint, compact/hide instance list, default columns 1.2:6.8:2, status-tinted rows, retain list scroll on switching, replace standalone /instances table with workspace entry and full management expansion.

- [x] Add browser regressions for frame-level controls, no duplicated header/reply hint, 12:68:20 columns/hide-restore, state-tinted accessible rows, prepared switching without loading replacement, list scroll/page/search retention, unified /instances entry and expanded table/nested dialogs.
- [x] Reuse the complete InstanceListPage in an embedded native dialog, preserving filters, pagination, creation, selection and bulk actions. Embedded state does not rewrite the workspace URL; old filtered bookmarks initialize expansion.
- [x] Add account-scoped, 15-second metadata handoff and list-position preferences. Prepare authorized target data before navigation; keep mandatory native browser-origin document isolation. Never persist access URLs or credentials.
- [x] Move rename/state/actions into both native-frame shell variants; keep keyboard names/tooltips for icon buttons. Retain dialog operation state when frame availability changes.
- [x] Update split defaults and hide/show behavior, retain safe layout bounds and responsive picker; use row tint plus accessible status names, not separate visible status badges.
- [x] Reuse current instance APIs and direct-entry routing. /instances selects last valid/current first instance or offers the full management view for an empty account, rather than a separate table page.
- [x] Verify on 54 only: types/lint, handoff tests, real-route browser tests, dashboard and Hermes/shared shell compatibility, visual review at five widths and locales. Build/deploy exact source and verify actual switching, native OpenCode/DSH, origin Service and health.
