[← Back to README](../README.md)

# OpenCode Workspace Guide

OpenCode is ClawManager's managed coding workspace. It uses the official OpenCode release and receives model access through the platform AI Gateway.

## Lite and Pro

| Mode | Runtime shape | Best for | Important boundary |
|---|---|---|---|
| Lite | isolated OpenCode process/workspace in a shared Runtime Pod | fast start and efficient shared capacity | no dedicated Pod per instance |
| Pro | dedicated desktop workload and workspace | stronger isolation and a full desktop | image changes affect future provisioning unless the instance is explicitly replaced/restarted |

Both modes keep the user workspace persistent according to the selected storage profile. Lite portal routing is adapted by ClawManager; Pro opens OpenCode from the dedicated desktop.

## Before creation

1. An administrator enables a compatible OpenCode Lite or Pro image.
2. At least one ordinary model is enabled in **AI Gateway → Models**.
3. For Lite, the shared OpenCode Runtime pool is healthy. If an image was just saved, the administrator must also complete the Lite rolling upgrade.
4. The user has enough CPU, memory, storage quota, and any required Resource Pack.

OpenCode receives a managed AI Gateway provider configuration. Users should not add unrelated external keys through OpenCode's provider connection screen unless the administrator explicitly designed that environment for it.

## Create and open the workspace

Under **My Instances → Create**, select OpenCode and Lite or Pro, then choose an enabled image, resource values, environment variables, and supported startup resources. After creation, the instance page provides lifecycle actions, the OpenCode terminal/desktop, and workspace files.

Use ClawManager Start, Stop, Restart, and Delete actions. Manual edits to generated workloads can be reverted by reconciliation. Save required files before deletion.

## Files, terminal, and persistence

- Store project files in the workspace path shown by ClawManager rather than temporary system directories.
- Use the right-side file panel for supported upload, download, edit, and delete operations.
- Pro desktop stream quality changes normally require the requested apply/restart action.
- A Share Link should have an expiry, credential, and only the workspace permission actually required.
- If the terminal works but files disappear after restart, check the configured storage profile and actual workspace path.

## Models and AI Gateway

OpenCode uses the platform's enabled model catalog and supports the request protocol configured by the administrator. Validate streaming and tool calls before production use. When requests fail, first check the instance state, then AI Gateway model health and AI Audit. A security model is not required for normal OpenCode use.

## Skill Hub compatibility

Skill Hub is a platform-wide capability shared by OpenClaw, Hermes, OpenCode, and DeepSeek Harness; it is not an OpenCode feature. For OpenCode specifically, Lite materializes skills below `{workspace}/home/.opencode/skills`, while managed HostPath Pro uses `/config/workspace/.opencode/skills`.

If skill preselection is absent during creation, install after the instance is ready. Refresh Instance Skill Management to confirm the effective version. Non-HostPath OpenCode Pro additionally depends on the selected Runtime Agent image implementing remote install/uninstall; a successful HostPath test does not prove every storage backend has the same support.

## Current boundaries

- OpenCode does not inherit OpenClaw configuration plans, OpenClaw workspace archives, or Team persona overlays.
- OpenCode is not currently used as a Team Leader or Worker in the standard Team creation flow.
- Scheduled tasks should be considered available only where the current UI explicitly exposes them.
- Runtime image and storage capabilities can differ even when both instances are labelled OpenCode.

## Troubleshooting

| Symptom | Check |
|---|---|
| OpenCode image is missing | Admin Settings image enablement and user quota. |
| Lite still runs the old image | Save alone is insufficient; complete the Lite rolling upgrade. |
| Portal is unavailable | Instance/Runtime health, restart, and shared pool events. |
| Model fails | Enabled model, provider health, protocol, and AI Audit. |
| Files do not persist | Workspace path, PVC/storage profile, and volume health. |
| Skill installation incomplete | Instance Skill Management, materialized path, and Runtime Agent capability. |

Acceptance should cover create/start/stop/restart, portal/desktop, AI Gateway streaming and tools, workspace persistence, Share Link scope, Skill inventory/install/collect when used, and clear error reporting.

See the [User Manual](./use_guide_en.md), [AI Gateway Guide](./aigateway.md), and [Skill Hub Guide](./skill-hub-guide_en.md).
