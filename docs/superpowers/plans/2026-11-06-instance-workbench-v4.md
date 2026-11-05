# Instance Workbench V4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the approved V4 instance workspace with manually created browser/terminal tabs and real multi-repository Git operations.

**Architecture:** ClawManager authenticates the owner/admin and resolves the current runtime binding for every operation. Runtime work executes as the assigned business identity, with the same workspace, sanitized environment and private network as the development agent. Git operations run in a fixed helper inside the business sandbox; terminals are runtime-owned PTY sessions; manually entered preview URLs receive an isolated preview origin and a generation-bound route to the business loopback network.

**Tech Stack:** Existing Go, Gin, React, TypeScript, xterm, Kubernetes runtime agents and sandbox/network helpers. Use existing dependencies and standard library functionality before introducing packages.

## Approved design and global constraints

- Visual reference: `/Users/mowen/code/src/github.com/iamlovingit/cm/artifacts/debug-services-preview-20261106/v4/review.html` and its nine screenshots. The user approved implementation on 2026-11-06.
- Keep existing sidebar, Agent UI and workspace file operations. Add `Git 项目` and `预览与终端` to the right workspace, including the Pro layout where workspace support exists.
- Browser and terminal tabs are created only by user actions. No automatic URL detection, port discovery, model reporting, opening, or tab switching when a command runs.
- Support multiple independent browser and terminal tabs. Switching panels/tabs must not unmount a live session or discard its output. Hiding a tab must not stop its process; explicitly ending a session does.
- Browser input is an HTTP/HTTPS URL of a current-instance loopback service, including localhost and IPv6 loopback. Reject credentials, external hosts, encoded authority tricks, platform management endpoints and stale generations.
- Browser access must support root-relative assets, redirects and WebSockets. User applications must have an origin separate from the CM UI/API and from other preview capabilities. Supply a development default and documented production ingress/domain settings.
- One instance can register multiple Git repositories inside its persistent workspace. Support cloning remote repositories, importing existing repositories, selecting repositories, status/diff, stage/unstage, branches, branch creation/switching, history, local commits, pull and explicit push.
- Never execute user Git hooks, credential helpers or shell commands as the management identity. Git/network commands execute inside the business sandbox with the current instance's credentials and policy.
- Git paths must stay in the selected instance workspace. Reject escaping paths, symlinked repository roots/metadata that escape the root, malformed Git refs, option injection and unsafe remote URL protocols. Commit messages and filenames are argv/stdin data, not shell code.
- Serialize registry mutations. Persist the project registry in the instance workspace, using bounded JSON, atomic replacement and path validation on every use. Repository selection does not silently change an existing Agent conversation or terminal cwd.
- Protect uncommitted work when switching branches or pulling. Do not add force-push, hard reset, discard, automatic stash, rebase, PR automation or account integration.
- Reuse existing instance Git credentials. Errors must explain missing Git identity, credentials, conflicts, unavailable executables and timeouts without leaking secrets.
- Idle sleep may stop services/sessions; terminal activity is not a keepalive exemption. Migration must fence new operations, cancel/join active helper work, stop old sessions and reject stale requests. Preserve code, `.git` and registry data; do not implement full dependency/VM migration.
- Reuse and strengthen the existing native container-reset protocol: no stale auxiliary writer may authorize source-slot reuse. Do not erase management stop ledgers or PVC data to clean the root filesystem.
- Owner/admin endpoints only for this first version. Existing shared-file access must not grant shell, Git mutation or application preview authority.
- Changes live in isolated branches at `.worktrees/workspace-v4/{ClawManager,AgentsRuntime}`. Preserve all pre-existing untracked documents in the original checkouts.
- Local Go verification uses `env -u GOROOT go ...` because the user's shell exports an incompatible GOROOT. Do not alter global machine settings.

## Contract

Frontend owner endpoints are under `/api/v1/instances/:id/workbench`:

