# Unified Instance Create Implementation Plan

> **For agentic workers:** Use executing-plans to implement this plan task-by-task; independent backend and browser regression work may use dispatching-parallel-agents. Steps use checkbox tracking.

**Goal:** Ship the approved v4 single-page creation workflow to server54.
**Architecture:** Reuse the existing creation service and lifecycle/import mechanisms, replace the three-step view with the approved form and live summary, extend the existing Lite batch template with shared metadata, and consolidate old entry points.
**Tech Stack:** React/TypeScript, existing CSS tokens and controls, Go/Gin, Playwright/Vite.

## Global Constraints

Build and execute application tests only on 10.130.81.54. Preserve unrelated dirty files. No new dependencies, no runtime image changes, no push/merge. All user authorization, risk policy, quota, native browser-origin and Lite/Pro limits remain effective.

### Task 1: Backend metadata contract
Files: `backend/internal/handlers/instance_handler.go`, `backend/internal/handlers/instance_handler_test.go`.
Interface: existing POST `/instances/batch/lite`; add optional `template.owner` and `template.description` and propagate to both request layers. Omitted owner remains nil. All existing env/skill/plan fields remain intact.
- [x] Add regression for metadata propagation, omitted owner, generated names and existing env/skill isolation.
- [x] Run it on54 and record failure before implementation.
- [x] Extend template and builder; run relevant handler tests on54.

### Task 2: Approved form and true submission
Files: `frontend/src/pages/instances/CreateInstancePage.tsx`, local creation helper/component files as needed, `frontend/src/pages/instances/InstanceListPage.tsx`, `frontend/src/lib/i18n.ts`, shared instance types only if necessary.
Interfaces: `instanceService.createInstance(payload)`, `batchCreateLiteInstances({name_prefix,count,start_index,template})`, `skillHubService.listAttachable()`, existing workspace import and system-image settings APIs.
- [x] Add browser regression asserting single-page batch switch, default four enabled types, no image selector, optional owner, conditional Pro controls, name/env/skill summary and shared batch payload. Verify failure against existing wizard on54.
- [x] Replace wizard with approved layout. Keep actual mode/image selection, resource validation, quota, supported imports and configuration plans. Parse multiline environment values without executing them. Empty owner becomes undefined.
- [x] Submit single or batch through existing API; display partial results and isolate archive retry from creation. Prevent duplicate submissions. Route old batch entry to `/instances/new?batch=1`.
- [x] Run typecheck, focused lint and form browser regression on54; verify locales and six widths, requests and no unintended writes.

### Task 3: Integration and release
Files: `DESIGN.md`, `.impeccable/design.json` if a contract changes, `docs/superpowers/tests/2026-11-12-unified-create-release.md`.
- [x] Update affected old browser tests to the new entry point while retaining their business assertions. Run workspace, create and relevant backend regression on54.
- [x] Review implementation against the approved spec and actual rendered captures.
- [x] Verify actual API persistence and navigation for disposable single/Pro and Lite batch inputs; clean up only task-created instances. Runtime-running wait failed because of existing recovery/image-pull blockers; see release evidence.
- [x] Commit only owned source/docs. Prepare exact source archive locally; run all frontend/Go/image builds on54 from current deployed app base. Deploy app only.
- [x] Verify rollout digest, served assets, actual creation screen and native origin Service ports/EndpointSlice. Preserve other workloads/images. Record evidence and deployment limitations.
- [ ] OpenCode/DSH native running-page validation: blocked by pre-existing Runtime needs_attention + unmatched idle-wake snapshots. No unsafe bypass applied; documented separately in docs/superpowers/tests/2026-11-12-unified-create-release.md.
