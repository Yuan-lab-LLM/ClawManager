# Team Agent Profile Acceptance Criteria

This checklist covers the phase 1/2 implementation for agency-agents inspired
Team role profiles in ClawManager.

## Scope

- The implementation uses compact ClawManager runtime profiles, not the full
  agency-agents Markdown files.
- No database migration is required.
- No Team API schema change is required.
- Existing Team `description`, `team.json`, and member
  `environment_overrides` fields remain the compatibility layer.

## Functional Acceptance

1. The Team creation page exposes the existing built-in templates:
   - Standard two-member Team
   - Delivery three-member Team
   - Product Discovery four-member Team
   - Quality Gate four-member Team
   - Full-stack Delivery five-member Team
   - API Integration five-member Team
   - Research Publication six-member Team
   - Software Engineering Team
2. Each built-in template member has a compact agency profile key.
3. Importing a built-in template preserves:
   - member id
   - display name
   - role
   - runtime type
   - resource preset
   - description
   - profile key
4. Creating a Team sends member-specific `environment_overrides`.
5. Members with a profile key receive all three aliases:
   - `CLAWMANAGER_RUNTIME_AGENTS_JSON`
   - `CLAWMANAGER_OPENCLAW_AGENTS_JSON`
   - `CLAWMANAGER_HERMES_AGENTS_JSON`
6. The generated agents JSON is valid and contains:
   - `schemaVersion: 1`
   - one `agent` item
   - profile key
   - source agency file path
   - member id
   - role
   - runtime type
   - system prompt
   - collaboration rules
   - output contract
7. Global environment variables still apply to every member and are merged with
   member profile env values.
8. Existing `description` values still appear in generated Team roster
   configuration so older runtimes keep a readable role summary.

## Compatibility Acceptance

1. A runtime that ignores `*_AGENTS_JSON` still starts normally.
2. OpenClaw members can inspect `CLAWMANAGER_OPENCLAW_AGENTS_JSON`.
3. Hermes members can inspect `CLAWMANAGER_HERMES_AGENTS_JSON` and
   `CLAWMANAGER_RUNTIME_AGENTS_JSON`.
4. Team Redis keys, inbox routing, event stream projection, and `team.json`
   schema are unchanged.

## Verification Commands

Frontend:

```bash
cd frontend
npm run build
```

Backend Team unit tests:

```bash
cd backend
go test ./internal/services/ -run TestTeam -v
```

Runtime smoke checks after creating a Team:

```bash
env | grep AGENTS_JSON
cat /etc/clawmanager/team/team.json
```

## Manual Runtime Acceptance

1. Create a two-member OpenClaw Team.
2. Verify Leader and Worker receive different profile payloads.
3. Dispatch a simple task to the Leader.
4. Confirm the Leader can read roster roles and coordinate via Team events.
5. Repeat with a Hermes Lite Worker when an enabled Hermes Lite gateway image is available; the Leader remains OpenClaw Lite.
6. Confirm the Hermes Worker applies the same profile aliases and Team roster context rather than treating the payload as an unsupported loader gap.