- `GET /capabilities` returns running/generation/workspace root and supported operations; unsupported old runtimes must show an honest upgrade-required state.
- `POST /git` accepts `GitRequest` and returns `GitResponse` inside the existing `{data: ...}` envelope.
- `GET|POST /terminals`, `GET|DELETE /terminals/:session`, `GET /terminals/:session/output?cursor=N`, `POST /terminals/:session/input`, `POST /terminals/:session/resize` provide persistent PTY sessions, bounded output with an explicit cursor and generation identity.
- `POST /previews` accepts `{url}` and returns `{id,url,generation}` where `url` is an isolated preview origin. Main API authorization never reaches the application. Preview lifecycle validates the current binding, expiry and assigned gateway on each new connection.
- `GET /processes`, `POST /processes/:process/stop`, `POST /processes/:process/restart` expose only provably owned processes/sessions; no fabricated Agent execution records. Process identity is pinned, not authorized by a numeric PID alone.

Runtime management requests use a control-authenticated `/v1/workbench` route with `instance_id`, `user_id`, `generation`, `gateway_id`, `operation` and operation data. The pool selects exactly one assigned worker and rejects mismatches/quarantined assignments. HTTP/WS preview streaming may use a separate scoped stream route forwarded over the existing private worker Unix transport; it must not be buffered through the JSON RPC transport.

### Git helper wire types

```go
type Request struct {
    Operation string   `json:"operation"`
    Path      string   `json:"path,omitempty"` // workspace-relative project root; empty means root
    Name      string   `json:"name,omitempty"`
    URL       string   `json:"url,omitempty"`
    Branch    string   `json:"branch,omitempty"`
    Paths     []string `json:"paths,omitempty"` // repository-relative files
    Staged    bool     `json:"staged,omitempty"`
    Message   string   `json:"message,omitempty"`
    Limit     int      `json:"limit,omitempty"`
}
```

Operations are `projects`, `import`, `clone`, `status`, `diff`, `stage`, `unstage`, `branches`, `checkout`, `create-branch`, `history`, `commit`, `pull`, `push`. The runtime engine API is `workspacegit.New(root string) (*Service, error)` and `(*Service).Execute(ctx context.Context, req Request) (Response, error)`. All persisted/displayed project paths are workspace-relative. Response fields are `projects`, `project`, `status`, `branches`, `history`, `diff`, `truncated`, `message`, as appropriate. Typed errors expose a safe `Code` and `Message`.

## Task 1: Git engine inside the business sandbox

**Files:** create `AgentsRuntime/clawmanager-agent/workspacegit/{types,service,registry,git,paths}.go` and focused `_test.go` files. Do not edit the main runtime launcher in this task.

**Produces:** the API above, bounded outputs, and a persistent multi-repository registry.

- [x] Write a real local-repository test: initialize a temporary bare remote and a working copy, import it, modify two files, stage one, commit it, assert the other remains modified, push and verify the remote ref.
- [x] Run `env -u GOROOT go test ./workspacegit` and observe the missing implementation failure.
- [x] Implement real Git operations using argument arrays, no shell interpolation; include clean environment controls, noninteractive remote access, timeouts, bounded stderr and secret redaction.
- [x] Add behavioral tests for multiple registered repositories, clone/import persistence, untracked and renamed filenames, staged/unstaged diffs, detached/unborn HEAD, dirty checkout/pull refusal, branch creation, missing identity, unsafe remote protocols and escaping/symlink paths.
- [x] Run `env -u GOROOT go test -race ./workspacegit`; commit this isolated task after it passes and write its report.

## Task 2: Runtime workbench execution and terminal sessions

**Files:** add `clawmanager-agent/workbench/`, `gateway/workbench.go`, `slots/workbench.go`, `slots/terminal_{linux,other}.go`; extend `app/run.go`, `slots/worker.go`, `slots/pool.go`, `slots/command.go`, `gateway/types.go` only at their extension seams.

**Consumes:** Task 1 Git engine. **Produces:** instance/generation-scoped workbench RPC, actual PTY sessions, cursor-buffered I/O, owned process/session lifecycle.

