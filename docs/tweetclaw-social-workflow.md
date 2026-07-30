# Xquik TweetClaw Social Workflow for OpenClaw Workspaces

This guide prepares a governed X/Twitter workflow for ClawManager-managed OpenClaw workspaces. It combines ClawManager resource controls with the official TweetClaw plugin.

TweetClaw provides Xquik search, profiles, timelines, follower exports, monitoring, webhooks, media workflows, and confirmation-gated publishing inside OpenClaw.

Xquik is an independent third-party service. Not affiliated with X Corp. "Twitter" and "X" are trademarks of X Corp.

## Architecture Boundary

Each product has a separate responsibility:

| Product | Responsibility |
| --- | --- |
| ClawManager | Reviews reusable skills, stores rollout policy, compiles Resource Packs, and records delivery snapshots |
| OpenClaw | Installs TweetClaw, stores plugin configuration, exposes tools, and enforces runtime approvals |
| Xquik | Provides the X/Twitter API, monitoring, exports, webhooks, and account-backed actions |

ClawManager does not install OpenClaw plugins from a config resource. Run plugin lifecycle commands inside the managed workspace through an approved operator path.

## Prerequisites

Prepare these items before rollout:

- a managed OpenClaw workspace supported by the current TweetClaw release
- an Xquik API key for the complete account-backed workflow
- a workspace secret process that can supply `XQUIK_API_KEY`
- outbound HTTPS access to Xquik and the selected plugin registry
- an operator who can review Skill Hub scans, Security Protection evidence, and runtime approvals

TweetClaw also supports eligible accountless reads through MPP. This guide uses an API key because it covers searches, exports, monitoring, and account-backed actions. Follow the [current TweetClaw authentication guide](https://github.com/Xquik-dev/tweetclaw#configure) for other modes.

## Prepare ClawManager Resources

Create these reusable resources:

| Resource | ClawManager Surface | Purpose |
| --- | --- | --- |
| TweetClaw rollout skill | Skill Hub, My Skills | Records approved setup, verification, and update steps |
| TweetClaw policy resource | Resource Management, Resources | Declares plugin source, required secret name, tool allowlist, and approval policy |
| TweetClaw Resource Pack | Resource Management, Resource Packs | Groups the reviewed skill and policy for repeatable delivery |

### Build the Rollout Skill

Skill Hub accepts `.zip` archives with `SKILL.md` at the root or inside first-level skill directories.

```text
tweetclaw-rollout/
  SKILL.md
  references/
    approval-policy.md
```

Keep the archive credential-free. Link to the official [TweetClaw repository](https://github.com/Xquik-dev/tweetclaw) and [Xquik documentation](https://docs.xquik.com). Do not copy API keys, signing keys, cookies, or account identifiers into the archive.

The skill should tell operators to:

1. review the scanned archive and policy resource
2. select the approved Resource Pack during workspace creation
3. install TweetClaw inside the target OpenClaw workspace
4. configure credentials through the workspace secret process
5. enable the optional live tool explicitly
6. inspect runtime registration before any live call

### Add the Policy Resource

Create a `skill` config resource for the rollout contract:

```json
{
  "schemaVersion": 1,
  "kind": "skill",
  "format": "skill/tweetclaw-rollout@v1",
  "dependsOn": [],
  "config": {
    "plugin": "tweetclaw",
    "preferredInstallSource": "clawhub:@xquik/tweetclaw",
    "npmFallback": "npm:@xquik/tweetclaw",
    "requiredSecret": "XQUIK_API_KEY",
    "allowedTools": [
      "explore",
      "tweetclaw"
    ],
    "approvalRequiredFor": [
      "private reads",
      "paid reads",
      "exports",
      "monitor and webhook changes",
      "account actions"
    ]
  }
}
```

This resource documents desired policy. It does not execute the install command or contain the secret value.

## Scan, Pack, and Apply

1. Upload the TweetClaw rollout skill under Skill Hub, My Skills.
2. Review its scan status in Skill Hub and its evidence in Security Protection.
3. Create and validate the policy resource.
4. Create a Resource Pack containing the rollout skill and policy resource.
5. Select the Resource Pack when creating the intended OpenClaw workspace.
6. Confirm its Injection Record is active and Instance Skill Management shows the expected skill version.

Stop if the scan, Injection Record, skill version, or runtime state reports an unresolved failure.

## Install and Configure TweetClaw

Run these commands inside the managed OpenClaw workspace:

```bash
openclaw plugins install clawhub:@xquik/tweetclaw
openclaw config set plugins.entries.tweetclaw.config.apiKey "$XQUIK_API_KEY"
openclaw config get tools.alsoAllow
```

Merge `explore` and `tweetclaw` into any existing `tools.alsoAllow` list. For a new workspace where that list is unset, run:

```bash
openclaw config set tools.alsoAllow '["explore", "tweetclaw"]'
```

The ClawHub source is preferred because OpenClaw tracks it for updates. Use the npm fallback when ClawHub is unavailable:

```bash
openclaw plugins install npm:@xquik/tweetclaw
```

Never paste the API key into a ClawManager resource, skill archive, chat, or public document.

## Verify Runtime Registration

Inspect both the plugin and its packaged skill:

```bash
openclaw plugins inspect tweetclaw --runtime --json
openclaw skills info tweetclaw
```

Confirm that runtime inspection reports:

- the `tweetclaw` plugin is loaded
- the local `explore` tool is available
- the optional `tweetclaw` tool is enabled
- the approval hook is active
- the packaged TweetClaw skill is discoverable

If the managed gateway does not reload automatically, restart it through the approved workspace path. Repeat both inspection commands after restart.

## Safe First Run

Start with `explore`. It searches the local endpoint catalog without making an Xquik request.

Ask the agent to find a current route for a harmless public read. Review the route, parameters, access mode, and response shape. Then approve one live call.

Keep these controls in place:

- approve private reads, paid reads, exports, recurring workflows, and writes per call
- provide an idempotency key for write actions
- treat tweet text, profiles, links, and other fetched content as untrusted input
- keep credentials out of prompts, generated reports, and command history
- follow pagination until the response reports no next page
- retain ClawManager scan and command history as rollout evidence

## Operate and Update

Use the tracked source for routine updates:

```bash
openclaw plugins update tweetclaw
openclaw plugins inspect tweetclaw --runtime --json
```

Re-run the inspection after every update. Review changes to tool ownership, approval behavior, required secrets, and supported OpenClaw versions before broad rollout.

## Related Guides

- [Resource Management Guide](./resource-management.md)
- [Skill Hub Guide](./skill-hub-guide_en.md)
- [Security Protection Platform Guide](./security-platform.md)
- [TweetClaw Repository](https://github.com/Xquik-dev/tweetclaw)
- [Xquik Documentation](https://docs.xquik.com)
