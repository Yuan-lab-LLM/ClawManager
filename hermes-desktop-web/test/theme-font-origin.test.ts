import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { adaptations, applyAdaptation } from '../scripts/web-adaptations.mjs'

async function themeFontLoader() {
  const file = 'themes/context.tsx'
  const original = await readFile(new URL(`../.upstream/source/apps/desktop/src/${file}`, import.meta.url), 'utf8')
  const adaptation = adaptations.find((entry: { file: string }) => entry.file === file)
  assert.ok(adaptation)
  const patched = applyAdaptation(original, adaptation)
  const source = ts.createSourceFile(file, patched, ts.ScriptTarget.ESNext, true, ts.ScriptKind.TSX)
  const helper = source.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === 'cmWebThemeFontUrl')
  assert.ok(helper)
  let resolved: ts.Node | undefined
  let injection: ts.Node | undefined
  const visit = (node: ts.Node) => {
    if (ts.isVariableStatement(node) && node.declarationList.declarations.some(row => row.name.getText(source) === 'cmFontUrl')) resolved = node
    if (ts.isIfStatement(node) && node.expression.getText(source) === 'cmFontUrl && !INJECTED_FONT_URLS.has(cmFontUrl)') injection = node
    ts.forEachChild(node, visit)
  }
  visit(source)
  assert.ok(resolved)
  assert.ok(injection)
  const code = ts.transpileModule(`${helper.getText(source)}\nreturn function(typo) { ${resolved.getText(source)}\n${injection.getText(source)} }`, {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext },
  }).outputText
  const links: Array<{ href?: string; rel?: string; dataset: Record<string, string> }> = []
  const seen = new Set<string>()
  const document = {
    createElement: (tag: string) => { assert.equal(tag, 'link'); return { dataset: {} } },
    head: { appendChild: (link: typeof links[number]) => links.push(link) },
  }
  const load = new Function('window', 'document', 'INJECTED_FONT_URLS', code)(
    { location: new URL('https://manager.example/hermes-desktop-web/?instance_id=1') }, document, seen,
  ) as (typo: { fontUrl?: string }) => void
  return { load, links, seen, original, patched }
}

test('theme stylesheet injection rejects cross-origin and non-web URLs before creating any DOM resource', async () => {
  const { load, links, seen } = await themeFontLoader()
  for (const fontUrl of [
    undefined, '', 'https://fonts.googleapis.com/css2?family=Courier+Prime', '//fonts.example/theme.css',
    'http://manager.example/font.css', 'https://manager.example:8443/font.css',
    'https://manager.example.attacker.invalid/font.css', 'https://user:pass@manager.example/font.css',
    'javascript:alert(1)', 'data:text/css,body{}', 'file:///font.css',
    'blob:https://manager.example/uuid', 'https://[invalid', '\\\\fonts.example\\theme.css',
  ]) load({ fontUrl })
  assert.deepEqual(links, [])
  assert.equal(seen.size, 0)
})

test('same-origin theme fonts are canonicalized and loaded once while bundled/system stacks remain intact', async () => {
  const { load, links, original, patched } = await themeFontLoader()
  load({ fontUrl: './fonts/theme.css' })
  load({ fontUrl: 'https://manager.example/hermes-desktop-web/fonts/theme.css' })
  load({ fontUrl: '/fonts/bundled.css' })
  assert.deepEqual(links.map(link => link.href), [
    'https://manager.example/hermes-desktop-web/fonts/theme.css', 'https://manager.example/fonts/bundled.css',
  ])
  for (const link of links) {
    assert.equal(link.rel, 'stylesheet')
    assert.equal(link.dataset.hermesThemeFont, 'true')
  }
  for (const line of ["    '--dt-font-sans': typo.fontSans,", "    '--dt-font-mono': typo.fontMono,"]) {
    assert.ok(original.includes(line))
    assert.ok(patched.includes(line))
  }
})

test('the exact locked stylesheet patch excludes the uncleared Collapse font, preserving licensed JetBrains and system fallback declarations', async () => {
  const adaptation = adaptations.find((entry: { file: string }) => entry.file === 'styles.css')
  assert.ok(adaptation)
  const original = await readFile(new URL('../.upstream/source/apps/desktop/src/styles.css', import.meta.url), 'utf8')
  const patched = applyAdaptation(original, adaptation)
  assert.ok(!patched.includes('Collapse-Bold.woff2'))
  for (const face of ['Regular', 'Bold', 'Italic']) assert.ok(patched.includes(`JetBrainsMono-${face}.woff2`))
  assert.ok(patched.includes('SIL OFL-1.1'))
  assert.ok(patched.includes('--font-sans: var(--dt-font-sans)'))
  assert.throws(() => applyAdaptation(original.replace('Collapse-Bold.woff2', 'Collapse-New.woff2'), adaptation), /anchor changed/)
})