- [x] First test: a mismatched generation/instance cannot execute Git or create a terminal; an active assigned instance can list projects through an injected business executor.
- [x] Add the fixed `managed-workbench-git` helper command and invoke it through the existing business command runner with JSON stdin. Derive scope from the current gateway, never the client.
- [x] Implement Linux PTY allocation/resizing and persistent session ownership, bounded replay buffers, explicit end, limits and reconnect. Keep sessions mounted in the UI and retain the actual runtime process when a view is hidden.
- [x] Test two sessions with independent cwd/output, input and resize, disconnect/reconnect without killing the process, buffer cursor expiry, output bounds, stale session rejection and per-instance isolation.
- [x] Join new command/session scopes to existing gateway teardown without making open sessions a permanent idle-stop blocker. Test stop/migration cancellation and no source-slot reuse before cleanup proof.
- [x] Run targeted runtime tests and Linux builds for each supported image binary, then commit.

## Task 3: Manual web preview transport

**Files:** extend `netrelay/{relay,command}.go` and runtime network-session code; add scoped preview dial/stream handlers in slots.

**Produces:** bounded, cancellation-aware access from an authenticated assigned worker to an explicitly requested business-loopback port, including raw WebSocket streams.

- [x] First test: a manual request can reach a loopback HTTP server inside the intended network scope; external addresses, malformed ports and stale assignments are rejected.
- [x] Use a private inherited Unix listener for the credential-free network helper. Restrict CONNECT targets to business loopback and tie every connection to the current network-session lifetime.
- [x] Preserve existing fixed native ingress and broker routes; do not add public listeners, automatic discovery or per-request namespace entry from untrusted argv.
- [x] Test HTML and binary bodies, cancellation, simultaneous ports, WebSocket echo, IPv4/IPv6 loopback and stop cleanup; run existing netrelay/slots tests and commit.

## Task 4: ClawManager owner APIs and isolated preview origins

**Files:** create `backend/internal/services/instance_workbench*.go`, `backend/internal/handlers/instance_workbench*.go`; extend `backend/cmd/server/main.go`, config and deployment examples at their normal registration points.

**Consumes:** runtime RPC/stream routes. **Produces:** the owner endpoint contract and generation-bound isolated preview host routing.

- [x] First handler tests: unauthenticated and unrelated owners cannot use workbench; read-only external shares do not acquire these capabilities; stopped/stale bindings fail closed.
- [x] Resolve live bindings and trusted agent endpoints through existing repositories/security transports. Bound RPC bodies, errors, duration and cancellation; redact credentials from logs.
- [x] Add expiring random preview capabilities on independent origins. Route arbitrary application paths and WebSockets through the scoped runtime transport; preserve redirects/cookies only in the application origin. Strip CM authorization/control headers.
- [x] Development origin defaults to a token subdomain of localhost on the backend port. Document and configure the production wildcard domain/origin template and ingress rather than pretending path rewriting can support every dev server.
- [x] Test current binding validation on navigation and WS connect, origin isolation, capability expiry, denied hosts/schemes, URL/query forwarding, root-relative resources, redirects, raw bodies and HMR/WS behavior.
- [x] Run scoped backend Go tests, the broader touched-package tests and commit.

## Task 5: Production React workspace UI

**Files:** create `frontend/src/{components/workbench,services/instanceWorkbenchService.ts,types/workbench.ts}`; integrate in `InstanceDetailPage.tsx`; reuse `WorkspaceFileManager`, existing xterm dependencies, i18n and CSS tokens.

- [x] First behavioral test: running an Agent command or updating instance state never creates a preview tab; only explicit browser/terminal actions do.
- [x] Implement File/Git/Preview tabs, persistent hidden views, the single new-tab menu, browser address validation/manual navigation, independent terminal I/O and sizing, explicit end/reconnect and honest stopped/migrated/error states.
- [x] Implement registered project selection, clone/import forms, branch selection/creation, status, staged/unstaged file changes, diff, history, commit, pull and push using real APIs. Keep local commit and remote push separate.
- [x] Match V4 layout and labels, keep selected tabs visible on narrow screens, and expose keyboard/focus/error/loading states. No sample task data, fake Git responses or fixed user/workspace paths in product code.
- [x] Run meaningful frontend behavior checks, TypeScript/Vite production build and scoped lint. Render desktop/mobile against controlled API fixtures and an actual local terminal/HTTP/Git integration harness where feasible; inspect screenshots, then commit.

