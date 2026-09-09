import assert from 'node:assert/strict'
import test from 'node:test'
import { createBridge } from '../src/bridge.ts'
import { BROWSER_DESKTOP_CAPABILITIES, BrowserDesktopUnsupportedError, createBrowserDesktopBridge } from '../src/browser-desktop-bridge.ts'

const origin = 'https://manager.example'
function fixture() {
  const requests: Array<{ path: string; options?: RequestInit }> = []
  const transport = createBridge(42, origin, (async (path: string, options?: RequestInit) => {
    requests.push({ path, options })
    if (path.endsWith('/session')) return Response.json({ success: true, data: {
      available: true, instance_id: 42, capabilities: ['chat', 'sessions'], versions: { bridge_version: '1' },
    } })
    if (path.endsWith('/ws-ticket')) return Response.json({ success: true, data: { url: `/api/v1/instances/42/hermes-desktop/ws?ticket=${requests.length}` } })
    return Response.json({ status: 'ok' })
  }) as typeof fetch)
  return { requests, transport, bridge: createBrowserDesktopBridge(42, origin, transport, { window: null, document: null, clipboard: null, now: () => 123 }) }
}

test('real renderer bridge exposes one authenticated managed connection without credentials or cached tickets', async () => {
  const { bridge, requests } = fixture()
  const connection = await bridge.getConnection()
  assert.equal(connection.baseUrl, `${origin}/api/v1/instances/42/hermes-desktop`)
  assert.equal(connection.connectionId, 'clawmanager:42')
  assert.equal(connection.profile, 'default')
  assert.equal(connection.mode, 'remote')
  assert.equal(connection.authMode, 'oauth')
  assert.equal(connection.wsUrl, '')
  assert.equal(connection.token, '')
  assert.deepEqual(connection.logs, [])
  assert.equal(requests[0].path, '/api/v1/instances/42/hermes-desktop/session')
  const registry = await bridge.connections.list()
  assert.equal(registry.primary, 'clawmanager:42')
  assert.equal(registry.connections.length, 1)
  assert.equal(registry.connections[0].tokenSet, false)
  assert.deepEqual(await bridge.profile.get(), { profile: 'default' })
})

test('default profile and own connection are normalized only after validation; other routing cannot fetch', async () => {
  const { bridge, requests } = fixture()
  for (const profile of ['', null, undefined, 'default']) await bridge.getConnection(profile)
  await bridge.getConnectionFor({ connectionId: 'clawmanager:42', profile: 'default' })
  await bridge.api({ path: '/api/status', connectionId: 'clawmanager:42', profile: 'default' })
  const count = requests.length
  for (const profile of ['other', ' default', 'default ', '/etc/passwd']) {
    await assert.rejects(bridge.getConnection(profile), /default profile/)
    await assert.rejects(bridge.getGatewayWsUrl(profile), /default profile/)
    assert.throws(() => bridge.api({ path: '/api/status', profile }), /default profile/)
  }
  for (const connectionId of ['local', 'clawmanager:43', 'ssh', 'https://evil.test']) {
    await assert.rejects(bridge.getConnectionFor({ connectionId }), /another connection/)
    await assert.rejects(bridge.getGatewayWsUrlFor({ connectionId }), /another connection/)
    assert.throws(() => bridge.api({ path: '/api/status', connectionId }), /another connection/)
  }
  assert.equal(requests.length, count)
})

test('every gateway dial mints a fresh instance-scoped ticket using the existing BFF transport', async () => {
  const { bridge, requests } = fixture()
  const first = await bridge.getGatewayWsUrl()
  const second = await bridge.getGatewayWsUrlFor({ connectionId: 'clawmanager:42', profile: 'default' })
  assert.notEqual(first.wsUrl, second.wsUrl)
  assert.equal(first.ok, true)
  assert.ok(requests.every(request => request.path === '/api/v1/instances/42/hermes-desktop/ws-ticket'))
  assert.ok(requests.every(request => request.options?.credentials === 'same-origin' && request.options?.redirect === 'error'))
  assert.ok(requests.every(request => !('Authorization' in (request.options?.headers ?? {}))))
})

test('boot progress reports only CM authorization, never fabricated Runtime or renderer readiness', async () => {
  const { bridge } = fixture()
  const progress = await bridge.getBootProgress()
  assert.equal(progress.phase, 'clawmanager.authenticated')
  assert.equal(progress.running, true)
  assert.equal(progress.fakeMode, false)
  assert.equal(progress.progress, 0)
  assert.equal(progress.timestamp, 123)
  let events = 0
  const offBoot = bridge.onBootProgress(() => events++)
  const offBackend = bridge.onBackendExit(() => events++)
  const offBootstrap = bridge.onBootstrapEvent(() => events++)
  const install = await bridge.getBootstrapState()
  assert.equal(install.active, false)
  assert.equal(install.completedAt, null)
  assert.deepEqual(install.stages, {})
  offBoot(); offBoot(); offBackend(); offBootstrap()
  assert.equal(events, 0)
  const rejected = createBrowserDesktopBridge(42, origin, createBridge(42, origin, (async () => new Response(null, { status: 401 })) as typeof fetch))
  await assert.rejects(rejected.getBootProgress())
  await assert.rejects(rejected.getConnection())
})

