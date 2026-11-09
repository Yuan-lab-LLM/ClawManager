# Runtime reconciliation deployment acceptance — 2026-11-10

Code commit: `773160e0644f2fdc17a576f4caec23fa34f11b92` on `preview`.

Test environment: `https://10.130.81.54:30443/admin/runtime-pods`.

Image: `clawmanager.local/clawmanager@sha256:927a3e9920bd39056099d1b806f2be0c9b8f9d5abad55b1226edfaf6095ccea4`.

The image extends the previously serving digest `6dfe4a1ddc2b54d61f6255e0bb9a74fd711ea3fd77626076cc34d1211c1ec7c7`. It replaces all four Linux executables (server, northbound gateway, backup controller and backup artifact tool), plus the current frontend index/assets. Existing Hermes desktop assets, nginx configuration, TLS Secret and Runtime images remain from that serving base. All four deployed executable hashes match the compiled payload.

The `clawmanager-app` rollout completed. `/api/v1/version` reports `preview-773160e0` / code commit above; migrations 106, 107 and 108 are recorded. The original five Runtime Pod names retain their original UIDs. The browser certificate fingerprint is unchanged, so this rollout does not require another certificate confirmation.

Real Chrome acceptance passed at desktop and mobile widths: the Recovery dialog loads the actual error instance #25; stopped instance #161 remains stopped at generation 16 and its retry action is disabled; a stale-generation POST is rejected with409 and leaves that instance unchanged; a nonexistent operation cannot create a retry task; stale Pod UID and unauthenticated inspection requests are rejected. No page errors occurred. Scheduling history remains readable with the new queue flag.

Existing instance #25 remains `error` at generation14 with no SQL binding and blocker `missing`. This rollout does not claim that unknown legacy source authority has been recovered. Older workers without complete physical identity/exit proof remain quarantined; repair/upgrade the source and use the documented recheck/retry procedure. The new controller does not treat these slots as idle or force-release their ownership.

Temporary local MariaDB, the owned node-loopback MySQL8.4 fixture, its container/snapshot, SSH tunnel and temporary frontend server were stopped. Temporary browser tokens were removed on both hosts. Zero Kubernetes test Pods were created. Imported deployment image content is retained; scratch OCI archives and hardlinks were removed. No business SQL was manually rewritten and no workspace/PVC was deleted.

See [the recovery guide](../../runtime-reconciliation-recovery.md) for operator steps and test coverage.
