# Feature Specification: GTManager M1 Branding and Chinese Localization

**Feature Branch**: `not-created`  
**Created**: 2026-04-29  
**Status**: Draft - plan exists; user review still required before tasks/business implementation  
**Input**: PRD "龙虾盒子集群化管控 PRD（R1）" page `688a7d88-227f-479b-9207-091ae544d210`, mem0 project memory, repository onboarding scan, and user-provided M1 direction.

## Scope Summary

M1 changes ClawManager's product-facing experience to GTManager and makes Chinese the default user experience. The implementation must keep the existing OpenClaw runtime management path intact and must not introduce GTClaw or a custom OpenClaw runtime into this manager path.

This spec authorizes only design review and planning. `plan.md` now exists, but no frontend, backend, deployment, product-facing documentation, or business implementation may begin until the user reviews the plan and approves generation of `tasks.md`.

## User Scenarios & Testing

### User Story 1 - Chinese GTManager First Impression (Priority: P1)

As an enterprise operator opening the product, I see GTManager as the product name and Chinese as the default language on the unauthenticated and authenticated manager surfaces.

**Why this priority**: This is the core outward-facing product shift from ClawManager to GTManager.

**Independent Test**: Open the login page and primary manager routes in a clean browser profile with no stored locale. The browser title, login copy, layout brand text, and main navigation appear in Chinese and use GTManager for the manager product name.

**Acceptance Scenarios**:

1. **Given** a clean browser profile, **When** the user opens `/login`, **Then** the page defaults to Chinese and displays GTManager as the product name.
2. **Given** a logged-in admin, **When** the admin opens the admin dashboard, users, instances, AI Gateway, security center, and settings routes, **Then** the manager shell and visible route titles use Chinese by default and the product brand is GTManager.
3. **Given** a logged-in regular user, **When** the user opens the workspace, instances, OpenClaw resource management, and settings routes, **Then** the manager shell and visible route titles use Chinese by default and the product brand is GTManager.

---

### User Story 2 - Preserve Runtime Semantics (Priority: P1)

As an operator, I can still understand that GTManager manages default OpenClaw runtime instances, without the UI implying that OpenClaw has been replaced by GTClaw or by a custom runtime.

**Why this priority**: The PRD decision keeps the default OpenClaw runtime. Branding must not create a false product/runtime contract.

**Independent Test**: Review instance creation, instance details, runtime status, OpenClaw resource management, and runtime command surfaces. Manager branding says GTManager, while runtime references remain OpenClaw where the managed runtime is being named.

**Acceptance Scenarios**:

1. **Given** an admin creates or reviews an instance, **When** the UI refers to the managed desktop runtime, **Then** the runtime name remains OpenClaw where technically accurate.
2. **Given** runtime status shows agent and OpenClaw state, **When** the user views the status, **Then** status semantics and API behavior are unchanged from the current OpenClaw path.
3. **Given** documentation or UI text names internal cluster services, API paths, database tables, or Kubernetes resources, **When** those names are technical identifiers, **Then** they may remain ClawManager/OpenClaw unless a later approved migration explicitly changes them.

---

### User Story 3 - Brand Asset Replacement Path (Priority: P2)

As the product owner, I can provide final GTManager visual assets and have the manager use them consistently for logo, favicon, and launch/loading surfaces without the implementation guessing asset paths.

**Why this priority**: User-designed assets exist but their paths have not been provided; M1 must not assume or invent them.

**Independent Test**: After the user provides approved asset paths, open the manager in a browser and confirm the header logo, favicon, and launch/loading surfaces use the approved GTManager assets.

**Acceptance Scenarios**:

1. **Given** the user has not provided final asset paths, **When** implementation planning starts, **Then** the asset replacement task remains explicitly blocked and no guessed logo path is used.
2. **Given** approved assets are provided, **When** the frontend is built and opened, **Then** GTManager logo and favicon assets render without broken image requests.
3. **Given** the browser tab and main shell are visible, **When** the user checks accessible labels or alt text, **Then** asset labels use GTManager rather than ClawManager.

---

### User Story 4 - OpenClaw Internal Localization Boundary (Priority: P3)

As an operator, I need a defined path for localizing OpenClaw's internal pages later without mixing that work into GTManager manager branding or changing the runtime identity.

**Why this priority**: The PRD calls for full default OpenClaw localization, including pages inside the image, but this must remain a resource/image localization path rather than a manager rename.

**Independent Test**: Review the resulting M1 plan and tasks. OpenClaw image-internal localization is represented as a separately gated resource package or image customization track with its own evidence requirements.

**Acceptance Scenarios**:

1. **Given** M1 implementation planning is reviewed, **When** OpenClaw internal page localization is listed, **Then** it is not implemented as a GTClaw runtime fork.
2. **Given** image-internal localization requires additional assets or image-build instructions, **When** those inputs are missing, **Then** the item remains gated rather than silently implemented.
3. **Given** later evidence is collected, **When** OpenClaw internal page localization is claimed, **Then** evidence includes an actual browser view inside the runtime image, not only manager UI screenshots.

## Edge Cases

