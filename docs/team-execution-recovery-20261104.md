# Team execution recovery maintenance

Scope: TEAM `codex/team-personal-integration`. Base App 5c091b43,
Runtime45ccedf; no personal/backup checkout changes, sandbox activation,
business schema migration or Team scheduling redesign.

## Repaired contracts

* Restore the native OpenCode completion tool's complete optional delivery and
  review schema. Required arguments remain status/summary. Known completion
  aliases normalize only on explicit completion calls; deterministic argument
  errors are distinct from uncertain effects. Model-supplied transport identity
  never replaces the authenticated active input.
* Observe every active root by paged SQL queries, including pending/dispatched,
  running/waiting and Leaders before delegation. Historical cards cannot hide
  current assignments behind a last-500 limit. Per-root errors do not stop the
  remaining sweep. Existing 30-second maintenance remains the scheduler.
* An explicit native failure is not a missing receipt. Confirmed-stopped turns
  get at most three same-root recovery notices with bounded delay; a fresh
  admitted execution suppresses the old failure. Unknown/live executions never
  admit a concurrent model. Independent native active/inbox checks retain/release
  execution identity; unresolved recovery becomes visible without closing roots.
* Accepted OpenCode delivery may receive its final public native body as a
  separately identified supplement. Gate by root/member/input/source message,
  business revision, accepted receipt and process generation/admission evidence.
  Preserve the original receipt/hash/review facts and root terminal state.
  Reasoning, tools and intermediate public prose stay private. Supplements have
  bounded context text with truncation/source information; no file is required.
* Publish immutable supplements through a private member-state journal; lost
  responses/restarts retry the same event, never another execution. The backend
  atomically commits delivery metadata, workflow version, public event and
  notification/ACK outbox. Explicit corrections update the same work item.
* Hash derived correction event IDs to fit the existing varchar(128). Failed
  Redis event projection may advance its cursor only after durable internal
  replay intent is recorded. Preserve original stream identity on replay.

## Preservation boundaries

Leader dispatch order, serialized member execution, dependency/review rules,
root completion ownership, normal OpenClaw/Hermes/ordinary instances and owner
permissions remain. No successful task is reopened and prose/files do not create
completion receipts. Authorization stays fail-closed; static error categories
improve diagnosis without exposing upstream response bodies or credentials.

## Validation and release

Use focused native tool-schema/bridge/public-delivery tests; real isolated
MySQL strict-ID/transaction rollback and Redis workflow/consumer checks; full
backend regression and the governed native typecheck. Correct Windows checkout
line endings only in the Lua capture fixture, retaining semantic mutation tests.
No frontend behavior changed, so reuse immutable accepted frontend assets.

`Dockerfile.team-maintenance` replaces only server/gateway binaries built from
this checkout. Runtime overlays preserve the accepted native UI, Agent,
entrypoint, core/data adapters and runtime security contract. OpenClaw updates
both installed/default Team plugin copies on the pinned 2026.10.2 base. Record
source revisions, native hash and immutable base/final image digests at release.

Before32543 maintenance verify no active selected Team execution, preserve actual
Deployment/Pod identities and workspaces, stop via official control APIs, replace
image plus advertised image ref/digest atomically, and restore original instance
identities/bindings. Keep excluded personal-contract pools untouched. Validate
real model roots on actual new images, public chat and member transcripts,
receipts and artifacts; local controlled native tests are not model acceptance.

Rollback uses the recorded M1 App/Runtime digests and preserved Deployment/env
configuration. No destructive data migration is introduced. Stop selected
executions before rollback and preserve pending publication journals/evidence;
never downgrade an enrolled instance to an ungoverned native binary. A blocked
unknown execution requires runtime diagnosis; finite recovery is not permission
to fabricate stop or completion evidence.


## Delivered release and real acceptance (2026-11-04)

Code sources: App `54799109a4fb2bed225c420a8177d079e4bccef5`, Runtime
`ba6478b77dfbb75177f88dfade5394d551834128`. Both are TEAM branch
`codex/team-personal-integration`; following documentation-only commits are not
changes to those image binaries. Native OpenCode stays 2.0.27 at upstream
`1f4d07a4a519185955ae8ab2ea3607805509e386`; governed executable SHA256
`8a377cda5c7bafbdec5640d1c89436fda111b719adbb3ba895081cce6a059cd4`.

| Published image | Registry digest |
| --- | --- |
| `10.130.14.23:5000/clawmanager-hxc-app:team-recovery-20261104-v3` | `sha256:62c037bb5ad3ff04981c9a581500a6e9b231f8190411e0bfd987be04b1fefd0b` |
| `10.130.14.23:5000/agentsruntime/opencode-lite:team-recovery-20261104-v2` | `sha256:e712800fc5cd62998a9b49239a3bfdc650665ee77980a1ae5b1313550177fbda` |
| `10.130.14.23:5000/agentsruntime/opencode:team-recovery-20261104-v2` | `sha256:e9b1f647de4405951c41f009c0c051ee80656069fd9e8ca15d0b713f868a7553` |
| `10.130.14.23:5000/agentsruntime/openclaw-lite:team-recovery-20261104-v1` | `sha256:251717d6d113216b0427a83fa3f150f5d5c6728b1d4598d4bc7eda425d021419` |

