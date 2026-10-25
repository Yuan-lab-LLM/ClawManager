export type ApiRequest = {
  path: string; method?: string; body?: unknown; profile?: string | null; connectionId?: string | null
}
export type SessionDescriptor = {
  available: boolean; instance_id: number; reason?: string; capabilities: string[]
  versions: { bridge_version: string; hermes_commit?: string }
}
type Envelope<T> = { success: boolean; data: T }

const READ_QUERIES: Record<string, string[]> = {
  '/api/status': [], '/api/config': [], '/api/config/defaults': [], '/api/config/schema': [], '/api/model/info': [], '/api/profiles': [],
  '/api/model/options': ['explicit_only', 'refresh', 'include_unconfigured'],
  '/api/sessions': ['limit', 'offset', 'min_messages', 'archived', 'order', 'source', 'exclude_sources'],
  '/api/profiles/sessions': ['limit', 'offset', 'min_messages', 'archived', 'order', 'source', 'exclude_sources'],
  '/api/profiles/sessions/sidebar': ['recents_profile', 'recents_limit', 'cron_limit', 'messaging_limit', 'recents_exclude', 'messaging_exclude']
}
const MANAGED_RUNTIME_PREFIXES = ['/actions', '/analytics', '/audio', '/cron', '/curator', '/env', '/gateway', '/git', '/hermes', '/learning', '/mcp', '/memory', '/messaging', '/model', '/ops', '/pairing', '/plugins', '/providers', '/skills', '/tools', '/webhooks']

