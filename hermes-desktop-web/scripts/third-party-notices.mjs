import { existsSync, readFileSync, readdirSync } from 'node:fs'
import path from 'node:path'
import { createHash } from 'node:crypto'

export const dependencyLicenseFile = 'THIRD-PARTY-LICENSES.md'
const legalName = /^(?:licen[cs]e|copying|notice|ofl|third[-_ ]party)/i
const staticPackages = ['@vscode/codicons', 'emojibase-data', 'katex', '@nous-research/ui']

function sfntLicenseNames(bytes) {
  let offset
  for (let index = 0; index < bytes.readUInt16BE(4); index++) {
    const record = 12 + index * 16
    if (bytes.toString('ascii', record, record + 4) === 'name') offset = bytes.readUInt32BE(record + 8)
  }
  if (offset === undefined) throw new Error('Pinned font has no name table')
  const base = offset + bytes.readUInt16BE(offset + 4)
  const names = new Set()
  for (let index = 0; index < bytes.readUInt16BE(offset + 2); index++) {
    const record = offset + 6 + index * 12
    if (bytes.readUInt16BE(record + 6) !== 13) continue
    const at = base + bytes.readUInt16BE(record + 10)
    const text = Buffer.from(bytes.subarray(at, at + bytes.readUInt16BE(record + 8)))
    const platform = bytes.readUInt16BE(record)
    names.add(platform === 0 || platform === 3 ? text.swap16().toString('utf16le') : text.toString('latin1'))
  }
  return [...names].join('\n\n')
}

export function fontAssets(root) {
  const fonts = []
  for (const directory of ['node_modules/katex/dist/fonts', '.upstream/source/apps/desktop/src/fonts', 'node_modules/@vscode/codicons/dist']) {
    for (const filename of readdirSync(path.join(root, directory)).sort()) {
      if (!/\.(?:ttf|woff2?)$/.test(filename)) continue
      const bytes = readFileSync(path.join(root, directory, filename))
      fonts.push({ filename, sha256: createHash('sha256').update(bytes).digest('hex') })
    }
  }
  let notice = '# Bundled font licenses\n\n' +
    'Fonts are copied byte-for-byte; Vite only gives asset files cache-busting names. The corresponding file hashes are in font-coverage.json. Commercial fonts from @nous-research/ui (including Collapse) are excluded.\n\n' +
    '## JetBrains Mono (Regular, Bold, Italic)\n\n' +
    'Copyright 2020 The JetBrains Mono Project Authors (https://github.com/JetBrains/JetBrainsMono)\n\n' +
    'The bundled font name table declares: This Font Software is licensed under the SIL Open Font License, Version 1.1. This license is available with a FAQ at: https://openfontlicense.org\n\n' +
    '## KaTeX fonts\n\nThe following copyright, Reserved Font Name and license declarations are extracted from name ID 13 of each installed KaTeX TTF, covering the corresponding unchanged TTF/WOFF/WOFF2 faces. These fonts have their own OFL terms, separate from the KaTeX JavaScript MIT license.\n'
  const directory = path.join(root, 'node_modules/katex/dist/fonts')
  for (const filename of readdirSync(directory).filter(name => name.endsWith('.ttf')).sort()) {
    const grant = sfntLicenseNames(readFileSync(path.join(directory, filename)))
    if (!grant.includes('SIL Open Font License, Version 1.1') || !grant.includes('Reserved Font Name')) throw new Error(`Pinned KaTeX font license changed: ${filename}`)
    notice += `\n### ${filename}\n\n${grant}\n`
  }
  notice += '\n## Codicons\n\nMicrosoft Corporation. See CODICONS-LICENSE.txt and THIRD-PARTY-NOTICES.md for the full CC BY 4.0 terms and the publisher\'s Git Logo / Jason Long CC BY 3.0 attribution.\n\n' +
    '## SIL Open Font License 1.1 — complete terms\n\nThe text below is preserved from the font publisher\'s OFL file (https://github.com/JetBrains/JetBrainsMono/blob/v2.304/OFL.txt), with whitespace normalized; the copyright notices for the actual bundled fonts are listed above.\n\n' +
    readFileSync(path.join(root, 'scripts/notices/OFL-1.1.txt'), 'utf8')
  return { fonts, notice }
}