Official `/home/hxc/clawmanager-apply.sh` applied the App with
`TENANT_SUFFIX=-hxc-peer`, `NODE_PORT=32543`. Final authenticated `/version`
returns App v3/source54799109. Actual App/Gateway Pods are Ready/restart0 and
imageIDs match the registry digest. Dedicated OpenCode access-origin settings
are preserved by the existing accepted deployment wrapper.

All nine original OpenCode Lite instances 567/568/569/571/572/574/576/578/580
are running on actual pool368, Pod UID
`8461cb1d-ae4f-4ffb-a230-eb822a3d040b`, final v2 image, Ready/restart0.
Pro570/573 retain their identities and data; actual dedicated Pods run finalv2,
Ready/restart0. Native active-session count was zero before maintenance. Private
workspace/config archives and original Deployment specs are retained under
`/home/hxc/team-recovery-20261104`; no instance reset or deletion was used.

Official scoped OpenClaw rollout180 finished/postflight; both selected members
579/581 are running on pool366, finalv1, Ready/restart0. Other seven previously
updated Team Claw instances retain the accepted M1 image. Personal-contract
pools/instances and 32443/production were excluded. This is not a claim that all
OpenClaw pools received this follow-up image. The older full-pool Deployment-name
collision is a separate unresolved limitation: this release used the supported
scoped upgrade, rather than changing historical placement identities globally.
Sandbox M2 remains outside this maintenance scope and retains its recorded
host/storage readiness blockers.

| Real task | Verified evidence |
| --- | --- |
| Team170/root372 | Optional full body is visible in native completion tool and accepted delivery; plain-text arithmetic reaches Leader and explicit root completion. Two cards total, revision1. |
| Team170/root373 | Worker uses only status/summary, then emits final public body. Supplement source `msg_1a4a22429001FBYlubO2YOzLHx` is persisted without changing the original content hash; Leader's actual team_status read and final answer include all four lines plus RECOVERY-BODY-20261104. One Worker card, revision1; no re-dispatch. |
| Team174/root374 | OpenClaw Leader579, OpenCode developer580 and Claw reviewer581. Developer writes calc-19x37.md; reviewer actually reads its242bytes and independently verifies703; Leader receives both formal results, writes final-answer.md and explicitly closes root. Three cards, revision1. |

After finalAppv3 deployment, northbound owner-authenticated reads confirmed all
three roots succeeded/completed, members idle/currentTask cleared, and all five
selected Redis inbox consumer groups pending=0/lag=0. New final Markdown preview
contains703; historical root362 artifact still contains493; cross-owner access
returns404. Public chat events, native member conversations (reasoning omitted),
SQL receipts/work items, Redis queues and artifact preview agree. The browser UI
was not visually exercised in this release; model prose claiming its own checks
is not substituted for these independent observations.

Local validation: full backend regression, focused final Leader-state regressions,
real disposable MySQL strict-ID/atomic rollback/CAS/review invalidation tests,
real Redis workflow/consumer/dependency tests, 21 native bridge/client tests,
model-facing schema tests and governed native typecheck passed. Fault cases cover
failed-before-delegation, stopped versus unknown execution, observation failures,
recovery admission/attempt exhaustion, projection replay and stale/foreign input.
No artificial shared-environment failure was injected to claim real recovery.
No frontend behavior changed; no repeated frontend or large-fleet stress matrix.

### Additional findings resolved during acceptance

Native2.0.27's event bus does not retain historical events by default. The
supervisor now reads persisted materialized messages with the official cursor
anchored to the admitted input, bounded pagination and next-input boundary;
only final finish=stop assistant text is published. Actual historical failed
Leader371 now classifies as native failure in the read-only probe; the original
upstream governance transport cause cannot be recovered from historical logs.
Observation read failures hold the original execution, without launching another
model. No global event retention was enabled.

Historical accepted Leader cards could also clear the current root before it
created new cards. Root ownership now protects both busy status and availability;
terminal callbacks from an older root cannot clear a newer root. This does not
change task admission, member serialization or phase scheduling.

### Rollback and retained evidence

Use the recorded pre-change App/Runtime Deployment specs and pinned M1 digests
in the private release directory. Check current active execution first; stop via
supported APIs and confirm native idle before replacing a Runtime. Restore image
and advertised image identity together, preserve workspace/config, original
instance IDs, bindings/generation fencing and pending delivery journals. App-only
rollback does not require data rollback: this release adds no business schema or
format migration. Do not downgrade to an ungoverned stock OpenCode executable,
replay a completed task, fabricate stopped evidence or reset user data.

Ignored local evidence prefix `team-repair-...-20261104` includes authenticated
final API assertions, actual Deployment/RS/Pod inventory, SQL result hash and
supplement readback, consumer groups, native public transcripts, test/build/push
logs and private preservation hashes. These contain operational data and are
intentionally excluded from Git. Source changes were committed by stage; image
pushes and 32543 publication are complete. Git branch push was not requested.
