# CONTROLUI_INTERNAL_LOCALIZATION_GAPS_ROOT_CAUSE_AND_PATCH_APPROVAL_PACKET

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology

## Verdict

```text
CONTROLUI_INTERNAL_LOCALIZATION_GAPS_ROOT_CAUSE_AND_PATCH_APPROVAL_PACKET_DONE
```

## Approval Phrase For Next Gate

```text
APPROVE_CONTROLUI_INTERNAL_LOCALIZATION_GAPS_ROOT_CAUSE_AND_PATCH_GATE
```

## Current State

The auth, scope, and runtime connection blockers have moved past the earlier failure point:

```text
internal UI reached
device signature invalid absent
device identity required absent
missing_scope absent
missing operator.read absent
```

The current acceptance failure is:

```text
internal localization incomplete
failure_surface=GTClaw/OpenClaw internal interface
not_failure_surface=entrance connection form
```

## User Observed English Residuals

The next gate must investigate and resolve these observed internal UI residuals:

```text
Message Assistant (Enter to send)
Default (Auto)
Default (off)
Form
Raw
No changes
Raw mode disabled (snapshot cannot safely round-trip raw text).
Open
Save
Apply
Update
Search settings...
Settings
Communication
Schema unavailable.
```

## Required Root Cause Work

The next gate must locate each English residual in the GTClaw/OpenClaw control-ui surface and classify the source:

```text
official OpenClaw control-ui bundle i18n entry missing
fallback locale miss
hardcoded English string
schema/config editor independent copy
model/session selector copy
chat input placeholder copy
```

For each residual, the evidence must record:

```text
residual text
source file path
source region shape
classification from the list above
chosen patch target
reason if the residual is intentionally retained as product name, code literal, or user data
```

## Patch Boundary For Next Gate

Preferred patch targets:

```text
official OpenClaw localization artifact
runtime control-ui artifact
zh-CN bundle
i18n source artifact if available
```

If a residual is hardcoded in a compiled bundle, the next gate must first record the entry id, file path, and snippet shape, then apply the smallest patch that localizes only the observed display text.

Hash and artifact rules:

```text
zh-CN artifact hash change record required
new zh-CN hash required in evidence
changed file list required
protected technical literals must remain preserved
```

Forbidden patch areas for the next gate:

```text
no backend auth/scope modification
no runtime auth predicate modification
no runtime scope propagation modification
no deployments modification
no missing_scope bypass
no operator.admin grant
no insecure auth
no global bypass
no direct browser device-less allow
no trustedProxy JSON marker trust
```

## Required Verification For Next Gate

The next gate must verify:

```text
residual English string rg scan passed or each remaining hit explained
remaining hit explanations limited to product name, code literal, or user data
zh-CN bundle hash readback recorded
artifact hash manifest updated for changed files
runtime image/readback verifier run if repackaged
browser/manual E2E deferred until a separate approval
```

The next gate must not treat the entrance connection form as final localization acceptance. Internal localization must be judged only inside the GTClaw/OpenClaw internal interface after the internal UI is reached.

## Current Gate Non-actions

This approval packet gate did not perform implementation or live validation:

```text
no patch
no build/tag/push/pull image
no deploy
no rollout
no kubectl mutation
no k3d mutation
no Helm mutation
no instance mutation
no browser E2E
no manual E2E
no DevTools
no Playwright
no cleanup old instance
no cleanup old pod
no cleanup old PVC
no cleanup old asset
no cleanup old session
no cleanup old evidence
no cleanup old image
no frontend modification
no deployments modification
no docs modification
no longterm write-back
no AgentTeam modification
no UnifiedFramework modification
no backend auth/scope modification
no runtime auth predicate modification
no runtime scope propagation modification
no database mutation
no direct SQL
no Mem0 write
no passes:true
no Close
no git stage/commit/push
```
