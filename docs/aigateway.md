[← Back to README](../README.md)

# AI Gateway User Guide

AI Gateway is the managed model-access layer for ClawManager workspaces. Administrators configure providers and policies once; OpenClaw, Hermes, OpenCode, DeepSeek Harness, Team members, and platform features then use the enabled models through the gateway.

## Before You Create an Instance

Configure and enable at least one ordinary model under **Admin Console → AI Gateway → Models**. A model does not need to be marked as a security model for normal instance creation.

A **security model** is optional. It is used only when a risk rule is configured to route sensitive requests to a separate model.

If no model is enabled, instance creation and model-powered features such as custom Team generation show a configuration error instead of starting an unusable workflow.

## The Five AI Gateway Areas

### Models

- Add provider URL, model name, credentials, protocol, pricing, and enabled state.
- Test provider health before making a model available to users.
- Mark a model as a security model only when it should receive policy-routed sensitive traffic.
- Configure managed **Thinking** for supported model/provider combinations.

Thinking is a persistent model setting. When it is off, managed agents cannot silently turn provider reasoning back on. When it is on, a supported runtime may still reduce or disable it for an individual session. Thinking can increase response time and reasoning-token usage; it does not expose private chain-of-thought text to users.

### AI Audit

Review request traces, provider responses, selected routes, policy hits, latency, errors, and other audit metadata. Audit access follows the viewer's platform permissions.

### Costs

Review token use, estimated external-provider charges, and internal accounting entries. Cost estimates depend on the pricing configured for each model.

### Session Usage

Compare model usage by user, runtime, instance, and conversation across sessions that report compatible usage metadata. Filter by time range and identity, then compare input, output, cached, and reasoning-token fields when the provider reports them. Estimated cost uses the price configured on the model.

Session Usage is an observability view. It does not edit conversations, change Team state, or act as a final provider invoice. Old, interrupted, or unsupported runtime sessions may have incomplete fields. Provider totals can also differ because retry behavior and token categories vary. Open the related instance for user-visible context and use **AI Audit** for request-level routing, errors, and policy evidence.

### Risk Rules

Define sensitive-content patterns, severity, action, and order. A matched rule can allow the request, block it, or route it to an enabled security model. Keep broad rules below narrow rules so specific policies are evaluated first.

## Supported Request Protocols

AI Gateway accepts the protocols currently used by managed runtimes:

- OpenAI Chat Completions
- OpenAI Responses
- Anthropic Messages

Choose the protocol that matches the upstream provider. Protocol translation is not a guarantee that every provider-specific extension is supported; test tool calls and streaming before assigning a model to production workspaces.

## Model Selection

1. A runtime requests an enabled model, or uses automatic selection.
2. The gateway applies the current model and risk-policy configuration.
3. If a rule blocks the request, the gateway returns a policy error.
4. If a rule requests secure routing, the gateway selects an enabled security model.
5. Otherwise, traffic stays on the selected ordinary model.

## User-Facing Troubleshooting

| Symptom | What to check |
|---|---|
| Instance or custom Team cannot be created | At least one model must be enabled; it does not have to be a security model. |
| Thinking toggle shows unsupported | The provider/model combination is not currently managed by ClawManager; leave it off. |
| Session Usage is empty | Confirm the runtime and session report usage, then check the selected time range and instance filter. |
| Cost is zero or incomplete | Add correct input/output pricing to the model. |
| Request is unexpectedly blocked or rerouted | Review matching Risk Rules and their order in AI Audit. |

## Related Guides

- [User Manual](./use_guide_en.md)
- [Security Protection Platform](./security-platform.md)
