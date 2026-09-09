// A managed dashboard lease belongs to the CM origin and a single instance.
// Never send its iframe to VITE_BACKEND_ORIGIN or put credentials in its URL.
export function resolveHermesDashboardUrl(value: string | null, instanceId: number, origin: string): string | null {
  if (!value) return null;
  try {
    const url = new URL(value, origin);
    const path = `/api/v1/instances/${instanceId}/proxy/chat`;
    if (url.origin !== origin || url.username || url.password || url.search || url.hash || (url.pathname !== path && url.pathname !== `${path}/`)) return null;
    return url.pathname;
  } catch {
    return null;
  }
}

export function isHermesDashboardErrorDocument(text: string): boolean {
  if (text.length > 1024) return false;
  try {
    const body = JSON.parse(text) as { success?: unknown; error?: unknown };
    return body.success === false && typeof body.error === 'string'
      && ['desktop_session_required', 'desktop_access_denied', 'desktop_unavailable', 'desktop_upstream_unavailable'].includes(body.error);
  } catch {
    return false;
  }
}
