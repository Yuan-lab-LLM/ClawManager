import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'

// Exercise the actual njs source with a minimal nginx request facade. These
// synthetic keys/tokens are local fixtures and never authenticate to a cluster.
const source = readFileSync(new URL('../deployments/nginx/njs/desktop_auth.js', import.meta.url), 'utf8')
const nginx = readFileSync(new URL('../deployments/nginx/nginx.conf', import.meta.url), 'utf8')
const key = 'hermes-dashboard-edge-test-only'
const fallback = 'http://127.0.0.1:9001'

function loadEdge(env = { INSTANCE_ACCESS_TOKEN_SECRET: key }) {
  const context = {
    Buffer,
    Date,
    process: { env },
    require(name) {
      assert.equal(name, 'crypto')
      return crypto
    },
  }
  const executable = source.replace(/export default \{ resolveTarget, cleanUri \};\s*$/, 'globalThis.edge = { resolveTarget, cleanUri };')
  assert.notEqual(executable, source, 'Expected the njs module export')
  vm.runInNewContext(executable, context, { filename: 'desktop_auth.js' })
  return context.edge
}

function token(overrides = {}) {
  const header = Buffer.from(JSON.stringify({ alg: 'HS256', typ: 'JWT' })).toString('base64url')
  const payload = Buffer.from(JSON.stringify({
    token_type: 'instance_access', instance_id: 42, instance_type: 'openclaw', upstream: 'runtime-42.test:8443',
    exp: Math.floor(Date.now() / 1000) + 600, ...overrides,
  })).toString('base64url')
  const input = `${header}.${payload}`
  return `${input}.${crypto.createHmac('sha256', key).update(input).digest('base64url')}`
}

function request({ cookie = '', query = '', id = '42', variables = {}, args } = {}) {
  const path = `/api/v1/instances/${id}/proxy/chat`
  const params = new URLSearchParams(query)
  return {
    variables: { inst_id: id, request_uri: path + (query ? `?${query}` : ''), ...variables },
    uri: path,
    args: args ?? Object.fromEntries(params),
    headersIn: cookie ? { Cookie: cookie } : {},
    error() {},
  }
}

test('Dashboard cookie is only a backend routing hint, including forged/empty values', () => {
  const edge = loadEdge()
  for (const value of ['opaque-cm-lease', 'forged.not-a-valid.signature', '']) {
    assert.equal(edge.resolveTarget(request({ cookie: `cm_hermes_dashboard_42=${value}` })), fallback)
  }
  // Routing does not require nginx to validate the backend-owned lease format.
  assert.equal(loadEdge({}).resolveTarget(request({ cookie: 'cm_hermes_dashboard_42=forged' })), fallback)
})

test('Dashboard cookie wins over otherwise valid direct-proxy JWTs', () => {
  const direct = token()
  assert.equal(loadEdge().resolveTarget(request({
    cookie: `unrelated=x; cm_hermes_dashboard_42=forged; instance_access_42=${direct}`,
    query: `token=${direct}`,
  })), fallback)
})

test('Cookie names must match the current instance exactly', () => {
  const edge = loadEdge()
  for (const cookie of ['cm_hermes_dashboard_43=forged', 'cm_hermes_dashboard_420=forged', 'prefix_cm_hermes_dashboard_42=forged']) {
    assert.equal(edge.resolveTarget(request({ cookie })), 'deny')
  }
  assert.equal(edge.resolveTarget(request({
    cookie: 'cm_hermes_dashboard_42=forged', variables: { inst_id: '', runtime_inst_id: '42' },
  })), fallback)
})

test('No Dashboard cookie preserves the existing direct and legacy token flows', () => {
  const edge = loadEdge()
  const direct = token()
  assert.equal(edge.resolveTarget(request({ query: `token=${direct}` })), 'https://runtime-42.test:8443')
  assert.equal(edge.resolveTarget(request({ cookie: `instance_access_42=${direct}` })), 'https://runtime-42.test:8443')
  assert.equal(edge.resolveTarget(request({ query: `token=${token({ upstream: '' })}` })), fallback)
  assert.equal(edge.resolveTarget(request()), 'deny')
  assert.equal(edge.resolveTarget(request({ query: 'token=forged' })), 'deny')
  assert.equal(edge.resolveTarget(request({ query: `token=${token({ instance_id: 43 })}` })), 'deny')
  assert.equal(edge.resolveTarget(request({ query: `token=${token({ exp: 1 })}` })), 'deny')
  assert.equal(loadEdge({}).resolveTarget(request({ query: `token=${direct}` })), 'deny')
})

test('A refreshed query JWT still rotates an expired legacy cookie', () => {
  const fresh = token({ upstream: 'fresh-runtime.test:8443' })
  assert.equal(loadEdge().resolveTarget(request({
    cookie: `instance_access_42=${token({ exp: 1 })}`, query: `token=${fresh}`,
  })), 'https://fresh-runtime.test:8443')
})

