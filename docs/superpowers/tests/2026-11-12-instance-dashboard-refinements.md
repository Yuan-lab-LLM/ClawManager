# Instance and Dashboard refinement release

Deployed source: `046a68873e017cd1bc07449e231f8976d46eff2d` (includes instance change `207d3b2`).
Environment: `https://10.130.81.54:30443`.
Image: `clawmanager.local/clawmanager@sha256:a672fd64c909c85d00bbd5cf7b1b29c02813e54a9baa7a3ec06f292e52426d95`.
Deployment: `clawmanager-system/clawmanager-app`, generation 22, ready 1/1.

## Delivered behavior

- Instance lifecycle badge omits the access helper line. Share Link is followed by Skill Management and a text-trigger Runtime management menu. Start/Stop, Restart, environment restart and guarded Delete retain their handlers and permissions.
- Skill Management uses a native modal. Closing/reopening preserves search, in-flight sync/install state, completion/errors and operation guards. Inventory loading errors are separate from operation failures. Ordinary polling pauses while closed.
- Bottom skill and session-usage panels are removed from both instance variants. Desktop columns default to Agent 2/3; pointer and keyboard resizing preserve the native iframe and unsent input. Tools hide/show and narrow stacked layout remain available.
- Homepage username has separate emphasis. Welcome counters and main-card collection/explanation copy are removed. Actual units, dates, statuses, error feedback and cost subtotal/estimate labels remain.
- Token and cost appear together as bounded smooth blue/orange curves with independent axes, actual sampled points, keyboard readout, gaps and currency separation. Cost currencies can be selected when several exist.
- Usage period state/query is local to its card, with retained content during loading, a reduced-motion-compatible progress treatment and local retry. The authenticated lightweight usage endpoint skips capacity, tasks, activity, observations, snapshots and recent-agent collection.

## Verification

All application execution and builds ran on `10.130.81.54`.

- TypeScript app check and targeted lint passed for new components and dashboard modules. The two legacy instance modules retain 10 baseline lint findings, down from 12; no new finding category/message was introduced. This is not a claim that repository-wide lint is clean.
- All 12 dashboard model tests passed, including bounded cubic geometry and unknown-versus-zero behavior.
- Real-route instance browser regression passed: menus, delete cancellation, empty/error skills, retained filter and pending sync across Escape/reopen, failed install retained after successful inventory refresh, pointer/keyboard resizing, draft/iframe preservation, tools hide/show, Lite/Pro, four widths and five locales.
- Full Workspace browser regression, 10 additional review groups and the isolated usage-card regression passed. Checked retained non-usage DOM and task expansion, usage-only requests, rapid period changes, local errors/retry, currencies, null gaps, Shanghai boundaries, five locales, four widths and reduced motion.
- Backend `go test ./internal/workspacedashboard ./internal/handlers ./cmd/server -count=1` passed. Real MySQL accounting/ownership regression passed in an isolated disposable DB, including lightweight usage without unrelated tables; the test DB was dropped.
- Exact archive inventory (2050 files), rebuilt binary hashes, runtime image identity and all 161 served frontend files matched the release. Runtime helper files and other deployment images remained unchanged.
- Actual deployed homepage checked at 1440, 1280, 768 and 390 widths. Actual `lite-openclaw-037` checked for top controls, modal, 2:1 default split, dragging without iframe replacement and zero lifecycle writes. Actual Today selection requested only `/api/v1/dashboard/workspace/usage`; three periods, auth 401 and invalid-range 400 passed.
- Native origin Service retains all 128 NodePorts to target 8444 and a ready EndpointSlice. Fresh Chrome with default TLS verification loaded actual OpenCode instance 50 and DSH instance 60 after one main-host certificate confirmation, with child probes 204 and no child confirmations. One initial main-login trust wait timed out; a fresh retry passed both native-page checks.
- Final health HTTP 200; 56 instances, 54 running and two manually stopped. Instance 65 remains running. No user-stopped instance was restarted. Owned test Vite servers were stopped.

Current live recorded costs have incomplete pricing and null values. The interface shows a subtotal/unknown value and no fabricated cost samples; the dual-series behavior is verified with actual recorded fixture costs.

Remote artifacts: `/root/cm-ui-refinements-20261112/release-046a68873e01/`, including rollout, HTTP/assets, production browser, actual instance/usage, native Service and final-health evidence. Native-page evidence: `/root/cm-ui-refinements-20261112/native/`. Credentials and private API responses remain on the server and are not committed.
