# Source Artifact Recovery Evidence - 2026-05-03

## Verdict

BLOCKED

`SOURCE_ARTIFACT_RECOVERY_DONE` was not reached because no approved local source artifact matched the required four allowlist after-hashes.

The recovery target remains unavailable:

`/tmp/gtclaw-runtime-patch`

No before-hash candidate was copied into `/tmp/gtclaw-runtime-patch`, and no content was manually rewritten to force a hash match.

## Gate Statement

Source artifact recovery only. This packet records recovery evidence only: no image build, no image tag, no image push, no image pull, no image inspect, no registry mutation, no runtime/K8S/database mutation, no fresh instance, no browser E2E, no passes:true, no Close.

## Dependency Gate

T017-T021 image delivery loop was already executed and BLOCKED because `/tmp/gtclaw-runtime-patch` was missing.

Blocked evidence:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-image-delivery-loop.md`

Current authorization was limited to recovering `/tmp/gtclaw-runtime-patch` source artifact and writing this recovery evidence.

## Required Four Allowlist Files

Only these four allowlist files were eligible for recovery:

| File |
| --- |
| `index.html` |
| `assets/i18n-B06L7jQN.js` |
| `assets/zh-CN-B26mMdbY.js` |
| `assets/index-M4TNVXB3.js` |

## Required After-Hash Table

| File | Required SHA-256 | Required size |
| --- | --- | ---: |
| `index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` |
| `assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | `42617` |
| `assets/zh-CN-B26mMdbY.js` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` | `23255` |
| `assets/index-M4TNVXB3.js` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` | `707959` |

## Recovery Sources Checked

| Source | Method | Result |
| --- | --- | --- |
| `/tmp/gtclaw-runtime-patch*`, `/tmp/gtclaw-runtime-baseline*`, and related `/tmp` control-ui paths | local filesystem search | no recoverable after-hash source found |
| `/Users/eduardogan/Desktop/GHJProject` source/vendor/artifact paths | local filename search for the four allowlist names | no after-hash source found |
| Local historical T6 image artifact `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029` | read-only `docker save` to `/tmp/gtclaw-runtime-patch-recovery-20260503233048/t6-image.tar`, offline layer listing/extraction only | target paths existed, but extracted content matched before-hashes, not required after-hashes |
| `/private/tmp/gtclaw-controlui-body-triage-current/control-ui` | local filesystem hash/size check | four target names existed, but content matched before-hashes |
| pnpm cache `openclaw@2026.4.14` control-ui path | local filesystem hash/size check | no full four-file after-hash match |
| `/Users/eduardogan`, `/private/tmp`, and `/var/folders` size-filtered scan | candidate size scan followed by SHA-256 comparison for the four required after-hashes | no exact after-hash file found |

## Candidate Hash Evidence

Historical T6 image artifact extraction found the target four paths in local OCI layers, but they did not satisfy the after-hash gate:

| File | Extracted SHA-256 | Extracted size | Classification |
| --- | --- | ---: | --- |
| `index.html` | `ed3560d9fa9b9156e62a405bc185c2d3495129ee3712ef8c536767f79d5778c7` | `3395` | before-hash |
| `assets/i18n-B06L7jQN.js` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` | `42702` | before-hash |
| `assets/zh-CN-B26mMdbY.js` | `9a4ecc8992d00443ef59de0be41090099d5a1feb25cf062c5c02470044277f29` | `23248` | before-hash |
| `assets/index-M4TNVXB3.js` | `eb8379dce913df535806486c8a4f637ab27f069c95925ef836821fbde2cb321b` | `707543` | before-hash |

The triage directory `/private/tmp/gtclaw-controlui-body-triage-current/control-ui` showed the same before-hash set and was also rejected.

## Recovery Target State

`/tmp/gtclaw-runtime-patch` was not created or populated because the exact four after-hash files were not found in an approved local source.

File list command result:

```text
/tmp/gtclaw-runtime-patch MISSING
```

Hash/size verification result:

```text
BLOCKED before target hash verification because /tmp/gtclaw-runtime-patch is missing.
```

Allowlist-only verification:

```text
BLOCKED; no target directory exists. No allowlist outside file was introduced.
```

## Runtime Pod Access

Runtime pod was accessed: no.

No running pod was read or copied from. No runtime pod/container files were read or modified. No `/opt/opensparrow/runtime/openclaw/dist/control-ui` source was used.

## Verification Commands

Commands run or to be run for this evidence gate:

```bash
find /tmp/gtclaw-runtime-patch -maxdepth 3 -type f | sort
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-source-artifact-recovery.md
rg -n "SOURCE_ARTIFACT_RECOVERY_DONE|BLOCKED|/tmp/gtclaw-runtime-patch|b26c425c|3c025ee1|37337fec|1dab28c0|four allowlist|no image build|no registry mutation|no fresh instance|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-source-artifact-recovery.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-source-artifact-recovery.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-image-delivery-loop.md
```

## Scope Statement

Files written by this recovery task:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-source-artifact-recovery.md`
- temporary recovery workspace under `/tmp/gtclaw-runtime-patch-recovery-20260503233048`

No existing evidence was modified. `spec.md`, `plan.md`, `tasks.md`, `backend/**`, `frontend/**`, `deployments/**`, `docs/**`, `longterm/**`, `AgentTeam/**`, runtime pod/container files, Kubernetes resources, Secrets, ConfigMaps, namespaces, database, image tags, registry state, `.codex/auth.json`, and `.codex/config.toml` were not modified.

## Stop Condition

The only source that can satisfy the current recovery target is a local artifact containing all four required after-hash files, or a separately approved runtime pod read/copy path. Under the current gate, runtime pod read access is not authorized, so recovery remains BLOCKED.
