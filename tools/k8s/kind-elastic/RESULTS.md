# Local acceptance results — 2026-10-19

Cluster `cm-elastic` is running in the separate Colima `cm-kind` profile.
Kubernetes **1.35.0**, containerd **2.2.0**, Linux **6.8**, cgroup **v2**.
PodLevelResources, MemoryQoS and Pod user namespaces are enabled. The node is Ready;
all nine application Pods are Ready, including four runtime Pods with **9/9**
containers each: one coordinator and eight slots. All current application containers
had zero restarts at final verification.

Access: **https://localhost:18443**. Credentials are private and generated locally:
`.deploy/kind-elastic-20261019/admin-login.private.txt`.
Kubeconfig: `.deploy/kind-elastic-20261019/kubeconfig`.

## Verified

- Eight-slot capacity is reported for all four runtimes.
- All four instances run with a fractional **0.5 CPU** business configuration.
- Native pages/assets, workspace API and business-process isolation pass for
  Hermes **2**, OpenCode **3**, DeepSeek Harness **4**, and OpenClaw **5**.
- Hermes uses its managed Desktop bridge and scoped cookie; sessions API returns
  200. OpenCode health returns healthy. DeepSeek Harness `settings/describe` succeeds.
- Business processes have zero capabilities, seccomp and no_new_privs enabled,
  private PID/user/mount namespaces, their own visible workspace, and no access
  to the tested other-instance directory, management paths, service-account token,
  management environment credentials or sysfs.
- At 1/32 memory scale, a slot with 32Mi request borrows above 64Mi, with 128Mi
  child and 512Mi parent hard limits. Exceeding the child limit OOM-kills only
  that slot; exceeding the aggregate limit produces a parent cgroup OOM.
  Actual `memory.min`, `memory.max` and `memory.events.local` were read.
- Each native runtime Pod's cgroup has an actual **2Gi** memory cap and **1 CPU**
  cap, matching the smaller local profile.
- All four formal **4 CPU / 16Gi** Pod configurations, with eight **1Gi request /
  4Gi limit** slots, pass server-side API dry-run admission.
- Failed creates with published assignments and pending SQL reservations can
  now stop through the normal API, with real confirmed teardown receipts.
  The four initial test reservations were stopped this way before replacing Pods.
- Focused backend config/Kubernetes tests, slot-client lifecycle tests and shared
  agent gateway/slot tests pass locally and in Linux remote builds. The CPU
  parsing and pending-stop regressions were reproduced before their fixes.
- New images were built on **172.16.1.12**, then pulled into kind. Production
  backend and four runtime Deployment image references remain unchanged.

## Image references

| Component | Registry manifest SHA256 |
|---|---|
| Backend | `bd539d1204a0d05b6bbe630c01a9a5bae7b685a5ca9ba5c69cc3c450001e74b8` |
| OpenClaw Lite | `f68d4a0a0250d6cd0fcc8034d5b0965071032047bfd82d032daca5da8cd92e01` |
| Hermes Lite | `ec02c16c31a04ec4aca9b10bcd04d0c563e28ed30b18d56d4d7bde9bbd82ef2c` |
| OpenCode Lite | `e96cd2d4f3bf801fce64d77bece4be0612b0b208d813029aedf53ef8198535d6` |
| DeepSeek Harness Lite | `0c991ab96cbf13f80fb536b9271a33e1fa362418e9d0b01bb90749a5396ba3cc` |

Full registry references and remote build/source records are in
`.deploy/kind-elastic-20261019/{images,backend-image,runtime-images}.json`.
Hermes payload metadata was regenerated and verified with the project's release
tool after replacing its agent. `desktop_web_accepted` remains **false**; this
development test does not claim signed release-campaign acceptance.

## Scope and evidence

The Mac has only 16GiB physical RAM; Colima uses 10GiB. Native applications use
the explicit smaller development profile from [README.md](README.md). Full-size
16GiB pressure/performance tests were **not** run. The 1/32 allocator test proves
the cgroup sharing/limit behavior, not native application performance at scale.

Storage is a local PVC backed by an 8GiB ext4 filesystem in the VM. This one-node
test does **not** validate Longhorn or cross-node migration. A dummy local model
entry permits startup; configure a real provider before conversation testing.
No inference or paid API calls were made, and browser interaction beyond HTTP
pages/assets and native APIs was not automated.

Machine-readable evidence lives under `.deploy/kind-elastic-20261019`:
`memory-acceptance.json`, `native-acceptance.json`, `formal-admission.json`,
`native-pod-budgets.json`, `stop-recovery.json`, `cluster-snapshot.json`, and
`production-readonly-check.json`. Generated Secrets and login/session files are
kept private with mode 0600 and are not included in these reports.
