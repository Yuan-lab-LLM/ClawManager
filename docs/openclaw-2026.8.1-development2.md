# OpenClaw 2026.8.1 Development 2

## Scope

Development 2 adds the ClawManager control plane required to test a data-safe
Lite upgrade from OpenClaw 2026.7.1-2 to 2026.8.1. Pro uses the same fixed
2026.8.1 image, but existing Pro instances are not automatically migrated.
Shell is outside this upgrade. No production environment or user data was
changed during development.

## Safety contract

- A target must be an immutable OpenClaw `@sha256` reference.
- Preflight is persisted and must be explicitly confirmed exactly once.
- The instance ID, user ID, fixed Lite HOME, `.openclaw` path, Team/member IDs,
  Redis configuration, assignment revisions and shared workspace stay unchanged.
- A running gateway is stopped before snapshot. 8.1 agents provide positive
  stop confirmation and a writer lease; legacy 7.1 agents use their synchronous
  stop endpoint plus a second full-workspace manifest check during snapshot.
- Snapshots live outside the active workspace, include complete file hashes and
  SQLite header checks, and are verified before image rollout.
- Restore extracts into a sibling staging directory, preserves failed 8.1 state
  as a sibling, and atomically activates the 7.1 snapshot. No live workspace is
  deleted or overwritten in place.
- Runtime capability, protocol, OpenClaw version, Redis Team plugin version,
  session store and image digest must all match before gateways are released.
- A missing audit write, capability, binding, snapshot, digest, Team invariant
  or timeout fails closed and triggers automatic rollback when configured.

## Team behavior

ClawManager remains the collaboration authority. OpenClaw 8.1 native Team,
A2A, Workboard, Cloud Worker and Swarm behavior is not adopted.

- Team dispatch checks the Team's own Redis maintenance key before mutating task
  data. The key has no TTL and is cleared only by verified postflight/rollback.
- Preflight blocks active tasks, assignments, pending outbox events, busy
  OpenClaw/Hermes members, invalid Leader topology and mixed Team state changes.
- After maintenance is set, durable Team state is checked again to close the
  preflight race.
- Only OpenClaw members receive upgrade items. Hermes Lite members and their
  sessions/configuration are unchanged.
- OpenClaw workers stop and restart serially; each new binding is verified
  before the next member is released. The OpenClaw Leader is last.
- Any member failure keeps the Team closed and rolls every changed OpenClaw
  member back as one consistency group.
- Custom Role Profiles continue to compile for both runtimes, while the
  OpenClaw configuration plan is hard-gated to `runtime_type=openclaw`.

## Redis Team 0.3.0 compatibility layer

- Uses OpenClaw 8.1 native Group inbound dispatch; channel capabilities declare
  both direct and group.
- Preserves plugin/channel/group identifiers and the existing Team session route.
- Removes Direct-DM SDK emulation, unconditional command authorization, JSONL
  scanning and private SQLite schema access.
- Emits bounded assistant/tool evidence through 8.1 hooks.
- Creates mutation tools from authenticated Team/session/assignment context;
  non-Team, cross-Team and stale-revision contexts cannot obtain those tools.
- Keeps each member consumer serial and drains safely under maintenance.
- Hermes Docker variants use the same canonical Hermes Redis Team plugin source.

## Other adaptations

- 8.1 Automation RPC reconciles only ClawManager declarations and preserves user
  automations; the old `cron/jobs.json` writer is not used for 8.1.
- Import is staged and path-validated, then atomically activated while preserving
  the previous workspace. Live SQLite trees are never removed in place.
- Runtime registration and the administrator pool API expose redacted version,
  digest and capability facts.
- The settings page implements persisted preflight, blockers/warnings, plan
  fingerprint and explicit confirmation with automatic rollback.
- Migrations `058` and `059` add only upgrade metadata/audit structures and
  update known old/default image values; custom image settings are not replaced.

## Development 3 boundary

Development 2 does not authorize a real user upgrade. Development 3 must push
the final common Lite/Pro image, record the registry digest, deploy to the test
environment, run a copied 7.1 fixture through success and forced rollback, and
execute OpenClaw-only, mixed Hermes, custom Team, channel, browser, automation,
transfer and same-host performance matrices. Production remains out of scope
until a later explicit write authorization.
