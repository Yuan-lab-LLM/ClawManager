export function normalizeLoopbackPreviewURL(value: unknown): string | null {
  if (typeof value !== "string" || value.length > 8192 || Array.from(value).some((character) => character.charCodeAt(0) <= 32 || character.charCodeAt(0) === 127)) return null;
  try {
    const url = new URL(value);
    if (!["http:", "https:"].includes(url.protocol) || url.username || url.password ||
        !["localhost", "127.0.0.1", "[::1]", "0.0.0.0"].includes(url.hostname)) return null;
    if (url.port && Number(url.port) < 1) return null;
    if (url.hostname === "0.0.0.0") url.hostname = "localhost";
    return url.href;
  } catch { return null; }
}

export function attachNativePreviewBridge(
  frame: HTMLIFrameElement,
  src: string,
  onPreview: (address: string, isCurrent: () => boolean) => void,
): () => void {
  const origin = new URL(src, window.location.href).origin;
  const source = frame.contentWindow;
  const messageType = "clawmanager:preview-bridge:open:" + crypto.randomUUID();
  let active = true;
  const isCurrent = () => active && frame.isConnected && frame.contentWindow === source;
  const activate = () => {
    if (isCurrent()) source?.postMessage({ type: messageType.replace(":open:", ":activate:") }, origin);
  };
  const receive = (event: MessageEvent) => {
    if (!isCurrent() || event.source !== source || event.origin !== origin ||
        !window.navigator.userActivation?.isActive ||
        event.data?.type !== messageType) return;
    const address = normalizeLoopbackPreviewURL(event.data.url);
    if (address) onPreview(address, isCurrent);
  };
  window.addEventListener("message", receive);
  frame.addEventListener("load", activate);
  activate();
  return () => {
    active = false;
    window.removeEventListener("message", receive);
    frame.removeEventListener("load", activate);
  };
}