function packageForModule(id) {
  const filename = id.split('?')[0]
  if (filename.startsWith('\0') || !filename.replaceAll('\\', '/').includes('/node_modules/')) return null
  let directory = path.dirname(filename)
  while (directory !== path.dirname(directory)) {
    if (existsSync(path.join(directory, 'package.json'))) {
      const metadata = JSON.parse(readFileSync(path.join(directory, 'package.json'), 'utf8'))
      if (metadata.name) return directory
    }
    directory = path.dirname(directory)
  }
  return null
}

// Vite's built-in report records the first license file for bundled JS modules.
// Preserve every root legal notice too, including packages supplying only
// copied data/fonts. Missing publisher text is recorded, never invented.
export function noticeAssets(root, moduleIds = []) {
  const directories = new Set(staticPackages.map(name => path.join(root, 'node_modules', name)))
  for (const id of moduleIds) {
    const directory = packageForModule(id)
    if (directory) directories.add(directory)
  }
  const packages = [...directories].map(directory => {
    const metadata = JSON.parse(readFileSync(path.join(directory, 'package.json'), 'utf8'))
    const legalFiles = readdirSync(directory, { withFileTypes: true })
      .filter(entry => entry.isFile() && legalName.test(entry.name))
      .map(entry => ({ name: entry.name, text: readFileSync(path.join(directory, entry.name), 'utf8') }))
      .sort((a, b) => a.name.localeCompare(b.name))
    return { name: metadata.name, version: metadata.version, identifier: metadata.license ?? null, legalFiles }
  }).sort((a, b) => `${a.name}@${a.version}`.localeCompare(`${b.name}@${b.version}`))
  const codicons = packages.find(entry => entry.name === '@vscode/codicons')
  const codiconsLicense = codicons?.legalFiles.find(file => file.name === 'LICENSE')?.text
  const codiconsCode = codicons?.legalFiles.find(file => file.name === 'LICENSE-CODE')?.text
  if (!codiconsLicense?.includes('Attribution 4.0 International') || !codiconsLicense.includes('Jason Long') || !codiconsCode?.includes('Microsoft Corporation')) {
    throw new Error('Pinned Codicons CC-BY attribution or MIT code license is missing')
  }
  const hermes = readFileSync(path.join(root, '.upstream/source/LICENSE'))
  const hermesBots = readFileSync(path.join(root, '.upstream/source/apps/desktop/src/plugins/hermes-bots/LICENSE'))
  const fontInfo = fontAssets(root)
  const baseline = JSON.parse(readFileSync(path.join(root, 'upstream.lock.json'), 'utf8'))
  const summary = `# Third-party notices\n\n` +
    `This distribution embeds the original Hermes Desktop renderer with reviewed ClawManager browser adaptations.\n` +
    `Hermes source commit: ${baseline.commit}. Copyright (c) 2025 Nous Research. ` +
    `The complete [Hermes MIT license](HERMES-LICENSE.txt) and the in-tree [Hermes Bots MIT license](HERMES-BOTS-LICENSE.txt), Copyright (c) 2026 Nous Research, are preserved.\n\n` +
    `Vite generates [bundled dependency licenses](${dependencyLicenseFile}). This supplementary report preserves all publisher root legal files found in the imported module graph and explicitly copied data/font packages. ` +
    `See [machine-readable coverage](license-coverage.json). A package license identifier without publisher text is explicitly listed as such; it is not a new license grant or a complete legal clearance of separately licensed assets.\n\n` +
    `## Codicons attribution\n\n` +
    `Codicons ${codicons.version}, Microsoft Corporation: https://github.com/microsoft/vscode-codicons. ` +
    `The icon font is redistributed unchanged, with its stylesheet bundled and resource URLs rebased by Vite. ` +
    `[CC BY 4.0 terms](CODICONS-LICENSE.txt) / https://creativecommons.org/licenses/by/4.0/. ` +
    `The publisher also credits the Git Logo to Jason Long (https://bsky.app/profile/jasonlong.me), licensed under CC BY 3.0 (https://creativecommons.org/licenses/by/3.0/); that attribution is preserved verbatim in the same file. ` +
    `Codicons code has a separate [MIT license](CODICONS-LICENSE-CODE.txt). No endorsement by these authors is implied.\n\n` +
    `## Additional assets\n\n` +
    `[Bundled font licenses](FONT-LICENSES.md) preserve the separate JetBrains Mono and KaTeX font SIL OFL 1.1 grants, copyright notices and reserved names; [font hashes](font-coverage.json) identify the original bytes. ` +
    `emojibase-data is copied locally; KaTeX and Codicons fonts are emitted by the bundler. ` +
    `@nous-research/ui 0.18.2 declares MIT but its installed package supplies no LICENSE text. Its Collapse font carries separate Keussel/Blaze Type licensing metadata; without a confirmed redistribution grant, the font is deliberately EXCLUDED from this build. Existing sans-serif fallbacks are used without replacing the App/layout. ` +
    `Theme fontUrl values are restricted to same-origin HTTP(S); remote fonts are not downloaded or redistributed by this build. Existing bundled/system font-family fallbacks are retained.\n`
  const notices = summary + packages.map(entry =>
    `\n## ${entry.name} - ${entry.version} (${entry.identifier ?? 'publisher did not declare an identifier'})\n` +
    (entry.legalFiles.length ? entry.legalFiles.map(file => `\n### ${file.name}\n\n${file.text}\n`).join('') : '\nThe installed publisher package does not supply a root license/notice text. The identifier above is metadata only.\n'),
  ).join('')
  const coverage = {
    scope: 'bundled-module-graph-and-explicit-static-packages',
    dependency_report: dependencyLicenseFile,
    static_packages: staticPackages,
    packages: packages.map(({ legalFiles, ...metadata }) => ({ ...metadata, legal_files: legalFiles.map(file => file.name) })),
    missing_publisher_text: packages.filter(entry => !entry.legalFiles.length).map(entry => `${entry.name}@${entry.version}`),
    separately_licensed_asset_review: 'Package metadata alone does not establish rights to every embedded font or image.',
  }
  return [
    { fileName: 'HERMES-LICENSE.txt', source: hermes },
    { fileName: 'HERMES-BOTS-LICENSE.txt', source: hermesBots },
    { fileName: 'CODICONS-LICENSE.txt', source: codiconsLicense },
    { fileName: 'CODICONS-LICENSE-CODE.txt', source: codiconsCode },
    { fileName: 'THIRD-PARTY-NOTICES.md', source: notices },
    { fileName: 'FONT-LICENSES.md', source: fontInfo.notice },
    { fileName: 'font-coverage.json', source: JSON.stringify(fontInfo.fonts, null, 2) + '\n' },
    { fileName: 'license-coverage.json', source: JSON.stringify(coverage, null, 2) + '\n' },
  ]
}

