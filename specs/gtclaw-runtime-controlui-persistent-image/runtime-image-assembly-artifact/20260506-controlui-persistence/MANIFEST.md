# Runtime Image Assembly Artifact Manifest

Artifact: `20260506-controlui-persistence`

Verdict scope: assembly artifact only; no build/tag/push/pull.

## Parent Image

- Parent host tag: `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656`
- Parent image index digest: `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`
- linux/arm64 digest: `sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97`

## Assembly Policy

- Dockerfile parent is digest-pinned to the prior origin-allowlist image index.
- Dockerfile platform is `linux/arm64`.
- Files are copied with `COPY --chmod=0644`.
- Destination root is `/usr/local/lib/node_modules/openclaw/dist/control-ui`.
- This artifact is only a repo-owned image assembly build context.
- This gate did not build, tag, push, pull, deploy, restart, or mutate runtime state.

## File Manifest

| Source path | Destination path | Mode | Size | SHA-256 |
|---|---|---:|---:|---|
| `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/index.html` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html` | `0644` | `3398` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js` | `0644` | `708145` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` |
| `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/i18n-B06L7jQN.js` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js` | `0644` | `42617` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/zh-CN-B26mMdbY.js` | `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js` | `0644` | `23255` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` |
