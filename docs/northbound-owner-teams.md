# Northbound Owner Teams

Owner Teams use the existing Team execution engine, but are distinct from legacy CM Teams. This feature does not upgrade OpenClaw, change Runtime images, or alter Worker tool parameters.

## Configuration and ownership

Enable `owner_teams_enabled` in Admin → Northbound → Access after deploying the compatible schema and App. For the owner portal, configure `owner_team_creation_user_id` to an active ClawManager account. This account supplies member quota, image policies and billing; it is NOT the end user's owner identity. Never derive it from the first instance. New deployments default to disabled and no creation account.

IEI routes under `/api/v1/ieisystem` derive owner exclusively from the validated IEI session cookie. Public routes below use `/api/northbound/v1`, require the existing API-key/session authentication and an explicit normalized `owner` query parameter. The authenticated provider user ID is never accepted from the request body. Owners and providers cannot enumerate each other's Teams, templates, files, tasks or operations. Legacy CM routes remain CM-only. Background execution supports both types.

New scope grants are explicit; old keys do not automatically gain Team privileges. Cookie writes enforce Origin and cross-site checks. Logout persists session revocation across App replicas; an authorization storage failure fails closed. Token/key material is never returned in a Team DTO.

## Complete route list

Member responses include a read-only `runtime_observation` with `state`
(`online`, `starting`, `offline`, `unknown`), an optional fixed `reason`, and
`observed_at`. This observation combines current instance/binding health and
independent collaboration presence. `last_seen_at` remains the last Team event;
an unused or quiet member does not need a task to become available. Presence
never changes assignment IDs, results or task completion. List summaries also
return `confirming_count` and `fault_count`; an initial confirmation is distinct
from an observed failure. Missing collaboration proof after the startup grace
period is reported for inspection; an unavailable status store is not reported
as a dead member. No new Runtime or model tool parameters are required.

| Method / relative path | Required scope |
| --- | --- |
| GET `/team-capabilities` | `teams:read` |
| GET `/teams` | `teams:read` |
| POST `/teams` | `teams:create` |
| GET `/teams/:id` | `teams:read` |
| PATCH `/teams/:id` | `teams:manage` |
| DELETE `/teams/:id` | `teams:delete` |
| GET `/teams/:id/members` | `teams:read` |
| DELETE `/teams/:id/members/:memberID` | `teams:manage` |
| GET `/teams/:id/tasks` | `teams:tasks:read` |
| POST `/teams/:id/tasks` | `teams:tasks:write` |
| GET `/teams/:id/events` | `teams:tasks:read` |
| GET `/teams/:id/workspace/files` | `teams:files:read` |
| GET `/teams/:id/workspace/preview` | `teams:files:read` |
| GET `/teams/:id/workspace/download` | `teams:files:read` |
| GET `/teams/:id/workspace/text` | `teams:files:read` |
| PUT `/teams/:id/workspace/text` | `teams:files:write` |
| POST `/teams/:id/workspace/upload` | `teams:files:write` |
| POST `/teams/:id/workspace/folders` | `teams:files:write` |
| PATCH `/teams/:id/workspace/entries` | `teams:files:write` |
| DELETE `/teams/:id/workspace/entries` | `teams:files:write` |
| GET `/team-templates` | `team-templates:read` |
| GET `/team-templates/:key` | `team-templates:read` |
| GET `/custom-team-templates` | `team-templates:read` |
| POST `/custom-team-templates` | `team-templates:generate` |
| GET `/custom-team-templates/:id` | `team-templates:read` |
| PATCH `/custom-team-templates/:id` | `team-templates:write` |
| DELETE `/custom-team-templates/:id` | `team-templates:write` |
| POST `/custom-team-templates/:id/revise` | `team-templates:generate` |
| POST `/custom-team-templates/:id/regenerate` | `team-templates:generate` |
| POST `/custom-team-templates/:id/members/:memberID/adjust` | `team-templates:generate` |
| POST `/custom-team-templates/:id/members/:memberID/regenerate` | `team-templates:generate` |
| PUT `/custom-team-templates/:id/members/:memberID/icon` | `team-templates:write` |
| GET `/team-operations/:operationID` | `original operation scope` |
| POST `/team-operations/:operationID/retry` | `original operation scope` |

The `/operations/:id` public alias can also retrieve a Team operation with the original scope and owner. IEI operation records additionally require the original session. Resource recovery resumes the original operation; it does not create another Team. Model generation with an unknown result is not automatically billed again.

## Create a Team

```http
POST /api/northbound/v1/teams?owner=alice%40example.com
Authorization: Bearer <your-northbound-token>
Idempotency-Key: team-create-unique-intent-001
Content-Type: application/json

{"name":"AI brief","template_key":"builtin-leader-worker","revision":1,"members":[{"member_id":"leader","runtime_type":"openclaw","instance_mode":"lite"},{"member_id":"worker","runtime_type":"hermes","instance_mode":"lite"}]}
```

Use keys/member IDs and revisions returned by the template catalog; the example is not a requirement to use this template. Either `template_key` or owner-private `template_id` is required, never both. Optional fields: description and supported communication_mode. The server resolves member images/resources; image/env/secret/PVC/path overrides and caller-supplied owner/user_id are rejected. Leader remains OpenClaw; supported workers are OpenClaw/Hermes, Lite/Pro subject to configured images and quota.

