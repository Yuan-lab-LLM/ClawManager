# CM Fluent 2 UI Migration Implementation Plan

> Execute in this session using subagent-driven-development, with independent file ownership and final integration review.

**Goal:** Apply the approved root DESIGN.md throughout CM and release the verified UI to 10.130.81.54.

**Architecture:** Replace the existing shared theme at its source, migrate legacy component styles, then refine Dashboard and embedded workspaces. Keep existing accessible native dialog/popover behavior, React Router, APIs, authorization, polling and lifecycle semantics. No business schema changes.

**Tech stack:** React 19, TypeScript, Tailwind/CSS, existing Lucide and runtime brand assets; semantic Fluent 2 Web tokens. Reuse current component contracts instead of replacing the application framework.

## Global constraints

- DESIGN.md is normative. Body 14/20, captions 12/16, title 16/22, Web blue #0f6cbd, control 32px, panel radius 8px, dialog radius 12px, header 48px, navigation 40px, default table row 44px.
- Preserve top navigation, sidebar edge collapse, one-line table cells, separate search/filter/batch actions, existing keyboard/focus semantics and automatic synchronization.
- Dashboard retains three independent top statistics areas, three simultaneous capacity gauges, Agent status ring with totals, aligned recent/pinned panels, real runtime logos at 32px and green activity heatmap.
- No local application runtime, container build or build binaries. Build, browser checks and deployment run on 10.130.81.54.
- Preserve existing .gitignore changes and unrelated untracked inputs. Continue the existing uiux feature branch; do not merge or push.

## Task 1: Theme, shell and shared controls

Files: frontend/src/styles/design-tokens.css, frontend/src/index.css, frontend/src/styles/management-ui.css, frontend/tailwind.config.js, shared shell/control components as required.

- [x] Map approved colors, typography, spacing, shapes, shadows and motion to --cm-* tokens; map legacy aliases to these tokens.
- [x] Remove legacy decorative tech gradients/looping decoration at its definitions. Implement complete primary/secondary, field, selected navigation, focus and disabled states.
- [x] Replace shell hardcoded geometry with tokens; unify dialogs/popovers, preserve native top-layer and focus behavior.
- [x] Route common Tailwind brand/neutral utilities through the shared palette; adapt ordinary form/modal surfaces without recoloring actual runtime logos or code editors.
- [x] Run remote type checks, relevant shared-control regressions and visual evidence at all supported widths.

## Task 2: Dashboard

Files: frontend/src/pages/dashboard/workspace/{workspace.css,Chart.tsx,Operations.tsx}, UserDashboard.tsx only for presentation if needed.

- [x] Replace private old08 tokens with shared Fluent tokens; normalize typography and shapes, keep vibrant chart palette and real 32px logos.
- [x] Remove accumulated conflicting legacy CSS rules where practical; preserve chart geometry/data semantics, recents/pin alignment and desktop single-screen goal.
- [x] Keep keyboard, reduced-motion and narrow-screen behavior; run existing model/browser regression scripts remotely.

## Task 3: Security and administrator legacy surfaces

Files: frontend/src/styles/{secplane-theme.css,protection-tokens.css}, frontend/src/pages/admin/security/* presentation helpers, narrowly scoped security page class changes.

- [x] Map security theme variables to common Fluent tokens, remove warm/red brand theme, 24–28px decorative rounds and gradients.
- [x] Normalize security panels, buttons, tables, forms, status badges, category navigation and overlays while preserving security semantics and actual threat colors.
- [x] Check overview, categories and detailed settings/error/empty states in the remote browser.

## Task 4: Teams, workbench and owner surfaces

Files: frontend/src/components/teams/*.css, frontend/src/components/workbench/workbench.css, frontend/src/styles/owner-*.css, ordinary auth/portal presentation as needed. Does not own Dashboard workspace.css or global shell/theme files.

- [x] Migrate legacy independent colors/rounds/shadows/fonts to shared Fluent tokens; preserve chat content, terminal/editor, layout containers and existing responsive thresholds.
- [x] Align workbench command controls, team cards/dialogs and owner components with the shared system; keep real brand icons and business text intact.
- [x] Check relevant existing interaction regressions remotely, especially team workspace and narrow layouts.

## Task 5: Integration, review and remote release

- [x] Transfer source-only snapshot to a dedicated directory on54, reusing existing remote dependencies without local runtime.
- [x] Run TypeScript, targeted lint and existing shared-control/workspace/team/runtime regressions. Add behavior regression only if this migration changes logic.
- [x] Browser-check actual app routes in English/Chinese at 1440/1280/768/390 and long locale overflow; check empty/error/loading, focus return, reduced motion and Dashboard framing. Capture source-bound evidence.
- [x] Independent code and visual review; fix material findings and recheck affected paths.
- [x] Build final image on54, record source and image digest, deploy only clawmanager-system/clawmanager-app, verify health and real browser asset version. Do not redeploy runtimes or the separate LTT environment.
- [x] Record completed evidence and limitations; update design metadata only where final implementation decisions require it.

## Delivery

Completed on54. Source `685030618de1c9b71c51b0fb375ae3a2d4241beb`, image `sha256:eb8102549c2a9a75d8aab7f7771ac1d94d24a07b521bc40fa4588904e17272e6`, generation19 ready1/1. See [validation](../tests/2026-11-12-cm-fluent2-ui-validation.md) and [release manifest](../tests/2026-11-12-cm-fluent2-release.json) for actual checks and limitations.
