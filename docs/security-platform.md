[← Back to README](../README.md)

# Security Protection Platform Guide

**Security Protection** is a dedicated administrator workspace in ClawManager. It is not part of user-side Resource Management. The overview combines runtime defense, host and container protection, component trust, identity, policy, collaboration governance, emergency response, and security events in one operational view.

![ClawManager Security Protection overview](./main/security-protection-current.png)

## Overview Dashboard

The header summarizes live alert data:

- defense hits since the start of the day;
- high-severity events in the last 24 hours;
- blocked or denied events in the last 24 hours;
- distinct affected agent instances in the last 24 hours.

Alerts refresh automatically. The lower event table shows the ten most recent cross-product events with time, source, scenario, evidence, target, and severity, and links to the complete event view.

## Operator Actions

- **Pod Live Aegis Configuration** opens the managed runtime security configuration and dispatch flow.
- **Export Report** downloads the currently loaded alerts as JSON Lines. It is disabled when there is no alert data.
- **Emergency Circuit Breaker** asks for a reason and confirmation, then dispatches the emergency state to the target runtimes. When active, the page shows who enabled it, when, and why, and provides a controlled disable action.

These are administrator actions with platform impact. Confirm the affected scope before changing live configuration or enabling the circuit breaker.

## KSecure Defense Model

The overview presents the product as **7 risk surfaces, 15 defense scenarios, and 4 defense layers**. Operators can switch between a layered view and a ring view, then open a risk-surface card for its scenario pages.

### Runtime layer

- **Agent runtime security**: input-surface detection, state and memory protection, dangerous decision/tool-call control, output redaction, protected assets, and human approval.

### Host layer

- **Environment isolation and hardening**: host hardening and container isolation/escape protection.

### Audit layer

- **Data and component trust**: Skill Scanner reports and configuration, plus controlled private-network egress exceptions.

### Control layer

- **Unified identity and permissions**: outbound governance and trusted destination controls.
- **Security policy and templates**: centralized policy governance.
- **Supervision and operational governance**: emergency circuit breaker and full-chain audit.
- **Collaboration access and communication**: Team collaboration governance and AI Gateway quota controls.

Each card is an entry point to the corresponding page. Available enforcement and dispatch actions depend on the deployed security services and configured runtime agents; a visible card does not by itself prove that every backend control is active.

## Recommended Operator Workflow

1. Review the four live metrics and recent events.
2. Identify the affected instance, rule, source, and scenario.
3. Open the corresponding risk surface and inspect its configuration or evidence.
4. Apply the narrowest appropriate policy or runtime change.
5. Use the emergency circuit breaker only when interruption is justified and the scope is understood.
6. Confirm new events and runtime state, then export evidence when required.

## Boundaries

- **Resource Management** prepares channels, skills, scheduled tasks, resource packs, and injection records. It is not a security dashboard.
- **Skill Scanner** is one data-and-component-trust scenario inside Security Protection. Users upload versions and read scan state/report in Skill Hub; administrators use Security Protection to inspect scanner health, failed jobs, model/Meta LLM configuration, Quick/Deep policy, and related security events. A completed scan is evidence, not an automatic publication or installation approval.
- Security Protection does not replace Kubernetes hardening, network policies, credential hygiene, backups, or an organization’s incident-response process.

## Related Guides

- [Skill Hub Guide](./skill-hub-guide_en.md)
- [Resource Management Guide](./resource-management.md)
- [AI Gateway User Guide](./aigateway.md)
- [User Manual](./use_guide_en.md)
