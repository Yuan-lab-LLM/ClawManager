# Evidence Index

| Scope | Claim / Packet | Artifact Path | Evidence Type | Status | Notes |
| --- | --- | --- | --- | --- | --- |
| Gate 1 | Seed packet structure | `docs/superpowers/releases/2026-04-15-p1-internal-rc/README.md` | doc | PASS | |
| Gate 1 | K3S manifest client dry-run | `/tmp/p1-k3s-dryrun.txt` | command output | PASS | deploy-preflight syntax check |
| Gate 1 | fresh-cluster replay created and preflight captured | `/tmp/p1-pkt19-preflight.txt`; `/tmp/p1-pkt19-cluster-create.txt` | command output | PASS | fresh `k3d` replay started from zero on Mac/ARM |
| Gate 1 | repo-default Mac/ARM deploy boundary reproduced | `/tmp/p1-pkt19-apply-default.txt` | command output | BLOCKED | fresh apply reached `skill-scanner` `ImagePullBackOff`; `ghcr.io/yuan-lab-llm/skill-scanner:latest` has no ARM64 manifest |
| Gate 1 | live-only ARM override commands captured for local validation | `/tmp/p1-pkt19-overrides.txt` | command output | PASS | exact `k3d image import` and `kubectl set image` path frozen for local Mac/ARM validation; not repo-default truth |
| Gate 2 | system image settings API reachable | `/tmp/p1-task3-system-images.json` | API body | PASS | `.data.items` returned a JSON array; ARM runtime bootstrap path exists |
| Gate 2 | admin models API reachable | `/tmp/p1-task3-admin-models.json` | API body | PASS | `.data.items` returned a JSON array; model bootstrap path exists |
| Gate 2 | fresh control-plane startup succeeded after live-only ARM overrides | `/tmp/p1-pkt19-rollout.txt` | command output | PASS | `mysql`, `clawmanager-app`, and `skill-scanner` all reached `Running`; `healthz` returned `200`; admin login passed |
| Gate 2 | fresh replay stopped at operator model-bootstrap boundary | `/tmp/p1-pkt19-bringup.txt`; `/tmp/p1-pkt19-verdict.txt` | API body + analysis note | BLOCKED | `/api/v1/admin/models` remained empty and operator model env was unset, so the replay stopped before validation-instance bring-up without hidden shortcuts |
| Gate 3 | frozen U3 inventory seeded | `docs/superpowers/releases/2026-04-15-p1-internal-rc/03-u3-task-inventory.md` | doc | PASS | versioned task scope artifact |
| Gate 3 | selected validation instance skills snapshot captured | `/tmp/p1-task4-instance-6-skills.json` | API body | PASS | canonical Source A returned 0 rows at capture time |
| Gate 3 | scanner rollout unavailable | `/tmp/p1-task5-skill-scanner-rollout.txt` | command output | BLOCKED | rollout timed out with 0 of 1 updated replicas available on the local ARM validation path |
| Gate 3 | scanner pod describe captured | `/tmp/p1-task5-skill-scanner-describe.txt` | kubectl describe | BLOCKED | `skill-scanner` pod is `Pending` with `ImagePullBackOff`; image pull failed with `no match for platform in manifest`, so Source B discovery and `L3` stay stopped |
| Gate 3 | local ARM64 dev candidate live override applied | `/tmp/p1-pkt16b-set-image.txt` | command output | PASS | `skill-scanner` deployment image temporarily set to `docker.io/library/skill-scanner:dev-arm64-pkt16a-20260415160730`; live-only override, not repo-default truth |
| Gate 3 | local ARM64 dev candidate rollout available | `/tmp/p1-pkt16b-rollout.txt` | command output | PASS | `skill-scanner` rolled out on the live ARM path with the local candidate override; this does not promote Gate 1 |
| Gate 3 | live scanner status captured after override | `/tmp/p1-pkt16b-status.txt` | command output | PASS | `clawmanager-app` and `skill-scanner` were both `1/1` at capture time on the live override path |
| Gate 3 | scanner config reachable after live override | `/tmp/p1-task5-admin-security-config.json` | API body | PASS | `/api/v1/admin/security/config` returned `.data` on the live override path |
| Gate 3 | admin skill catalog snapshot captured but returned zero rows | `/tmp/p1-task5-admin-skills.json` | API body | BLOCKED | `/api/v1/admin/skills` returned an empty array on the live ARM64 dev candidate override path, so no Source B rows could be appended |
| Gate 3 | `/admin/skills` route and legal write paths verified | `/tmp/p1-pkt17a-routes.txt` | code trace | PASS | `/api/v1/admin/skills` is a DB-backed `skills` list; legal row creation paths are user import and agent inventory/upload |
| Gate 3 | live API and MySQL skill tables confirmed empty | `/tmp/p1-pkt17a-db-or-api-state.txt` | API body + DB query | BLOCKED | `skills`, `instance_skills`, `skill_blobs`, `skill_versions`, and `security_scan_jobs` were all `0`; current live path has not exercised a legal skill-row creation path |
| Gate 3 | Source B empty-state root cause frozen | `/tmp/p1-pkt17a-verdict.txt` | analysis note | BLOCKED | scanner readiness alone does not populate `skills`; under the currently observed P1 path, Source B does not naturally surface |
| Gate 3 | real agent inventory route exercised on instance 6 | `/tmp/p1-pkt18b-runtime-or-agent-proof.txt` | API body | PASS | `POST /api/v1/agent/skills/inventory` succeeded with a real agent session token from the runtime environment |
| Gate 3 | before/after skill state remained empty after real agent inventory report | `/tmp/p1-pkt18b-before-state.txt`; `/tmp/p1-pkt18b-after-state.txt` | API body + DB query | BLOCKED | the runtime reported an empty inventory; `skills`, `instance_skills`, `skill_blobs`, `skill_versions`, and `security_scan_jobs` remained `0`, and no `collect_skill_package` command was created |
