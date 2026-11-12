# Team portal analytics synchronization — 2026-11-05

## Source and preservation boundary

Implementation target is the TEAM checkout `codex/team-personal-integration`,
starting at `9e8e1b7f`. The PERSONAL checkout is read-only: selected committed
portal functionality was ported from `codex/openclaw-102-team-integration`
at `3a607442`, not merged wholesale and not taken from its dirty working tree.
No personal upgrade/restart controller, production configuration, historical SQL,
Runtime source or Worker tool contract was changed.

The Team tab, owner isolation, templates, six-member layout, internal scrolling,
member links opening new tabs, file search/edit/preview, ordinary instance Pro
access and the Lite-only new-Team admission policy remain in place. Dispatch,
serial execution, work-item identities/revisions, dependencies and formal Leader
completion retain their existing implementation.

## Adapted capabilities

- Background statistics use bounded, independently checkpointed source/history/
  recent/repair/projection/publish/ranking/report phases. Source failures do not
  cancel later phases; activity cancelled before persistence remains buffered.
- Active/quiet history selection, backoff, generation fences, cross-day repairs,
  isolated readers and diagnostics reuse the existing owner records table.
  Readers must be reaped before their guard is released; an uncertain exit is
  not treated as a completed read or permission to launch unlimited replacements.
- OpenClaw 7.1 JSONL remains supported; 10.2 SQLite, compressed events and cold
  archives have bounded read-only adapters. Unknown formats, corruption and
  unsafe network-filesystem WAL reads remain failures rather than measured zero.
- Team's OpenCode 2.0.27 differs from the personal source: it materializes native
  input/results in `session_v2`/`session_message`. A separate read adapter selects
  that format when present and retains the old format otherwise. It excludes
  child sessions, tagged Team automation, synthetic continuations, failed replies
  and tool-only output. Repeated successful replies count one human input.
- Calendar-aware visits, heatmap sizing, snapshot/batch timestamps and bounded
  read-failure notices are merged into the Team Dashboard composition. Visits
  are added to a request-owned copy, never to a shared cached snapshot.
- Owner session invalidation covers the separate Team client and pages. An
  upstream/provider 401, permission 403 or temporary 503 does not become an owner
  logout. Queued responses cannot restore invalidated owner content.
- The instance workspace can collapse without destroying its Runtime frame.
  Connection recovery only refreshes access/cookies, is bounded and origin/
  frame checked; it does not restart a Runtime or model execution.
- Administrator-only northbound portal controls now expose saved user statistics,
  bounded sync diagnostics, safe next-batch retry and versioned calendar preview/
  publish/rollback. Merely querying these records does not trigger native scans.
  Existing Team settings and scopes were retained during manual composition.

## Persistence and rollback

No schema migration was added or renamed. Personal `063` and Team `067` owner
records migrations have the same table contract; Team keeps its own migration
identity. No epoch, XP, reward, owner, task, instance, binding or workspace reset
is part of this change. The live preflight found historical reader guards idle.

New control/diagnostic/calendar records use the existing versioned records
envelope. Legacy readers select explicit record types and ignore those extra
types. A rollback must stop the new App and confirm its reader processes have
exited before starting the prior App; never delete those records, clear a held
guard on a timer or reset statistics as a rollback shortcut. Existing owner facts
and unique evidence keys remain authoritative. Use the retained prior deployment
spec and image, not a broad Runtime manifest.

## Necessary verification

- Before implementation, a cancelled activity-flush test reproduced discarded
  uncommitted activity; it passes after the fix.
- Targeted real MySQL checks cover preserved rewards/facts, old generation
  rejection, bounded repair/replay, source failure isolation, restart/checkpoints,
  safe diagnostics and global saved-statistics pagination.
- Linux image tests exercise the actual isolated reader and SQLite CLI,
  OpenClaw 10.2 retired JSONL/WAL handling and the new OpenCode v2 human/automation
  separation, including unchanged database bytes after reading.
- Frontend production build, owner failure classification, existing Team
  creation/ordinary-Pro checks and a local browser fixture of the compiled UI.
  The fixture covers four Dashboard tabs, Team-only owners, six member links,
  wide/short/mobile containment, workspace collapse, retained transient-failure
  snapshots and session invalidation. It is not a live model conversation or
  authenticated live-browser visual acceptance.
- Windows service tests depend on Unix paths/hardlink semantics; acceptance uses
  Linux for the full backend suite, with the sibling Runtime, repository assets
  and Node available. Initial fixture/dependency failures are retained in local
  logs and are not reported as passes.

Task-owned evidence is kept outside Git in the local ignored diagnostic directory.
No credentials, cookies, signed links or private transcripts are included here.

## Candidate and release record

Released: `10.130.14.23:5000/clawmanager-hxc-app:team-portal-sync-20261105-v2`.
Application source: `37262db126151638409f4fe08ea9559654613e63`. The initial v1
used `c2f9240ec55fe454883a7dec76548d0f6e008703`; v2 replaces only the server
binary for the live history-output defect below. The other three binaries and
frontend are unchanged from the accepted v1 build.

