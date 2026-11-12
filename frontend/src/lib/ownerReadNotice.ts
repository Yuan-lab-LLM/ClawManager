export const OWNER_SESSION_INVALID = 'clawmanager:owner-session-invalid';
export const OWNER_SESSION_MESSAGE = '访问会话已失效，请从智慧协作平台重新进入。';

export function isOwnerSessionFailure(error: unknown): boolean {
  return (error as {response?: {status?: number}} | null)?.response?.status === 401;
}

// These endpoints authenticate the portal itself, not an upstream Runtime.
// A Runtime/access/workspace 401 must not invalidate the owner session.
export function isOwnerSessionEndpoint(url: string): boolean {
  return ['/session','/session/refresh','/instances','/dashboard','/growth','/growth/sync','/visit','/reports'].includes(url.split('?')[0]);
}

// Team and file requests use another HTTP client. Only the Core's explicit
// owner-auth rejection can invalidate their session, never an upstream 401.
export function isConfirmedOwnerSessionFailure(error: unknown): boolean {
  if (!isOwnerSessionFailure(error)) return false;
  const data = (error as {response?:{data?:{error?:string;message?:string}}}).response?.data;
  return [data?.error, data?.message].some(value =>
    value === 'IEI system session is required' || value === 'IEI system session is invalid or expired');
}

export interface ReadFailure { count: number; firstAt: number; lastAt: number }
export function nextReadFailure(previous: ReadFailure | null, now: number): ReadFailure {
  return {count:(previous?.count ?? 0)+1,firstAt:previous?.firstAt ?? now,lastAt:now};
}
export function isPersistentReadFailure(failure: ReadFailure): boolean {
  return failure.count >= 2 && failure.lastAt - failure.firstAt >= 60_000;
}
