# Hermes Lite v0.22.5

This update targets newly provisioned Hermes Lite instances. Runtime and ClawManager must be deployed as a compatible pair. It does not introduce an old-workspace migration API or batch migration tooling. Existing 0.21.0 workspaces must not be pointed at the new image.

The pinned source is `NousResearch/hermes-agent` tag `v0.22.5`, commit `bf0203a794a07aac3ff764972ab9087baa4e4f4e`. Source archives, patch files and release artifacts remain integrity checked. Release compatibility does not imply signed end-to-end acceptance.

## GitHub architecture integration

- The runtime implementation stays in `hermes/lite`; its executable is `hermes/cmd/hermes-lite-agent`, composed with the public shared `clawmanager-agent/gateway` module.
- NFS credential-scoped preparation, isolated slots, automatic recovery, idle stop, managed credentials and Provider grouping remain in their existing integration paths.
- Python 3.14, the upstream sealed package-manager environment and headless TUI build replace the 0.21 build recipe. Startup instrumentation, bounded skill traversal and read-only probes for already initialized stores reduce repeat startup work.
- Version-specific Team completion and external-skill deduplication adapters preserve GitHub behavior in the new split tool round and skill-sync state. They run after the version-locked startup patch. The Team supervisor and release inventory are retained.
- ClawManager keeps its root-level `hermes-desktop-web` package and adapts the browser bridge and bidirectional RPC to the new protocol. Approval responses remain socket/session scoped and preserve GitHub's restricted approval projection. Verified renderer caches are replaced atomically to remove retired upstream files.

## Scope and provenance

Adapted from GitLab runtime commit `061e329` and ClawManager commit `4223b774`. The initial sync excluded migration commits and uncommitted session-schema/FTS/model-catalog fixes. The 2026-11-09 picker follow-up incorporates the reviewed model-catalog cold-load patch and its tests; legacy session-schema/FTS migration remains excluded. Pro/Webtop is outside this Lite upgrade.

Before publishing, build the runtime image, run its release verification and perform a paired new-instance smoke test (create, model selection, chat, restart and Team where enabled). Do not reuse previous release signatures or claim deployment acceptance from local unit tests.

## Local validation (2026-11-09)

- ClawManager Hermes service tests passed on Linux. Renderer: 36 tests, TypeScript check, dependency-closure audit and production build passed.
- Runtime Hermes module and shared Gateway Go tests passed on Linux.
- Python boundary/supply-chain/adapter suite: 140 tests, 131 passed and 9 skipped because they require an installed Hermes application environment.
- Docker `patched-source` target built successfully from the verified upstream archive, including the v0.22.5 Team completion and skill-deduplication adapters.
- These were the initial source-level checks. Subsequent full-image and isolated cluster verification is recorded below; it does not constitute signed release promotion.

### Model picker follow-up

The Web picker accepts only the local `default`/`current` profile labels and removes them before forwarding to the authenticated instance. Ordinary model catalogue reads use background manifest and optional models.dev metadata refresh and the upstream 1.5-second custom-provider discovery budget; explicit Refresh Models retains full discovery. Configured/cached model rows remain fallback when discovery times out. The patch, upstream files and regression tests are included in the pinned release inputs.

## Shared slot migration and retry

In `nfs-rootless-v1`, a successful stop request initially returns HTTP 202 while the slot container exits. The control plane now waits within the stop deadline for the exact kubelet termination commitment and retries temporary replacement-worker unavailability using the same persisted proof. Pod UID, operation, worker epoch, assignment and generation checks remain mandatory. An absent or mismatched proof never authorizes a target start.

Migration uses the existing transactional source-release path to tolerate stop heartbeats and reconcile slot occupancy. History retries include `retry_operation_id`: the failed operation, old binding and single generation fence must still match. A later stop intent, replaced source Pod or replay after a completed move is rejected. This is a shared control-plane fix; only Hermes was exercised in the deployed acceptance environment.

## Paired image and cluster verification (2026-11-09)

- Full manager and runtime images were built and imported into an isolated Kubernetes test environment. Nonroot runtime capability verification and installed-application HTTP/WebSocket smoke checks passed.
- Four Hermes instances ran across two Pods with four slots each, using a dedicated NFS export and `nfs-rootless-v1`. Initial model catalogue reads returned three providers and 24 models in 42–252 ms.
- Authenticated HTTPS/BFF WebSocket checks passed for model selection, session creation, streamed real-provider replies, event replay, reconnect/resume and preserved conversation context. Browser-click acceptance remains unverified.
- Retrying an actual failed source stop completed in about 10 seconds without advancing the restart fence twice. A subsequent live cross-Pod migration completed in about 12 seconds. The old container termination and replacement were checked; stale retry was rejected. Neighbor instance generations and container IDs remained unchanged, and existing NFS probe files survived.
- Manager regressions: 44 Hermes tests and 49 public migration/container-stop tests passed. Migration tests used a disposable MySQL with no skips; five affected test groups were rechecked after adding stale-retry rejection. Frontend runtime-management tests: 11 passed. TypeScript and production image builds passed.

This verifies a single-node, cross-Pod setup with a native test NFS server. Cross-node scheduling and Longhorn RWX are not covered. Legacy workspace migration, Team end-to-end acceptance and signed release promotion remain outside these results. The namespace-limited service account also lacks the cluster-wide Pod-list permission used by the existing dashboard overview; that separate RBAC issue is not changed by this upgrade.
