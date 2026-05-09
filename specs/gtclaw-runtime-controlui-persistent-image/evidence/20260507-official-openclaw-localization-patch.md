# Official OpenClaw localization patch implementation

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Implementation Patch

Gate: CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH_GATE

Approval token used:

- APPROVE_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH_GATE

Dependency gates:

- CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH_APPROVAL_PACKET_DONE
- CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PLUGIN_DISCOVERY_DONE

## Verdict

CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH_DONE

This gate implemented only the approved static control-ui artifact overlay localization patch. It did not continue trustedProxy/runtime auth work, did not build an image, and did not perform plugin or skill distribution.

## Artifact paths

Source artifact:

- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/`

New artifact:

- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/`

Manifest:

- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/MANIFEST.md`

## Changed files

Changed relative to the source artifact:

- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/zh-CN-B26mMdbY.js`
- `specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/MANIFEST.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-official-openclaw-localization-patch.md`

Copied unchanged relative to the source artifact:

- `index.html`
- `assets/i18n-B06L7jQN.js`
- `assets/index-M4TNVXB3.js`

`assets/i18n-B06L7jQN.js` was not patched; it was copied unchanged because the static artifact needs the existing zh-CN loader chunk. `assets/index-M4TNVXB3.js` was not patched; it was copied unchanged to preserve the existing persistence wiring from the source artifact. No default-locale, title, or persistence rewiring was required.

## Patch summary

The patch keeps `index.html` title `GTClaw 控制台` and focuses localization edits on the `zh-CN` locale chunk.

Representative user-visible changes in `assets/zh-CN-B26mMdbY.js`:

- `Gateway` inside Chinese control-ui copy is localized as `网关`.
- `网关 token` is localized as `网关令牌`.
- connection setup copy now says `控制台 URL` where it referred to the dashboard URL.
- `Dreaming` status labels are localized as `梦境模式`.
- a remaining usage-detail label `Skills` is localized as `技能`.
- language option descriptions for several locales are localized into Chinese.

## Hash diff

