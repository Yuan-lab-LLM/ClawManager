# Runtime Control UI Source Artifact Recovery Gate - 2026-05-06

Worker: RuntimeControlUISourceArtifactRecoveryWorker

Verdict: `RUNTIME_CONTROL_UI_SOURCE_ARTIFACT_RECOVERY_DONE`

Not `RUNTIME_CONTROL_UI_SOURCE_ARTIFACT_RECOVERY_BLOCKED`: the approved source files existed, their source SHA-256 and size matched the approval packet values, the destination root is repo-owned, and byte-for-byte equality was proven after copy.

This gate performed only repo-owned artifact recovery by byte-for-byte copy. It did not implement the persisted gatewayUrl fix, did not modify copied file content, did not modify `/tmp/gtclaw-runtime-patch/**`, did not copy any file outside the four approved files, did not build/tag/push/pull, did not deploy, did not restart backend, did not create/delete/modify a fresh instance, did not mutate K8S/runtime/database/registry state, did not run browser E2E, did not clear browser storage/cache/cookies, did not edit a WebSocket URL, did not enter a gateway token/password/key, did not write Mem0 or longterm, did not set `passes:true`, did not Close, and did not git stage/commit/push.

## Dependency Gate Record

| Dependency | Status used |
| --- | --- |
| Runtime Control UI Source Artifact Recovery Approval Packet | `RUNTIME_CONTROL_UI_SOURCE_ARTIFACT_RECOVERY_APPROVAL_PACKET_DONE` |
| Control UI Runtime Persistence Fix Implementation Gate | `CONTROL_UI_RUNTIME_PERSISTENCE_FIX_IMPLEMENTATION_BLOCKED` |

Dependency blocker carried forward:

- The implementation gate had no writable repo-owned runtime control-ui artifact/build context containing the persisted gatewayUrl logic.
- The only located bundle was `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js`.
- `/tmp/gtclaw-runtime-patch/**` remained read-only reference material for this gate.

## Recovered Repo-owned Artifact

Destination artifact root:

`specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source`

Files present after recovery:

```text
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/i18n-B06L7jQN.js
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/zh-CN-B26mMdbY.js
specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/index.html
```

Only the target directory and `assets/` subdirectory were created for this artifact. Only the four approved files were copied.

## Exact Copy Command

```bash
set -e
mkdir -p specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets
cp -p /tmp/gtclaw-runtime-patch/index.html specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/index.html
cp -p /tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js
cp -p /tmp/gtclaw-runtime-patch/assets/i18n-B06L7jQN.js specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/i18n-B06L7jQN.js
cp -p /tmp/gtclaw-runtime-patch/assets/zh-CN-B26mMdbY.js specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/zh-CN-B26mMdbY.js
```

## Byte-for-byte Copy Evidence

| Source path | Destination path | source mode | destination mode | source size | destination size | source sha256 | destination sha256 |
| --- | --- | ---: | ---: | ---: | ---: | --- | --- |
| `/tmp/gtclaw-runtime-patch/index.html` | `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/index.html` | `0644` | `0644` | `3398` | `3398` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| `/tmp/gtclaw-runtime-patch/assets/index-M4TNVXB3.js` | `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js` | `0644` | `0644` | `707959` | `707959` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` |
| `/tmp/gtclaw-runtime-patch/assets/i18n-B06L7jQN.js` | `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/i18n-B06L7jQN.js` | `0644` | `0644` | `42617` | `42617` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `/tmp/gtclaw-runtime-patch/assets/zh-CN-B26mMdbY.js` | `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/zh-CN-B26mMdbY.js` | `0644` | `0644` | `23255` | `23255` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` |

Literal conclusion: `no modifications to copied content during recovery`

## Repo-owned Artifact Proof

Repository root realpath:

`/Users/eduardogan/Desktop/GHJProject/ClawManager`

Destination root realpath:

`/Users/eduardogan/Desktop/GHJProject/ClawManager/specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source`

