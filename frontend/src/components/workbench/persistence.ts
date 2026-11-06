import { normalizeLoopbackPreviewURL } from "../../lib/nativePreviewBridge.ts";
import { initialWorkbench, MAX_WORKBENCH_TABS, workbenchReducer, type WorkbenchState } from "./state.ts";

export interface WorkbenchView {
  state: WorkbenchState;
  visible: boolean;
}
type ViewStorage = Pick<Storage, "getItem" | "setItem">;
const sensitiveParameter = /token|password|passwd|secret|credential|authorization|api.?key|signature|^(auth|authcode|sig|code|key|session)$/i;

export function isRestoredPreviewCurrent(state: WorkbenchState, id: string, address: string, generation: number): boolean {
  return state.generation === generation && state.tabs.some(tab => tab.id === id && tab.kind === "browser" &&
    !tab.hidden && !tab.stale && tab.address === address && !tab.url && !tab.previewError &&
    (tab.restorePreview === true || tab.previewPending === true));
}

export function workbenchViewKey(userId: number | null | undefined, instanceId: number | null | undefined): string | null {
  return Number.isSafeInteger(userId) && userId! > 0 && Number.isSafeInteger(instanceId) && instanceId! > 0
    ? `cm:workbench:v1:${userId}:${instanceId}` : null;
}

function storedAddress(value: unknown): string | null {
  if (value === "" || value === undefined) return "";
  if (typeof value !== "string") return null;
  const normalized = normalizeLoopbackPreviewURL(value.includes("://") ? value : `http://${value}`);
  if (!normalized) return null;
  const url = new URL(normalized);
  for (const key of [...url.searchParams.keys()]) if (sensitiveParameter.test(key)) url.searchParams.delete(key);
  // Hash routes are useful, but OAuth fragments must never become durable UI state.
  if (url.hash && [...new URLSearchParams(url.hash.slice(1)).keys()].some(key => sensitiveParameter.test(key))) url.hash = "";
  return url.href;
}

export function readWorkbenchView(key: string | null, generation?: number, storage?: ViewStorage): WorkbenchView {
  const fallback = { state: initialWorkbench(generation ?? 0), visible: true };
  if (!key) return fallback;
  try {
    const raw = (storage ?? window.localStorage).getItem(key);
    if (!raw || raw.length > 280000) return fallback;
    const saved = JSON.parse(raw);
    if (saved?.version !== 1 || !Number.isSafeInteger(saved.generation) || saved.generation < 0 || !Array.isArray(saved.tabs)) return fallback;
    const state = initialWorkbench(saved.generation);
    const ids = new Set<string>();
    for (const entry of saved.tabs) {
      if (state.tabs.length >= MAX_WORKBENCH_TABS) break;
      if (!entry || typeof entry.id !== "string" || !entry.id || entry.id.length > 128 || ids.has(entry.id)) continue;
      const address = storedAddress(entry.address);
      if (address === null) continue;
      ids.add(entry.id);
      state.tabs.push({ id: entry.id, kind: "browser", title: address, address, hidden: entry.hidden === true, stale: entry.stale === true, restorePreview: !!address && entry.stale !== true });
    }
    if (["files", "git", "preview"].includes(saved.panel)) state.panel = saved.panel;
    state.active = state.tabs.find(tab => tab.id === saved.active && !tab.hidden)?.id ?? state.tabs.find(tab => !tab.hidden)?.id;
    return { state: generation === undefined ? state : workbenchReducer(state, { type: "generation", generation }), visible: saved.visible !== false };
  } catch { return fallback; }
}

export function writeWorkbenchView(key: string | null, view: WorkbenchView, storage?: ViewStorage): void {
  if (!key) return;
  const tabs = view.state.tabs.filter(tab => tab.kind === "browser").slice(0, MAX_WORKBENCH_TABS).flatMap(tab => {
    const address = storedAddress(tab.address);
    return address === null ? [] : [{ id: tab.id, address, hidden: tab.hidden, stale: tab.stale }];
  });
  try {
    (storage ?? window.localStorage).setItem(key, JSON.stringify({ version: 1, generation: view.state.generation, panel: view.state.panel, active: view.state.active, visible: view.visible, tabs }));
  } catch { /* Storage can be disabled or full; the current workbench remains usable. */ }
}
