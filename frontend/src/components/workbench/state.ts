import type { TerminalSession } from "../../types/workbench";
export const MAX_WORKBENCH_TABS = 32;
export type WorkbenchPanel = "files" | "git" | "preview";
export interface WorkbenchTab {
  id: string;
  kind: "browser" | "terminal";
  title: string;
  hidden: boolean;
  stale: boolean;
  address?: string;
  url?: string;
  viewer_url?: string;
  credentialless?: boolean;
  previewPending?: boolean;
  previewError?: string;
  restorePreview?: boolean;
  restoreTerminal?: boolean;
  terminalGeneration?: number;
  terminalEnded?: boolean;
  session?: TerminalSession;
}
export interface GitViewState {
  selected: string | null;
  folderPath: string;
  adding: "clone" | "import" | null;
  path: string;
  name: string;
  url: string;
  branch: string;
  creatingBranch: boolean;
  message: string;
  mode: "changes" | "history";
  busy: boolean;
  error: string;
  selection: { repo: string; path: string; staged: boolean } | null;
}
export interface WorkspaceFileView {
  currentPath?: string;
  previewPath: string | null;
  editPath: string | null;
  pageIndex: number;
  sortColumn: "name" | "size" | "modified";
  sortDirection: "asc" | "desc";
  expanded: boolean;
  searchOpen: boolean;
  searchInput: string;
}
export interface WorkbenchState {
  generation: number;
  panel: WorkbenchPanel;
  full: boolean;
  showSessions: boolean;
  tabs: WorkbenchTab[];
  git: GitViewState;
  files: WorkspaceFileView;
  active?: string;
}
export type WorkbenchEvent =
  | { type: "panel"; panel: WorkbenchPanel }
  | { type: "view.patch"; patch: Partial<Pick<WorkbenchState, "full" | "showSessions">> }
  | { type: "git.patch"; patch: Partial<GitViewState> }
  | { type: "files.patch"; patch: Partial<WorkspaceFileView> }
  | { type: "browser.new"; id: string }
  | { type: "browser.expireLegacy" }
  | {
      type: "browser.navigate";
      id: string;
      address: string;
      url: string;
      viewer_url?: string;
      credentialless?: boolean;
      previewPending?: boolean;
      previewError?: string;
      generation: number;
    }
  | { type: "terminal.open"; session: TerminalSession }
  | {
      type: "tab.select" | "tab.restore" | "tab.hide" | "tab.remove";
      id: string;
    }
  | { type: "terminal.update"; session: TerminalSession }
  | { type: "terminal.reconcile"; generation: number; sessions: TerminalSession[] }
  | { type: "generation"; generation: number };
export function initialWorkbench(generation: number): WorkbenchState {
  return {
    generation,
    panel: "files",
    full: false,
    showSessions: false,
    tabs: [],
    files: { previewPath: null, editPath: null, pageIndex: 0, sortColumn: "name", sortDirection: "asc", expanded: false, searchOpen: false, searchInput: "" },
    git: {
      selected: null,
      folderPath: "",
      adding: null,
      path: "",
      name: "",
      url: "",
      branch: "",
      creatingBranch: false,
      message: "",
      mode: "changes",
      busy: false,
      error: "",
      selection: null,
    },
  };
}
export function workbenchReducer(
  state: WorkbenchState,
  event: WorkbenchEvent,
): WorkbenchState {
  switch (event.type) {
    case "view.patch":
      return { ...state, ...event.patch };
    case "git.patch":
      return { ...state, git: { ...state.git, ...event.patch } };
    case "files.patch":
      return { ...state, files: { ...state.files, ...event.patch } };
    case "panel":
      return { ...state, panel: event.panel };
    case "browser.new":
      if (state.tabs.length >= MAX_WORKBENCH_TABS) return state;
      return {
        ...state,
        panel: "preview",
        active: event.id,
        tabs: [
          ...state.tabs,
          {
            id: event.id,
            kind: "browser",
            title: "",
            address: "",
            hidden: false,
            stale: false,
          },
        ],
      };
    case "browser.expireLegacy": {
      const legacy = (tab: WorkbenchTab) => tab.kind === "browser" && !!tab.url && tab.credentialless !== true;
      if (!state.tabs.some(legacy)) return state;
      return {
        ...state,
        tabs: state.tabs.map((tab) => legacy(tab)
          ? { ...tab, stale: true, url: undefined, viewer_url: undefined, credentialless: undefined }
          : tab),
      };
    }
    case "browser.navigate":
      if (event.generation !== state.generation) return state;
      return {
        ...state,
        tabs: state.tabs.map((tab) =>
          tab.id === event.id
            ? {
                ...tab,
                address: event.address,
                url: event.url,
                viewer_url: event.viewer_url,
                credentialless: event.credentialless,
                previewPending: event.previewPending,
                previewError: event.previewError,
                restorePreview: undefined,
                title: event.address,
                stale: false,
              }
            : tab,
        ),
      };
    case "terminal.open": {
      if (event.session.generation !== state.generation) return state;
      const existing = state.tabs.some((tab) => tab.id === event.session.id);
      if (!existing && state.tabs.length >= MAX_WORKBENCH_TABS) return state;
      return {
        ...state,
        panel: "preview",
        active: event.session.id,
        tabs: existing
          ? state.tabs.map((tab) =>
              tab.id === event.session.id ? { ...tab, session: event.session, hidden: false, restoreTerminal: undefined, terminalEnded: event.session.exited, terminalGeneration: event.session.generation } : tab,
            )
          : [
              ...state.tabs,
              {
                id: event.session.id,
                kind: "terminal",
                title: event.session.name,
                session: event.session,
                hidden: false,
                stale: false,
              },
            ],
      };
    }
    case "terminal.reconcile": {
      if (event.generation !== state.generation || !state.tabs.some(tab => tab.restoreTerminal)) return state;
      return { ...state, tabs: state.tabs.map(tab => {
        if (!tab.restoreTerminal || tab.kind !== "terminal" || tab.stale || tab.terminalGeneration !== event.generation) return tab;
        const session = event.sessions.find(session => session.id === tab.id && session.generation === event.generation);
        return { ...tab, session, title: session?.name ?? tab.title, restoreTerminal: undefined, terminalEnded: session?.exited ?? true };
      }) };
    }
    case "terminal.update":
      return {
        ...state,
        tabs: state.tabs.map((tab) =>
          tab.id === event.session.id
            ? { ...tab, session: event.session }
            : tab,
        ),
      };
    case "tab.select":
    case "tab.restore":
      return {
        ...state,
        panel: "preview",
        active: event.id,
        tabs: state.tabs.map((tab) =>
          tab.id === event.id ? { ...tab, hidden: false } : tab,
        ),
      };
    case "tab.hide":
    case "tab.remove": {
      const tabs =
        event.type === "tab.remove"
          ? state.tabs.filter((tab) => tab.id !== event.id)
          : state.tabs.map((tab) =>
              tab.id === event.id ? { ...tab, hidden: true, previewPending: undefined } : tab,
            );
      return {
        ...state,
        tabs,
        active:
          state.active === event.id
            ? tabs.find((tab) => !tab.hidden)?.id
            : state.active,
      };
    }
    case "generation":
      return event.generation === state.generation
        ? state
        : {
            ...state,
            generation: event.generation,
            tabs: state.tabs.map((tab) => ({
              ...tab,
              stale: true,
              url: undefined,
              viewer_url: undefined,
              credentialless: undefined,
              previewPending: undefined,
              previewError: undefined,
              restorePreview: undefined,
              restoreTerminal: undefined,
            })),
          };
    default:
      return state;
  }
}
