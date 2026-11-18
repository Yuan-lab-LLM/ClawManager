# Model activity liveness staging acceptance

Fix commit `831939ec026ba21d07135b856ee622dc316756c1`; application image `clawmanager.local/clawmanager@sha256:1d3488b31d5c44bf2d9d8ed1a8fbde99155c0d498bc916a4bc976eee71590eb0`. Migration 109 adds renewable model request leases and expiry receipts without discarding legacy records.

The initial real SQL regression failed with an expired request still counted active. Final verification passed real SQL admission/renewal/reconciliation/final-stop/migration/concurrency cases, existing automatic-stop regressions, fake-time direct chat/stream/image cancellation and watchdog cases, gateway/handler/controller race checks, four-type automatic stop retry/data-retention integration, and real SQL controller tests with both exited and still-terminating old processes. Two focused independent reviews found no critical or important issues. Darwin service tests used the existing unrelated Linux-only NFS compile overlay.

Staging observation at 17:29 CST:

- Instance 118 entered `stopped`, automatic-stop phase `completed`, reason `idle_timeout`, with no blocker or remaining binding.
- Its one legacy unfinished request was automatically closed as `legacy_process_exited`; all 84 current-generation scopes were finished. No manual database repair or manual Stop was used.
- Last model activity stayed `2026-11-10T08:47:33.409958Z`; cleanup did not restart the idle clock.
- Seven observed project files, workspace PVC UID, certificate and share link were unchanged. The previous Runtime Pod was subsequently reclaimed through normal maintenance.
- The operator changed idle settings during this work from version 9 (60-second checks / 300-second idle) to version 10 (60-second checks / 1800-second idle). Deployment did not overwrite this setting. Physical acceptance used the current 1800-second policy; fake-time tests separately cover sustained five-minute inference protection.

Private node receipt: `/root/cm-model-lease-20261110/verified.json`; observations: `observations.json`. Disposable SQL tests used isolated local databases, never application business tables.

The user subsequently created OpenClaw instances 170–199. They exposed a separate zero-capacity/default-lineage recovery gap: the approved canonical OpenClaw source had been retired and its deletion fence correctly prevented reuse. Their recovery is separate from this liveness fix; later non-OpenClaw user instances were running during the observation.
