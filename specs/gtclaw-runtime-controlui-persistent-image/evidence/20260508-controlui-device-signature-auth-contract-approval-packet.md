# Control UI device signature auth contract approval packet

Date: 2026-05-08
Role: Worker
Topology: serial
Task type: CONTROLUI_DEVICE_SIGNATURE_AUTH_CONTRACT_APPROVAL_PACKET

## Verdict

CONTROLUI_DEVICE_SIGNATURE_AUTH_CONTRACT_APPROVAL_PACKET_DONE

This is an approval packet only. It does not implement code, does not build or pull an image, does not run browser E2E, and does not change runtime, instance, database, cluster, browser storage, or registry state.

## Dependency gates

- CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_RERUN_DONE
- GPT_PRO_DECISION_REQUIRE_RUNTIME_CONTRACT_CHANGE

## Requested approval

Proposed next implementation gate: CONTROLUI_DEVICE_SIGNATURE_AUTH_CONTRACT_IMPLEMENTATION_GATE

Requested approval token: APPROVE_CONTROLUI_DEVICE_SIGNATURE_AUTH_CONTRACT_IMPLEMENTATION_GATE

The requested implementation gate should be allowed to review and patch the backend bridge and the OpenClaw runtime auth contract only within an explicitly approved file scope. Runtime source intake must be confirmed before any runtime-side patch is attempted; if the runtime gateway auth validator source is still unavailable, the next gate must BLOCK instead of guessing.

## Current observed blocker

Runtime target:

- Instance id/name: 16 / oc2gi-loc-221427
- Browser route shape observed: /api/v1/instances/16/control-ui/chat?session=main
- Stale route check: observed route is instance 16 and not stale instance 10, 11, 15, or another prior runtime.
- Visible localized shell: GTClaw 控制台
- Locale evidence: zh-CN visible shell and translated manual connection text were observed.
- Blocking error: device signature invalid

Observed localization evidence confirms that ClawManager routes to the real instance 16 Control UI and that the official OpenClaw control-ui shell is localized. The remaining failure is an auth-contract blocker on the first WebSocket connection, not a routing or static asset problem.

## Why the localization gate is not enough

The localization gates validated static control-ui assets, route correctness, and visible zh-CN presentation. They did not and should not modify the backend WebSocket bridge, trustedProxy behavior, runtime gateway validator, device signature schema, or runtime auth contract.

The known failure is caused by mismatched signature inputs across the browser, backend bridge, and runtime validator:

- Browser-side Control UI creates params.device and device.signature from its original auth input and nonce.
- The backend WebSocket bridge replaces first-connect params.auth with a server-owned OpenClaw gateway token before forwarding upstream.
- The forwarded payload therefore mixes backend-owned params.auth with browser-generated params.device and device.signature that were signed against different input.
- Runtime validation sees inconsistent auth and device signature material and returns device signature invalid.

Changing translations, page labels, route handling, or the localized artifact cannot resolve this protocol mismatch. The fix line must align the backend bridge and runtime auth contract.

## Proposed contract direction for the implementation gate

The next gate should review the backend bridge/runtime boundary and implement only an approved contract shape. The expected direction is:

- Keep the server-owned OpenClaw gateway token backend-side.
- Do not expose the gateway token to browser-visible JavaScript, URLs, logs, screenshots, or evidence.
- Do not forward browser-generated params.device or device.signature when the backend has replaced params.auth.
- Prefer a runtime-confirmed trustedProxy flow where the backend strips browser auth/device signature material from the first connect payload and uses a non-sensitive trustedProxy marker only if runtime source confirms and validates that marker safely.
- Runtime must not accept trustedProxy marker spoofing from direct clients. The marker must be valid only across the authenticated backend-to-runtime boundary.
- Preserve direct OpenClaw clients that intentionally use the existing token, password, pairing, or device-signature flow.

