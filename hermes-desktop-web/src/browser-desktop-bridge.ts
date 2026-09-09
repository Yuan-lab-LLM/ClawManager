import type {} from '../.upstream/source/apps/desktop/src/global'
import type { DesktopBootProgress, HermesConnection, HermesWindowState } from '../.upstream/source/apps/desktop/src/global'
import { createBridge, type BrowserBridge } from './bridge'

type DesktopBridge = Window['hermesDesktop']
type BrowserEnvironment = {
  window?: Window | null
  document?: Document | null
  clipboard?: Pick<Clipboard, 'readText' | 'writeText'> | null
  now?: () => number
}

/** Capabilities of THIS client, not capabilities of the remote Runtime. */
export const BROWSER_DESKTOP_CAPABILITIES = Object.freeze({
  managedConnection: true,
  defaultProfileOnly: true,
  nativeWindows: false,
  nativeFilesystem: false,
  nativeTerminal: false,
  nativeUpdates: false,
  nativePlugins: false,
  nativeNotifications: true,
  nativeMicrophone: true,
  nativeBrowser: true,
})

export class BrowserDesktopUnsupportedError extends Error {
  readonly code = 'desktop_browser_unsupported'
  constructor(readonly capability: string) {
    super(`The managed browser renderer does not support native Desktop capability: ${capability}`)
    this.name = 'BrowserDesktopUnsupportedError'
  }
}

const unsupported = (capability: string) => async (..._args: unknown[]): Promise<never> => {
  throw new BrowserDesktopUnsupportedError(capability)
}
const unsupportedSync = (capability: string) => (..._args: unknown[]): never => {
  throw new BrowserDesktopUnsupportedError(capability)
}

/** No OS producer exists in this client. Subscription is inert, never a fabricated event. */
const noNativeEvents = (..._args: unknown[]): (() => void) => () => {}

