# Admin Overview Implementation Plan

> **For agentic workers:** Execute the backend task with subagent-driven-development and integrate the UI and release in the current UIUX2 workspace. Do not commit or modify unrelated work.

**Goal:** Implement the approved single-screen admin overview and deploy only the CM application to test cluster 10.130.81.54.

**Architecture:** A small admin-only aggregation endpoint supplies period-bounded SQL totals and hourly usage, separate from the existing bounded cost report. Extend the existing cluster resource reader with optional kubelet actuals and retain request/capacity semantics. React consumes these reads independently with automatic refresh and renders the approved two-column design using native SVG.

**Tech Stack:** Existing Go/Gin/upper SQL, client-go, React/TypeScript/React Query, native SVG/CSS.

## Global Constraints

- Match docs/uiux/2026-11-16-admin-overview-preview.html: first row two groups of four aligned to shared chart columns; Token/conversation primary curve; CPU/memory/disk smooth real/request curves; resource gauges plus node-details entry; one card with paired Runtime/instance pies; model Token pie. No Session or quality/governance module.
- Use actual API data only. Unknown remains null/dash; zero is only an observed zero. Keep independent pie denominators, all model shares, no slot diagrams, no production sample numbers.
- Four Runtime types plus unknown types are included. Instance counts and Pod counts are independent, not inferred from one another.
- Token/model totals/history share the same Shanghai today window. Conversation means confirmed human turn, not model call or replayed chat transcript. Expose source gaps honestly. Never sum currencies into a fictitious USD total.
- Existing admin authentication, routes, native lifecycle boundaries and unrelated dirty UIUX2 changes remain intact. Keep previous running app/source snapshot as release base.
- Do not build production frontend, binaries or images locally. Build on 10.130.81.54; local read-only/source tests/type checks/browser checks are allowed. CM deployment only; do not change AgentRuntime Deployments or images.
- Maintain aligned readable Fluent 2 cards, automatic synchronization, keyboard chart/dialog interactions and focus return. Desktop 1280x720 and 1440x900 are single-screen; smaller layouts may scroll without clipping.

### Task 1: Backend data and real resource measurements
- [x] Implement GET /admin/dashboard under admin authorization, using bounded-time SQL aggregates, not paginated rows. Provide nullable source-aware usage/cost/activity/quality/conversation/hourly/model/type fields.
- [x] Extend ResourceSummary with optional used and observed_at. Read kubelet stats/summary through the already authorized nodes/proxy API with request deadline. CPU usageNanoCores, memory workingSetBytes, fs usedBytes match node capacity; never fall back to requests as usage.
- [x] Fix aggregate disk requested to remain ephemeral storage requests consistent with per-node disk and physical fs capacity, not persistent instance allocation.
- [x] Add tests before implementation for authorization, window bounds, missing-vs-zero, currency separation, no query pagination truncation and metrics conversion/failure. Run Go tests on54.

### Task 2: React overview and regression checks
- [x] Replace frontend/src/pages/admin/AdminDashboard.tsx with approved layout, adding focused overview components, types/model helpers/text/CSS files rather than copying the standalone HTML script.
- [x] Use React Query for independent overview and resource reads, abort signals, 30s polling and focus/reconnect refresh. Preserve good data if one source fails; expose retry. No refresh button or update timestamp.
- [x] Use smoothPath/chartSegments from the existing Workspace chart model and native SVG; all missing values stay gaps. Real CPU/memory/disk observations accumulate during the page visit; never invent history before collection.
- [x] Node list remains in resource-card dialog with search/filter/pagination and correct Escape/focus behavior. Keep routing to Runtime management.
- [x] Add meaningful data-derivation tests, then browser checks of all important states/locales/viewports and real layout/keyboard interactions. Local TypeScript checking must use --noEmit.

### Task 3: Review, build and test deployment
- [x] Independently review changed data paths/UI/auth/empty behavior; fix important findings.
- [x] Hash and transfer verified source archive to54, build full frontend/backend and overlay only expected artifacts on the current CM image. Use immutable digest and record before/after state.
- [x] Deploy only clawmanager-app with optimistic image guard; wait readiness, verify version/index/API and unchanged Runtime images/configs. Verify128 browser-origin NodePorts target8444 and ready EndpointSlice; verify OpenCode/DSH actual route where available.
- [x] Test deployed overview against live real values and desktop/mobile layouts. Generate docs/reports/2026-11-16-admin-overview.html with exact coverage, data limits, image digest and evidence.

### Final visual adjustment and release evidence
- User requested larger, better-proportioned charts at2560×1271. Gauges and pies now scale with card width/height; compact and mobile layouts preserved.
- Source browser checks:5locales ×6viewports (2560×1271,1920×720,1440×900,1280×720,768×900,390×900), allPASS. Model5/5 and no-emit TypeScript PASS. Independent CSS review approved.
- Remote V3 build/deploy: source10c827e52c6f59d5c5c7d4a327cb2ee4b591a58cd7f77e13c9b87cde18433845; image024ce2d96c7e36b3b0eaa933ada47047abb2407bc1e60c9726664b204fc44a24. Final serving pod clawmanager-app-59fcf8865d-2z2rx ready1/1.
- Deployed browser11/11 PASS; sixviewports +language/API/auth/nativeOpenCode/DSH. Final source/index/binary/digest/NodePort and unchanged otherDeployment checks PASS. V2 built but never deployed; V3 supersedes first-release mobile defect.
- HTML test report: docs/reports/2026-11-16-admin-overview.html. Production builds occurred only54. No Runtime upgrades or instance lifecycle mutations.

### Conversation count follow-up
User found zero conversations despite real use. Replaced owner-bound XP source with attributed recorded gateway-turn grouping; see2026-11-16-admin-conversation-fix.md. Final release975f7fef4e95 / image18c6467... / v2026.11.16-admin-dialogue supersedesV3. Live12/12PASS, daily8/hourly8andnonzerocurve, unchangedRuntimeconfiguration. HTMLreportupdatedwithlatestproof.