## Task 6: End-to-end verification and completion audit

- [x] Verify cloning a local test remote, modifying/staging/committing/pushing, importing a second repository and switching between them against the real engine.
- [x] Verify a real PTY and real HTTP/WebSocket echo server through runtime/CM transport; check two terminal sessions and multiple browser origins do not cross streams.
- [x] Exercise idle stop, migration/stale generation and native container reset with workbench activity; assert preserved repository state and terminated source execution.
- [x] Run existing relevant backend/runtime regressions, frontend build/behavior checks and Linux image builds. Document environment-dependent checks and complete all executable checks before claiming completion.
- [x] Obtain task and final code review, fix material findings, inspect final diffs and reconcile every approved requirement with authoritative test/render evidence.
- [x] Integrate the reviewed feature branches into the user's working checkouts without overwriting their untracked documents; provide changed behavior, validation and any required preview-domain/runtime-image setup.


## Progress

- Initial state: ClawManager `cd7b74a`, AgentsRuntime `4510b71`, both on `preview` before isolated feature branches.
- Frontend baseline production build: passed (existing chunk-size/Browserslist warnings).
- Go baseline initially hit the exported GOROOT/tool-version mismatch; re-running with `env -u GOROOT` against the installed Go toolchain.
- Runtime and backend production changes have not started at plan creation.

## Verification audit (2026-11-06)

| Requirement | Evidence |
| --- | --- |
| Manual browser/terminal creation; multiple persistent tabs | Reducer5cases, realReact+PTYbrowserharness; noAgent/URLdiscoveryevents; ended-output/mobileactive-tab regressions |
| Multi-repository Git operations and workspace persistence | 26realGitengine tests under race; realbrowserclone/stage-one/commit-one/keep-other-dirty/explicitpush/import-second/history; atomicworkspace registry |
| Same business execution identity/files/network | Real default/native sandbox PTY+Git acceptance; actual UID/cwd/env/resize and detachedwriter cleanup; production nonroot native network bootstrap and preview scope acceptance |
| Scoped HTTP/WS preview with isolated origins | SharedhashedSQLcapabilities; realMySQLreplica/expiry/limits; CM→pool→Unixworker→Unixhelper→app wiregate; binary/path/query/appauth/cookies/controlheaders/WS/genclose |
| Idle/migration cleanup and stale rejection | Manager/worker/pool identity tests; previewdoesnotblockidle; combinedin-flightGit+terminal cancellation/join ordering; native stop/reset authority regressions; actual namespace teardown/reassignment |
| Existing regressions and builds | LinuxCMservices/handlers/workbench broad relevant race tests; sixRuntimepackages+fourracescopes; allRuntimeLinuxentrybinarybuilds; CM Linuxserverbuild; frontendlint/tsc/Vite/OpenCodeentry |
| Validation scope | LocaldisposableLinuxcontainers/actualnamespaces/realprocesses and controlleddata; no productionclusterdeployment/migration performed; productioningress/DNS/TLS setup documented |

Task reviews are approved for exact committed artifacts. Final cross-repository review approved CM `95d9858` and Runtime `01df177`; both branches were fast-forwarded into the original `preview` checkouts. Post-merge verification passed: frontend5state cases/OpenCodeentry/productionbuild/expandedrealGit+PTYbrowser, all6Runtimepackages under race, and CM→Runtime HTTP/WS wire gate (0.05s). All43pre-existing untracked files were verified byte-for-byte unchanged. Build warning noise was reproduced on the untouched CM base `cd7b74a` (Vite8.0.11 baseline3.73s), including CSS plugin timing; no dependency or build-plugin changes were made.


Completion: implemented and locally integrated on 2026-11-06. Production rollout remains a separate operator action: update compatible Runtime images, apply database migration101 through normal startup, and configure wildcard preview DNS/TLS/ingress. Full dependency-environment migration remains outside the agreed first-version scope.
