# Workbuddy Pro Runtime Progress

## Scope

Workbuddy is a first-class, Pro-only desktop runtime. It uses a dedicated Kubernetes Deployment, Service, and PVC rather than the shared Lite runtime pool.

## Implemented

- Added `workbuddy` to the instance type schema and create API validation.
- Added a fixed `Workbuddy Pro` image card in system settings and the instance creation wizard.
- Reused the managed runtime environment injection used by OpenClaw and Hermes, including ClawManager LLM gateway and instance Agent variables.
- Enabled Webtop desktop defaults, instance-specific `SUBFOLDER`, HTTPS/WSS upstream proxying, clipboard settings, and the `/config` persistent mount.
- Enabled the Portal and instance detail workspace browser for Workbuddy Pro. The browser accesses the mounted `/config` directory through the runtime agent and does not require a server-side `workspace_path` for Pro desktop instances.
- Added the Workbuddy runtime icon to the instance creation wizard.
- Added managed runtime labels, LLM session attribution, and server-side skill scan eligibility.
- Added a migration that converts an existing custom desktop image card named `workbuddy` into the fixed Workbuddy runtime card.

## Compatibility

Existing instances remain on their recorded instance type. A legacy custom Workbuddy instance must be recreated as `workbuddy`, or explicitly migrated and restarted, before it receives the new proxy and environment behavior.

Workbuddy is not registered as a Lite runtime type and is not scheduled into the OpenClaw or Hermes shared runtime pools.
