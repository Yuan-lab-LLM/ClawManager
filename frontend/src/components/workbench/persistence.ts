import { normalizeLoopbackPreviewURL } from "../../lib/nativePreviewBridge.ts";
import { initialWorkbench, MAX_WORKBENCH_TABS, workbenchReducer, type GitViewState, type WorkspaceFileView, type WorkbenchState } from "./state.ts";

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

// The server fingerprint is non-secret and changes when share authority changes.
// Never use a bearer share code, signed cookie, or owner scope as a durable key.
export function sharedWorkbenchViewKey(scope: unknown, instanceId: number | undefined): string | null {
  return typeof scope === "string" && /^[a-f0-9]{64}$/.test(scope) && Number.isSafeInteger(instanceId) && instanceId! > 0
    ? `cm:workbench:share:v1:${scope}:${instanceId}` : null;
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

function boundedText(value: unknown, max = 8192): string {
  return typeof value === "string" && value.length <= max && !value.includes("\0") ? value : "";
}

function storedGit(value: Partial<GitViewState> | undefined): GitViewState {
  const git = initialWorkbench(0).git;
  if (!value || typeof value !== "object") return git;
  for (const key of ["folderPath", "path", "name", "branch", "message"] as const) git[key] = boundedText(value[key], key === "message" ? 65536 : 8192);
  git.selected = typeof value.selected === "string" ? boundedText(value.selected) : null;
  git.adding = value.adding === "clone" || value.adding === "import" ? value.adding : null;
  git.creatingBranch = value.creatingBranch === true;
  git.mode = value.mode === "history" ? "history" : "changes";
  git.url = boundedText(value.url);
  try {
    const url = new URL(git.url);
    url.username = ""; url.password = "";
    for (const key of [...url.searchParams.keys()]) if (sensitiveParameter.test(key)) url.searchParams.delete(key);
    if (url.hash && [...new URLSearchParams(url.hash.slice(1)).keys()].some(key => sensitiveParameter.test(key))) url.hash = "";
    git.url = url.href;
  } catch {
    // SSH remotes remain useful drafts; malformed URLs may contain unfinished credentials.
    if (git.url.includes("://")) git.url = "";
  }
  if (value.selection && typeof value.selection.repo === "string" && typeof value.selection.path === "string") git.selection = { repo: boundedText(value.selection.repo), path: boundedText(value.selection.path), staged: value.selection.staged === true };
  return git;
}

function storedFiles(value: Partial<WorkspaceFileView> | undefined): WorkspaceFileView {
  const files = initialWorkbench(0).files;
  if (!value || typeof value !== "object") return files;
  if (typeof value.currentPath === "string") files.currentPath = boundedText(value.currentPath);
  files.previewPath = typeof value.previewPath === "string" ? boundedText(value.previewPath) : null;
  files.editPath = typeof value.editPath === "string" ? boundedText(value.editPath) : null;
  files.pageIndex = Number.isSafeInteger(value.pageIndex) && value.pageIndex! >= 0 && value.pageIndex! <= 1000000 ? value.pageIndex! : 0;
  files.sortColumn = value.sortColumn === "size" || value.sortColumn === "modified" ? value.sortColumn : "name";
  files.sortDirection = value.sortDirection === "desc" ? "desc" : "asc";
  files.expanded = value.expanded === true;
  files.searchOpen = value.searchOpen === true;
  files.searchInput = boundedText(value.searchInput, 1024);
  return files;
}

export function readWorkbenchView(key: string | null, generation?: number, storage?: ViewStorage): WorkbenchView {
  const fallback = { state: initialWorkbench(generation ?? 0), visible: true };
  if (!key) return fallback;
  try {
    const raw = (storage ?? window.localStorage).getItem(key);
    if (!raw || raw.length > 500000) return fallback;
    const saved = JSON.parse(raw);
    if (saved?.version !== 1 || !Number.isSafeInteger(saved.generation) || saved.generation < 0 || !Array.isArray(saved.tabs)) return fallback;
    const state = initialWorkbench(saved.generation);
    const ids = new Set<string>();
    for (const entry of saved.tabs) {
      if (state.tabs.length >= MAX_WORKBENCH_TABS) break;
      if (!entry || typeof entry.id !== "string" || !entry.id || entry.id.length > 128 || ids.has(entry.id)) continue;
      if (entry.kind === "terminal") {
        if (!/^[a-zA-Z0-9_-]+$/.test(entry.id) || !Number.isSafeInteger(entry.generation) || entry.generation < 0) continue;
        ids.add(entry.id);
        const stale = entry.stale === true || entry.generation !== saved.generation;
        state.tabs.push({ id: entry.id, kind: "terminal", title: boundedText(entry.title, 256), hidden: entry.hidden === true, stale, terminalGeneration: entry.generation, terminalEnded: entry.ended === true, restoreTerminal: !stale });
        continue;
      }
      const address = storedAddress(entry.address);
      if (address === null) continue;
      ids.add(entry.id);
      state.tabs.push({ id: entry.id, kind: "browser", title: address, address, hidden: entry.hidden === true, stale: entry.stale === true, restorePreview: !!address && entry.stale !== true });
    }
    state.git = storedGit(saved.git);
    state.files = storedFiles(saved.files);
    state.full = saved.full === true;
    state.showSessions = saved.showSessions === true;
    if (["files", "git", "preview"].includes(saved.panel)) state.panel = saved.panel;
    state.active = state.tabs.find(tab => tab.id === saved.active && !tab.hidden)?.id ?? state.tabs.find(tab => !tab.hidden)?.id;
    return { state: generation === undefined ? state : workbenchReducer(state, { type: "generation", generation }), visible: saved.visible !== false };
  } catch { return fallback; }
}

export function writeWorkbenchView(key: string | null, view: WorkbenchView, storage?: ViewStorage): void {
  if (!key) return;
  const tabs = view.state.tabs.slice(0, MAX_WORKBENCH_TABS).flatMap<Record<string, unknown>>(tab => {
    if (tab.kind === "terminal") return [{ id: tab.id, kind: tab.kind, title: tab.title, hidden: tab.hidden, stale: tab.stale, generation: tab.session?.generation ?? tab.terminalGeneration ?? view.state.generation, ended: tab.session?.exited ?? tab.terminalEnded ?? false }];
    const address = storedAddress(tab.address);
    return address === null ? [] : [{ id: tab.id, address, hidden: tab.hidden, stale: tab.stale }];
  });
  try {
    (storage ?? window.localStorage).setItem(key, JSON.stringify({ version: 1, generation: view.state.generation, panel: view.state.panel, full: view.state.full, showSessions: view.state.showSessions, active: view.state.active, visible: view.visible, tabs, git: storedGit(view.state.git), files: storedFiles(view.state.files) }));
  } catch { /* Storage can be disabled or full; the current workbench remains usable. */ }
}
