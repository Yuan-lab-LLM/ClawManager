# Security / Skill Scanner Guide

Security Center is the review and scanning surface for skill assets in ClawManager. It works with `skill-scanner` to help teams understand asset coverage, risk posture, and scanning status before skills are reused across workspaces.

## What It Covers

- skill asset inventory across the platform
- scan status, coverage, and recent scan jobs
- risk-level distribution for discovered and uploaded skills
- scanner configuration, including external analysis integrations where configured

## Main Workflows

1. Review the asset inventory and identify high-risk or unscanned skills.
2. Start incremental or full scans from Security Center.
3. Inspect recent scan jobs and detailed outcomes.
4. Tune scanner configuration and analysis integrations.
5. Feed scanning results back into skill approval and workspace rollout decisions.

## Why It Matters

- keeps reusable skills visible and reviewable
- adds a security checkpoint to the resource supply chain
- supports scale by replacing ad hoc per-instance trust decisions with centralized scanning workflows

## Related Guides

- [Resource Management Guide](./resource-management.md)
- [Agent Control Plane Guide](./agent-control-plane.md)
- [AI Gateway Guide](./aigateway.md)
