# U3 Task Inventory

Version: `u3-v1`

## Source Policy
- Source A: selected validation instance-reported skills inventory snapshot
- Canonical Source A path: `GET /api/v1/instances/:id/skills`
- runtime `/skills` only cross-check
- Source B: final FastSkill / skill-scanner discovery snapshot
- No task may enter `L3` unless it is recorded in the table below.
- If a new task appears after freeze, record it in `07-known-issues.md` as deferred scope unless Commander explicitly re-freezes `U3`.

## Frozen Rows
| Row | Source | Snapshot Method | Task Label | Validation Packet | Status | Evidence Path | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |

Snapshot capture note: selected validation instance 6 returned zero rows from canonical Source A at capture time.

Source B live-only override note: `skill-scanner` rolled out with local ARM64 dev candidate `docker.io/library/skill-scanner:dev-arm64-pkt16a-20260415160730`, but `/api/v1/admin/skills` returned `0` rows, so no Source B rows were appended. `P1-PKT-17A` verified that `/api/v1/admin/skills` is a DB-backed list of persisted `skills` rows and that the current live P1 path has not exercised any legal skill-row creation path (`POST /api/v1/skills/import`, `POST /api/v1/agent/skills/inventory`, or `POST /api/v1/agent/skills/upload`). `P1-PKT-18B` then exercised the product-correct agent inventory route with a real agent session on instance 6, but the runtime reported an empty inventory and no `skills`, `instance_skills`, `skill_blobs`, or `skill_versions` rows were created. This does not change repo-default or fresh-deploy truth.
