# Focused instance workbench release

Deployed source: `5a2ff694b640f09e5f457b87b86a6355a74e0085`.
Environment: `https://10.130.81.54:30443/instances`.
Image: `clawmanager.local/clawmanager@sha256:55fd59e1cdf835dcc810cc9d46071ec1ab682b8b1c6e11b312aba88732ae1438`.
Deployment: `clawmanager-system/clawmanager-app`, generation 24, ready 1/1.

## Delivered

- Removed duplicate page header and model-reply hint/polling. Editable instance name and lifecycle state now live in the Agent frame header. Share, skills and runtime management use labelled icon buttons with tooltips; runtime actions and confirmation behavior remain intact.
- Instances/Agent/tools default to 12:68:20. Both separators support pointer and keyboard controls. The instance list can hide/restore; account-scoped session preferences retain proportions. Restoring either side pane preserves minimum Agent width.
- List rows use semantic state tints rather than visible status labels. Accessible names retain instance type and actual state.
- Switching prepares fresh authorized target metadata before navigation, retains the current workspace while waiting and persists list search/page/scroll. Normal SPA transitions retain the outer CM shell. A 15-second handoff contains only validated display/runtime metadata, no access URLs or credentials. Native browser-origin document isolation remains unchanged.
- `/instances` resumes the last authorized instance, falls back to a running instance, and offers management/creation for an empty inventory. The sidebar expansion reuses the complete management table, including filters, creation, selection, bulk actions, lifecycle operations and history. Embedded filters do not rewrite the workspace URL; legacy filtered links initialize the expanded view.
- The same header slots apply to Hermes pending/unavailable/ready views without remounting skill operation state. Mobile pickers expose full management, with nested Escape handling and focus restoration. Share configuration uses a bounded native top-layer popover so it is not clipped inside the Agent frame.

## Verification

All application execution, tests, bundling, binary and image builds ran on `10.130.81.54`; local work was source preparation, static inspection and transfer only.

- TypeScript and targeted lint passed for the new/modified reusable components and navigation modules. No repository-wide lint claim.
- Sixteen navigation/Hermes unit cases passed. The Hermes browser regression also verifies header controller identity/state through running/unavailable/running, plus existing conversation recovery, new chat and user/instance isolation.
- Real-route browser regression passed: header controls, rename validation/error/stale responses, single-flight metadata, server-side search/pagination/shrink/retry, account isolation, 12:68:20 split, hide/restore bounds, drag-preserved iframe/draft, prepared switching with the old frame retained, outer shell/document identity, list scroll retention, last-work entry, filtered bookmarks, expanded management, nested mobile dialogs, Lite/Pro, five widths (2048/1440/1280/768/390) and five locales.
- Full dashboard browser regression passed. Actual deployed dashboard checks passed at four widths with aligned panels, theme, gauges, logos, running/total values and isolated token-period requests.
- Exact source inventory (2059 files), all four rebuilt binary hashes, serving image and all 161 frontend files matched. Runtime helpers/nginx files and other deployment images remained unchanged.
- Actual deployed instance checks passed: frame-level icons, rename entry/cancel, expanded table, list hiding, default ratios, pointer resize without iframe replacement, skills modal, server search, switching and `/instances` resume. No name/lifecycle mutation was sent in production checks.
- Actual OpenCode 50 and DSH 60 loaded in fresh Chrome with default TLS verification, one main-host certificate confirmation, child certificate probes HTTP 204, zero child certificate confirmations and visible native composers. The test harness waits for the main interstitial DOM before confirming it; TLS verification was not disabled.
- Browser-origin Service retains 128 NodePorts (31600–31727), target 8444, matching app selector and a ready EndpointSlice. Health endpoint HTTP 200; manually stopped instances 71/72 were preserved; instance 65 remains running.

## Auto model repair

The reported instance 69 Auto failures were caused by invalid catalog configuration: both active models had `context_window_tokens=10` (ten tokens). Audit records rejected both with `context_capacity_exceeded`, before provider inference; actual manual calls had 13K–19K input tokens. The UI/API use raw token counts, with no K-token conversion. Provider discovery supplied no authoritative context capacity.

At 16:49:11 Asia/Shanghai, the normal administrator API changed only model IDs 1/2 from 10 to documented unknown/default value 0, preserving all other fields. No provider limits were invented and known-window capacity checks remain enforced. Private configuration backup is stored only on 54.

The original OpenClaw-sized prompt/output shape passed the actual routing regression after correction; known insufficient capacities still reject. One isolated real Auto request (`max_tokens=16`) returned HTTP 200 with content, selected DeepSeek-V4.1-Flash and completed with 12 output tokens. No message was sent to an existing user conversation. Post-rollout read-only API verification at 16:56:29 confirmed both corrected values persisted. No source change was required for this configuration repair. Zero means unknown, not unlimited provider capacity.

## Runtime observation requiring follow-up

During final monitoring, instance 68 (`lite-openclaw-035`) reported `gateway_process_exited` at 08:56:36 UTC while its hosting runtime Pod remained 9/9 Running. Its runtime-agent log first reported the gateway listener absent. The same instance had a similar historical exit on November 11. This is a runtime child-process failure path, distinct from Auto's pre-inference capacity rejection; available logs do not establish the trigger or prove causal independence from other concurrent activity.

At 08:58:40 UTC, the instance remained error/generation 3. Built-in recovery had attempted twice, phase `placing`, with the next retry scheduled for 09:03:30 UTC. Recovery had not completed. No manual instance start/restart, configuration overwrite or workspace mutation was performed. The final snapshot was 56 total: 53 running, 1 failed, 2 manually stopped. Application health was HTTP 200. This release does not claim that this separate runtime failure was fixed.

Remote release evidence: `/root/cm-instance-focus-20261112/release-5a2ff694b640/`; native evidence: `/root/cm-instance-focus-20261112/native/`; Auto incident evidence: `/root/cm-auto-routing-20261112/`. Credentials/private snapshots and native screenshots are not committed.
