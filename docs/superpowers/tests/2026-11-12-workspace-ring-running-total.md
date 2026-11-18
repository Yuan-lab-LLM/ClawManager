# Agent ring: running / total — 2026-11-12

User requested that the center show running count / total count rather than the total alone.

- Running = busy + idle + confirmed running (activity unknown). Sleeping, starting, failed, offline and unknown runtime status are excluded. Total is the full state aggregate, independent of the capped Agent list.
- Center label is localized “运行 / 总数” / “Running / Total”; the accessible chart description includes the same pair. Unknown statistics retain the unavailable mask. Existing full-state ring shares and legends remain intact.
- The ring is140px with a single-line pair. Targeted model regression reproduced the missing running aggregate before the change and passed after. All11 model tests, TypeScript, targeted lint, full Dashboard browser suite,10 focused browser groups and server-side production build passed. Browser assertions include3/8 and302/307 aggregate examples; five locales and four viewport widths remain covered.
- Independent bounded code/screenshot review found no blocker.

## Server54 release

Source: `e40d465a2af8be82d905d488aa3ca206a2bce311`. Image: `clawmanager.local/clawmanager@sha256:c170f71e478827cc505b067779e9503b35fc0ca44c3c0021c1c7bde732346f94`. Application generation20, ready1/1. All builds and browser/runtime checks ran on54.

Live Chromium verified the displayed pair against actual API counts, the “运行 / 总数” label,1440×900/1280×800/390 layouts, aligned work panels and no JavaScript errors. At verification the actual pair was54 /56, with two instances stopped manually. App health HTTP200. OpenCode and DSH native pages/composers were rechecked with default TLS verification and only one manager-host confirmation, ensuring the browser-origin Service repair remains effective after rollout. No runtime images changed. The owned9439 test server was stopped.

Evidence: `/root/cm-ring-count-20261112/`, final image/source/rollout/browser/health evidence under `release-e40d465a2af8/`, native entry checks under `native/`. No merge or push.
