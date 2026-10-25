import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { adaptations, applyAdaptation } from '../scripts/web-adaptations.mjs'

async function patched(file: string) {
  const adaptation = adaptations.find((entry: { file: string }) => entry.file === file)
  const original = await readFile(new URL(`../.upstream/source/apps/desktop/src/${file}`, import.meta.url), 'utf8')
  return ts.createSourceFile(file, adaptation ? applyAdaptation(original, adaptation) : original, ts.ScriptTarget.ESNext, true, ts.ScriptKind.TSX)
}

// Execute the actual patched declarations without importing the native stores.
// Any missed early gate would touch the unresolved native identifiers and fail.
async function declaration(file: string, name: string, notices: Error[] = []) {
  const source = await patched(file)
  const statement = source.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === name)
  assert.ok(statement, name)
  const output = ts.transpileModule(statement.getText(source).replace(/^export\s+/, ''), {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext, jsx: ts.JsxEmit.React },
  }).outputText
  return new Function('cmWebNotifyError', `${output}; return ${name}`)((error: Error) => notices.push(error)) as (...args: unknown[]) => unknown
}

test('fixed-source background gates never poll native pet, paths, project trees or repositories', async () => {
  const cases: Array<[string, string, unknown[]]> = [
    ['components/pet/floating-pet.tsx', 'FloatingPet', []],
    ['app/session/hooks/use-context-suggestions.ts', 'useContextSuggestions', [{}]],
    ['store/projects.ts', 'refreshProjects', []],
    ['store/projects.ts', 'refreshProjectTree', []],
    ['store/projects.ts', 'scanAndRecordRepos', []],
  ]
  for (const [file, name, args] of cases) {
    const fn = await declaration(file, name)
    const result = await fn(...args)
    assert.ok(result === undefined || result === null, `${name} must not fabricate a Runtime payload`)
  }
})

test('managed browser settings retain the complete settings navigation', async () => {
  const source = await patched('app/settings/index.tsx')
  const text = source.getFullText()

  assert.match(text, /SECTIONS\.map\(s => `config:\$\{s\.id\}`/)
  assert.match(text, /'providers'[\s\S]*'gateway'[\s\S]*'keys'[\s\S]*'plugins'/)
  assert.match(text, /useRouteEnumParam\('tab', SETTINGS_VIEWS, 'config:model'/)
  assert.doesNotMatch(text, /SETTINGS_VIEWS\.includes\(item\.id as SettingsViewId\)/)
  assert.match(text, /<OverlayNav footer=\{navFooter\}/)
  assert.match(text, /edgeBadge=\{searchPill\}/)

  const appearance = (await patched('app/settings/appearance-settings.tsx')).getFullText()
  assert.match(appearance, /<MarketplaceThemeResults /)
  assert.match(appearance, /action=\{<LanguageSwitcher \/>\}/)
  assert.match(appearance, /<TerminalFontSetting \/>/)
  assert.match(appearance, /<PetSettings \/>/)

  const notifications = (await patched('app/settings/notifications-settings.tsx')).getFullText()
  const browserNotifications = notifications.slice(notifications.indexOf('export function NotificationsSettings'))
  assert.match(browserNotifications, /completionSoundTitle/)
  assert.match(browserNotifications, /setNativeNotifyEnabled/)
})
