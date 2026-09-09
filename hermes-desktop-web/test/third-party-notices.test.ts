import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import { dependencyLicenseFile, fontAssets, noticeAssets, thirdPartyNotices } from '../scripts/third-party-notices.mjs'

const root = fileURLToPath(new URL('..', import.meta.url))

test('published notices preserve complete Hermes MIT, Codicons CC-BY attribution and the separate code license', () => {
  const assets = new Map(noticeAssets(root).map(asset => [asset.fileName, asset.source]))
  for (const [output, input] of [
    ['HERMES-LICENSE.txt', '.upstream/source/LICENSE'],
    ['HERMES-BOTS-LICENSE.txt', '.upstream/source/apps/desktop/src/plugins/hermes-bots/LICENSE'],
    ['CODICONS-LICENSE.txt', 'node_modules/@vscode/codicons/LICENSE'],
    ['CODICONS-LICENSE-CODE.txt', 'node_modules/@vscode/codicons/LICENSE-CODE'],
  ]) assert.equal(Buffer.from(assets.get(output)!).toString(), readFileSync(path.join(root, input), 'utf8'))
  const cc = String(assets.get('CODICONS-LICENSE.txt'))
  assert.match(cc, /Attribution 4\.0 International/)
  assert.match(cc, /Git Logo by \[Jason Long\]/)
  assert.match(cc, /creativecommons\.org\/licenses\/by\/3\.0/)
  const notice = String(assets.get('THIRD-PARTY-NOTICES.md'))
  assert.match(notice, /Microsoft Corporation/)
  assert.match(notice, /redistributed unchanged/)
  assert.match(notice, /emojibase-data/)
  assert.match(notice, /KaTeX/)
})

test('module and static-asset coverage records available license files without inventing missing publisher grants', () => {
  const assets = new Map(noticeAssets(root, [path.join(root, 'node_modules/react/index.js')]).map(asset => [asset.fileName, asset.source]))
  const coverage = JSON.parse(String(assets.get('license-coverage.json')))
  assert.ok(coverage.packages.some((entry: { name: string; legal_files: string[] }) => entry.name === 'react' && entry.legal_files.includes('LICENSE')))
  assert.ok(coverage.packages.some((entry: { name: string; legal_files: string[] }) => entry.name === '@vscode/codicons' && entry.legal_files.includes('LICENSE-CODE')))
  assert.ok(coverage.missing_publisher_text.includes('@nous-research/ui@0.18.2'))
  assert.ok(!JSON.stringify(coverage).includes(root))
  assert.equal(coverage.dependency_report, dependencyLicenseFile)
})

test('license report uses a visible root static path and build hooks require a nonempty real dependency report', () => {
  assert.equal(dependencyLicenseFile, 'THIRD-PARTY-LICENSES.md')
  const config = readFileSync(path.join(root, 'vite.config.ts'), 'utf8')
  assert.match(config, /license: \{ fileName: dependencyLicenseFile \}/)
  const plugin = thirdPartyNotices(root)
  assert.ok(plugin.generateBundle)
  assert.ok(plugin.writeBundle)
  assert.ok(plugin.transformIndexHtml)
  const nginx = readFileSync(path.join(root, '../deployments/nginx/nginx.conf'), 'utf8')
  assert.match(nginx, /location \^~ \/hermes-desktop-web\//)
  assert.ok(nginx.includes('try_files $uri $uri/ =404;'))
})

test('font notices preserve separate OFL grants, copyright and reserved names instead of applying package MIT to fonts', () => {
  const { fonts, notice } = fontAssets(root)
  assert.ok(fonts.some(font => font.filename === 'JetBrainsMono-Regular.woff2'))
  assert.ok(fonts.some(font => font.filename === 'KaTeX_Main-Regular.ttf'))
  assert.ok(fonts.some(font => font.filename === 'codicon.ttf'))
  assert.ok(fonts.every(font => /^[a-f0-9]{64}$/.test(font.sha256) && !font.filename.includes('Collapse')))
  for (const text of ['Copyright 2020 The JetBrains Mono Project Authors', 'Reserved Font Name KaTeX_Main', 'Design Science', 'Khan Academy', 'SIL OPEN FONT LICENSE Version 1.1', 'PERMISSION & CONDITIONS', 'DISCLAIMER']) assert.ok(notice.includes(text), text)
})
