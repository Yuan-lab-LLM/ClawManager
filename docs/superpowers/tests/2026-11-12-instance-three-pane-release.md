# Instance three-pane release validation

Deployed source: `fde59544bb107c010fc97bf677d6517188e9c2b1`.
Environment: `https://10.130.81.54:30443/instances/70` (`lite-openclaw-037`).
Image: `clawmanager.local/clawmanager@sha256:52fb719478353fa7e88e6c52f5d210fb10074482eab75cde7c7612cc793ee39c`.
Deployment: `clawmanager-system/clawmanager-app`, generation 23, ready 1/1.

## Delivered

- Removed Back and the type/mode/runtime/workspace-size/timestamp row. Kept lifecycle state and existing management controls.
- Clicking the instance name opens inline editing. Unicode-aware 3–50 character validation, trim, Save/Enter, Cancel/Escape, pending guard, retained server errors and focus restoration. Only the name field is submitted; no runtime restart is requested.
- Added an authenticated instance list with real type icons, names, actual statuses, server search, pagination, current selection and immediate current-name updates. Search is not limited to the visible page. Live inventory shrink corrects an invalid page.
- Desktop Instances/Agent/tools default to 2:6:2. Both separators support pointer and keyboard resizing and reset. Restoring tools after resizing a hidden pane preserves minimum sizes. Resizing keeps the same iframe and unsent input.
- Narrow screens use an instance-picker modal; Escape closes it even from search, and focus returns. Agent/tools retain usable heights in Lite and Pro.
- Detail controllers are keyed by user and route ID. Old metadata, external-access data, dialogs and actions cannot populate another controller. Metadata polling is single-flight, and successful rename invalidates older metadata results.
- Narrow file controls wrap instead of clipping; file table scrolling is local to its pane. The styling is scoped to the instance split.

## Verification

All application execution, bundling, binaries and image builds ran on `10.130.81.54`; local work was source preparation/validation/transfer only.

- TypeScript and targeted lint passed for new instance components. Repository-wide lint was not claimed clean.
- Real-route browser regression passed: validation/error/cancel/rename, stale response, six-second metadata request without overlapping polls, authorized search beyond page one, pagination shrink, empty/retry, account isolation, mobile modal/focus, both resize handles, hidden-tools bounds, unsent draft and iframe identity, Lite/Pro, five viewport widths (2048/1440/1280/768/390), five locales and long names.
- File control reachability assertions caught an initial clipped Upload button; the scoped toolbar fix passed all five widths. OpenCode default-entry regression passed.
- Full Workspace dashboard browser regression passed, including account isolation, periods, task/pin behavior, currencies, gaps, keyboard, responsive layouts, locales and reduced motion.
- Read-only review found four concrete boundary issues; pagination, restore bounds, route state isolation and slow metadata starvation were fixed. Focused rereview found no remaining important/critical issues.
- Exact archived source inventory (2054 files), rebuilt binary hashes, serving image and all 161 frontend files matched. Stable runtime helpers/nginx files and other deployment images remained unchanged.
- Actual deployed `lite-openclaw-037` verified at 1440 and 2048 with the exact default ratios, both separators, name editor entry/cancel, skills modal, instance search and switching to another running instance. No actual name or lifecycle operation was written during production checks.
- Dashboard production checks and its isolated usage endpoint passed. Actual OpenCode 50 and DSH 60 loaded in fresh Chrome with default TLS verification, one main-host certificate confirmation, child probe 204 and zero child confirmations.
- Browser-origin Service retains 128 NodePorts to target 8444 and a ready EndpointSlice. Final health HTTP 200; 56 instances: 54 running and two manually stopped. Instance 65 remains running; user-stopped instances were not restarted.

Remote evidence: `/root/cm-three-pane-20261112/release-fde59544bb10/`; native-browser evidence: `/root/cm-three-pane-20261112/native/`. Credentials/private snapshots are not committed. Owned browser test servers close in their cleanup paths.
