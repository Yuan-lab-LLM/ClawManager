const RENDERER_PATHS = new Set(["/hermes-desktop-web/", "/hermes-desktop-web/index.html"]);

const HERMES_PAGES = new Set(["settings", "command-center", "skills", "messaging", "webhooks", "artifacts", "cron", "profiles", "agents", "starmap"]);

export function hermesSessionRoute(value: unknown): string | null {
  if (value === "#/" || value === "#" || value === "") return "#/";
  if (typeof value !== "string" || !value.startsWith("#/")) return null;
  try {
    const id = decodeURIComponent(value.slice(2));
    return /^[A-Za-z0-9_:-]{1,160}$/.test(id) && !HERMES_PAGES.has(id) ? `#/${encodeURIComponent(id)}` : null;
  } catch { return null; }
}

function hermesSessionKey(userId: number | undefined, instanceId: number): string | null {
  return Number.isSafeInteger(userId) && userId! > 0 && Number.isSafeInteger(instanceId) && instanceId > 0
    ? `clawmanager:hermes-session:v1:${userId}:${instanceId}` : null;
}

export function readHermesSessionRoute(storage: Storage, userId: number | undefined, instanceId: number): string | null {
  const key = hermesSessionKey(userId, instanceId);
  if (!key) return null;
  try { const value = storage.getItem(key); return value === null ? null : hermesSessionRoute(value); } catch { return null; }
}

export function writeHermesSessionRoute(storage: Storage, userId: number | undefined, instanceId: number, value: unknown): void {
  const key = hermesSessionKey(userId, instanceId), route = hermesSessionRoute(value);
  if (!key || route === null) return;
  try { storage.setItem(key, route); } catch { /* Navigation still works when storage is blocked/full. */ }
}

// Keep the renderer's drafts/transcript storage isolated. Only its non-secret
// session route is remembered by the authenticated Manager, across frame loads.
export function rememberHermesSession(frame: HTMLIFrameElement, storage: Storage, userId: number | undefined, instanceId: number): () => void {
  let detach = () => {};
  const attach = () => {
    detach();
    const child = frame.contentWindow;
    if (!child) return;
    try {
      const url = new URL(child.location.href);
      url.hash = "";
      if (!resolveHermesDesktopRendererUrl(url.href, instanceId, window.location.origin)) return;
      const save = () => writeHermesSessionRoute(storage, userId, instanceId, child.location.hash);
      const history = child.history, push = history.pushState, replace = history.replaceState;
      const pushState: History["pushState"] = function (this: History, ...args) { push.apply(this, args); save(); };
      const replaceState: History["replaceState"] = function (this: History, ...args) { replace.apply(this, args); save(); };
      history.pushState = pushState;
      history.replaceState = replaceState;
      child.addEventListener("hashchange", save);
      child.addEventListener("popstate", save);
      save();
      detach = () => {
        child.removeEventListener("hashchange", save);
        child.removeEventListener("popstate", save);
        if (history.pushState === pushState) history.pushState = push;
        if (history.replaceState === replaceState) history.replaceState = replace;
      };
    } catch { /* An unavailable or cross-origin frame has no restorable route. */ }
  };
  frame.addEventListener("load", attach);
  attach();
  return () => { frame.removeEventListener("load", attach); detach(); };
}

const UNAVAILABLE_REASONS: Readonly<Record<string, string>> = {
  feature_disabled: "featureDisabled",
  runtime_capability_unsupported: "runtimeUnsupported",
  runtime_unsupported: "runtimeUnsupported",
  runtime_auth_unavailable: "securityUnavailable",
  runtime_origin_unavailable: "securityUnavailable",
  ticket_store_unavailable: "securityUnavailable",
  unsupported_instance: "unsupportedInstance",
  team_not_supported: "unsupportedInstance",
  runtime_unavailable: "runtimeNotReady",
  instance_not_running: "runtimeNotReady",
  runtime_not_ready: "runtimeNotReady",
};

export function resolveHermesDesktopView({
  instanceId, instanceAvailable, pending, failed, capability,
}: {
  instanceId: number;
  instanceAvailable: boolean;
  pending: boolean;
  failed: boolean;
  capability?: { instance_id: number; available: boolean; reason?: string };
}): { mode: "unavailable" | "desktop" | "pending"; reason?: string } {
  if (!instanceAvailable) return { mode: "unavailable" };
  if (pending && !capability && !failed) {
    return { mode: "pending", reason: "checking" };
  }
  const matchesInstance = capability?.instance_id === instanceId;
  if (!failed && matchesInstance && capability.available === true) {
    return { mode: "desktop" };
  }
  // Only fixed, translated explanations reach the UI, never raw upstream errors.
  const reason = failed ? "probeFailed"
    : matchesInstance && typeof capability.reason === "string" && Object.hasOwn(UNAVAILABLE_REASONS, capability.reason)
      ? UNAVAILABLE_REASONS[capability.reason] : "capabilityUnknown";
  return { mode: "unavailable", reason };
}

export function resolveHermesDesktopRendererUrl(value: string | undefined, instanceId: number, origin: string): string | null {
  if (!value || !Number.isSafeInteger(instanceId) || instanceId <= 0) return null;
  try {
    const url = new URL(value, origin);
    if (url.origin !== origin || url.username || url.password || url.hash || !RENDERER_PATHS.has(url.pathname)) return null;
    // Only the non-secret instance identifier belongs in the renderer URL.
    if (url.searchParams.get("instance_id") !== String(instanceId)) return null;
    if ([...url.searchParams.keys()].some((key) => key !== "instance_id") || url.searchParams.getAll("instance_id").length !== 1) return null;
    return `${url.pathname}${url.search}`;
  } catch {
    return null;
  }
}

export function isHermesDesktopFrameMessage(
  event: Pick<MessageEvent, "origin" | "source" | "data">,
  frameWindow: Window | null,
  instanceId: number,
  origin: string,
): event is MessageEvent<{ type: "clawmanager:hermes-desktop:ready" | "clawmanager:hermes-desktop:error"; instanceId: number }> {
  if (!frameWindow || event.source !== frameWindow || event.origin !== origin) return false;
  if (!event.data || typeof event.data !== "object" || event.data.instanceId !== instanceId) return false;
  return event.data.type === "clawmanager:hermes-desktop:ready" || event.data.type === "clawmanager:hermes-desktop:error";
}

export function hermesDesktopRenewalDelay(expiresAt: string | undefined, now = Date.now()): number | null {
  const expires = expiresAt ? Date.parse(expiresAt) : Number.NaN;
  if (!Number.isFinite(expires) || expires <= now) return null;
  // Renew a minute early; bounded to avoid rapid loops from invalid server expiries.
  return Math.max(1000, Math.min(expires - now - 60_000, 8 * 60_000));
}
