# Admin and User Guide

This guide maps the main product surfaces for administrators and end users. It is the best starting point when you want to understand how ClawManager is experienced in day-to-day use rather than how it is deployed.

## Admin Experience

Administrators use ClawManager to:

- manage users, quotas, and platform-wide policies
- review instances and cluster-level operations
- govern AI Gateway models, audit trails, cost analysis, and risk rules
- manage Security Center and `skill-scanner` operations
- prepare reusable resources that users can apply to workspaces

## User Experience

End users use ClawManager to:

- create or access OpenClaw workspaces
- create a template-based Team and describe a shared goal in the Team chat
- open workspaces through the portal experience
- inspect runtime status, agent signals, and recent command activity
- attach or remove skills from an instance when permitted
- consume platform-governed AI access through AI Gateway

## Product Areas

- [AI Gateway Guide](./aigateway.md)
- [Agent Control Plane Guide](./agent-control-plane.md)
- [Resource Management Guide](./resource-management.md)
- [Security / Skill Scanner Guide](./security-skill-scanner.md)
- [Team Workspace Quick Guide](./team-workspaces-guide_en.md)

## Suggested Walkthrough

1. Start with the AI Gateway overview if your team cares most about model governance.
2. Review Agent Control Plane if your focus is runtime visibility and operations.
3. Review Resource Management and Security Center if you want reusable channels, skills, and scan-backed workflows.
4. Create a Team from a role template when a task benefits from coordinated planning, delivery, and review.

## Desktop Clipboard

Desktop-mode instances based on WebTop, including OpenClaw Pro and Hermes, use Selkies for browser-to-desktop clipboard synchronization. New instances enable clipboard synchronization in both directions by default.

| Direction | Default behavior |
| --- | --- |
| Local browser / host to WebTop | Enabled. Copy content locally, focus an application inside WebTop, then use `Ctrl+V` in that application. |
| WebTop to local browser / host | Enabled. Copy content inside WebTop to make it available locally. |

Copying locally only updates the WebTop clipboard. It does not automatically place content into a chat box, terminal, or editor: first focus the target application inside the remote desktop, then paste there.

### Change clipboard behavior for one new instance

In the instance creation page, use **Custom variables**. Values must be entered as plain text: do not include Markdown backticks, quotes, or spaces.

#### Disable clipboard synchronization in both directions

Use this when Unicode/IME input occasionally inserts old clipboard content, particularly under a busy desktop or browser workload.

| Variable | Value |
| --- | --- |
| `SELKIES_CLIPBOARD_ENABLED` | `false|locked` |
| `SELKIES_CLIPBOARD_IN_ENABLED` | `false|locked` |
| `SELKIES_CLIPBOARD_OUT_ENABLED` | `false|locked` |

#### Allow only local browser / host to WebTop

Use this when users need to paste external content into WebTop but must not copy data from WebTop back to the local machine.

| Variable | Value |
| --- | --- |
| `SELKIES_CLIPBOARD_ENABLED` | `true|locked` |
| `SELKIES_CLIPBOARD_IN_ENABLED` | `true|locked` |
| `SELKIES_CLIPBOARD_OUT_ENABLED` | `false|locked` |

`SELKIES_CLIPBOARD_ENABLED` is a boolean setting. Do not set it to `in|locked` or `out|locked`: Selkies interprets those as disabled. The `in` and `out` modes are calculated internally from the three boolean settings above.

For legacy Kasm-compatible images, also set `KASM_SVC_SEND_CUT_TEXT=-SendCutText 0` and `KASM_SVC_ACCEPT_CUT_TEXT=-AcceptCutText 0` when disabling all clipboard synchronization. Current Selkies WebSocket desktops rely on the three `SELKIES_CLIPBOARD_*` settings.

### When changes take effect

Clipboard environment variables are read when the desktop Pod starts. They affect only the new instance created with those variables. To change an existing instance, first update its Deployment environment through the control plane or Kubernetes, then restart its Pod; alternatively, create a new instance with the desired custom variables. Changing an environment variable in a terminal inside a running WebTop session has no effect.

### Verify the effective configuration

Inside the WebTop terminal, run:

```bash
env | grep SELKIES_CLIPBOARD
```

For bidirectional synchronization, the output must be exactly:

```text
SELKIES_CLIPBOARD_ENABLED=true|locked
SELKIES_CLIPBOARD_IN_ENABLED=true|locked
SELKIES_CLIPBOARD_OUT_ENABLED=true|locked
```

Runtime logs provide the authoritative confirmation. A healthy inbound-only configuration reports:

```text
clipboard_enabled: (True, True)
clipboard_in_enabled: (True, True)
clipboard_out_enabled: (False, True)
```

When content from the local browser is copied into WebTop, the runtime logs:

```text
Clipboard direction=inbound ... status=applied
```

If the logs instead show `clipboard_enabled: (False, ...)`, check for an invalid value. A common mistake is entering a leading backtick, such as `` `true`` rather than `true|locked`.

### IME and Unicode input troubleshooting

KWin/Wayland can use an internal clipboard fallback to inject Unicode characters. Disabling Selkies clipboard synchronization prevents local clipboard content from being synchronized into WebTop, but it does not remove that internal Unicode fallback. If a user reports that Chinese, Hangul, or another IME input sporadically becomes clipboard text:

1. Create a diagnostic instance with both clipboard directions disabled.
2. Keep a unique sentinel value in the local clipboard and repeatedly enter Unicode text in the affected WebTop application.
3. Confirm that the sentinel appears only after an explicit paste, never as part of ordinary typing.
4. Review the runtime logs for `Clipboard direction=inbound ... status=applied` while reproducing the issue.

For a permanent runtime-level solution, use a Unicode input path that does not temporarily write to the system clipboard. Clipboard environment overrides are an instance-level mitigation and should be preferred when the issue is limited to a particular user or workload.
