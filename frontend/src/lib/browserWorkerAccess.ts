// Renew before the HttpOnly cookie expires; renewal need not reload the desktop.
export function browserWorkerRenewalDelay(expiresAt?: string, now = Date.now()): number | null {
  if (!expiresAt) return null;
  const expires = Date.parse(expiresAt);
  if (!Number.isFinite(expires)) return null;
  return Math.max(1_000, expires - now - 60_000);
}

export function browserWorkerAccessIsFresh(expiresAt?: string, now = Date.now()): boolean {
  return !!expiresAt && Date.parse(expiresAt) > now + 60_000;
}

export async function refreshBrowserWorkerAccess(
  renew: () => Promise<boolean>,
  reload: () => void,
): Promise<void> {
  // Never reload using a stale/expired cookie if renewal failed.
  if (await renew()) reload();
}
