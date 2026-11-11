export interface BrowserOriginMetadata {
  browser_origin_mode?: "chrome-single-host";
  manager_origin?: string;
}

// A credentialless partition lives for this document. Never reuse it for a
// different instance, including after visiting an intermediate list or portal.
let documentInstance: number | undefined;
let navigating = false;

export function allowBrowserInstance(
  instanceId: number | null | undefined,
  metadata?: BrowserOriginMetadata,
): boolean {
  if (navigating) return false;
  if (metadata?.browser_origin_mode === "chrome-single-host") {
    let manager: URL;
    try {
      manager = new URL(metadata.manager_origin ?? "");
      if (!["http:", "https:"].includes(manager.protocol)) return false;
    } catch {
      return false;
    }
    if (manager.origin !== window.location.origin) {
      navigating = true;
      const current = new URL(window.location.href);
      window.location.replace(manager.origin + current.pathname + current.search + current.hash);
      return false;
    }
    if (documentInstance === undefined && instanceId) documentInstance = instanceId;
  }
  if (documentInstance !== undefined && instanceId && instanceId !== documentInstance) {
    navigating = true;
    window.location.replace(window.location.href);
    return false;
  }
  return true;
}

export function supportsCredentiallessPreview(): boolean {
  return "credentialless" in HTMLIFrameElement.prototype;
}

export function trustedPreviewViewer(url: string | undefined, metadata?: BrowserOriginMetadata): string | undefined {
  if (!url) return undefined;
  try {
    const viewer = new URL(url, window.location.href);
    const manager = new URL(metadata?.manager_origin ?? window.location.origin);
    return viewer.origin === manager.origin && viewer.pathname === "/__clawmanager_browser/view"
      ? viewer.href
      : undefined;
  } catch {
    return undefined;
  }
}
