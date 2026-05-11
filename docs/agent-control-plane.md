# Agent Control Plane Guide

Agent Control Plane is the runtime orchestration layer for managed runtime instances in ClawManager. It allows the platform to understand live runtime state, distribute commands, and keep each managed workspace aligned with the desired state defined by the control plane.

## Core Responsibilities

- agent bootstrap and registration for managed runtime instances
- authenticated session lifecycle between the runtime agent and the platform
- heartbeat-driven runtime and health reporting
- desired power state and desired config revision tracking
- command dispatch and completion tracking for runtime operations

## Runtime Signals

The control plane keeps a runtime view that includes:

- agent identity, version, and last heartbeat
- runtime status and v1 compatibility status fields
- current and desired config revision
- reported summary data such as agent, runtime, and skill counts
- recent command history and execution outcomes

## Typical Commands

Examples of platform-driven runtime actions include:

- start, stop, and restart operations
- config revision apply and reload
- health checks and system info collection
- skill install, update, removal, quarantine, and inventory refresh

## Where It Shows Up in the Product

- instance detail views for agent status and runtime summaries
- runtime command history and execution feedback
- workflows that apply config revisions or skill-related changes to a workspace

## Related Guides

- [Runtime Agent Integration Guide](./runtime-agent-integration-guide.md)
- [Hermes Runtime Image and Agent Development Guide](./hermes-runtime-agent-development.md)
- [Admin and User Guide](./admin-user-guide.md)
- [Resource Management Guide](./resource-management.md)
- [Security / Skill Scanner Guide](./security-skill-scanner.md)
- [Developer Guide](./developer-guide.md)