test('native actions reject explicitly, unsupported optional hooks and unknown methods are absent', async () => {
  const { bridge, requests } = fixture()
  for (const action of [bridge.readDir, bridge.openWindow, bridge.openSessionWindow, bridge.openSessionInTerminal,
    bridge.updates.check, bridge.updates.apply, bridge.uninstall.run, bridge.terminal.start,
    bridge.terminal.write, bridge.connections.save, bridge.themes.fetchMarketplace,
    bridge.continueBootstrapLocal, bridge.repairBootstrap, bridge.cloud.login, bridge.readPluginSource]) {
    await assert.rejects(action(), (error: unknown) => error instanceof BrowserDesktopUnsupportedError && error.code === 'desktop_browser_unsupported')
  }
  assert.equal(await bridge.notify({ title: 'test' }), false)
  assert.equal(await bridge.requestMicrophoneAccess(), false)
  assert.deepEqual(await bridge.profile.set('default'), { profile: 'default' })
  assert.throws(() => bridge.quickEntry.pushState(), BrowserDesktopUnsupportedError)
  assert.throws(() => bridge.petOverlay.pushState(), BrowserDesktopUnsupportedError)
  assert.equal(requests.length, 0)
  for (const key of ['runShell', 'setTitleBarTheme', 'setNativeTheme', 'setTranslucency', 'setActiveWork', 'getOnBattery', 'hud', 'git', 'desktopPluginsRoot']) {
    assert.equal(Object.hasOwn(bridge, key), false, key)
  }
  assert.equal(Object.isFrozen(bridge), true)
  assert.equal(Object.isFrozen(bridge.connections), true)
  assert.equal(BROWSER_DESKTOP_CAPABILITIES.nativeWindows, false)
  assert.equal(BROWSER_DESKTOP_CAPABILITIES.nativePlugins, false)
})

test('API does not accept uploads, arbitrary URLs, or transport methods beyond the BFF policy', async () => {
  const { bridge, requests } = fixture()
  await assert.rejects(bridge.api({ path: '/api/status', upload: { filename: 'private.txt', bytes: new ArrayBuffer(0) } }), BrowserDesktopUnsupportedError)
  await assert.rejects(async () => bridge.api({ path: 'https://evil.test/api/status' }))
  await assert.rejects(async () => bridge.api({ path: '/api/auth/session' }))
  assert.equal(requests.length, 0)
})

test('browser clipboard captures native methods before upstream shim, and permission failures stay failures', async () => {
  const { transport } = fixture()
  const writes: string[] = []
  const clipboard = { readText: async () => 'selected text', writeText: async (text: string) => { writes.push(text) } }
  const bridge = createBrowserDesktopBridge(42, origin, transport, { clipboard, window: null })
  clipboard.writeText = async () => { throw new Error('upstream shim must not recurse') }
  assert.equal(await bridge.writeClipboard('hello'), true)
  assert.equal(await bridge.readClipboard(), 'selected text')
  assert.deepEqual(writes, ['hello'])
  const denied = createBrowserDesktopBridge(42, origin, transport, { clipboard: { readText: async () => { throw new Error('permission denied') }, writeText: async () => { throw new Error('permission denied') } }, window: null })
  await assert.rejects(denied.writeClipboard('hello'), /permission denied/)
  await assert.rejects(denied.readClipboard(), /permission denied/)
  await assert.rejects(fixture().bridge.writeClipboard('hello'), BrowserDesktopUnsupportedError)
})

test('window state follows real browser visibility/fullscreen events and unsubscribe removes listeners', () => {
  const { transport } = fixture()
  const document = Object.assign(new EventTarget(), { hidden: false, fullscreenElement: null as object | null })
  const window = new EventTarget()
  const bridge = createBrowserDesktopBridge(42, origin, transport, { document: document as unknown as Document, window: window as unknown as Window, clipboard: null })
  const states: Array<{ isVisible?: boolean; isFullscreen: boolean }> = []
  const unsubscribe = bridge.onWindowStateChanged((state: { isVisible?: boolean; isFullscreen: boolean }) => states.push(state))
  document.hidden = true
  document.dispatchEvent(new Event('visibilitychange'))
  document.fullscreenElement = {}
  document.dispatchEvent(new Event('fullscreenchange'))
  assert.deepEqual(states.map(state => [state.isVisible, state.isFullscreen]), [[true, false], [false, false], [false, true]])
  unsubscribe(); unsubscribe()
  window.dispatchEvent(new Event('focus'))
  document.dispatchEvent(new Event('visibilitychange'))
  assert.equal(states.length, 3)
})

test('invalid managed instance or non-origin URLs are rejected before exposing a bridge', () => {
  const { transport } = fixture()
  for (const invalid of ['javascript:alert(1)', 'https://u:p@manager.example', `${origin}/path`, `${origin}/`, `${origin}?token=x`]) {
    assert.throws(() => createBrowserDesktopBridge(42, invalid, transport))
  }
  for (const id of [0, -1, 1.5, Number.NaN]) assert.throws(() => createBrowserDesktopBridge(id, origin, transport))
})