/** @returns {import('vite').Plugin} */
export function thirdPartyNotices(root) {
  return {
    name: 'clawmanager:desktop-third-party-notices',
    transformIndexHtml() {
      return [{ tag: 'link', attrs: { rel: 'license', href: './THIRD-PARTY-NOTICES.md' }, injectTo: 'head' }]
    },
    generateBundle(_options, bundle) {
      const allowedFonts = new Set(fontAssets(root).fonts.map(font => font.sha256))
      for (const asset of Object.values(bundle)) {
        if (asset.type !== 'asset' || !/\.(?:ttf|woff2?|otf)$/i.test(asset.fileName)) continue
        if (!allowedFonts.has(createHash('sha256').update(asset.source).digest('hex'))) {
          throw new Error(`Unreviewed or modified font reached the output: ${asset.fileName}`)
        }
      }
      for (const asset of noticeAssets(root, [...this.getModuleIds()])) this.emitFile({ type: 'asset', ...asset })
    },
    writeBundle(options) {
      const directory = options.dir ?? path.join(root, 'dist')
      const generated = readFileSync(path.join(directory, dependencyLicenseFile), 'utf8')
      for (const name of ['react', 'react-dom', '@assistant-ui/core', '@assistant-ui/store', '@assistant-ui/tap']) {
        if (!generated.includes(`## ${name} - `)) throw new Error(`Vite bundled license report omitted ${name}`)
      }
    },
  }
}
