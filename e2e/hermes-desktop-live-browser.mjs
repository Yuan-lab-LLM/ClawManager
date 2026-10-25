import { readFile, rm } from 'node:fs/promises'
import { spawn } from 'node:child_process'
import path from 'node:path'

const [origin, instanceId, cookieJar, chromePath = 'C:/Program Files/Google/Chrome/Application/chrome.exe'] = process.argv.slice(2)
if (!origin || !/^\d+$/.test(instanceId || '') || !cookieJar) throw new Error('usage: node hermes-desktop-live-browser.mjs <origin> <instance-id> <cookie-jar> [chrome]')

const cookieName = `cm_hermes_desktop_${instanceId}`
const cookieLine = (await readFile(cookieJar, 'utf8')).split(/\r?\n/).map(line => line.replace(/^#HttpOnly_/, '')).find(line => !line.startsWith('#') && line.split('\t')[5] === cookieName)
if (!cookieLine) throw new Error(`missing ${cookieName} cookie`)
const cookieValue = cookieLine.split('\t')[6]
const port = 9333
const tempRoot = path.resolve(process.env.TEMP || '.')
const profile = path.join(tempRoot, `cm-hermes-browser-${process.pid}`)
if (path.dirname(profile) !== tempRoot) throw new Error('invalid temporary Chrome profile path')
const chrome = spawn(chromePath, [
  '--headless=new', '--ignore-certificate-errors', '--disable-gpu', '--no-first-run', '--no-default-browser-check',
  `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, 'about:blank',
], { stdio: 'ignore', windowsHide: true })

const delay = ms => new Promise(resolve => setTimeout(resolve, ms))
let tab
for (let i = 0; i < 50; i++) {
  try {
    tab = await fetch(`http://127.0.0.1:${port}/json/list`).then(response => response.json()).then(tabs => tabs.find(tab => tab.type === 'page' && !tab.url.startsWith('chrome-extension:')))
    if (tab?.webSocketDebuggerUrl) break
  } catch {}
  await delay(100)
}
if (!tab?.webSocketDebuggerUrl) throw new Error('Chrome DevTools did not start')

const ws = new WebSocket(tab.webSocketDebuggerUrl)
await new Promise((resolve, reject) => { ws.onopen = resolve; ws.onerror = reject })
let sequence = 0
const pending = new Map()
const failures = []
const networkFailures = []
const skillHubResponses = []
ws.onmessage = event => {
  const message = JSON.parse(event.data)
  if (message.id && pending.has(message.id)) {
    const { resolve, reject } = pending.get(message.id)
    pending.delete(message.id)
    return message.error ? reject(new Error(message.error.message)) : resolve(message.result)
  }
  if (message.method === 'Runtime.exceptionThrown') failures.push(message.params.exceptionDetails?.text || 'runtime exception')
  if (message.method === 'Log.entryAdded' && message.params.entry.level === 'error') failures.push(message.params.entry.text)
  if (message.method === 'Network.responseReceived') {
    if (message.params.response.url.startsWith('https://hermes-agent.nousresearch.com/docs/skills')) skillHubResponses.push(message.params.response.status)
    if (message.params.response.status >= 400) networkFailures.push(`${message.params.response.status} ${message.params.response.url}`)
  }
}
const cdp = (method, params = {}) => new Promise((resolve, reject) => {
  const id = ++sequence
  pending.set(id, { resolve, reject })
  ws.send(JSON.stringify({ id, method, params }))
})

try {
  await cdp('Runtime.enable')
  await cdp('Log.enable')
  await cdp('Network.enable')
  await cdp('Page.enable')
  await cdp('Network.setCookie', { name: cookieName, value: cookieValue, url: `${origin}/api/v1/instances/${instanceId}/hermes-desktop/`, secure: true, httpOnly: true, sameSite: 'Strict' })
  await cdp('Page.navigate', { url: `${origin}/hermes-desktop-web/?instance_id=${instanceId}#/artifacts` })
  await delay(8000)
  const evaluate = async expression => {
    const result = await cdp('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true })
    if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text)
    return result.result.value
  }
  const result = await evaluate(`(async () => {
    const base = '/api/v1/instances/${instanceId}/hermes-desktop/api'
    const paths = ['config/schema','config','model/options?explicit_only=1&include_unconfigured=1','env','providers/custom-endpoints','providers/oauth','tools/toolsets','skills','memory','mcp/servers','cron/jobs','messaging/platforms']
    const status = {}
    for (const path of paths) {
      try { status[path] = (await fetch(base + '/' + path, { credentials: 'same-origin' })).status }
      catch (error) { status[path] = String(error) }
    }
    let schema = {}
    try { schema = await fetch(base + '/config/schema', { credentials: 'same-origin' }).then(r => r.json()) } catch {}
    const buttons = [...document.querySelectorAll('button')]
    const artifactsOpened = location.hash.includes('/artifacts')
    const artifactsText = document.body.innerText
    location.hash = '/skills'
    await new Promise(resolve => setTimeout(resolve, 6000))
    const skillHubFrame = document.querySelector('iframe[src^="https://hermes-agent.nousresearch.com/docs/skills"]')
    const skillHubFrameFound = Boolean(skillHubFrame)
    const skillHubBlocked = /该内容被屏蔽|content is blocked/i.test(document.body.innerText)
    const settings = buttons.find(button => /settings|设置/i.test((button.getAttribute('aria-label') || '') + ' ' + (button.textContent || '')))
    settings?.click()
    await new Promise(resolve => setTimeout(resolve, 2500))
    const text = document.body.innerText
    const labels = ['Model','Chat','Appearance','Workspace','Safety','Browser','Memory & Context','Voice','Advanced','Notifications','Billing','Providers','Gateways','Keyboard Shortcuts','Tools & Keys','Plugins','Archived Chats','About']
    const tabs = {}
    for (const label of labels) {
      const button = [...document.querySelectorAll('button')].find(item => item.textContent?.trim() === label)
      if (!button) { tabs[label] = 'missing'; continue }
      button.click()
      await new Promise(resolve => setTimeout(resolve, 500))
      tabs[label] = /failed to load|加载失败|request failed|请求失败/i.test(document.body.innerText) ? 'failed' : 'ok'
    }
    return {
      title: document.title,
      href: location.href,
      apiStatus: status,
      schemaFields: Object.keys(schema.fields || {}).length,
      artifactsOpened,
      artifactsFailed: /Artifacts failed to load|Failed to fetch|Skipped \d+ of \d+ recent sessions/i.test(artifactsText),
      skillHubFrameFound,
      skillHubBlocked,
      settingsOpened: Boolean(settings),
      settingsFailed: /Settings failed to load|设置加载失败/i.test(text),
      visibleSettings: ['Model','Chat','Appearance','Workspace','Safety','Browser','Memory','Voice','Providers','Gateways','Plugins','About'].filter(label => text.includes(label)),
      tabs,
      bodyLength: text.length,
    }
  })()`)
  result.browserErrors = [...new Set(failures.filter(error => !error.includes('ERR_FILE_NOT_FOUND') && !error.includes('navigator.vibrate')))].slice(0, 20)
  result.networkFailures = [...new Set(networkFailures)].slice(0, 20)
  result.skillHubResponses = [...new Set(skillHubResponses)]
  console.log(JSON.stringify(result, null, 2))
  if (!result.artifactsOpened || result.artifactsFailed || !result.skillHubFrameFound || result.skillHubBlocked || !result.skillHubResponses.includes(200) || result.settingsFailed || result.schemaFields < 100 || Object.values(result.apiStatus).some(code => code !== 200) || Object.values(result.tabs).some(state => state !== 'ok') || result.browserErrors.length || result.networkFailures.length) process.exitCode = 1
} finally {
  ws.close()
  if (chrome.exitCode === null) {
    const exited = new Promise(resolve => chrome.once('exit', resolve))
    chrome.kill()
    await Promise.race([exited, delay(2000)])
  }
  await rm(profile, { recursive: true, force: true })
}
