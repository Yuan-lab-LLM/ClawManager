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
export interface WorkbenchState {
  generation: number;
  panel: WorkbenchPanel;
  tabs: WorkbenchTab[];
  git: GitViewState;
  active?: string;
}
export type WorkbenchEvent =
  | { type: "panel"; panel: WorkbenchPanel }
  | { type: "git.patch"; patch: Partial<GitViewState> }
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
  | { type: "generation"; generation: number };
export function initialWorkbench(generation: number): WorkbenchState {
  return {
    generation,
    panel: "files",
    tabs: [],
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
    case "git.patch":
      return { ...state, git: { ...state.git, ...event.patch } };
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
              tab.id === event.session.id ? { ...tab, hidden: false } : tab,
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
              tab.id === event.id ? { ...tab, hidden: true } : tab,
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
            })),
          };
    default:
      return state;
  }
}