| File | Source size | Source SHA-256 | New size | New SHA-256 | Result |
| --- | ---: | --- | ---: | --- | --- |
| `index.html` | 3398 | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | 3398 | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | unchanged |
| `assets/i18n-B06L7jQN.js` | 42617 | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | 42617 | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | unchanged |
| `assets/index-M4TNVXB3.js` | 708145 | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` | 708145 | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` | unchanged |
| `assets/zh-CN-B26mMdbY.js` | 23255 | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` | 23258 | `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` | localized |

## String coverage scan

Targeted zh-CN locale coverage after patch:

| Term | Count |
| --- | ---: |
| `GTClaw 控制台` | 1 |
| `连接` | 17 |
| `设置` | 16 |
| `技能` | 5 |
| `代理` | 19 |
| `实例` | 4 |
| `会话` | 44 |
| `聊天` | 8 |
| `配置` | 7 |
| `网关` | 22 |
| `终端` | 1 |
| `审批` | 1 |
| `日志` | 9 |
| `错误` | 10 |
| `断开` | 1 |
| `重新连接` | 1 |

Residual English/protocol terms in the changed locale chunk:

- `Gateway`: 0
- `Dreaming`: 0
- `Skills`: 0
- `token`: retained only as placeholder/key/LLM-token terminology, not as a gateway label

## Protected literal scan

The protected literal policy is recorded in `MANIFEST.md`.

Changed locale chunk protected literal counts:

| Literal | Count |
| --- | ---: |
| `openclaw` | 2 |
| `--no-open` | 1 |
| `docs/openclaw.json` | 0 |
| `.openclaw` | 0 |
| `control-ui` | 0 |

The preserved `openclaw` occurrences are compatibility literals in `openclaw.json` and `openclaw dashboard --no-open`. They were not localized.

## Verification commands

File and hash checks:

```bash
find specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization -type f | sort
shasum -a 256 specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/index.html specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/index.html specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/* specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/*
wc -c specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/index.html specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/index.html specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/* specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/*
```

String and protected literal checks:

```bash
rg -n "GTClaw|GTClaw 控制台|简体中文|连接|设置|技能|代理|实例|会话|聊天|配置|网关|终端|审批|日志|错误|断开|重新连接" specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization
rg -n "ClawManager Desktop|OpenClaw Control|OpenClaw 控制台" specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization || true
rg -n "docs/openclaw\\.json|--no-open|control-ui|\\.openclaw|openclaw" specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization
node --input-type=module - <<'NODE'
import { pathToFileURL } from 'node:url';
const mod = await import(pathToFileURL('specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/assets/zh-CN-B26mMdbY.js'));
const zh = mod.zh_CN;
console.log(zh.login.subtitle, zh.nav.settings, zh.usage.details.skills, zh.channels.gatewayUrlConfirmation.title, zh.overview.access.token, zh.dreaming.header.on);
NODE
```

Evidence and hygiene checks:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-official-openclaw-localization-patch.md
sed -n '1,360p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-official-openclaw-localization-patch.md
rg -n "CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH_DONE|CONTROLUI_OFFICIAL_OPENCLAW_LOCALIZATION_PATCH_BLOCKED|GTClaw|zh-CN|i18n|static control-ui artifact overlay|MANIFEST|SHA-256|protected literal|no trustedProxy patch|no plugin|no skill distribution|no build/tag/push|no browser E2E|no Mem0 write|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-official-openclaw-localization-patch.md
rg -n -i -o "(api[_-]?key|access[_-]?token|gateway[_-]?token|password|secret|authorization|bearer|jwt|private[_-]?key)[[:space:]]*[:=][[:space:]<>]*[^[:space:]<>]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-official-openclaw-localization-patch.md specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization | wc -l
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-official-openclaw-localization-patch.md
```

## Verification result summary

- Artifact file listing showed `MANIFEST.md`, `index.html`, `assets/i18n-B06L7jQN.js`, `assets/index-M4TNVXB3.js`, and `assets/zh-CN-B26mMdbY.js`.
- Hash checks confirmed `index.html`, `assets/i18n-B06L7jQN.js`, and `assets/index-M4TNVXB3.js` are unchanged from the source artifact.
- Hash checks confirmed `assets/zh-CN-B26mMdbY.js` changed from SHA-256 `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` to SHA-256 `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f`.
- String coverage scan found the required GTClaw, zh-CN, i18n, navigation, settings, connection, chat, agent, skill, instance, session, config, gateway, terminal, approval, log, error, disconnect, and reconnect terms.
- Rejected-brand scan found no `ClawManager Desktop`, `OpenClaw Control`, or `OpenClaw 控制台` strings in the new artifact.
- Protected literal scan found preserved `openclaw` and `--no-open` technical literals. These are compatibility identifiers, not display branding replacements.
- `node --input-type=module` imported the patched zh-CN chunk and resolved representative localized fields: `GTClaw 控制台`, `设置`, `技能`, `更改网关 URL`, `网关令牌`, and `梦境模式已开启`.
- `git diff --check` exited 0 for the new artifact and evidence file.
- Required evidence marker scan exited 0.
- Evidence-only secret-shape scan count was 0.
- Full artifact secret-shape scan count was 26. Manual review of the matched shapes found static UI labels, field names, and control-flow references in copied unchanged bundles, not plaintext token/password/key/cookie/bearer/auth header/access URL values.
- Scoped git status showed only the new localization artifact directory and this new evidence file as untracked in the requested path scope.

## Next gate recommendation

Recommended next gate:

`CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_APPROVAL_PACKET`

That gate should approve copying the new static artifact into a runtime image assembly context. It should still separately prohibit build/tag/push until a later explicit build gate.

## Forbidden actions statement

Forbidden actions were not executed. Specifically: no trustedProxy patch, no runtime auth contract patch, no backend modification, no frontend modification, no deployments modification, no docs modification, no longterm modification, no AgentTeam modification, no UnifiedFramework modification, no old artifact modification, no runtime startup artifact modification, no runtime image assembly artifact modification, no existing evidence modification, no plugin work, no skill distribution, no build/tag/push, no image pull, no container run, no browser E2E, no DevTools, no Playwright, no kubectl, no k3d, no Helm, no instance create/delete/modify, no database access or modification, no Mem0 write, no passes:true, no Close, and no git stage/commit/push.

No token, password, key, cookie, bearer material, JWT, authorization header plaintext, private key, or access URL plaintext was recorded.
