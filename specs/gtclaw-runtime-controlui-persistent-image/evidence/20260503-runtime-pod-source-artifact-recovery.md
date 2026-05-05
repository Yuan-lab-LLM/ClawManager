# Runtime Pod Source Artifact Recovery Evidence - 2026-05-03

## Verdict

RUNTIME_POD_SOURCE_ARTIFACT_RECOVERY_DONE

The prior SourceArtifactRecoveryWorker result was BLOCKED because local artifact search and the historical T6 image did not recover the required after-hash source artifact. Commander/user then explicitly approved read-only pod access to the known repaired runtime pod and local writes only under `/tmp/gtclaw-runtime-patch` and `/tmp/gtclaw-runtime-pod-recovery-*`.

This packet records source artifact recovery only. It does not prove persistent image delivery, built artifact hash extraction, fresh deployed pod hash extraction, browser E2E, or Close readiness.

## Dependency Gate

Prior blocked evidence:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-source-artifact-recovery.md`

Current unblock source was limited to this repaired runtime pod:

| Field | Value |
| --- | --- |
| Namespace | `clawmanager-user-1` |
| Pod | `clawreef-3-gtclaw-t8-dev-20260501001159` |
| Container | `desktop` |
| Source path | `/usr/local/lib/node_modules/openclaw/dist/control-ui` |
| Access mode | read-only pod access |

The forbidden `/opt/opensparrow/runtime/openclaw/dist/control-ui` path was not used.

## Pod Read Source Used

Four allowlist files were copied from the repaired runtime pod to a temporary local workspace:

`/tmp/gtclaw-runtime-pod-recovery-sZCKho`

The local recovery target was then rebuilt as:

`/tmp/gtclaw-runtime-patch`

Only these four allowlist files were copied:

| Recovered file |
| --- |
| `index.html` |
| `assets/i18n-B06L7jQN.js` |
| `assets/zh-CN-B26mMdbY.js` |
| `assets/index-M4TNVXB3.js` |

## /tmp/gtclaw-runtime-patch File List

Command:

```bash
find /tmp/gtclaw-runtime-patch -maxdepth 3 -type f | sort
```

Result:

```text
/tmp/gtclaw-runtime-patch/assets/i18n-B06L7jQN.js
/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js
/tmp/gtclaw-runtime-patch/assets/zh-CN-B26mMdbY.js
/tmp/gtclaw-runtime-patch/index.html
```

## Hash/Size Verification

| File | SHA-256 | Size |
| --- | --- | ---: |
| `index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` |
| `assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | `42617` |
| `assets/zh-CN-B26mMdbY.js` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` | `23255` |
| `assets/index-M4TNVXB3.js` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` | `707959` |

Result: all four files match the required after-hash and byte-size table.

## Allowlist-Only Verification

Command:

```bash
find /tmp/gtclaw-runtime-patch -maxdepth 3 -type f | sed 's#^/tmp/gtclaw-runtime-patch/##' | sort | comm -3 - <(printf '%s\n' assets/i18n-B06L7jQN.js assets/index-M4TNVXB3.js assets/zh-CN-B26mMdbY.js index.html | sort)
```

Result:

```text

```

Interpretation: empty output confirms no allowlist outside file is present.

## Confirmation

- pod read-only: yes
- no pod write: yes
- no `kubectl cp` write into pod: yes
- no runtime pod/container file mutation: yes
- no runtime process restart: yes
- no Kubernetes resource, Secret, ConfigMap, namespace, or database mutation: yes
- no image build/tag/push/pull/inspect: yes
- no registry mutation: yes
- no fresh instance: yes
- no browser E2E: yes
- no broad OpenClaw to GTClaw replacement: yes
- no token/cookie/secret/token-bearing URL recorded: yes
- no passes:true: yes
- no Close: yes
- no Mem0 or longterm write: yes

## Verification Command Results

Temporary source workspace hash/size check:

```text
b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec  3398  index.html
3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63  42617  assets/i18n-B06L7jQN.js
37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809  23255  assets/zh-CN-B26mMdbY.js
1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299  707959  assets/index-M4TNVXB3.js
```

Final target hash/size check:

```text
b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec  3398  index.html
3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63  42617  assets/i18n-B06L7jQN.js
37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809  23255  assets/zh-CN-B26mMdbY.js
1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299  707959  assets/index-M4TNVXB3.js
```

## File Written

Only this repository file was written:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-runtime-pod-source-artifact-recovery.md`

Local artifact output written:

`/tmp/gtclaw-runtime-patch`

Temporary workspace written:

`/tmp/gtclaw-runtime-pod-recovery-sZCKho`

## Scope Statement

This recovery task wrote only the new evidence file, `/tmp/gtclaw-runtime-patch/**`, and a temporary workspace under `/tmp/gtclaw-runtime-pod-recovery-*`.

No existing evidence was modified. `spec.md`, `plan.md`, `tasks.md`, `backend/**`, `frontend/**`, `deployments/**`, `docs/**`, `longterm/**`, `AgentTeam/**`, runtime pod/container files, Kubernetes resources, Secrets, ConfigMaps, namespaces, database, image tags, registry state, `.codex/auth.json`, and `.codex/config.toml` were not modified.

## Gate Statement

source artifact recovery only, read-only pod access plus local `/tmp` artifact write, no image build/tag/push/pull/inspect, no runtime/K8S/database mutation, no registry mutation, no fresh instance, no browser E2E, no passes:true, no Close.
