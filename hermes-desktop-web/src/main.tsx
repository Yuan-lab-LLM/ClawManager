// Install the browser boundary BEFORE evaluating upstream's module-level stores.
// App, router, layout, sidebar, composer and transcript come from the real
// renderer, not a second application made from selected chat components.
import { createBridge } from './bridge'
import { createBrowserDesktopBridge } from './browser-desktop-bridge'
import { installRendererStorage } from './renderer-storage'

const params = new URLSearchParams(window.location.search)
const instanceId = Number(params.get('instance_id'))

function notifyParent(type: 'ready' | 'error') {
  if (window.parent !== window) window.parent.postMessage({
    type: `clawmanager:hermes-desktop:${type}`, instanceId,
    ...(type === 'error' ? { message: 'Hermes Desktop 加载失败，请刷新重试。' } : {})
  }, window.location.origin)
}

async function start() {
  if (!Number.isSafeInteger(instanceId) || instanceId <= 0 ||
    [...params.keys()].some(key => key !== 'instance_id') || params.getAll('instance_id').length !== 1) {
    throw new Error('Invalid renderer scope')
  }
  // Manager instances and users share a browser origin. Never load Desktop
  // drafts/transcripts/connection preferences from that shared storage.
  installRendererStorage(window)
  const bff = createBridge(instanceId, window.location.origin)
  await bff.session()
  window.hermesDesktop = createBrowserDesktopBridge(instanceId, window.location.origin, bff)

  const { $desktopBoot } = await import('@/store/boot')
  $desktopBoot.listen(state => {
    if (state.phase === 'renderer.ready' && !state.running && !state.error) notifyParent('ready')
    else if (state.phase === 'renderer.error') notifyParent('error')
  })
  await import('@/main')
}

void start().catch(() => {
  const root = document.getElementById('root')
  if (root) {
    root.textContent = 'Hermes Desktop 加载失败，请返回 ClawManager 刷新重试。'
    root.setAttribute('role', 'alert')
  }
  notifyParent('error')
})
