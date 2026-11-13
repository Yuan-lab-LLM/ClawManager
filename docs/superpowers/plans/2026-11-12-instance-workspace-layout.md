# Instance workspace layout implementation plan

> Execute inline using executing-plans; user-approved design is the annotated instance screenshot from 2026-11-12.

**Goal:** Give the Agent conversation more space while moving infrequent management controls into accessible menus and dialogs.

**Architecture:** Keep existing lifecycle handlers, permissions, workspace state and skill services. Extend the shared ActionMenu with an optional text trigger, introduce a local resizable workspace split, and present the existing skills UI inside a native modal dialog. No new dependencies or backend changes.

**Constraints:** Follow DESIGN.md Fluent tokens. Build and run application checks only on 10.130.81.54. Preserve Share Link, restart environment configuration, delete confirmation, workspace hide/show, iframe continuity, Lite/Pro compatibility and browser-origin routing.

- [x] Add a real-route browser regression in `frontend/tests/instance-workspace-layout.browser.mjs`. Verify the new management trigger, skills modal, default 2:1 split, pointer/keyboard resizing, iframe continuity and hidden workspace. Run on 54 and observe failure before implementing.
- [x] Update `frontend/src/components/ActionMenu.tsx` with optional text trigger and disabled state; use it in `frontend/src/pages/instances/InstanceDetailPage.tsx`. Omit availability helper text in that header only. Keep restart environment and delete confirmation.
- [x] Add `InstanceSkillsDialog.tsx`; add embedded presentation to `InstanceSkillHubPanel.tsx`. Put its trigger next to Share Link and remove the bottom skills/session-usage sections from both detail variants. Remove obsolete Lite bottom-panel height reservations.
- [x] Add `InstanceWorkspaceSplit.tsx` and scoped CSS: desktop starts at 2:1, bounded pointer resizing with iframe-safe drag capture, keyboard arrows/Home/End and double-click reset. Narrow screens retain stacked layout; hiding tools uses the full Agent width.
- [x] Run remote TypeScript, targeted lint and browser regression. Cover 1440/1280/768/390, five locales, empty/error skills, modal Escape/focus return, lifecycle disabled states and no unintended mutations. Review screenshots.
- [x] Commit exact sources, build/deploy on 54 from the currently deployed image, verify actual instance page plus OpenCode/DSH native pages, NodePort Service and ready EndpointSlice. Record release evidence without secrets.