The implementation gate must not silently weaken runtime auth. If the runtime source cannot support a trustedProxy contract with clear tests, it must return BLOCKED with the missing source or contract requirement.

## Likely file scope for backend bridge/runtime contract review

Backend bridge mandatory review and likely edit scope:

- backend/internal/services/instance_proxy_service.go
- backend/internal/services/instance_proxy_service_test.go

Backend adjacent review scope, edit only if directly required:

- backend/internal/handlers/instance_handler.go
- backend/internal/services/instance_access_service.go
- backend/internal/services/instance_service.go
- backend/internal/services/instance_runtime.go

Runtime mandatory source review scope, exact paths pending source intake:

- OpenClaw gateway WebSocket first-connect schema and parser.
- OpenClaw gateway auth validator.
- Device signature validation code.
- Pairing/device-token retry handling.
- Trusted proxy marker validation, if such a contract exists or must be added.
- Runtime protocol tests for direct clients and backend-proxied clients.

Conditional later scope, only after explicit approval:

- Runtime startup or image assembly configuration only if a config knob is required for the approved runtime contract.
- Control-ui artifact only if the runtime contract requires a browser-visible protocol change. No localization or UI text patch is requested by this packet.

This approval packet does not authorize editing any of the files listed above. It only identifies the likely scope for the next implementation gate.

## Acceptance checks for the proposed implementation gate

The next gate should require all of the following before any downstream browser rerun:

- Unit or protocol tests prove backend first-connect forwarding no longer mixes rewritten params.auth with stale params.device or stale device.signature.
- Runtime tests prove trustedProxy behavior, if introduced, is accepted only from the authenticated backend-to-runtime boundary.
- Runtime tests prove direct clients with spoofed trustedProxy marker still fail.
- Direct token/password/device-signature flows remain compatible unless an explicit breaking contract is approved.
- Evidence confirms no gateway token, password, key, cookie, bearer value, authorization value, or full access URL is recorded.
- Any image build, fresh instance deployment, or browser rerun must be separate later gates with separate approval.

## Risks

- Security regression if trustedProxy can be spoofed by a direct client.
- Credential exposure if a server-owned gateway token is placed into browser-visible JSON, logs, screenshots, URLs, or evidence.
- Regression for direct OpenClaw clients using password, pairing, device-token retry, or signed device flows.
- Runtime source mismatch if the patched code is not the source used in the image.
- Incomplete contract alignment could replace device signature invalid with a different connection failure such as 1006, manual connection form fallback, or origin rejection.
- Evidence ambiguity if a later browser rerun uses stale route state instead of instance 16.

## Rollback boundaries

- Backend bridge changes must be isolated and revertible without changing database rows, instance records, sessions, assets, Services, PVCs, registry tags, or existing evidence.
- Runtime changes must use a new explicitly approved source/image path in later gates; existing image tags must not be mutated.
- If runtime source intake is incomplete, stop at BLOCKED before implementation.
- If tests show trustedProxy can be spoofed or direct clients regress unexpectedly, stop at BLOCKED before image build or deployment.
- If a later browser E2E still shows device signature invalid, record it as an auth-contract blocker and do not work around it by exposing secrets or asking the user to type credentials.

## Forbidden actions confirmation

The following actions were not executed in this gate:

- no backend source edits
- no frontend source edits
- no runtime/control-ui artifact edits
- no image assembly edits
- no image build, tag, push, pull, or container run
- no browser E2E
- no DevTools
- no Playwright
- no browser storage/cache/cookie cleanup
- no kubectl
- no k3d
- no Helm
- no instance mutation
- no database mutation
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push
- no token/password/key/cookie/bearer/auth header/access URL plaintext recorded

## Sensitive material handling

This packet records only route shape, instance identity, observed non-sensitive visible text, error class, proposed file scope, risks, and approval boundary. It intentionally omits secret values, browser storage contents, request headers, cookies, token-bearing URLs, and access URLs.
