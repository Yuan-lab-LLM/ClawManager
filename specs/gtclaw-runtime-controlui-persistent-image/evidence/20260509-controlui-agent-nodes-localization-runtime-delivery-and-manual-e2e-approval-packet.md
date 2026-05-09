# Control UI Agent And Nodes Localization Runtime Delivery And Manual E2E Approval Packet

Date/timezone: 2026-05-09, Asia/Shanghai

Gate: CONTROLUI_AGENT_NODES_LOCALIZATION_RUNTIME_DELIVERY_AND_MANUAL_E2E_APPROVAL_PACKET_GATE

Verdict:

- CONTROLUI_AGENT_NODES_LOCALIZATION_RUNTIME_DELIVERY_AND_MANUAL_E2E_APPROVAL_PACKET_DONE

## Dependencies

- CONTROLUI_AGENT_CHUNK_SOURCE_RECOVERY_AND_LOCALIZATION_PATCH_DONE
- CONTROLUI_AGENT_AND_NODES_LOCALIZATION_RESIDUAL_ROOT_CAUSE_AND_PATCH_BLOCKED
- CONTROLUI_INTERNAL_LOCALIZATION_LIVE_RESIDUAL_SWEEP_RUNTIME_DELIVERY_DONE
- Commander read-only review passed for `agents-_34Q844e.js` and `nodes-BBk4VzkK.js`.
- Source and assembly copies match, and syntax plus hash checks pass.

## Next Gate Request

Approval phrase requested for the next gate:

- APPROVE_CONTROLUI_AGENT_NODES_LOCALIZATION_RUNTIME_DELIVERY_AND_MANUAL_E2E_GATE

The next gate is approved to:

- build/push a new runtime image containing the latest agents and nodes localization patches.
- if capacity requires action, only process superseded instance 24 / oc2gi-iloc-rs-103428.
- keep instances 17 and 18 untouched.
- create exactly one fresh replacement instance.
- return the replacement instance id/name and browser target for user manual E2E.
- leave final localization acceptance to the user.

## Required Hash Verification

The next gate must verify these runtime Control UI asset hashes in image and running-container readback:

| Asset | Required SHA-256 |
| --- | --- |
| `agents-_34Q844e.js` | `1cee67ec6347781b3bd965b77710241fc44a91f30f265053ab81d3b9fb4caea7` |
| `nodes-BBk4VzkK.js` | `25db132ab7efa57f47640d39fdd33bf10f0a75e4073b79cefc837754fa2424b4` |
| `index-M4TNVXB3.js` | `6063d70921c49ed7d5bacc04066e05a28e3efbe8239e93e564de902a732c69a6` |
| `skills-BRWdbtpV.js` | `36ec81b82b11995e9033a4c737814b65f0891e2534155429bd9515f9ad375a22` |
| `skills-shared-D6eRDyeb.js` | `f16051ca30ea6e74b308ec4c86f93bcad8f57112aa70ca9ae14211d59789c13b` |
| `config-form-x_UhxUYO.js` | `8e6ab9a3a394485eff7670cb79204d52a3c973c3febdb83eeb9c9d528518c245` |
| `zh-CN-B26mMdbY.js` | `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` |

## Required Safety Verification

The next gate must verify:

- `operator.read` plus `operator.pairing` retained.
- no operator.admin grant.
- no missing_scope bypass.
- no auth/scope modification.
- no insecure auth.
- no global bypass.
- no direct browser device-less allow.

## Manual E2E Acceptance Focus

User manual E2E should check:

- internal UI can be reached and chat ready is reached.
- 代理 page residuals are cleared for `Copy ID`, `Overview`, `Files`, `Tools`, `Skills`, `Channels`, `Cron Jobs`, `Core Files`, `Bootstrap persona`, `Workspace`, and `Select a file to edit`.
- 节点 page residuals are cleared for `Binding`, `Devices`, `No paired devices`, `Nodes`, and `No nodes found`.
- `main`, `AGENTS`, `SOUL`, `TOOLS`, `IDENTITY`, `USER`, `HEARTBEAT`, `BOOTSTRAP`, `MEMORY`, and `MISSING` may remain as code or file literals.
- dynamic metadata may be recorded as deferred residual.

## Packet Gate Non-Actions

This approval packet gate performed no delivery action:

- no build/tag/push
- no image pull
- no deploy
- no kubectl/k3d/Helm mutation
- no instance/database mutation
- no browser E2E
- no manual E2E
- no cleanup
- no JS/Dockerfile/MANIFEST/backend/runtime auth/scope modification
- no operator.admin grant
- no missing_scope bypass
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push
