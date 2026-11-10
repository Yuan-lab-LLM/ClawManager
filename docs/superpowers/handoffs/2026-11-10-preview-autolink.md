# Native preview autolink acceptance — 2026-11-10

OpenCode rendered the existing instance 118 conversation link as `http://127.0.0.1:3000（PID`: its Markdown autolinker included adjacent Chinese prose. URL parsing failed, so the common bridge allowed the browser's default new-tab navigation. Live Chrome reproduced one popup and zero workbench tabs before the fix.

Commit `a0275ff5fcef35916b5a173ac3d64d6048000fbf` recovers an exact loopback authority followed by Chinese punctuation only after normal URL parsing fails. Existing URL, origin, source, gesture and stale-frame checks remain; valid Chinese paths are preserved. The injected script uses ASCII Unicode escapes to avoid depending on the upstream document encoding.

Verification:

- The new Chrome regression failed on the popup assertion before the fix. The final browser suite passed the reproduced autolink, IPv6, wildcard normalization, Chinese paths, external links, stale responses, certificate behavior and real InstanceDetailPage retry/refresh/tab-limit behavior. UTF-8 fixtures and explicit activation readiness replace an implicit timing assumption.
- Frontend bridge source/origin/gesture/URL/stale checks passed. Focused backend native bridge and OpenCode bootstrap/proxy tests passed; Darwin uses the existing temporary Linux-only NFS test overlay. Independent focused review found no critical or important issues.
- Staging runs `preview-a0275ff5`, image `clawmanager.local/clawmanager@sha256:d5236a34a1e99c23ef337854571a67a7bc4b8e0de00e2104ace49b3da550f2d9`.
- Real Chrome clicked the original malformed conversation anchor in both authenticated details and anonymous share views. Each opened the right preview titled `林墨的博客 · 墨迹`, produced zero laptop popups, and restored the preview after leaving and reopening the page.
- Instance 118 remained running at generation 11, binding Runtime 171. Its Pod UID, container IDs and restart counts were unchanged; seven observed project files, PVC UID, certificate and existing share link were unchanged. Only the application Deployment was updated; no test Pods or instances were created.

Local private evidence: `/tmp/cm-preview-click/acceptance.json`, owner/share screenshots and regression logs. Node receipt: `/root/cm-preview-click-20261110/verified.json`. Credentials and share URLs are excluded from this repository.
