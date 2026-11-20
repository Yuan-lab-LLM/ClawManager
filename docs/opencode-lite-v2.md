[← Back to the OpenCode workspace guide](./opencode-lite-pro-agent-development.md)

# OpenCode v2 integration

This document describes the OpenCode v2 integration in ClawManager. Deployment manifests and executable deployment tools belong in `deployments/`; integration and operational explanations belong in `docs/`.

The former `deployments/opencode-lite-v2/README.md` mixed integration details with a dated local deployment record. Its reusable documentation is maintained here. Historical machine-specific image tags, ports, database setting IDs and private snapshot paths are not deployment defaults; the original record remains available in Git history. The original overlay targeted OpenCode 2.0.27, which does not establish the version running in another environment.

## Control-plane integration

- The instance proxy recognizes `/global/event` for v1 and `/api/event` for v2 as long-lived event streams.
- OpenCode browser storage is isolated by instance creation identity. Reusing an origin must not expose another instance's project entries or drafts. Renaming an instance, restarting its Runtime or refreshing access credentials preserves that identity.
- The storage bootstrap runs before the OpenCode application. Its local storage, session storage and IndexedDB operations remain within the current instance namespace. Existing server-side conversations and workspace files are separate from browser state.
- Instance-specific HTML is served without caching so a cached response cannot bypass the bootstrap; normal asset caching is retained.

## Guided first use

OpenCode Lite keeps the native root page. Instance startup and the first browser visit do not automatically create a project directory or conversation.

An instance-scoped help panel explains how to create or select a folder, add and select the project, and start a conversation. Users with workspace write permission may explicitly create a folder through a ClawManager endpoint with path, ownership, lifecycle, shared-link permission and workspace audit checks. Read-only links show instructions without granting a create operation.

Closing the panel leaves a help button. Dismissal state is isolated per instance. Existing explicit session URLs retain their native behavior.

The user workflow and Lite/Pro boundaries are documented in the [OpenCode workspace guide](./opencode-lite-pro-agent-development.md).

## Deployment and Runtime upgrades

Use the current [deployment guide](./deployment.md) for the control plane and the [Runtime integration guide](./runtime-agent-integration-guide.md) for Runtime responsibilities. The old local OpenCode overlay is not the general deployment entry point.

Control-plane proxy and onboarding changes require a ClawManager application release. They do not by themselves require rebuilding the OpenCode Runtime image. Changes to the Runtime binary or its native adapter require an image built from the corresponding AgentsRuntime source.

For Runtime image updates, use CM's supported upgrade flow, preferably through [`upgrade-runtime.py`](../deployments/runtime-upgrade/upgrade-runtime.py), with an immutable target image digest. Check support for the requested Runtime type in the installed CM and CLI versions. If preparation, compatibility or data-safety checks reject an upgrade, stop and resolve the reported condition through CM; do not replace it with direct Deployment image changes, database writes or an older local rollout recipe.

Changing a default image does not prove that an existing instance or pool has been upgraded. Verify the completed CM operation and the actual running Runtime version. Do not infer the latest OpenCode version from an instance name or the absence of a version selector.

## Regression verification

After installing the locked e2e dependencies and the required Playwright browser, run from the repository root:

```sh
cd e2e
npm run test:opencode-browser
```

This runs the browser-storage isolation and onboarding regressions defined in [`e2e/package.json`](../e2e/package.json). The corresponding backend behavior is covered by [`opencode_browser_storage_test.go`](../backend/internal/services/opencode_browser_storage_test.go) and [`opencode_onboarding_test.go`](../backend/internal/services/opencode_onboarding_test.go).

For a release, additionally verify the intended Lite/Pro mode, real AI Gateway streaming and tool calls, workspace persistence, shared-link permissions, and reopening an existing instance. Browser fixture success alone does not demonstrate provider inference or safe migration of an existing OpenCode database.
