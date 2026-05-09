# Harness Cleanup Inventory

Date: 2026-05-07
Scope: ClawManager framework and GTClaw follow-up working state
Status: cleanup inventory only

## Purpose

This document converts the current scattered harness state into an explicit
inventory. Cleanup means classify, index, alias, and freeze current state for
the next gated action. It does not mean delete, rewrite history, close old
sessions, clean old assets, stage, commit, push, deploy, or mark acceptance.

The active short-term framework is Scheme A: use the current ClawManager
spec/evidence/longterm structure as the base harness. Scheme B remains the
long-term direction: gradually migrate toward the stronger CodeSpec-style
contract and typed execution model when a task naturally touches the relevant
area.

## Current GTClaw State

Feature: gtclaw-runtime-controlui-persistent-image

Latest accepted gate:
FRESH_INSTANCE_RUNTIME_DEPLOYMENT_APPROVAL_PACKET_CONTROLUI_PERSISTENCE_DONE

The fresh instance/runtime deployment gate for the latest control-ui persistence
image has not been executed yet. The current legal next action is to request or
receive explicit approval for that deployment gate, then run only the approved
fresh-instance deployment scope.

Latest candidate runtime image:

- Host tag: localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712
- In-cluster tag: k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712
- Image index digest: sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908
- Linux arm64 digest: sha256:0616ee761cee6ed35addd9becfe7ec0cbc6df4821c67c9f8a0607490f6e33d0a

Browser/manual E2E has not passed for this latest image. Therefore this follow-up
cannot use passes:true, no Close, and no longterm workspace write-back.

## Worktree Categories

Framework migration:

- AGENTS.md
- .specify/memory/constitution.md
- longterm/README.md
- longterm/METHOD.md
- longterm/METHOD.zh-CN.md
- longterm/CHECKLIST.md
- AgentTeam/
- UnifiedFramework/

Backend WS challenge bridge:

- backend/internal/services/instance_proxy_service.go
- backend/internal/services/instance_proxy_service_test.go

Runtime startup artifact origin allowlist:

- specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/Dockerfile
- specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/MANIFEST.md
- specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/defaults/openclaw-agent/config.yaml
- specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/usr/local/bin/openclaw-ensure-controlui-origin
- specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/usr/local/bin/openclaw-gateway-with-origin-allowlist

Runtime control-ui persistence source artifact:

- specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/

Runtime image assembly artifact:

- specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/

Evidence corpus:

- specs/gtclaw-runtime-controlui-persistent-image/evidence/
- Current observed corpus size: 99 markdown evidence files before INDEX.md
- Historical evidence must remain immutable unless a later explicit correction
  gate approves an addendum.

## Evidence Groups

The evidence corpus is grouped by delivery stage rather than by worker name:

- 20260503: baseline runtime image, source inspection, image delivery, initial
  runtime delivery artifacts.
- 20260504: browser/runtime investigation, 18789 listener checks, fresh instance
  testing, first runtime deployment loops.
- 20260505: runtime startup artifact, backend persistent runtime checks, final
  F-007 close-era evidence, governance and generated-artifact cleanup.
- 20260506: post-close follow-up covering WS challenge bridge, origin allowlist,
  stale route root cause, control-ui persistence source recovery, assembly image
  creation, latest build/tag/push, and fresh-instance approval packet.

## Cleanup Rules

- no delete of old evidence, generated assets, sessions, or runtime artifacts.
- no rewrite historical evidence.
- no direct implementation under this cleanup gate.
- no build/tag/push/pull.
- no deploy/restart.
- no fresh instance creation, deletion, or mutation.
- no browser E2E, Chrome DevTools, Playwright, storage cleanup, cache cleanup, or
  cookie cleanup.
- no K8S, runtime, database, or registry mutation.
- no token, password, or key input.
- no Mem0 write and no longterm write-back.
- no passes:true.
- no Close.
- no git stage, commit, or push.

## Decision

Use this inventory as the current map. Future work should reference this file
and the feature evidence INDEX.md before issuing new prompts or approving gates.
The next GTClaw gate remains the fresh instance/runtime deployment gate for the
latest control-ui persistence image, and it still requires explicit user
approval before execution.