Response: `202 {"data":{"operation_id":"...","status":"queued","team_id":123,"attempt_count":0}}`. Poll `GET /team-operations/:operationID?owner=...`. Same key + same intent replays the original operation even if template/account/image defaults later change. Same key + different intent returns 409. Partial creation retains member identities, storage and secrets; it does not delete failed same-name Teams. Finished resource failures support `POST /team-operations/:operationID/retry` with `{"expected_attempt_count":1}` copied from the last operation response. Uncertain in-flight effects remain protected rather than duplicated.

Creation remains pending until every member's actual Runtime is ready, not merely after instance records are created. Readiness observations run approximately every 15 seconds without consuming creation retries. Lite readiness verifies the current binding generation, live Pod identity and Gateway state; older agents use the bounded health-check fallback. If readiness does not converge within 15 minutes, the operation reports `OWNER_TEAM_RUNTIME_TIMEOUT` and preserves all resources. Retry resumes the original operation and member identities with a new readiness deadline; it does not recreate members or repeat an accepted bootstrap. Previously successful operations are not reopened.

## Chat, history and files

Dispatch: `{"message_id":"stable-message-uuid","target_member_id":"leader","execution_type":"team","payload":{"prompt":"Prepare a brief"}}`. Target an existing Worker with `execution_type":"single_member"`; optional `context_task_id` must be from this Team. Reuse message_id until acceptance is known. Owner display/audit identity is server-controlled. Worker results do not close a Leader-mediated root; existing formal completion and validation rules are unchanged.

List tasks/events using bounded `limit` and `before_id`. Follow `next_before_id` even if a page contains no visible events. Hidden diagnostics, analysis and control envelopes are not public chat. Lists return pagination/counts scoped identically. Ordinary instance lists exclude only northbound Team members, including creation residues identified by their stable provisioning operation; usage collection still includes them.

Workspace GET list/preview/download/text takes relative `path`. PUT text: `{"path":"report.md","text":"# Updated","version":"<version-from-read>"}`; stale versions return409 and leave remote content unchanged. POST folders: `{"path":"","name":"drafts"}`. PATCH entries: `{"path":"report.md","new_name":"final.md"}`. DELETE entries takes path query. Upload is multipart with path, files and corresponding relative_paths (max100 files /64MiB total). File JSON transport is bounded at8MiB; ordinary public JSON limits remain unchanged. Download streams binary, without the JSON response cap. Existing symlink/traversal, preview and text validation rules apply.

## Templates and management

Built-ins are read-only, revisioned business templates. Owner-private templates use the existing generation/parsing engine and revision CAS:

- Generate: `{"intent":"Research and review","member_count":3}`.
- Rename: `{"name":"New name","expected_revision":1}`.
- Revise: `{"name":"New name","intent":"Revised duties","member_count":3,"expected_revision":1}`.
- Regenerate whole/member: `{"expected_revision":1}`.
- Adjust member: `{"instruction":"Focus on primary sources","expected_revision":1}`.
- Change member icon: `{"icon_override":"<catalog icon>","expected_revision":1}` (empty restores automatic).

Model actions return202; unknown paid results are not automatically repeated. Template edits/deletion do not modify frozen existing Teams. Team PATCH accepts name/description only and never renames directories or instance identities. Team/member DELETE requires stable Idempotency-Key and `{"confirm_data_loss":true}`; it can delete member/shared data. Leader and active-task protections remain. Dispatch and destructive admission use the same durable lock; ordinary instance reset/delete cannot bypass Team protections.

## Portal and rollback

The owner portal uses the selected master/detail Team workspace, independent of dashboard statistics. Member links enter northbound instance details. Full Team chat reuses the original work area with one built-in file browser (no duplicated outer instance file browser). Navigation/drafts/reading anchors are scoped by owner+Team and never resend on return.

Migration077 is additive/restartable. Existing Teams/templates remain cm with null owner; IDs, paths, Redis keys and histories are not rewritten. Back up MySQL before deployment. Do NOT roll back to an App predating resource_scope while owner Teams exist: it can mix resource types. Use a compatible rollback build that keeps scope guards/background consumers, with the new entry disabled. Disabling entry never deletes data or cancels existing background work.


### Portal workspace presentation

The northbound Team dashboard uses a bounded master/detail workspace. Template
lists and editor content scroll inside the panel; headings and form actions stay
visible. Team and standalone instance cards share portal geometry tokens, with
neutral surfaces and an optional hover-only rotating Team border (disabled by
reduced-motion preferences).

Template drafts are scoped by owner and template in session storage and retain
the revision they were based on. Initial loading must finish before the editor
is exposed. Refreshing the list never replaces form input. A version conflict
keeps the draft; loading the latest revision is an explicit action and never
retries a mutation automatically. Generation continues to use the existing
idempotent operation APIs. Saving only the name does not call generation.

Native dialogs contain team creation, management and member-duty adjustment;
closing an edited dialog requires acknowledging the unsaved input. These UI
changes do not alter Team ownership, dispatch, receipts, Runtime selection, or
file editing/preview semantics.


The compact template editor keeps Team information beside one selected member's
full duties and deliverables on wide screens. Actual template members use a
three-column selector with up to two rows; medium screens switch information
panes and phones use an accessible member select. A requested member count is
an input to template revision, never a reason to invent member placeholders.

Operation feedback floats at the upper right without changing panel dimensions.
Success dismisses automatically; pending/error feedback remains available, and
closing a notice never cancels or forgets the accepted operation. A bounded,
owner-scoped browser-session history stores only notice summaries and operation
IDs. Inspecting an operation reads its current status through the existing
scoped API; retries continue to use the established recovery endpoint. Late
progress cannot overwrite a recorded terminal result.