test('Verified legacy Hermes JWTs never use historical upstream claims, regardless of mode', () => {
  const edge = loadEdge()
  for (const instanceType of ['hermes', 'Hermes', ' HERMES ']) {
    for (const instanceMode of [undefined, 'pro', 'lite']) {
      const legacy = token({ instance_type: instanceType, instance_mode: instanceMode })
      assert.equal(edge.resolveTarget(request({ query: `token=${legacy}` })), fallback)
      assert.equal(edge.resolveTarget(request({ cookie: `instance_access_42=${legacy}` })), fallback)
    }
  }
  for (const instanceType of ['openclaw', 'opencode', 'deepseek-harness', 'ubuntu']) {
    const legacy = token({ instance_type: instanceType })
    assert.equal(edge.resolveTarget(request({ query: `token=${legacy}` })), 'https://runtime-42.test:8443')
  }
  assert.equal(edge.resolveTarget(request({ query: `token=${token({ instance_type: 'hermes', exp: 1 })}` })), 'deny')
})

test('cleanUri removes CM access JWTs but preserves BFF WS tickets and business query', () => {
  const edge = loadEdge()
  for (const access of [token(), token({ exp: 1 })]) {
    const r = request({
      cookie: 'cm_hermes_dashboard_42=opaque-cm-lease',
      query: `channel=chat%2F42&token=${access}&ticket=cm-one-use-ticket&resume=1`,
    })
    assert.equal(edge.resolveTarget(r), fallback)
    assert.equal(edge.cleanUri(r), '/api/v1/instances/42/proxy/chat?channel=chat%2F42&ticket=cm-one-use-ticket&resume=1')
  }
  const r = request({ cookie: 'cm_hermes_dashboard_42=opaque-cm-lease', query: 'ticket=cm-one-use-ticket&channel=chat-42' })
  assert.equal(edge.cleanUri(r), r.variables.request_uri)
})

test('Other runtimes retain their own token query while CM cookie authorizes routing', () => {
  const r = request({ cookie: `instance_access_42=${token()}`, query: 'token=runtime-owned-token&channel=chat%2F42' })
  const edge = loadEdge()
  assert.equal(edge.resolveTarget(r), 'https://runtime-42.test:8443')
  assert.equal(edge.cleanUri(r), r.variables.request_uri)
})

test('nginx proxy logs contain paths, never raw requests, query, or credential headers', () => {
  for (const name of ['desktop_proxy', 'hermes_desktop']) {
    const match = nginx.match(new RegExp(`log_format\\s+${name}\\s+([\\s\\S]*?);`))
    assert(match, `Missing nginx log format ${name}`)
    const variables = [...match[1].matchAll(/\$[A-Za-z_][A-Za-z_0-9]*/g)].map(item => item[0])
    assert(variables.includes('$uri'), `${name} must log only the path`)
    for (const forbidden of ['$request', '$request_uri', '$args', '$query_string', '$desktop_clean_uri', '$http_cookie', '$http_authorization', '$http_sec_websocket_protocol']) {
      assert(!variables.includes(forbidden), `${name} logs sensitive variable ${forbidden}`)
    }
  }
  assert.match(nginx, /proxy_pass \$desktop_target\$desktop_clean_uri;/, 'Forwarding must still preserve the cleaned business query')
  assert.match(nginx, /access_log \/var\/log\/nginx\/access\.log desktop_proxy;/)
  assert.match(nginx, /client_max_body_size 1024k;/, 'The dedicated Desktop limit must not be rewritten by start.sh')
})

test('SSO bootstrap preserves the external Host port and rejection/error logs stay private', () => {
  const defaults = nginx.slice(nginx.indexOf('server_name _;'), nginx.indexOf('location = /healthz'))
  assert.match(defaults, /access_log \/var\/log\/nginx\/access\.log hermes_desktop;/)
  assert.match(defaults, /error_log \/dev\/null;/)
  const locations = [...nginx.matchAll(/location\s+([^\n]+?)\s*\{([\s\S]*?)\n        \}/g)]
  const access = locations.find(([, header]) => header.includes('/access$'))
  assert(access, 'Missing dedicated same-origin SSO bootstrap location')
  assert.match(access[2], /proxy_set_header Host \$http_host;/)
  for (const selector of ['/access$', '/hermes-desktop', '(?<inst_id>', '@desktop_denied']) {
    const location = locations.find(([, header]) => header.includes(selector))
    assert(location, `Missing protected location ${selector}`)
    assert.match(location[2], /access_log \/var\/log\/nginx\/access\.log (desktop_proxy|hermes_desktop);/)
    // nginx error_log has no variable/redaction formatter and normally embeds
    // raw request URIs on upstream failures, including unused/replayed tickets.
    assert.match(location[2], /error_log \/dev\/null;/)
  }
})
