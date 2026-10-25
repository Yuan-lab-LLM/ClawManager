import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const sourceRoot = path.join(root, '.upstream/source/apps/desktop/src')
const noticeImport = `import { ManagedFeatureNotice as CMWebUnavailable } from ${JSON.stringify(path.join(root, 'src/managed-feature-notice.tsx').replaceAll('\\', '/'))};\n`
const clarifyImport = `import { scopedClarifyParams as cmScopedClarify } from ${JSON.stringify(path.join(root, 'src/scoped-requests.ts').replaceAll('\\', '/'))};\n`
const notice = feature => `<CMWebUnavailable feature=${JSON.stringify(feature)} />`

// Deliberate Web capability gates, applied to verified bytes during compilation.
// Every anchor must match exactly once. No patch edits App/main/layout/routes,
// no DOM masking, and no weakening of Runtime authentication or acceptance.
export const adaptations = [
  {
    file: 'styles.css', reason: 'Do not redistribute the separately licensed Collapse font without a documented webfont grant; keep the original sans-serif fallback.',
    edits: [
      ["@font-face {\n  font-family: 'Collapse';\n  font-style: normal;\n  font-weight: 700;\n  font-display: swap;\n  src: url('../../../node_modules/@nous-research/ui/dist/fonts/Collapse-Bold.woff2') format('woff2');\n}", '/* CM Web: Collapse is not redistributed; existing font-family fallbacks remain in effect. */'],
      ['/* JetBrains Mono — bundled terminal font (Apache-2.0)', '/* JetBrains Mono — bundled terminal font (SIL OFL-1.1)']
    ]
  },
  {
    file: 'themes/context.tsx', reason: 'Theme stylesheets must stay same-origin; retain the original bundled/system font-family fallbacks without weakening CSP.',
    prepend: `function cmWebThemeFontUrl(value: string | undefined): string | null {
  if (!value || typeof window === 'undefined') return null
  try {
    const url = new URL(value, window.location.href)
    if (!['http:', 'https:'].includes(url.protocol) || url.origin !== window.location.origin || url.username || url.password) return null
    return url.href
  } catch { return null }
}
`,
    edits: [
      ["  if (typo.fontUrl && !INJECTED_FONT_URLS.has(typo.fontUrl)) {", "  const cmFontUrl = cmWebThemeFontUrl(typo.fontUrl)\n  if (cmFontUrl && !INJECTED_FONT_URLS.has(cmFontUrl)) {"],
      ['    link.href = typo.fontUrl', '    link.href = cmFontUrl'],
      ['    INJECTED_FONT_URLS.add(typo.fontUrl)', '    INJECTED_FONT_URLS.add(cmFontUrl)']
    ]
  },
  {
    file: 'components/assistant-ui/tool/approval.tsx', reason: 'Respect BFF deny-only recovery: do not offer Run or a keyboard approval when a replay cannot safely show the command.',
    edits: [
      ["  const allowSession = choices ? choices.includes('session') : true", "  const allowOnce = choices ? choices.includes('once') : true\n  const allowSession = false // CM never offers a standing approval."],
      ["  const allowAlways = choices ? choices.includes('always') : allowPermanent", "  const allowAlways = false // CM never offers permanent approval."],
      ["    async (choice: ApprovalChoice) => {", "    async (choice: ApprovalChoice) => {\n      if (choice !== 'deny' && (choice !== 'once' || !allowOnce)) return"],
      ["    [busy, copy.gatewayDisconnected, copy.sendFailed, gateway, request.requestId, request.sessionId]", "    [allowOnce, busy, copy.gatewayDisconnected, copy.sendFailed, gateway, request.requestId, request.sessionId]"],
      ["            disabled={busy}\n            onClick={() => void respond('once')}", "            disabled={busy || !allowOnce}\n            title={!allowOnce ? '恢复的审批未包含可安全展示的命令，请拒绝后重试。' : undefined}\n            onClick={() => void respond('once')}"]
    ]
  },
  {
    file: 'components/pet/floating-pet.tsx', reason: 'No pet Runtime capability is exposed to the browser; do not start pet.info polling.',
    edits: [['export function FloatingPet() {', 'export function FloatingPet() {\n  return null // Pet settings disclose this unavailable capability.']]
  },
  {
    file: 'app/settings/pet-settings.tsx', reason: 'Show the missing managed-browser pet capability explicitly instead of offering native actions.',
    prepend: noticeImport,
    edits: [['export function PetSettings() {', `export function PetSettings() {\n  return ${notice('桌宠资源与原生窗口')}`]]
  },
  {
    file: 'app/session/hooks/use-context-suggestions.ts', reason: 'Do not auto-enumerate filesystem paths through complete.path in the managed browser.',
    edits: [['}: ContextSuggestionsOptions) {', '}: ContextSuggestionsOptions) {\n  return // Runtime path discovery is not a CM Web capability.']]
  },
  {
    file: 'store/projects.ts', reason: 'Stop background project-tree reads and native repository discovery without inventing a successful empty Runtime response.',
    edits: [
      ['export async function refreshProjects(): Promise<void> {', 'export async function refreshProjects(): Promise<void> {\n  return // Project management belongs to the CM workspace.'],
      ['export async function refreshProjectTree(): Promise<void> {', 'export async function refreshProjectTree(): Promise<void> {\n  return // No project-tree polling in a managed browser.'],
      ['export async function scanAndRecordRepos(force = false): Promise<void> {', 'export async function scanAndRecordRepos(force = false): Promise<void> {\n  return // Never scan or register host repositories from this client.']
    ]
  },
  {
    file: 'app/chat/sidebar/project-dialog.tsx', reason: 'Keep the original project dialog entry but explain that native project/folder management is unavailable.',
    prepend: noticeImport,
    edits: [['export function ProjectDialog() {', 'function NativeProjectDialog() {']],
    append: '\nexport function ProjectDialog() {\n  const state = useStore($projectDialog)\n  return <Dialog open={state !== null} onOpenChange={open => { if (!open) closeProjectDialog() }}><DialogContent><DialogHeader><DialogTitle>ClawManager Web</DialogTitle><DialogDescription>项目与目录由 ClawManager 工作区管理。</DialogDescription></DialogHeader><CMWebUnavailable feature="原生项目 / 文件夹管理" /><DialogFooter><Button onClick={closeProjectDialog}>关闭</Button></DialogFooter></DialogContent></Dialog>\n}\n'
  },
  {
    file: 'store/session-states.ts', reason: 'Keep clarify request ownership explicit at the existing owner-routed dispatch seam.',
    prepend: clarifyImport,
    edits: [['  return requestForSessionProfile<T>(owner, ambientRequest, method, params, timeoutMs, signal)', "  if (method === 'clarify.respond') params = cmScopedClarify(sessionId, params)\n  return requestForSessionProfile<T>(owner, ambientRequest, method, params, timeoutMs, signal)"]]
  },
  {
    file: 'store/clarify.ts', reason: 'A composer skip must carry the same explicit session as its pending clarification.',
    prepend: clarifyImport,
    edits: [["await $gateway.get()?.request('clarify.respond', { request_id: request.requestId, answer: '' })", "await $gateway.get()?.request('clarify.respond', cmScopedClarify(request.sessionId, { request_id: request.requestId, answer: '' }))"]]
  },
  {
    file: 'store/quick-entry.ts', reason: 'Required bridge method declarations do not imply an available native global-hotkey window.',
    edits: [['export function canUseQuickEntry(): boolean {', 'export function canUseQuickEntry(): boolean {\n  return false // CM Web has no native Quick Entry window.']]
  },
  {
    file: 'app/session/hooks/use-session-actions/index.ts', reason: 'Create chat in the managed instance workspace, without enabling native Desktop tools.',
    edits: [["  return {\n    cols: 96,\n    source: 'desktop',\n    ...(cwd && { cwd }),", "  return {\n    cols: 96,\n    source: 'web', // Workspace selection belongs to the managed Runtime."]]
  },
  {
    file: 'store/pet-overlay.ts', reason: 'Keep in-page pet presentation, but do not open, restore or push state to an OS overlay.',
    prepend: "import { notifyError as cmWebNotifyError } from '@/store/notifications';\n",
    edits: [
      ['export function popOutPet(petRect: PetOverlayBounds): void {', 'export function popOutPet(petRect: PetOverlayBounds): void {\n  cmWebNotifyError(new Error("ClawManager Web 未开放原生桌宠窗口"), "Native overlay unavailable")\n  return'],
      ['export function restorePetOverlay(): void {', 'export function restorePetOverlay(): void {\n  return // No native overlay exists in a managed browser.'],
      ['export function initPetOverlayBridge(): () => void {', 'export function initPetOverlayBridge(): () => void {\n  return () => {} // No native window message producer.']
    ]
  },
  {
    file: 'app/contrib/hooks/use-quick-entry-bridge.ts', reason: 'No native global-hotkey window or outbound state push.',
    edits: [['export function useQuickEntryBridge({ startFreshSessionDraft, submitText }: QuickEntryBridgeParams): void {', 'export function useQuickEntryBridge({ startFreshSessionDraft, submitText }: QuickEntryBridgeParams): void {\n  return // Quick Entry is a native-window-only capability.']]
  },
  {
    file: 'app/contrib/hooks/use-pet-bridge.ts', reason: 'No native pet overlay window or outbound state push.',
    edits: [['export function usePetBridge({ requestGateway, resumeSession, submitText }: PetBridgeParams): void {', 'export function usePetBridge({ requestGateway, resumeSession, submitText }: PetBridgeParams): void {\n  return // Pet overlay is a native-window-only capability.']]
  },
  {
    file: 'contrib/runtime-loader.ts', reason: 'Do not evaluate runtime/plugin filesystem source in the Manager origin.',
    edits: [
      ['export function watchRuntimePlugins(): void {', 'export function watchRuntimePlugins(): void {\n  return // CM Web has no runtime code-loading capability.'],
      ['): Promise<null | string> {\n  installPluginSdk()', '): Promise<null | string> {\n  throw new Error("ClawManager Web does not execute runtime desktop plugins")\n  installPluginSdk()'],
      ['export const discoverRuntimePlugins = scanDiskPlugins', 'export const discoverRuntimePlugins = async () => { throw new Error("Runtime desktop plugins are unavailable in ClawManager Web") }']
    ]
  },
  {
    file: 'store/session-sync.ts', reason: 'No Electron-wide cross-user/instance browser channel.',
    edits: [["const channel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel(CHANNEL)", 'const channel: BroadcastChannel | null = null // Each CM frame is isolated.']]
  },
  {
    file: 'app/right-sidebar/terminal/persistent.tsx', reason: 'No native shell/PTY process capability in this renderer.',
    edits: [['export function PersistentTerminal({ onAddSelectionToChat }: PersistentTerminalProps) {', 'export function PersistentTerminal({ onAddSelectionToChat }: PersistentTerminalProps) {\n  return null // The original terminal pane displays a capability notice.']]
  },
  {
    file: 'app/contrib/surfaces.tsx', reason: 'Keep the original terminal pane/tab but clearly disable native execution.',
    prepend: noticeImport,
    edits: [['export const TerminalSurface = memo(function TerminalSurface() {', `export const TerminalSurface = memo(function TerminalSurface() {\n  return ${notice('本机终端 / Shell')}`]]
  },
  {
    file: 'app/contrib/panes.tsx', reason: 'No arbitrary host filesystem or Git access; CM Workspace remains the supported route.',
    prepend: noticeImport,
    edits: [
      ['export function FilesPane() {', `export function FilesPane() {\n  return ${notice('原生文件浏览器（请使用 ClawManager 工作区）')}`],
      ['export function ReviewPaneContent() {', `export function ReviewPaneContent() {\n  return ${notice('原生 Git / 代码审查')}`]
    ]
  },
  {
    file: 'app/chat/right-rail/preview-pane.tsx', reason: 'Electron webview/host preview is not an ordinary web iframe capability.',
    prepend: noticeImport,
    edits: [['export function PreviewPane({ embedded = false, onRestartServer, reloadRequest = 0, tabId, target }: PreviewPaneProps) {', `export function PreviewPane({ embedded = false, onRestartServer, reloadRequest = 0, tabId, target }: PreviewPaneProps) {\n  return ${notice('Electron 内嵌浏览器 / 原生预览')}`]]
  },
]

