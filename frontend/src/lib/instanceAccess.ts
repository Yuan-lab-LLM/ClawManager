/** Install a renewed capability on the runtime origin while preserving its UI. */
export async function refreshDedicatedRuntimeCookie(accessURL: string | null) {
  if (!accessURL) return;

  const target = new URL(accessURL, window.location.href);
  const token = target.searchParams.get("token");
  if (target.origin === window.location.origin || !token) return;

  const refreshURL = new URL("/__clawmanager_access_refresh", target.origin);
  refreshURL.searchParams.set("token", token);
  const response = await window.fetch(refreshURL.toString(), {
    method: "GET",
    credentials: "include",
    mode: "no-cors",
    cache: "no-store",
    referrerPolicy: "no-referrer",
    signal: AbortSignal.timeout(15_000),
  });
  // Cross-origin responses are opaque; readable failures must still retry.
  if (response.type !== "opaque" && !response.ok) {
    throw new Error("Failed to refresh instance access cookie");
  }
}
