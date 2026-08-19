[← Back to README](../README.md)

# Resource Management User Guide

Resource Management is the user-side workspace configuration center for reusable OpenClaw startup content. It is separate from the administrator **Security Protection** console: Resource Management prepares and delivers configuration; Security Protection observes and governs platform risk.

![OpenClaw Resource Management](./main/resource-management-current.png)

## Page Layout

The page has three tabs:

- **Resources**: search and manage individual resource definitions.
- **Resource Packs**: combine reusable resources into one selectable startup package.
- **Injection Records**: inspect the snapshots compiled when an instance was created and reused on restart.

## Resources

The current resource list exposes four types:

- **Channels**: create, edit, enable/disable, clone, and delete reusable communication configurations. Built-in forms and JSON editing are available for supported channel templates such as Telegram, DingTalk, WeCom, Slack, and Feishu. Keep credentials in the intended secret/configuration fields rather than descriptions.
- **Skills**: upload one or more ZIP packages, resolve import conflicts, download an uploaded package, or delete it. For catalog browsing, version ownership, publication, and later installation, use **Skill Hub**.
- **Agents**: visible as a reserved resource type, but not configurable from this page yet.
- **Scheduled Tasks**: create and edit reusable OpenClaw jobs using a simple form or advanced JSON. Supported schedules include cron expressions, fixed intervals, and one-time execution, with announce, webhook, or no-delivery modes.

Session templates and log policies exist in the underlying resource model but are intentionally hidden from this page.

## Resource Packs

A resource pack combines enabled resources and eligible uploaded skills into a repeatable startup selection. Users can create, edit, enable/disable, clone, and delete packs. Choose a pack during instance creation when multiple workspaces need the same baseline; use manual resources when only a few items are needed.

## Injection Records

An injection record is a read-only snapshot of what ClawManager compiled for an instance. The table shows snapshot ID, delivery mode, resource count, environment-variable count, status, and creation time. Modes include no injection, manual selection, resource pack, and archive restore; statuses include compiled, active, and failed.

Injection records help answer “what was delivered?” They are not security events and do not replace instance-side verification.

## Relationship to Other Features

- **Skill Hub** manages the reusable skill catalog, owners, tags, versions, publication, and installation to compatible OpenClaw, Hermes, OpenCode, and DeepSeek Harness instances.
- **Instance creation** selects archives, resource packs, manual resources, or skills when the chosen runtime supports them.
- **Instance Skill Management** shows the materialized skill version in a running workspace.
- **Security Protection** is a separate administrator feature for runtime defense, isolation, policy, emergency response, and audit. Skill Scanner is one scenario inside that platform, not a Resource Management tab.

## Related Guides

- [Skill Hub Guide](./skill-hub-guide_en.md)
- [Security Protection Platform Guide](./security-platform.md)
- [Security Protection Platform Guide](./security-platform.md)
- [User Guide](./use_guide_en.md)
