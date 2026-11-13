# Input focus and instance selection release

Source: `7f5444d3d23bc30a9b54f2e82c59bd302a7f4f2a`.
Deployed to `https://10.130.81.54:30443/instances` as `v2026.11.12-input-focus`.
Image: `clawmanager.local/clawmanager@sha256:ce19e250cc0da237107d254da0f5136b8737847c714612e043053ff6ebc8a219`.
Deployment generation 25, serving 1/1 ready replica.

## Changes

- Replaced hard-coded black focus outlines in the shared theme, management controls, legacy protection/security surfaces and workbench with `--cm-focus`, which aliases brand blue and uses system Highlight in forced colors. Keyboard focus remains visible. Composite searches show one outer indicator, suppressing duplicate inner outlines/shadows.
- Strengthened running/error/stopped/sleeping/transition row tints. Current selection has a full 2px blue border, rendered independently of focus shadows so it remains visible while keyboard-focused. Status stays in accessible names and hover text.
- Moved the instance list hide/restore control from the Agent toolbar to the middle of the list divider. Resizing remains available above/below the button; double-click reset targets the divider itself. Collapse/restore transfers focus to the replacement button and retains layout bounds and preferences.
- Updated DESIGN.md and its sidecar to match the approved focus, selection and edge-control behavior.

## Verification

All application tests, bundling, binary and image builds ran on 54. Local work was source preparation, static inspection, transfer and rendered-image review.

- Browser regression reproduced the original nested black input frame before the fix, then passed after it. Real instance-route checks cover search/inline form focus, selected border while focused, edge hide/restore focus, pointer/keyboard resize, iframe/draft preservation, Lite/Pro, five viewport widths, five locales, search/pagination/errors/account isolation and management dialogs.
- Administrator browser regression passed for shared expandable search focus, filtering, keyboard/popover focus, policy requests and containment. Full Dashboard browser regression passed. TypeScript and targeted component lint passed; no repository-wide lint claim.
- Focused review caught selected-border shadow interference; selection now uses a separate border. Rereview found no remaining important issue.
- Actual deployed checks passed for single-indicator search focus, divider midpoint control, selected border under keyboard focus, forced-colors focus, hide/restore, resizing, skills, switching, resumed entry and Dashboard interactions. Rendered screenshots were reviewed.
- Exact archived source inventory (2059 files), four rebuilt binaries, serving image and all 161 frontend files matched. Runtime helper/config files and other deployment images were preserved.
- Actual OpenCode 50 and DSH 60 loaded with native composers in fresh Chrome using normal TLS verification. Only the main-host certificate was confirmed once; child probes returned 204 and needed no additional certificate confirmations. Browser-origin Service retains all 128 NodePorts to 8444, a matching selector and ready endpoint.
- Final application health HTTP 200 at 09:24:13 UTC: 56 instances, 54 running and 2 manually stopped. Instance 65 remains running. The earlier instance 68 runtime error is no longer present in this observation; this visual release did not perform a manual runtime repair or establish its recovery cause.

Remote evidence: `/root/cm-input-focus-20261112/release-7f5444d3d23b/` and `/root/cm-input-focus-20261112/native/`. Screenshots/private snapshots are not committed. Owned test servers and browser processes closed in cleanup.
