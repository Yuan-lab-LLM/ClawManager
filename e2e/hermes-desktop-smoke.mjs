import assert from 'node:assert/strict'
import http from 'node:http'
import https from 'node:https'

// Non-authenticated deployment checks only. No user credentials, model calls,
// writes to instances, or automatic redirect following are permitted here.
const [target, expectedVersion, ...options] = process.argv.slice(2)
const usage = 'Usage: node e2e/hermes-desktop-smoke.mjs <origin> <expected-version> [--insecure] [--hermes-instance <ordinary-lite-instance-id>] [--build-input <sha256>]'
let insecure = false
let hermesInstanceID
let expectedBuildInput
for (let index = 0; index < options.length; index++) {
  if (options[index] === '--insecure' && !insecure) {
    insecure = true
  } else if (options[index] === '--hermes-instance' && hermesInstanceID === undefined) {
    hermesInstanceID = options[++index]
    if (!/^[1-9][0-9]*$/.test(hermesInstanceID ?? '') || !Number.isSafeInteger(Number(hermesInstanceID))) throw new Error(usage)
  } else if (options[index] === '--build-input' && expectedBuildInput === undefined) {
    expectedBuildInput = options[++index]
    if (!/^[a-f0-9]{64}$/.test(expectedBuildInput ?? '')) throw new Error(usage)
  } else {
    throw new Error(usage)
  }
}
if (!target || !expectedVersion) throw new Error(usage)
const origin = new URL(target).origin
assert(['http:', 'https:'].includes(new URL(origin).protocol), 'Only HTTP(S) origins are supported')
const canaryMarker = `smoke-hermes-auth-${Date.now().toString(36)}`
const checks = []
function json(body, label) {
  try { return JSON.parse(body) } catch { throw new Error(`Invalid JSON response: ${label}`) }
}
async function request(path, { method = 'GET', headers = {}, status = 200 } = {}) {
  const url = new URL(path, origin)
  assert.equal(url.origin, origin, 'Refusing a cross-origin resource')
  const response = await new Promise((resolve, reject) => {
    const transport = url.protocol === 'https:' ? https : http
    const req = transport.request(url, {
      method, headers, rejectUnauthorized: !insecure, timeout: 15_000,
    }, res => {
      const chunks = []
      let size = 0
      res.on('data', chunk => {
        size += chunk.length
        if (size > 8 * 1024 * 1024) return res.destroy(new Error('Response exceeds smoke-test limit'))
        chunks.push(chunk)
      })
      res.on('error', reject)
      res.on('end', () => resolve({ status: res.statusCode, headers: res.headers, body: Buffer.concat(chunks).toString('utf8') }))
    })
    req.on('upgrade', (_response, socket) => {
      socket.destroy()
      reject(new Error(`Unexpected WebSocket upgrade on unauthenticated request: ${url.pathname}`))
    })
    req.on('timeout', () => req.destroy(new Error(`Timeout: ${url.pathname}`)))
    req.on('error', reject)
    req.end()
  })
  assert.equal(response.status, status, `${method} ${url.pathname}`)
  if (status >= 400) {
    assert(response.headers['set-cookie'] === undefined, 'Denied requests must not mint cookies')
    assert(response.headers.location === undefined, 'Denied requests must not redirect to an upstream login page')
    assert(!response.body.includes(canaryMarker), 'Denied responses must not reflect synthetic credential canaries')
    assert(!/hermes_session_at|hermes-gateway-ticket\.|type=["']password["']|<form\b/i.test(response.body), 'Denied response exposed an upstream session or login form')
  }
  checks.push({ method, pathname: url.pathname, status: response.status })
  return response
}

await request('/healthz')
const version = json((await request('/api/v1/version')).body, 'version').data
assert(version?.version === expectedVersion, 'Deployment is serving another build')
const main = await request('/')
assert(/<!doctype html>/i.test(main.body), 'Missing main frontend HTML')
const mainAssets = [...main.body.matchAll(/(?:src|href)="([^"]+\.(?:js|css))"/g)].map(match => match[1])
assert(mainAssets.some(path => path.endsWith('.js')), 'Missing main frontend JavaScript')
assert(mainAssets.some(path => path.endsWith('.css')), 'Missing main frontend CSS')
for (const asset of mainAssets) {
  const res = await request(asset)
  assert(res.body.length > 1000, 'Unexpectedly small main frontend asset')
  assert((asset.endsWith('.js') ? /javascript/ : /text\/css/).test(res.headers['content-type']), 'Wrong main frontend asset MIME type')
}
const renderer = await request('/hermes-desktop-web/')
assert(/text\/html/.test(renderer.headers['content-type']), 'Wrong renderer HTML MIME type')
assert(/frame-ancestors 'self'/.test(renderer.headers['content-security-policy']), 'Renderer framing boundary is missing')
assert(/frame-src https:\/\/hermes-agent\.nousresearch\.com/.test(renderer.headers['content-security-policy']), 'Skills Hub frame boundary is missing')
assert(/<meta[^>]+http-equiv="Content-Security-Policy"[^>]+frame-src https:\/\/hermes-agent\.nousresearch\.com/.test(renderer.body), 'Renderer meta CSP is missing the Skills Hub frame boundary')
assert(/(?:^|;)\s*default-src 'none'(?:;|$)/.test(renderer.headers['content-security-policy']), 'Renderer default CSP must remain closed')
assert(/(?:^|;)\s*connect-src 'self'(?:;|$)/.test(renderer.headers['content-security-policy']), 'Renderer connections must remain same-origin')
assert(/(?:^|;)\s*script-src 'self'(?:;|$)/.test(renderer.headers['content-security-policy']), 'Renderer script CSP must not allow inline/eval/remote code')
assert.equal(renderer.headers['x-content-type-options'], 'nosniff')
assert.equal(renderer.headers['referrer-policy'], 'no-referrer')
const info = json((await request('/hermes-desktop-web/build-info.json')).body, 'renderer build-info')
assert(info.scope === 'desktop-renderer', 'Expected complete Desktop renderer scope')
assert(info.renderer_entry === 'apps/desktop/src/main.tsx', 'Missing original Desktop main entry')
assert(info.acceptance === 'not-asserted-by-build', 'Build metadata must not claim Runtime/browser acceptance')
assert(/^[a-f0-9]{64}$/.test(info.build_input_sha256), 'Missing renderer build input digest')
if (expectedBuildInput) assert(info.build_input_sha256 === expectedBuildInput, 'Renderer build inputs differ from the tested release')
assert(info.dependency_audit?.policy === 'exact-upstream-runtime-version-and-integrity', 'Runtime dependencies are not locked to the original renderer')
assert(info.dependency_audit?.roots === 66 && info.dependency_audit?.resolved_nodes === 511, 'Unexpected locked renderer dependency closure size')
assert(info.dependency_audit?.closure_sha256 === '223942ec5775c9f3afdfdc7fb1b289511053ba0cc9911080a2e774fcb8c58f63', 'Unexpected locked renderer dependency closure digest')
for (const source of ['apps/desktop/src/app/index.tsx', 'apps/desktop/src/app/contrib/controller.tsx', 'apps/desktop/src/app/routes.ts']) {
  assert(/^[a-f0-9]{40}$/.test(info.source_files?.[source]), `Missing locked original renderer source: ${source}`)
}
assert(info.bridge_version === '1', 'Unexpected browser bridge version')
assert(info.hermes_commit === '29112bef099274229cadff79cdff7bf7b99c4b77', 'Unexpected original renderer commit')
const assets = [...renderer.body.matchAll(/(?:src|href)="([^"]+\.(?:js|css))"/g)].map(match => match[1])
assert(assets.some(path => path.endsWith('.js')), 'Missing renderer JavaScript')
const dynamicRendererAssets = new Set()
for (const asset of assets) {
  const resourceURL = new URL(asset, `${origin}/hermes-desktop-web/`)
  const res = await request(resourceURL.href)
  // Real Desktop styles and routes are loaded by the dynamic, verified
  // upstream main entry; the CM bootstrap does not need its own stylesheet.
  assert(res.body.length > 100, 'Unexpectedly small renderer asset')
  assert((asset.endsWith('.js') ? /javascript/ : /text\/css/).test(res.headers['content-type']), 'Wrong renderer asset MIME type')
  if (asset.endsWith('.js')) {
    // Read the compiled graph rather than pinning a historical Core hash or
    // accepting just the tiny CM bootstrap. Do not fetch unrelated lazy code.
    for (const match of res.body.matchAll(/["'`]((?:\.\/|assets\/)main-[A-Za-z0-9_-]+\.(?:js|css))["'`]/g)) {
      const resolved = new URL(match[1], resourceURL)
      assert(resolved.origin === origin && /^\/hermes-desktop-web\/assets\/main-[A-Za-z0-9_-]+\.(?:js|css)$/.test(resolved.pathname), 'Unexpected Desktop main asset location')
      dynamicRendererAssets.add(resolved.href)
    }
  }
}
assert([...dynamicRendererAssets].some(asset => asset.endsWith('.js')), 'Missing dynamic original Desktop JavaScript')
assert([...dynamicRendererAssets].some(asset => asset.endsWith('.css')), 'Missing dynamic original Desktop stylesheet')
for (const asset of dynamicRendererAssets) {
  const res = await request(asset)
  assert((asset.endsWith('.js') ? /javascript/ : /text\/css/).test(res.headers['content-type']), 'Wrong original Desktop asset MIME type')
  assert(res.body.length > (asset.endsWith('.js') ? 1_000_000 : 200_000), 'Original Desktop asset is unexpectedly small')
  if (asset.endsWith('.css')) {
    for (const selector of ['.min-h-0', '.min-w-0', '.overflow-hidden', '.shrink-0', '.w-full']) assert(res.body.includes(selector), 'Original Desktop layout CSS is missing')
    assert(!res.body.includes('Collapse-Bold'), 'Uncleared commercial font is still referenced')
  }
}
assert(/rel="license"[^>]+THIRD-PARTY-NOTICES\.md/.test(renderer.body), 'Renderer license index link is missing')
for (const [filename, required] of [
  ['THIRD-PARTY-LICENSES.md', ['## react - ', '## @assistant-ui/core - ', '## @assistant-ui/tap - 0.9.8']],
  ['THIRD-PARTY-NOTICES.md', ['Codicons attribution', 'Jason Long', 'HERMES-BOTS-LICENSE.txt']],
  ['FONT-LICENSES.md', ['SIL OPEN FONT LICENSE Version 1.1', 'JetBrains Mono Project Authors', 'Reserved Font Name KaTeX_Main']],
  ['HERMES-LICENSE.txt', ['MIT License', 'Copyright (c) 2025 Nous Research']],
  ['HERMES-BOTS-LICENSE.txt', ['MIT License', 'Copyright (c) 2026 Nous Research']],
  ['CODICONS-LICENSE.txt', ['Attribution 4.0 International', 'Jason Long', 'licenses/by/3.0/']],
  ['CODICONS-LICENSE-CODE.txt', ['MIT License', 'Microsoft Corporation']],
]) {
  const res = await request(`/hermes-desktop-web/${filename}`)
  for (const text of required) assert(res.body.includes(text), `Missing published license attribution in ${filename}`)
}
await request('/hermes-desktop-web/assets/smoke-missing-resource.js', { status: 404 })
const base = '/api/v1/instances/1/hermes-desktop'
await request(`${base}/bootstrap`, { status: 401 })
await request(`${base}/session`, { headers: { Origin: origin }, status: 401 })
await request(`${base}/ws-ticket`, { method: 'POST', headers: { Origin: origin }, status: 401 })
await request(`${base}/ws?ticket=${canaryMarker}`, { headers: { Origin: origin }, status: 401 })
await request(`${base}/session`, { headers: { Origin: 'https://cross-origin.invalid' }, status: 403 })
await request(`${base}/ws-ticket`, { method: 'POST', status: 403 })

console.log(JSON.stringify({
  origin, tlsVerification: !insecure, version: expectedVersion, rendererCommit: info.hermes_commit,
  buildInput: info.build_input_sha256, dynamicRendererAssetCount: dynamicRendererAssets.size,
  hermesInstanceID, canaryMarker,
  // Only the operator's separate log scan can prove the canary is absent from
  // application/edge logs; response checks alone do not establish that claim.
  logCanaryCheck: 'pending operator scan of all ClawManager Pod access/error logs',
  checkCount: checks.length,
  statusCounts: checks.reduce((counts, check) => ({ ...counts, [check.status]: (counts[check.status] ?? 0) + 1 }), {}),
}, null, 2))