- A user has `clawmanager_locale=en` or another locale stored in local storage: M1 must define whether explicit prior preference is preserved or whether the first M1 release resets default language. Recommended behavior for implementation planning is to preserve explicit user choice while defaulting new/empty profiles to Chinese.
- Some frontend text is still hardcoded outside `frontend/src/lib/i18n.ts`: M1 must inventory and route product-facing text through i18n before claiming Chinese default coverage.
- Existing locale keys include Japanese, Korean, and German: M1 must not degrade the application by deleting those locales unless the user separately approves a supported-locale reduction.
- Backend error messages may surface directly in the UI: M1 must identify user-visible backend errors and decide in plan whether to map them through frontend translations or leave them technical.
- Asset paths are unavailable: final asset replacement cannot be accepted, but spec/plan/tasks can still define the blocked handoff point.
- The repository contains both product-facing documentation and technical runbooks: M1 must distinguish outward-facing GTManager wording from technical identifiers and historical evidence that should remain ClawManager for traceability.

## Requirements

### Functional Requirements

- **FR-001**: The manager product name shown to end users MUST be GTManager on current product-facing frontend surfaces.
- **FR-002**: The default locale for new or empty browser profiles MUST be Chinese.
- **FR-003**: The language switcher MUST continue to let users choose another supported locale when translations exist.
- **FR-004**: Product-facing ClawManager strings in frontend metadata, login/register pages, admin layout, user layout, primary route titles, and visible navigation MUST be reviewed and replaced or translated according to the GTManager wording policy.
- **FR-005**: Runtime references MUST continue to use OpenClaw when referring to the default managed runtime, runtime status, OpenClaw resource management, OpenClaw archives, OpenClaw bootstrap, or OpenClaw image behavior.
- **FR-006**: M1 MUST NOT rename backend module names, Go packages, API prefixes, Kubernetes namespaces, Service names, container image names, database tables, audit table names, or already-captured historical evidence unless a later approved plan explicitly includes such a migration.
- **FR-007**: Static assets MUST support replacement of logo, favicon, and launch/loading visuals with user-approved GTManager assets.
- **FR-008**: Implementation MUST NOT assume final logo or asset paths. Final asset replacement remains blocked until the user provides exact source paths or files.
- **FR-009**: Visual theme changes MUST use the existing frontend styling system and CSS override approach; M1 must not introduce a second design system.
- **FR-010**: M1 MUST identify hardcoded product-facing English strings and move them into the existing i18n resource mechanism before declaring default Chinese coverage for those surfaces.
- **FR-011**: Documentation updates, when approved in the plan, MUST distinguish product-facing GTManager copy from technical ClawManager identifiers that remain unchanged.
- **FR-012**: E2E evidence MUST be captured before any M1 artifact is marked `passes: true`, completed, accepted, or passed.

### Key Entities

- **Manager Product Brand**: The outward-facing name, title, labels, and asset identity of the management product. For M1 this is GTManager.
- **Runtime Product Name**: The managed desktop runtime identity. For M1 this remains OpenClaw.
- **Locale Resource Package**: The frontend i18n resources and related static labels used to render Chinese by default while preserving supported locale switching.
- **Brand Asset Package**: User-provided logo, favicon, and launch/loading image files plus any required metadata such as alt text and target filenames.
- **E2E Evidence Packet**: The final verification record containing environment, steps, screenshots or logs, result, and reviewer feedback.

## Success Criteria

### Measurable Outcomes

- **SC-001**: In a clean browser profile, `/login` renders Chinese copy and GTManager product name without manual language switching.
- **SC-002**: After admin login, the admin shell and core admin routes render Chinese navigation and GTManager manager branding.
- **SC-003**: After user login, the user shell and core user routes render Chinese navigation and GTManager manager branding.
- **SC-004**: A repository text scan after implementation shows no unintended product-facing `ClawManager` strings in frontend source or browser metadata, excluding approved technical identifiers and historical docs.
- **SC-005**: Existing health and API entry points remain unchanged: `/healthz`, `/api/v1`, `https://localhost:30443`, and the runtime agent control path over `http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001`.
- **SC-006**: E2E verification is recorded with either Playwright evidence or explicit human test feedback. Without this evidence, M1 remains "E2E pending".

## Out of Scope

- Renaming repository, Go module, package names, backend routes, API prefix, Kubernetes namespace, Services, deployments, database schema, audit tables, or container images.
- Introducing GTClaw, a custom OpenClaw runtime, or a runtime fork as part of the GTManager manager branding path.
- Changing the validated K3S runtime agent control path from HTTP `clawmanager-gateway:9001`.
- Marking any feature `passes: true` before E2E evidence exists.
- Committing or modifying `.codex/auth.json`, `.codex/config.toml`, secrets, or provider credentials.

## E2E Acceptance Gate

M1 can only move to Close after one of these evidence paths exists:

1. **Automated Playwright E2E** against the deployed manager at `https://localhost:30443`, covering clean-profile login page, admin login, admin dashboard/navigation, user-facing shell if a regular user is available, browser title/favicon requests, logout, and `/healthz`.
2. **Manual E2E feedback** from the user or assigned tester, recording environment, exact routes tested, screenshots or observations, language/branding results, and any remaining issues.

Build, lint, type checking, unit tests, API checks, and YAML validation are prerequisite evidence only. They do not satisfy final M1 acceptance by themselves.

## Proposed Review Questions

1. Should M1 preserve explicit non-Chinese locale preferences already stored in browser local storage, or should first M1 launch force Chinese once?
2. Should outward-facing docs such as `README.md` move to GTManager in M1, or should M1 limit docs updates to a short migration note and keep historical ClawManager docs intact for traceability?
3. Should OpenClaw image-internal localization be part of M1 implementation, or should M1 only define the resource/image localization path and leave actual image modification for a separate approved feature?
