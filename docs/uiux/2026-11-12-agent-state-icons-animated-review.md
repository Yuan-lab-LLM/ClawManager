# Agent state icon v2: approved motion treatment

Preview: https://10.130.81.54:30443/uiux/2026-11-12-agent-state-icons-animated-preview.html
V1 remains available for comparison. Both are self-contained HTML. V2 was approved for instance-list integration, with the error correction below.

All seven current Agent types have four motion treatments:

- Running: original brand, gentle2.8s breathing/raised movement.
- Stopped:700ms color-to-gray power-down and settling, then static. Replay restarts it; a stopped Agent does not continuously look active.
- Error (approved correction): original icon stays still and opaque. Only the68%-of-icon red triangle/exclamation scales. No red circle, background halo, original-icon shake or opacity flashing. Static/reduced-motion mode keeps the warning shape.
- Sleeping: resting pose, optional closed eyes for OpenClaw, Zz and breathing.

Controls:32/48/64px, click-to-enlarge, all-motion toggle, replay. CSS respects reduced motion. IntersectionObserver pauses offscreen icons; document visibility pauses hidden-page motion. Rendering a new inspector reconnects observation without retaining removed nodes.

Verified on54:7types/28matrix variants; all four state animation names for every type; stopped iteration count1 and final grayscale; enlarged warning; replay; static toggle; reduced motion for all four states; offscreen pause; image loading and type/size/inspector controls;1440/1280/768/390 widths without outer overflow; zero API requests and page errors. Rendered output reviewed. Remote evidence: `/root/cm-agent-state-icons-animated-review-20261112/`.

Both HTML previews are included in the state-icons image artifact, so they survive application container replacement. The user approved V2 with error badge scaling only. The reusable AgentStateIcon component and instance-list integration follow that approved correction; deployment evidence is recorded separately.