export function validateApiRequest(request: ApiRequest): string {
  if (request.profile || request.connectionId) throw new Error('此页面不支持切换 Profile 或外部连接。')
  const method = request.method || 'GET'
  const configWrite = method === 'PUT' && request.path.split('?')[0] === '/api/config'
  const preliminaryPath = request.path.split('?')[0]
  const managedSessionWrite = /^(PATCH|DELETE)$/.test(method) && /^\/api\/sessions\/[A-Za-z0-9_:-]{1,160}$/.test(preliminaryPath)
  const managedRuntimeRequest = /^(GET|POST|PUT|PATCH|DELETE)$/.test(method) &&
    /^\/api\/[A-Za-z0-9_.:-]+(?:\/[A-Za-z0-9_.:-]+)*$/.test(preliminaryPath) &&
    MANAGED_RUNTIME_PREFIXES.some(prefix => preliminaryPath === `/api${prefix}` || preliminaryPath.startsWith(`/api${prefix}/`))
  if (method !== 'GET' && !configWrite && !managedRuntimeRequest && !managedSessionWrite) throw new Error('不支持此 API 操作。')
  if (method === 'GET' && request.body !== undefined) throw new Error('读取请求不能携带正文。')
  if (configWrite) {
    const body = request.body as Record<string, unknown> | undefined
    if (!body || typeof body !== 'object' || Array.isArray(body) || Object.keys(body).length !== 1 ||
      !body.config || typeof body.config !== 'object' || Array.isArray(body.config)) throw new Error('配置正文无效。')
    if (request.path.includes('?')) throw new Error('配置写入不能携带查询参数。')
  }
  if (!request.path.startsWith('/api/') || /[\\#]/.test(request.path) || request.path.split('?').length > 2 || request.path.length > 4096) throw new Error('非法 API 路径。')
  const [pathname, query = ''] = request.path.split('?')
  if (pathname.includes('%')) throw new Error('非法 API 路径。')
  let allowed = Object.hasOwn(READ_QUERIES, pathname) ? READ_QUERIES[pathname] : undefined
  if (/^\/api\/sessions\/[A-Za-z0-9_:-]{1,160}$/.test(pathname)) allowed = []
  if (/^\/api\/sessions\/[A-Za-z0-9_:-]{1,160}\/messages$/.test(pathname)) allowed = ['limit', 'offset', 'order', 'include_compacted']
  const genericManaged = (managedRuntimeRequest && pathname !== '/api/model/options') || managedSessionWrite
  if (!allowed && !genericManaged) throw new Error('未开放的 Hermes API。')
  const params = new URLSearchParams(query)
  for (const [key, value] of params) {
    if (genericManaged) {
      if (!/^[A-Za-z0-9_-]{1,80}$/.test(key) || value.length > 4096 || params.getAll(key).length !== 1) throw new Error('非法 API 参数。')
      if (managedSessionWrite && (key !== 'profile' || !['default', 'current'].includes(value))) throw new Error('不支持其他 Profile。')
      continue
    }
    if (!allowed) throw new Error('未开放的 Hermes API。')
    if ((!allowed.includes(key) && key !== 'profile') || params.getAll(key).length !== 1) throw new Error('未开放的 API 参数。')
    if (key === 'profile' || key === 'recents_profile') {
      if (!['default', 'current'].includes(value) && !(value === 'all' && (key === 'recents_profile' || pathname === '/api/profiles/sessions'))) throw new Error('不支持其他 Profile。')
    } else if (['limit', 'offset', 'min_messages', 'recents_limit', 'cron_limit', 'messaging_limit'].includes(key)) {
      const max = key === 'offset' || key === 'min_messages' ? 10000 : pathname.endsWith('/messages') ? 500 : 100
      if (!/^\d+$/.test(value) || Number(value) > max) throw new Error('API 分页超出范围。')
    } else if (['explicit_only', 'refresh', 'include_unconfigured', 'include_compacted'].includes(key)) {
      if (!['1', 'true'].includes(value)) throw new Error('非法 API 开关。')
    } else if (key === 'archived') {
      if (!['exclude', 'include', 'only'].includes(value)) throw new Error('非法会话过滤。')
    } else if (key === 'order') {
      if (!['recent', 'created', 'latest', 'oldest'].includes(value)) throw new Error('非法会话排序。')
    } else if (value.length > 256 || !/^[a-zA-Z0-9_-]+(?:,[a-zA-Z0-9_-]+)*$/.test(value) || (key === 'source' && value.includes(','))) {
      throw new Error('非法会话来源过滤。')
    }
  }
  return request.path
}

export function createBridge(instanceId: number, origin: string, requestFetch: typeof fetch = fetch) {
  if (!Number.isSafeInteger(instanceId) || instanceId <= 0) throw new Error('无效的实例 ID。')
  const base = `/api/v1/instances/${instanceId}/hermes-desktop`
  async function read<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
    const response = await requestFetch(`${base}${path}`, {
      method, credentials: 'same-origin', redirect: 'error', cache: 'no-store',
      headers: method === 'GET' ? {} : { 'Content-Type': 'application/json' },
      body: method === 'GET' ? undefined : JSON.stringify(body ?? {}),
      signal: AbortSignal.timeout(60_000)
    })
    if (!response.ok) {
      if (response.status === 401) throw new Error('访问会话已失效，请在 ClawManager 中重新打开 Desktop Web。')
      throw new Error(`Hermes 服务请求失败（HTTP ${response.status}），可切回经典 Dashboard。`)
    }
    return response.json() as Promise<T>
  }
  const unsupported = (name: string) => async () => { throw new Error(`Desktop Web 未开放原生能力：${name}`) }
  return Object.freeze({
    async session(): Promise<SessionDescriptor> {
      const response = await read<Envelope<SessionDescriptor>>('/session')
      if (!response.success || response.data.instance_id !== instanceId || !response.data.available) throw new Error('当前实例暂不支持 Desktop Web。')
      if (response.data.versions.bridge_version !== '1') throw new Error('Desktop Web 协议版本不兼容。')
      return response.data
    },
    async getConnection() { return { baseUrl: `${origin}${base}`, mode: 'remote', authMode: 'oauth' } },
    async getGatewayWsUrl() {
      const response = await read<Envelope<{ url: string }>>('/ws-ticket', 'POST')
      if (!response.success) throw new Error('无法建立 WebSocket 会话。')
      const url = new URL(response.data.url, origin)
      const page = new URL(origin)
      if (url.host !== page.host || url.pathname !== `${base}/ws` || url.username || url.password || url.hash ||
        !['http:', 'https:', 'ws:', 'wss:'].includes(url.protocol) ||
        [...url.searchParams.keys()].some(key => key !== 'ticket') || url.searchParams.getAll('ticket').length !== 1 || !url.searchParams.get('ticket')) {
        throw new Error('拒绝非当前实例的 WebSocket 地址。')
      }
      url.protocol = page.protocol === 'https:' ? 'wss:' : 'ws:'
      return { url: url.toString(), authMode: 'oauth' as const }
    },
    async api<T>(request: ApiRequest): Promise<T> {
      return read<T>(validateApiRequest(request), request.method || 'GET', request.body)
    },
    readFileDataUrl: unsupported('readFileDataUrl'), readFileText: unsupported('readFileText'),
    readDir: unsupported('readDir'), selectPaths: unsupported('selectPaths'), selectSavePath: unsupported('selectSavePath'),
    openSessionWindow: unsupported('openSessionWindow'), openSessionInTerminal: unsupported('openSessionInTerminal'),
    openWindow: unsupported('openWindow'), getConnections: unsupported('getConnections'),
    setConnectionConfig: unsupported('setConnectionConfig'), updateApp: unsupported('updateApp')
  })
}
export type BrowserBridge = ReturnType<typeof createBridge>
