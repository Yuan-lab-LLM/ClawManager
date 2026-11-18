# Instance three-pane workspace implementation plan

User-approved design: annotated screenshots from 2026-11-12. Remove Back and runtime/type/mode/storage/timestamp metadata. Instance name supports renaming. Add a searchable instance list inspired by the supplied conversation-list reference, retaining CM Fluent styling. Desktop pane widths default to Instances : Agent : workspace = 2:6:2; both separators support pointer and keyboard resizing.

**Architecture:** Reuse authenticated instance list/update APIs and existing native-origin navigation guards. Extend InstanceWorkspaceSplit to a bounded three-pane grid. A separate InstanceSwitcher uses account-scoped pagination/search; the current Instance prop wins over stale list rows. InstanceNameEditor submits only `{name}` and preserves drafts/errors until success/cancel. Do not restart runtimes or replace iframes for rename/resizing. Narrow screens use a keyboard-accessible instance-picker dialog and retain the existing stacked Agent/tools layout.

- [x] Extend real-route browser fixtures for paginated authorized list/search, rename validation/error/cancel/success, stale metadata response, route switching and two resize handles. Observe missing-feature failure before implementation.
- [x] Add InstanceSwitcher with real logos/current status, pagination, scoped query cache and empty/error/retry states. No invented messages, unread counts or background instance changes.
- [x] Add inline name editing with Unicode-aware 3–50 character validation, Enter/Save, Escape/Cancel, pending guard, API error retention and focus return. Invalidate account-specific switcher/list queries and discard older metadata responses.
- [x] Remove redundant Back and metadata; integrate the switcher and three-pane split for Lite/Pro. Both separators preserve the native frame, unsent input and workbench state. Hide tools without removing its controller; offer a narrow-screen picker.
- [x] Run checks on 54 only: TypeScript, targeted lint, browser regression at 1440/1280/768/390 plus large screen and five locales; inspect screenshots. Sync DESIGN.md and sidecar.
- [x] Commit exact sources, build and deploy on 10.130.81.54, verify actual instance/default ratios/rename entry/switcher plus dashboard and native OpenCode/DSH origins, NodePort Service and final health. Avoid writes to real instance names while testing the entry.
