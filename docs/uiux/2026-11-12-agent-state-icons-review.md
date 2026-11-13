# Agent state icon review v1

Preview: https://10.130.81.54:30443/uiux/2026-11-12-agent-state-icons-preview.html
Self-contained source: `2026-11-12-agent-state-icons-preview.html`.

Seven current V2 types: OpenClaw, Hermes, OpenCode, DeepSeek Harness, WorkBuddy, Codex and Claude Code. Each has running, stopped, error and sleeping variants. Running retains the exact repository asset; stopped uses grayscale plus reduced opacity so monochrome brands also differ; error adds a red triangle/exclamation badge; sleeping adds a slight resting pose, Zz and optional breathing motion. OpenClaw also has closed eyes. Brand text/shape stays recognizable, and state has an accessible name.

The HTML offers 32/48/64px comparison, click-to-enlarge, a native-size list sample and motion toggle; system reduced-motion is respected. All assets are embedded, and the preview makes no business API requests.

On54 browser checks passed:7 types/28 matrix cells, image loading, original/grayscale/error/sleep variants, selection/type/size controls, motion toggle/reduced motion,1440/1280/768/390 widths without outer horizontal overflow, zero API calls and page errors. Rendered desktop output reviewed. Evidence is at `/root/cm-agent-state-icons-review-20261112/`.

This is an icon review, not an implementation or approval of list behavior. The previously built gradient candidate (`91e7bbf`, image902bf1037f19…) was not deployed after the user redirected work to icon review. During initial review the application image remained `ce19e250cc0d…` and only the static preview was copied to nginx. Following approval, the state-icons release includes both previews in its immutable image artifact and integrates the accepted treatment into the instance list. See the state-icons release verification for the final image and checks.