Conclusion: the destination root is located under the repository root, so it is a repo-owned artifact path.

Path-limited destination status after recovery:

```text
?? specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/
```

## Stop Conditions Checked

| Stop condition | Result |
| --- | --- |
| Any source file missing | Not triggered. All four approved source files were present. |
| Source hash or size differs from approval packet | Not triggered. All source SHA-256 and size values matched approval packet values. |
| Destination not in repo | Not triggered. Destination root realpath is under repository root realpath. |
| Byte-for-byte equality cannot be proven | Not triggered. All destination SHA-256 and size values exactly match source and approval packet values. |

## Explicit Negatives

- no copied file content modification
- no persisted gatewayUrl bug patch
- no implementation
- no `/tmp/gtclaw-runtime-patch/**` modification
- no copy outside the four approved files
- no backend modification
- no frontend modification
- no runtime-startup-artifact modification
- no deployments modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec/plan/tasks modification
- no existing evidence modification
- no build/tag/push/pull
- no backend deploy/restart
- no fresh instance create/delete/modify
- no K8S mutation
- no runtime mutation
- no database mutation
- no registry mutation
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no localStorage cleanup
- no sessionStorage cleanup
- no IndexedDB cleanup
- no cache cleanup
- no cookies cleanup
- no manual WebSocket URL edit
- no gateway token/password/key entry
- no secrets/token/cookie/access URL plaintext output
- no Mem0
- no longterm write-back
- no passes:true
- no Close
- no git stage/commit/push

## Recommended Next Gate

Recommended next gate after user approval:

`Control UI Runtime Persistence Fix Implementation Gate rerun`

The rerun may use the recovered repo-owned artifact as the writable target if explicitly approved. This recovery evidence does not authorize that implementation gate by itself.

## Verification Plan

Required checks for this recovery gate:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-control-ui-source-artifact-recovery.md
shasum -a 256 for source and destination files; all pairs must match approval packet values
stat or ls -l for source/destination mode and size
rg -n "RUNTIME_CONTROL_UI_SOURCE_ARTIFACT_RECOVERY_DONE|RUNTIME_CONTROL_UI_SOURCE_ARTIFACT_RECOVERY_BLOCKED|byte-for-byte copy|repo-owned artifact|source sha256|destination sha256|no modifications to copied content during recovery|index-M4TNVXB3.js|i18n-B06L7jQN.js|zh-CN-B26mMdbY.js|no implementation|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-control-ui-source-artifact-recovery.md
secret-shape scan with matched values suppressed over destination artifact + evidence
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-control-ui-source-artifact-recovery.md
```

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-control-ui-source-artifact-recovery.md` | `0` | No whitespace errors. |
| source/destination SHA-256 and size comparison against approval packet values | `0` | All four pairs matched the approval packet SHA-256 and size values. |
| source/destination `stat` mode and size check | `0` | All four source and destination files have mode `0644`; sizes match exactly. |
| required marker `rg` scan | `0` | Required markers found, including DONE/BLOCKED verdicts, `byte-for-byte copy`, `repo-owned artifact`, `source sha256`, `destination sha256`, `no modifications to copied content during recovery`, `index-M4TNVXB3.js`, `i18n-B06L7jQN.js`, `zh-CN-B26mMdbY.js`, `no implementation`, `no build/tag/push/pull`, `no browser E2E`, `no passes:true`, and `no Close`. |
| destination file count | `0` | Exactly four files are present under the recovered artifact root. |
| destination directory list | `0` | Only the artifact root and `assets/` directory exist under the recovered artifact root. |
| secret-shape scan with matched values suppressed over destination artifact + evidence | `0` | `secret_shape_match_count=26`; matched values were suppressed and no live secret, token, cookie, credential, or access URL value was printed. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-control-ui-source-artifact-recovery.md` | `0` | Shows only the new recovered artifact directory and this new recovery evidence in the requested path scope. |