export function applyAdaptation(code, adaptation) {
  const marker = adaptation.file.endsWith('.css') ? `/* CM_WEB_REVIEWED:${adaptation.file} */\n` : `// CM_WEB_REVIEWED:${adaptation.file}\n`
  if (code.startsWith(marker)) throw new Error(`Reviewed Web adaptation anchor changed: ${adaptation.file}`)
  for (const [before, after] of adaptation.edits) {
    if (code.split(before).length !== 2) throw new Error(`Reviewed Web adaptation anchor changed: ${adaptation.file}`)
    code = code.replace(before, after)
  }
  return marker + (adaptation.prepend ?? '') + code + (adaptation.append ?? '')
}

export function webAdaptations() {
  return {
    name: 'clawmanager:reviewed-desktop-web-capabilities', enforce: 'pre',
    async buildStart() {
      for (const adaptation of adaptations) applyAdaptation(await readFile(path.join(sourceRoot, adaptation.file), 'utf8'), adaptation)
    },
    transform(code, id) {
      const relative = path.relative(sourceRoot, id.split('?')[0]).replaceAll('\\', '/')
      const adaptation = adaptations.find(entry => entry.file === relative)
      return adaptation ? { code: applyAdaptation(code, adaptation), map: null } : null
    }
  }
}
