# Control UI Runtime Persistence Fix Implementation Gate

Date: 2026-05-06
Worker: ControlUIRuntimePersistenceFixImplementationWorker
Verdict: CONTROL_UI_RUNTIME_PERSISTENCE_FIX_IMPLEMENTATION_BLOCKED

## Blocker

The gate could not find a writable, already approved runtime control-ui artifact or build-context file that contains the `persisted gatewayUrl` behavior.

The approved implementation target is the runtime control-ui bundle/source artifact. The only located bundle artifact is `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js`, and the current gate explicitly makes `/tmp/gtclaw-runtime-patch/**` read-only reference material. The repository feature build context currently contains runtime startup files only, not the target control-ui JS bundle.

Because no approved writable target exists, this gate did not modify a control-ui bundle, did not create a replacement source artifact, and did not patch `/tmp/gtclaw-runtime-patch/**`.

## Dependency Summary

Approved prerequisite:

- `CONTROL_UI_RUNTIME_PERSISTENCE_FIX_APPROVAL_PACKET_DONE`
- `CONTROL_UI_STALE_ROUTE_ROOT_CAUSE_DONE`
- root cause: a control-ui bundle persisted gatewayUrl can override the current GTManager-mediated page-derived WebSocket URL.

Current observed failure carried from the approval packet:

- page route: `/api/v1/instances/11/control-ui/chat?session=main`
- stale form route shape: `/api/v1/instances/10/control-ui`
- required mediated route shape: `/api/v1/instances/<id>/control-ui`

## Target Search

Repository feature artifact search:

- searched `specs/gtclaw-runtime-controlui-persistent-image/**`
- no `.js`, `.html`, `.css`, or sourcemap control-ui artifact exists under the repository feature directory
- current repository build context is `runtime-startup-artifact/20260505-bind-lan-auth`
- that build context contains Dockerfile, startup config, service run wrapper, and helper scripts only

Read-only reference artifact:

| Path | Mode | Size | sha256 |
| --- | ---: | ---: | --- |
| `/tmp/gtclaw-runtime-patch/index.html` | 644 | 3398 | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| `/tmp/gtclaw-runtime-patch/assets/i18n-B06L7jQN.js` | 644 | 42617 | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js` | 644 | 707959 | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` |
| `/tmp/gtclaw-runtime-patch/assets/zh-CN-B26mMdbY.js` | 644 | 23255 | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` |

Static reference check:

- `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js` contains `gatewayUrl`, `localStorage`, `sessionStorage`, and page-route code references.
- It is the likely minified target for the persisted gatewayUrl precedence fix.
- It remains read-only under this gate.

## Implementation Status

Target file path:

- none modified
- blocked candidate: `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js`
- missing approved repository target: runtime control-ui bundle JS file inside a repo-owned artifact or build context

before sha256:

- read-only candidate before sha256: `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299`

after sha256:

- not applicable because no target file was modified

Exact patch method:

- no patch applied
- stopped at target authorization check before implementation

Semantic summary of intended fix not applied:

- On a GTManager-mediated route, the current page-derived WebSocket URL should be authoritative.
- A persisted gatewayUrl from localStorage or sessionStorage must not override the active URL for `/api/v1/instances/<id>/control-ui`.
- Standalone gateway behavior should remain compatible outside GTManager-mediated routes.

Why no storage cleanup/manual URL/token is required:

- The approved design fixes precedence in the control-ui bundle rather than clearing localStorage, clearing sessionStorage, or asking the user to hand-edit a WebSocket URL.
- The backend bridge already performs server-side token injection, so the browser user must not enter an OpenClaw gateway token.
- This gate did not expose or request any gateway token, password, key, cookie, credential, or access URL value.

## Tests And Checks Run

Read-only checks:

- `git status --short`
- `find specs/gtclaw-runtime-controlui-persistent-image -maxdepth 5 -type f`
- `find specs/gtclaw-runtime-controlui-persistent-image -type f`
- `find /tmp/gtclaw-runtime-patch -maxdepth 5 -type f`
- `rg` over approval and root-cause evidence for persistence requirements
- `rg` over the feature directory for `gatewayUrl`, `localStorage`, `sessionStorage`, and control-ui markers
- `rg` over `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js` for bundle behavior markers
- `shasum -a 256` and `ls -l` over `/tmp/gtclaw-runtime-patch/**`

JS validation:

- no modified JS file exists, so `node --check` was not applicable
- no minified bundle patch marker was expected because implementation was blocked before edit

## Recommended Next Gate

Recommended next gate:

`Runtime Control UI Source Artifact Recovery Approval Packet`

The next approval should explicitly name one writable repository-owned artifact or build context for the control-ui bundle, for example a recovered copy of the four `/tmp/gtclaw-runtime-patch/**` files under `specs/gtclaw-runtime-controlui-persistent-image/**`, and should authorize modifying the target bundle JS plus any required build-context copy metadata. Without that approval, this worker cannot legally turn the read-only `/tmp` reference into a persistent image artifact.

## Explicit Negatives

This gate performed:

- no backend modification
- no frontend GTManager source modification
- no deployments modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec/plan/tasks modification
- no existing evidence modification
- no `/tmp/gtclaw-runtime-patch/**` modification
- no localStorage cleanup
- no sessionStorage cleanup
- no IndexedDB cleanup
- no cache cleanup
- no cookies cleanup
- no manual URL requirement
- no gateway token requirement
- no storage cleanup
- no backend WS bridge semantic change
- no backend Origin rewrite
- no build/tag/push/pull
- no backend deploy/restart
- no fresh instance creation/deletion/modification
- no K8S/runtime/database/registry mutation
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no manual pod patch
- no manual Service patch
- no `kubectl cp`
- no secret, token, cookie, credential, or access URL plaintext output
- no Mem0 write
- no longterm write-back
- no passes:true
- no Close
- no git stage/commit/push
