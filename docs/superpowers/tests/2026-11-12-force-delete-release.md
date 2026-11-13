# Explicit force deletion and left-side list restore release

CM source: `6425052af233bf467360ace7c730bde4d32031eb` (includes UI commit `3b1b201`).
Agent source: `b0235bc05da1cbad9fda6d89fcfd95c82758c913` in AgentsRuntime.
Environment: `https://10.130.81.54:30443/instances`.
CM version: `v2026.11.12-force-delete`, generation29, ready1/1.
CM image: `clawmanager.local/clawmanager@sha256:954c94fc3ff7bbe79b9d4f5a1da13f0f2de3e8c23ee2d85542bc702228ccf008`.

## User-approved behavior

Confirmed deletion immediately admits a durable deleting intent and terminates the target execution regardless of running tasks. It can explicitly supersede that execution's unfinished graceful stop. Single and batch HTTP deletion queue cleanup; normal Stop and idle sleep retain their existing policy. Permission, owner/generation, Team and upgrade checks remain. Internal Service.Delete/Team workflows still wait for actual cleanup proof before discarding shared resources.

The list restore entry is at the far left with explicit text, independent of Agent loading/error/header controls. It also works after refreshing a collapsed layout and in fullscreen, returning focus to the visible desktop Hide or narrow-screen instance picker. The redundant list-header fullscreen/expand-management icon is removed. List actions remain Create Instance then Instance management; the empty workspace retains its creation prompt/button and explicit management text.

## Implementation

- MarkDeleting uses a row transaction, owner/generation checks and runtime-authority revocation. Old reports and stale Lite/Pro updates cannot revive deletion. A cross-replica watcher cancels superseded stop requests and releases their lifecycle locks normally.
- Only explicit force deletion uses `kind=force-delete,graceful=false`. The same operation and complete pinned execution identity are required for CAS takeover. The Agent cancels old preparation, immediately kills the pinned process, joins cleanup, and writes the real C1 termination commitment. Stale in-flight requests cannot publish old receipts after takeover. A genuine old C1 that won the completion race can be persisted/reconciled as its actual operation; no new receipt is fabricated.
- Slot/writer reservations are released only after matching kubelet termination and resource cleanup proof. Other slots are preserved. Classic runtime endpoints have an explicit force selector; a legacy process starter without a safe dedicated force hook returns501 rather than silently waiting or claiming successful force deletion.
- The initial asynchronous Service.Delete implementation was rejected in review because Team cleanup treats its return as completed proof. Final code preserves internal synchronous semantics and uses QueueDelete only at the HTTP boundary.

## Build and deployment on54

All Go/frontend application tests, binary/image builds, browser tests and deployment ran on54. Local work was source preparation/static inspection/transfer. CM source inventory2068 files and all163 served frontend files matched; four rebuilt binaries and serving image matched. Existing app helpers/config files and OCI base layers were preserved.

Agent overlays preserve the existing production native payload, base layers and runtime configuration. Hermes ran its original image-owned release verifier with the new manager hash. The image cards, app bootstrap image pins, container images, and coordinator/worker image-reference/digest environment values were coordinated; initial startup/placement guards correctly rejected mismatches until these release settings agreed. Other namespaces were not changed.

| Runtime | Overlay digest |
| --- | --- |
| OpenClaw | `sha256:282435732e3249bcbf147f4f5e05f129ec496289b63411c68d5b019b2a915997` |
| Hermes | `sha256:12a19630bea0f275b9636181951296e12ec906b7f1c282c34e2864721a5744db` |
| OpenCode | `sha256:7ba6888e43480496704a10d9a496b1398863515166acab5c544ccc9c9d91ed31` |
| DeepSeek Harness | `sha256:09d83082158d6898c1f5420d53ecbce3055e2af8f2659b906094ef4b3dd1c916` |

## Verification

- Full merged CM packages passed: services50.463s, handlers91.955s, repository0.032s, services/k8s8.851s. Separate disposable-MySQL cases covered immediate delete admission under an occupied lock,250ms cross-replica stop cancellation, owner/generation fencing and Lite/Pro stale updates. Targeted race tests passed. Temporary MySQL Pod/forward33319 were removed; existing33309 was untouched.
- Force protocol tests passed fresh force deletion, graceful takeover, real-old-C1 completion race, exact CM/Agent C1 shared vector, ordinary retry policy, and legacy explicit selector. Team integration regression retains member/binding/shared resources when native force/proof fails. Source review/rereview found no remaining important issues.
- Runtime full `go test -race ./...` passed using a safe TMPDIR. Repeated concurrent cases covered pending startup, primary wait cancellation, cleanup/receipt races, invalid identities, replay and unaffected sibling slots.
- UI TypeScript and full browser regression passed Lite/Pro, running503/stopped/error, collapsed refresh, keyboard/focus, fullscreen placement without overlap,1440/1280/768/390 plus existing2048/five-locale/iframe/snapshot/account/deletion cases. Actual production browser verified the left entry, absence of redundant list fullscreen, management/create/empty state and native controls.
- Actual OpenCode76 and DSH78 composers loaded in fresh Chrome with normal TLS verification, one main-host certificate confirmation, child probes204 and no child-origin confirmation.
- Live test instances76/77/78 executed600-second terminal tasks with SIGTERM ignored. OpenCode76/77 shared runtime Pod27. Single delete76 was accepted in0.0223s and fully cleaned in21.929s while77's task kept running. Batch delete77/78 was accepted in0.0244s, failed0, and fully cleaned in32.339s. Database absence and original31 statuses/generations were verified. Earlier disposable warmup instances73/74/75 were force-cleaned in1.062s. All disposable instances were removed; temporary pool replicas restored to their original settings.
- Browser-origin Service still exposes128 NodePorts31600–31727 to8444 with matching selector and ready endpoint. Final health200 at11:41:17UTC:31 sleeping instances,0 running/error/offline; app generation29 ready1/1. Actual Dashboard/browser checks passed with no page errors. All four idle pool replicas returned to0, their prior setting.

## Stuck46 migration

The user explicitly authorized force deletion of the already-stuck46. Its old Agent lacked the new force protocol, so rolling its live container could have destroyed the old termination evidence. A reviewed one-time helper instead verified PodUID/slot/container ID, operation/generation/worker epoch/digest and the unique monitor/native/Agent parent chain, UID/GID, PID start time, cgroup and namespaces. It opened a pidfd and repeated the complete plan comparison before SIGKILL of that monitor alone.

The old Agent remained alive to perform its own cleanup and publish a genuine old C1. CM persisted proof, reconciled and released the slot; the lifecycle lock became free. Only then did the ordinary authenticated delete complete (HTTP202, subsequent GET404). The other31 instances retained their statuses/generations. Old46's proof was saved privately before deletion; runtime image changes followed actual release. No whole-Pod kill or synthetic proof was used.

Evidence: `/root/cm-force-delete-20261112/` (release `release-6425052af233`, live-force-delete-result.json, delete46-result.json, native/browser-result.json); Agent build/race manifests: `/root/cm-force-delete-runtime-20261112/`. Credentials, private plans, response snapshots and screenshots are not committed.