The image reuses the accepted prior image by immutable digest for unchanged
entrypoint, nginx and Hermes UI layers; all four Go binaries and the built portal
assets are replaced from this Team checkout. The standard repository Dockerfile
also contains the required SQLite dependency and builds the new source normally.
The exact candidate starts against a disposable MySQL, serves the expected
version and matching frontend, passes nginx validation and preserves `no-store`
on the SPA entry. The isolated-reader command exits before normal server setup.

Published to 32543 with registry digest
`sha256:2ed0e0c47c2a0fc5d1078bce937fb2076755c5bd49d86703a22664fbed8bfee7`.
App Pod UID `fe4bd854-33ca-41d1-94f9-d4ac0de66a4e`, northbound Gateway Pod UID
`b0be5001-62df-4b86-b46e-e06b38421816`: both Ready, restart count zero and the
same verified digest. Their environment and volumes match the saved previous
specs; 28 Runtime Pod identities/images/restart counts are unchanged.

Full Linux backend tests, frontend build, the compiled-page browser fixture and
13 targeted real-MySQL tests passed. Actual candidate startup, reader early-exit,
SQLite CLI, nginx and SPA no-store checks passed. The hosted frontend and server
hashes match the accepted candidate. No new migration was applied; 238 retained
identity/reward/login/migration/terminal records were compared unchanged.

Fresh owner-cookie login, Dashboard/visits, saved administrator statistics,
sync diagnostics and the built-in 2026 calendar work on 32543. Anonymous and
owner-cookie requests to administrator records are rejected. An independent
northbound challenge/JWE login and Team capability read also succeeded. The
apply script's final external challenge probe failed during rollout (v1 timed
out; v2 reported connection refused). These were not treated as successful
checks and the deployment was not reapplied. Subsequent independent authenticated
checks establish recovery of that access path.

The first new sync batch completed at `2026-11-05T03:05:13Z` (about 124 seconds).
Bootstrap, numeric usage, projection, publish, ranking and reports succeeded.
Recent/history each processed nine of eleven selected instances; reads for
582/584 reached their finite allowance. Historical repair yielded its allowance.
These partial stages did not block publication. We did not accept recurring
timeouts as the final result: the follow-up below fixes their source.

### Live acceptance follow-up: large SQLite snapshot output

The actual 582 database contained 477 hot events and no cold archives. The
indexed SELECT produced a 2,126,550-byte JSON object in about 0.25 seconds;
SQLite 3.48's `-json` output re-escaped that already-JSON cell with many small
writes and exhausted the 30-second reader allowance. Fingerprinting, SQL index
selection and decompression were not the bottleneck.

For this one SELECT only, v2 uses raw, header-free list output and directly
decodes its single JSON object. Other row queries retain JSON mode. Read-only
and query-only access, existing budgets/caps, strict decoding, identity fences,
WAL refusal and cold-archive verification remain in force. No timeout was
increased and no Runtime history was changed.

A real multi-megabyte SQLite regression failed at its five-second deadline
before the fix and passed in 0.28–0.31 seconds afterwards. Adjacent hot/cold,
retired-JSONL, committed-WAL and OpenCode v2 tests passed. The separate private
CLI-recovery-volume test was not run and is not claimed as evidence.

On 32543, actual `read` and `inspect` for both 582/584 now finish in 0.31–0.45
seconds with no error or partial result. Zero human questions from these Team
automation sessions is correct; it is not converted into invented statistics.
The normal 03:30 UTC batch `208016` completed successfully in 1.17 seconds:
history 11/11 in 487 ms, recent 3/3 in 229 ms, with repair, projection, publish,
ranking and reports all successful. It moved all four recent/history schedules
back to `ready`, with their next check at 03:40 UTC. No direct SQL reset or forced
model execution was used. Historical backfill remains bounded by the normal scheduler;
successful current reads do not claim that all historical repair is exhausted.

The existing Team175 delivery report remains 3,166 bytes with SHA256
`1f942bcccfa6ff3c266e5e7ea622fc8c0caa91c202f2e698056561d3b4d28de5`.
Existing completed roots 372/373/374/378/380/381 remain completed. No additional
model task or live-browser visual test was performed for this statistics change.
Only local compiled-page fixtures used browser automation.

Private previous deployment specs and retained-state evidence are under
`/home/hxc/team-portal-sync-20261105` (restricted permissions); the immediate v1
specs are separately retained in `/home/hxc/team-portal-sync-20261105-v2`.
The previous App
image is `10.130.14.23:5000/clawmanager-hxc-app:team-lite-policy-20261104-v1`
at digest `sha256:e458fa0732260efeef04c8cc6b32c62ac35a059d2c9aa4cab1736aab2a6777d8`.
No production environment, 32443 tenant, personal checkout or Runtime image was
changed. Git commits are local; the requested registry push is complete.
