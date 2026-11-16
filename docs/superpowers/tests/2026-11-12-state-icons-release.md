# Approved Agent state icons release

Source: `3f6fb8e9d5a4b21d311a3adb707cab3d1e3daf56`.
Environment: `https://10.130.81.54:30443/instances`.
Version: `v2026.11.12-state-icons`.
Image: `clawmanager.local/clawmanager@sha256:5355df0fd56603888faf0b607de759946f594d0fb8c65709f3f2c67c5beb6498`.
Deployment: `clawmanager-system/clawmanager-app`, generation26, ready1/1.

## Approved behavior

The user approved animated icons for all seven Agent types with one specific error correction: the original fault icon must never shake, and its red background circle must be removed. Only the red triangle/exclamation scales.

Reusable AgentStateIcon consumes the existing OpenClaw, Hermes, OpenCode, DeepSeek Harness, WorkBuddy, Codex and Claude Code assets. Running retains native colors with subtle breathing; stopped powers down once then remains gray; automatic sleep maps to resting pose/Zz and breathing, with OpenClaw closed eyes. Error art stays opaque/static and has no halo; its red warning badge alone pulses. Unknown transitional states preserve the original icon, and legacy types use the existing Monitor fallback. Accessible names describe the true state.

Instance list rows now have neutral backgrounds, with state communicated by the icon. Soft blue current-selection highlighting remains. Reduced motion, offscreen pausing and document-hidden pausing are supported. Component listeners/observers clean up on unmount.

The release also delivers the previously verified continuation work: hide/restore beside Agent fullscreen, safe15-second account/query/page-scoped list snapshots, scroll/search/page retention through mandatory native document resets, and short content/iframe transitions. Hidden desktop polling cannot overwrite the active mobile snapshot. Live selection still verifies authorization; browser-origin guards remain unchanged. Native-frame stylesheet loading and recovery retain their existing boundary checks.

## Verification on54

Local work was source preparation/static validation/transfer; application execution, browser tests, binary/image builds and deployment all ran on54.

- TypeScript, focused component lint and eight navigation snapshot tests passed.
- Committed component browser regression passed all7×4 variants, native asset loading, static error artwork with badge-only scaling, no halo, stopped settling, sleep alias/eyes, unknown/fallback/accessibility, offscreen/document pausing and reduced motion.
- Real-route browser regression passed status mapping (including manual stop and idle-timeout sleep), neutral fault row, static fault artwork, warning motion, safe cache reload while inventory requests were blocked, hidden desktop/mobile snapshot race, account isolation, search/pages/errors, metadata races, pointer/keyboard resize, hide/restore, skills/focus, Lite/Pro, five widths and five locales. No real lifecycle mutation.
- Administrator and full Dashboard browser regressions passed. Standalone frame recovery regression passed blank/401 renewals, backoff preserving unsent drafts, recovery budget exhaustion/retry and zero page errors.
- Review and rereview found no remaining important issues after fixing the mobile snapshot overwrite race and normalizing null/empty stopped reasons. Actual read-only56-row inventory and56 detail payloads passed the snapshot sanitizer.
- Exact source inventory2064 files, four rebuilt binaries, serving image and all163 frontend artifact files matched. Both self-contained icon previews are included in the image. Stable runtime helpers/config and other workload images were preserved.
- Actual deployed list verifies real running/stopped icons, preserved keyboard focus/selection, fullscreen-adjacent hide control, reduced motion, snapshots with inventory request blocking, resize/skills/search/switch/resume and dashboard behavior. Rendered output reviewed. A separate browser-only response fixture exercised the deployed built bundle's fault state: native art animation/transform none, opacity1, no halo and warning-only pulse. It did not create a runtime fault or write lifecycle state.
- Actual OpenCode50 and DSH60 native composers loaded in fresh Chrome with normal TLS verification, one main-host certificate confirmation, child probes204 and no child confirmation. Origin Service retains128 NodePorts31600–31727, target8444, matching app selector and ready endpoint.
- Final application health200 at10:12:18UTC:56 total,54 running,2 manually stopped. Instance65 remains running. No manually stopped instances were restarted.

Remote evidence: `/root/cm-state-icons-20261112/release-3f6fb8e9d5a4/` and `/root/cm-state-icons-20261112/native/`. Credentials, private screenshots and snapshots are not committed. Owned test servers/browsers close in cleanup.