export function createBrowserDesktopBridge(
  instanceId: number,
  origin: string,
  transport: BrowserBridge = createBridge(instanceId, origin),
  environment: BrowserEnvironment = {},
) {
  const pageOrigin = new URL(origin)
  if (!['http:', 'https:'].includes(pageOrigin.protocol) || pageOrigin.origin !== origin || pageOrigin.username || pageOrigin.password) {
    throw new Error('A plain HTTP(S) ClawManager origin is required')
  }
  if (!Number.isSafeInteger(instanceId) || instanceId <= 0) throw new Error('A valid managed instance is required')
  const connectionId = `clawmanager:${instanceId}`
  const baseUrl = `${origin}/api/v1/instances/${instanceId}/hermes-desktop`
  const browserWindow = environment.window === undefined ? (typeof window === 'undefined' ? null : window) : environment.window
  const browserDocument = environment.document === undefined ? browserWindow?.document ?? null : environment.document
  const clipboard = environment.clipboard === undefined ? browserWindow?.navigator.clipboard ?? null : environment.clipboard
  // Capture native methods BEFORE upstream installClipboardShim wraps writeText.
  // Looking them up on each call would recurse back into this bridge forever.
  const readClipboardText = clipboard?.readText?.bind(clipboard)
  const writeClipboardText = clipboard?.writeText?.bind(clipboard)
  const now = environment.now ?? Date.now
  const selectedFiles = new Map<string, File>()
  const selectedFilePaths = new WeakMap<File, string>()
  let selectedFileSequence = 0

  function rememberFile(file: File): string {
    const existing = selectedFilePaths.get(file)
    if (existing) return existing
    const path = `browser-file:${++selectedFileSequence}:${file.name}`
    selectedFilePaths.set(file, path)
    selectedFiles.set(path, file)
    return path
  }

  function selectedFile(path: string): File {
    const file = selectedFiles.get(path)
    if (!file) throw new Error('The selected browser file is no longer available; select it again')
    return file
  }

  async function fileDataUrl(path: string): Promise<string> {
    const file = selectedFile(path)
    return await new Promise<string>((resolve, reject) => {
      const reader = new FileReader()
      reader.onerror = () => reject(reader.error || new Error('Could not read the selected file'))
      reader.onload = () => resolve(String(reader.result || ''))
      reader.readAsDataURL(file)
    })
  }

  async function pickFiles(options: Parameters<DesktopBridge['selectPaths']>[0] = {}): Promise<string[]> {
    if (!browserDocument) throw new BrowserDesktopUnsupportedError('selectPaths')
    return await new Promise(resolve => {
      const input = browserDocument.createElement('input')
      input.type = 'file'
      input.multiple = Boolean(options.multiple)
      if (options.directories) input.setAttribute('webkitdirectory', '')
      if (options.filters?.length) input.accept = options.filters.flatMap(filter => filter.extensions.map(extension => `.${extension}`)).join(',')
      input.onchange = () => resolve(Array.from(input.files || []).map(rememberFile))
      input.oncancel = () => resolve([])
      input.click()
    })
  }

  function assertManagedConnectionInput(payload: { mode?: string; profile?: string | null; remoteAuthMode?: string; remoteUrl?: string }) {
    assertScope(payload.profile)
    if (payload.mode !== 'remote' || payload.remoteAuthMode !== 'oauth' || (payload.remoteUrl && payload.remoteUrl.replace(/\/$/, '') !== baseUrl)) {
      throw new Error('ClawManager manages this instance connection; only its current authenticated gateway can be used')
    }
  }

  function assertScope(profile?: string | null, requestedConnection?: string | null): void {
    if (profile !== undefined && profile !== null && profile !== '' && profile !== 'default') {
      throw new Error('The managed browser renderer only supports its default profile')
    }
    if (requestedConnection !== undefined && requestedConnection !== null && requestedConnection !== '' && requestedConnection !== connectionId) {
      throw new Error('The managed browser renderer cannot access another connection')
    }
  }

  const windowState = (): HermesWindowState => ({
    isFullscreen: Boolean(browserDocument?.fullscreenElement),
    isVisible: browserDocument ? !browserDocument.hidden : true,
    nativeOverlayWidth: 0,
    windowButtonPosition: null,
  })

  async function getConnection(profile?: string | null): Promise<HermesConnection> {
    assertScope(profile)
    await transport.session()
    return {
      ...windowState(), baseUrl, mode: 'remote', authMode: 'oauth', remoteKind: 'url',
      remoteHost: pageOrigin.host, remoteIdentity: connectionId,
      connectionId, profile: 'default', registryScoped: true,
      // Never expose a Runtime token or cache a one-time WebSocket URL.
      token: '', wsUrl: '', logs: [],
    }
  }

  const registry = () => ({
    version: 2, primary: connectionId, lastUsed: connectionId, launchMode: 'primary' as const,
    secureTokenStorage: false,
    connections: [{
      id: connectionId, kind: 'remote' as const, label: `ClawManager · ${instanceId}`,
      url: baseUrl, authMode: 'oauth' as const, tokenSet: false, tokenPreview: null,
    }],
  })

  async function getConnectionConfig(profile?: string | null) {
    assertScope(profile)
    await transport.session()
    return {
      envOverride: false, mode: 'remote' as const, profile: 'default', remoteAuthMode: 'oauth' as const,
      remoteOauthConnected: true, remoteTokenPreview: null, remoteTokenSet: false,
      secureTokenStorage: false, remoteTokenPlainText: false, remoteUrl: baseUrl,
      cloudOrg: '', sshHost: '', sshUser: '', sshPort: null, sshKeyPath: '', sshRemoteHermesPath: '', sshRemoteProfile: '',
    }
  }

  const bridge = {
    glassSupported: false,
    translucencySupported: false,
    getConnection,
    async getConnectionFor(route: { connectionId?: string | null; profile?: string | null }) {
      assertScope(route.profile, route.connectionId)
      return getConnection(route.profile)
    },
    async getGatewayWsUrl(profile?: string | null) {
      assertScope(profile)
      const ticket = await transport.getGatewayWsUrl()
      return { ok: true as const, wsUrl: ticket.url }
    },
    async getGatewayWsUrlFor(route: { connectionId?: string | null; profile?: string | null }) {
      assertScope(route.profile, route.connectionId)
      const ticket = await transport.getGatewayWsUrl()
      return { ok: true as const, wsUrl: ticket.url }
    },
    async getProfileRoutes(profiles: string[]) {
      profiles.forEach(profile => assertScope(profile))
      return [{ connectionId, mode: 'remote' as const, profile: 'default', targetProfile: 'default' }]
    },
    async revalidateConnection() {
      // CM owns routing/lifecycle. Revalidate its lease, never claim to have spawned a backend.
      await transport.session()
      return { ok: true, rebuilt: false }
    },
    touchBackend: unsupported('touchBackend'),
    api<T>(request: Parameters<DesktopBridge['api']>[0]): Promise<T> {
      assertScope(request.profile, request.connectionId)
      if (request.upload !== undefined) return Promise.reject(new BrowserDesktopUnsupportedError('api.upload'))
      // Scoping has been validated above; transport always targets this one BFF.
      return transport.api<T>({ path: request.path, method: request.method, body: request.body })
    },
    async getBootProgress(): Promise<DesktopBootProgress> {
      await transport.session()
      return {
        phase: 'clawmanager.authenticated', progress: 0, running: true, fakeMode: false,
        message: 'ClawManager authorization validated; starting the Desktop renderer.',
        error: null, timestamp: now(),
      }
    },
    onBootProgress: noNativeEvents,
    onBackendExit: noNativeEvents,
    onPreviewFileChanged: noNativeEvents,
    onBootstrapEvent: noNativeEvents,
    async getBootstrapState() {
      // There is no LOCAL installer in a managed browser. This is not a backend-ready signal.
      return { active: false, manifest: null, stages: {}, error: null, log: [], startedAt: null, completedAt: null, setupChoice: null, unsupportedPlatform: null }
    },
    onWindowStateChanged(callback: (state: HermesWindowState) => void) {
      const publish = () => callback(windowState())
      browserDocument?.addEventListener('visibilitychange', publish)
      browserDocument?.addEventListener('fullscreenchange', publish)
      browserWindow?.addEventListener('focus', publish)
      browserWindow?.addEventListener('blur', publish)
      publish()
      return () => {
        browserDocument?.removeEventListener('visibilitychange', publish)
        browserDocument?.removeEventListener('fullscreenchange', publish)
        browserWindow?.removeEventListener('focus', publish)
        browserWindow?.removeEventListener('blur', publish)
      }
    },
    async writeClipboard(text: string) {
      if (!writeClipboardText) throw new BrowserDesktopUnsupportedError('writeClipboard')
      await writeClipboardText(text)
      return true
    },
    async readClipboard() {
      if (!readClipboardText) throw new BrowserDesktopUnsupportedError('readClipboard')
      return readClipboardText()
    },
    profile: {
      get: async () => ({ profile: 'default' }),
      remember: async (name: string | null) => { assertScope(name); return { profile: 'default' } },
      set: async (name: string | null) => { assertScope(name); return { profile: 'default' } },
    },
    connections: {
      list: async () => registry(),
      save: unsupported('connections.save'), remove: unsupported('connections.remove'),
      setPrimary: unsupported('connections.setPrimary'), test: unsupported('connections.test'),
      onChanged: noNativeEvents,
    },
    getConnectionConfig,
    async saveConnectionConfig(payload: Parameters<DesktopBridge['saveConnectionConfig']>[0]) {
      assertManagedConnectionInput(payload)
      return getConnectionConfig(payload.profile)
    },
    async applyConnectionConfig(payload: Parameters<DesktopBridge['applyConnectionConfig']>[0]) {
      assertManagedConnectionInput(payload)
      await transport.session()
      return getConnectionConfig(payload.profile)
    },
    async testConnectionConfig(payload: Parameters<DesktopBridge['testConnectionConfig']>[0]) {
      assertManagedConnectionInput(payload)
      const session = await transport.session()
      return { ok: true, reachable: true, baseUrl, version: session.versions.hermes_commit ?? null }
    },
    getSecretStorageEncryption: unsupported('getSecretStorageEncryption'),
    setSecretStorageEncryption: unsupported('setSecretStorageEncryption'),
    sshConfigHosts: unsupported('sshConfigHosts'), sshResolveHost: unsupported('sshResolveHost'),
    probeConnectionConfig: unsupported('probeConnectionConfig'),
    oauthLoginConnectionConfig: unsupported('oauthLoginConnectionConfig'),
    oauthLogoutConnectionConfig: unsupported('oauthLogoutConnectionConfig'),
    cloud: {
      status: unsupported('cloud.status'), login: unsupported('cloud.login'), logout: unsupported('cloud.logout'),
      discover: unsupported('cloud.discover'), agentSignIn: unsupported('cloud.agentSignIn'),
    },
    openSessionWindow: unsupported('openSessionWindow'), openSessionInTerminal: unsupported('openSessionInTerminal'),
    openWindow: unsupported('openWindow'), openBrowserWindow: unsupported('openBrowserWindow'),
    onBrowserPopoutClosed: noNativeEvents, claimAmbientCue: unsupported('claimAmbientCue'),
    petOverlay: {
      open: unsupported('petOverlay.open'), close: unsupported('petOverlay.close'),
      setBounds: unsupportedSync('petOverlay.setBounds'), setIgnoreMouse: unsupportedSync('petOverlay.setIgnoreMouse'),
      setFocusable: unsupportedSync('petOverlay.setFocusable'), pushState: unsupportedSync('petOverlay.pushState'),
      control: unsupportedSync('petOverlay.control'), onState: noNativeEvents, onControl: noNativeEvents,
    },
    quickEntry: {
      getSettings: unsupported('quickEntry.getSettings'), setSettings: unsupported('quickEntry.setSettings'),
      submit: unsupportedSync('quickEntry.submit'), dismiss: unsupportedSync('quickEntry.dismiss'),
      pushState: unsupportedSync('quickEntry.pushState'), onState: noNativeEvents, onSubmit: noNativeEvents, onShown: noNativeEvents,
    },
    async notify(payload: Parameters<DesktopBridge['notify']>[0]) {
      const NotificationApi = (browserWindow as (Window & { Notification?: typeof globalThis.Notification }) | null)?.Notification
      if (!NotificationApi) return false
      let permission = NotificationApi.permission
      if (permission === 'default') permission = await NotificationApi.requestPermission()
      if (permission !== 'granted') return false
      const notification = new NotificationApi(payload.title || 'Hermes', { body: payload.body, silent: payload.silent, tag: payload.tag || payload.sessionId })
      if (payload.activate) notification.onclick = () => { browserWindow?.focus(); browserWindow!.location.hash = payload.activate!; notification.close() }
      return true
    },
    async requestMicrophoneAccess() {
      const stream = await browserWindow?.navigator.mediaDevices?.getUserMedia({ audio: true })
      if (!stream) return false
      stream.getTracks().forEach(track => track.stop())
      return true
    },
    readFileDataUrl: fileDataUrl, readFileDataUrlForAttach: fileDataUrl,
    async readFileText(path: string) {
      const file = selectedFile(path)
      return { path, text: await file.text(), byteSize: file.size, mimeType: file.type || 'text/plain', truncated: false }
    },
    readPluginSource: unsupported('readPluginSource'),
    readDir: unsupported('readDir'), selectPaths: pickFiles, selectSavePath: unsupported('selectSavePath'),
    saveImageFromUrl: unsupported('saveImageFromUrl'), saveImageBuffer: unsupported('saveImageBuffer'),
    saveClipboardImage: unsupported('saveClipboardImage'), getPathForFile: (file: File) => rememberFile(file),
    normalizePreviewTarget: unsupported('normalizePreviewTarget'), watchPreviewFile: unsupported('watchPreviewFile'),
    stopPreviewFileWatch: unsupported('stopPreviewFileWatch'),
    async openExternal(rawUrl: string) {
      const url = new URL(rawUrl, origin)
      if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) throw new Error('Only HTTP(S) links can be opened')
      const opened = browserWindow?.open(url.href, '_blank', 'noopener,noreferrer')
      if (opened) opened.opener = null
    },
    async fetchLinkTitle(rawUrl: string) {
      const url = new URL(rawUrl)
      if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) throw new Error('Only HTTP(S) links can be inspected')
      const response = await fetch(url.href, { method: 'GET', redirect: 'follow', signal: AbortSignal.timeout(10_000) })
      const html = await response.text()
      return html.match(/<title[^>]*>([^<]*)<\/title>/i)?.[1]?.trim() || url.hostname
    },
    sanitizeWorkspaceCwd: unsupported('sanitizeWorkspaceCwd'),
    settings: {
      // No machine directory is inspected or invented by this client.
      getDefaultProjectDir: async () => ({ dir: null, resolvedCwd: '', defaultLabel: 'Managed Runtime workspace' }),
      pickDefaultProjectDir: unsupported('settings.pickDefaultProjectDir'),
      setDefaultProjectDir: unsupported('settings.setDefaultProjectDir'),
    },
    revealLogs: unsupported('revealLogs'), getRecentLogs: unsupported('getRecentLogs'),
    terminal: {
      cwd: unsupported('terminal.cwd'), dispose: unsupported('terminal.dispose'),
      resize: unsupported('terminal.resize'), start: unsupported('terminal.start'), write: unsupported('terminal.write'),
      onData: noNativeEvents, onExit: noNativeEvents,
    },
    continueBootstrapLocal: unsupported('continueBootstrapLocal'), resetBootstrap: unsupported('resetBootstrap'),
    repairBootstrap: unsupported('repairBootstrap'), cancelBootstrap: unsupported('cancelBootstrap'),
    async getVersion() {
      const session = await transport.session()
      return { appVersion: session.versions.bridge_version, electronVersion: '', nodeVersion: '', platform: 'web', hermesRoot: 'managed-runtime' }
    },
    updates: {
      check: unsupported('updates.check'), apply: unsupported('updates.apply'),
      getBranch: unsupported('updates.getBranch'), setBranch: unsupported('updates.setBranch'), onProgress: noNativeEvents,
    },
    uninstall: { summary: unsupported('uninstall.summary'), run: unsupported('uninstall.run') },
    themes: { fetchMarketplace: unsupported('themes.fetchMarketplace'), searchMarketplace: unsupported('themes.searchMarketplace') },
    findInPage: unsupported('findInPage'), stopFindInPage: unsupported('stopFindInPage'),
    onFoundInPage: noNativeEvents, onOpenFindBarRequested: noNativeEvents,
  } satisfies DesktopBridge

  // Required native methods reject explicitly. Unsupported OPTIONAL machine hooks
  // remain absent, so upstream feature checks cannot confuse a stub for support.
  for (const value of Object.values(bridge)) {
    if (value !== null && typeof value === 'object') Object.freeze(value)
  }
  return Object.freeze(bridge)
}

export type BrowserDesktopBridge = ReturnType<typeof createBrowserDesktopBridge>
