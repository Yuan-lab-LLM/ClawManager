import { useWorkbenchServices } from "./WorkbenchServices";
import { allowBrowserInstance } from "../../lib/browserOrigin";
import type { KeyboardEvent } from "react";
import { useCallback, useEffect, useReducer, useState, useRef, useId } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Files,
  GitBranch,
  Globe,
  Terminal,
  Plus,
  ChevronDown,
  Maximize2,
  Minimize2,
  X,
  RefreshCw,
  Square,
  RotateCcw,
  Activity,
} from "lucide-react";
import { useI18n } from "../../contexts/I18nContext";
import { useAuthStore } from "../../stores/authStore";
import { WorkspaceFileManager } from "../WorkspaceFileManager";
import {
  workbenchError,
  workbenchPreviewError,
} from "../../services/instanceWorkbenchService";
import type { TerminalSession, WorkbenchProcess } from "../../types/workbench";
import {
  initialWorkbench,
  workbenchReducer,
  MAX_WORKBENCH_TABS,
} from "./state";
import { GitProjects } from "./GitProjects";
import { WorkbenchTerminal } from "./WorkbenchTerminal";
import { WorkbenchBrowser } from "./WorkbenchBrowser";
import type { WorkbenchState, WorkbenchEvent, WorkspaceFileView } from "./state";
import { isRestoredPreviewCurrent } from "./persistence";
import "./workbench.css";
function navigateTabs(event: KeyboardEvent<HTMLDivElement>) {
  if (
    event.altKey ||
    event.ctrlKey ||
    event.metaKey ||
    !["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)
  )
    return;
  const tabs = Array.from(
    event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]'),
  );
  if (!tabs.length) return;
  const current = Math.max(
    0,
    tabs.findIndex((tab) => tab === document.activeElement),
  );
  const next =
    event.key === "Home"
      ? 0
      : event.key === "End"
        ? tabs.length - 1
        : (current + (event.key === "ArrowRight" ? 1 : -1) + tabs.length) %
          tabs.length;
  event.preventDefault();
  tabs[next].click();
  tabs[next].focus();
}
interface Props {
  instanceId: number;
  runtimeGeneration: number;
  isRunning: boolean;
  visible?: boolean;
  initialPath?: string;
  onSelectDirectory?: (path: string) => void | Promise<void>;
  selectingDirectory?: boolean;
  sharedState?: WorkbenchState;
  onWorkbenchEvent?: (event: WorkbenchEvent) => void;
}
export function InstanceWorkbench({
  instanceId,
  runtimeGeneration,
  isRunning,
  visible = true,
  sharedState,
  onWorkbenchEvent,
  ...files
}: Props) {
  const { t } = useI18n();
  const { service, workspace, scope, canWrite, workspaceKey } = useWorkbenchServices();
  const viewID = useId();
  const [localState, localDispatch] = useReducer(
    workbenchReducer,
    runtimeGeneration,
    initialWorkbench,
  );
  const state = sharedState ?? localState;
  const dispatch = onWorkbenchEvent ?? localDispatch;
  const full = state.full && visible;
  const showSessions = state.showSessions;
  const setFull = useCallback((full: boolean) => dispatch({ type: "view.patch", patch: { full } }), [dispatch]);
  const setShowSessions = useCallback((showSessions: boolean) => dispatch({ type: "view.patch", patch: { showSessions } }), [dispatch]);
  const onFilesChange = useCallback((view: WorkspaceFileView) => dispatch({ type: "files.patch", patch: view }), [dispatch]);
  const userId = useAuthStore(auth => auth.user?.id);
  const rootRef = useRef<HTMLElement>(null);
  const programTabsRef = useRef<HTMLDivElement>(null);
  const activeTabRef = useRef<HTMLDivElement>(null);
  const [rendered, setRendered] = useState(false);
  useEffect(() => {
    const node = rootRef.current;
    if (!node) return;
    const observer = new ResizeObserver(() => {
      const rect = node.getBoundingClientRect();
      setRendered(rect.width > 0 && rect.height > 0);
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, []);
  const [newMenu, setNewMenu] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [sessions, setSessions] = useState<TerminalSession[]>([]);
  const [processes, setProcesses] = useState<WorkbenchProcess[]>([]);
  const caps = useQuery({
    queryKey: [
      "workbench-capabilities",
      scope,
      instanceId,
      runtimeGeneration,
      isRunning,
    ],
    queryFn: async ({ signal }) => {
      const result = await service.capabilities(instanceId, signal);
      if (!allowBrowserInstance(instanceId, result)) throw new Error("Instance navigation in progress");
      return result;
    },
    enabled: isRunning,
    staleTime: 30000,
    // Only this read-only probe can be replayed; mutations keep explicit user retries.
    retry: (failureCount, error) => {
      const code = (error as { response?: { data?: { error_code?: string } } })
        ?.response?.data?.error_code;
      return failureCount < 3 && !!code &&
        ["instance_busy", "stop_unconfirmed", "cancelled", "timeout"].includes(code);
    },
    retryDelay: (attempt) => 2000 * 2 ** attempt,
  });
  const generation = caps.data?.generation ?? runtimeGeneration;
  useEffect(() => {
    dispatch({ type: "generation", generation });
  }, [generation, dispatch]);
  useEffect(() => {
    if (caps.data?.browser_origin_mode === "chrome-single-host") dispatch({ type: "browser.expireLegacy" });
  }, [caps.data?.browser_origin_mode, state.tabs, dispatch]);
  const scopeAllowed = allowBrowserInstance(instanceId, caps.data);
  const ready = isRunning && !!caps.data && scopeAllowed;
  const restoringTerminals = state.tabs.some(tab => tab.restoreTerminal && !tab.stale && tab.terminalGeneration === generation);
  const terminalRestoration = useQuery({
    queryKey: ["workbench-terminal-restore", scope, userId, instanceId, generation],
    queryFn: ({ signal }) => service.terminals(instanceId, generation, signal),
    enabled: ready && !!caps.data?.terminal && state.generation === generation && restoringTerminals,
    staleTime: 0,
    refetchOnMount: "always",
    retry: false,
  });
  useEffect(() => {
    if (terminalRestoration.data && terminalRestoration.isFetchedAfterMount && !terminalRestoration.isFetching && !terminalRestoration.isError && state.generation === generation) {
      dispatch({ type: "terminal.reconcile", generation, sessions: terminalRestoration.data });
    }
  }, [terminalRestoration.data, terminalRestoration.isFetchedAfterMount, terminalRestoration.isFetching, terminalRestoration.isError, state.generation, generation, dispatch]);
  const currentView = useRef(state);
  currentView.current = state;
  const previewScope = useRef({ instanceId, generation, isRunning });
  previewScope.current = { instanceId, generation, isRunning };
  const mounted = useRef(false);
  const restoredRequests = useRef(new Set<string>());
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);
  useEffect(() => {
    if (!ready || !caps.data?.preview || state.generation !== generation) return;
    for (const tab of state.tabs) {
      if (!tab.restorePreview || tab.hidden || tab.stale || !tab.address) continue;
      const address = tab.address;
      const requestKey = `${generation}:${tab.id}:${address}`;
      if (restoredRequests.current.has(requestKey)) continue;
      restoredRequests.current.add(requestKey);
      dispatch({ type: "browser.navigate", id: tab.id, address, url: "", previewPending: true, generation });
      const isCurrent = () => mounted.current && previewScope.current.instanceId === instanceId && previewScope.current.generation === generation && previewScope.current.isRunning && isRestoredPreviewCurrent(currentView.current, tab.id, address, generation);
      void service.preview(instanceId, generation, address).then(result => {
        if (!isCurrent() || result.generation !== generation) return;
        dispatch({ type: "browser.navigate", id: tab.id, address, url: result.url, viewer_url: result.viewer_url, credentialless: result.credentialless, generation });
      }, error => {
        if (isCurrent()) dispatch({ type: "browser.navigate", id: tab.id, address, url: "", previewError: workbenchPreviewError(error, t), generation });
      });
    }
  }, [service, ready, caps.data?.preview, state.generation, state.tabs, generation, instanceId, dispatch, t]);
  const selected = state.tabs.find(
    (tab) => tab.id === state.active && !tab.hidden,
  );
  const hidden = state.tabs.filter((tab) => tab.hidden);
  useEffect(() => {
    const strip = programTabsRef.current;
    if (!strip || !visible || !rendered || state.panel !== "preview") return;
    const revealActiveTab = () => {
      const active = activeTabRef.current;
      if (!active || !strip.clientWidth || !strip.clientHeight) return;
      const viewport = strip.getBoundingClientRect();
      const tab = active.getBoundingClientRect();
      const offset =
        tab.left < viewport.left
          ? tab.left - viewport.left
          : tab.right > viewport.right
            ? tab.right - viewport.right
            : 0;
      if (offset) strip.scrollBy({ left: offset, behavior: "instant" });
    };
    const observer = new ResizeObserver(revealActiveTab);
    observer.observe(strip);
    revealActiveTab();
    return () => observer.disconnect();
  }, [state.active, state.panel, state.tabs, visible, rendered]);
  const createTerminal = async () => {
    if (state.tabs.length >= MAX_WORKBENCH_TABS) {
      setError(t("workbench.sessionLimit"));
      setNewMenu(false);
      return;
    }
    setNewMenu(false);
    setBusy(true);
    setError("");
    try {
      const session = await service.createTerminal(instanceId, generation);
      dispatch({ type: "terminal.open", session });
    } catch (err) {
      setError(workbenchError(err));
    } finally {
      setBusy(false);
    }
  };
  const sessionList = useCallback(async () => {
    setBusy(true);
    setError("");
    try {
      const [listed, owned] = await Promise.all([
        caps.data?.terminal
          ? service.terminals(instanceId, generation)
          : Promise.resolve([]),
        caps.data?.processes
          ? service.processes(instanceId, generation)
          : Promise.resolve([]),
      ]);
      setSessions(listed);
      setProcesses(owned);
    } catch (err) {
      setError(workbenchError(err));
    } finally {
      setBusy(false);
    }
  }, [caps.data?.terminal, caps.data?.processes, instanceId, generation]);
  useEffect(() => {
    if (showSessions && ready) void sessionList();
  }, [showSessions, ready, sessionList]);
  const endSession = async (session: TerminalSession) => {
    if (!window.confirm(t("workbench.endingConfirm"))) return;
    setBusy(true);
    setError("");
    try {
      const ended = await service.closeTerminal(
        instanceId,
        generation,
        session.id,
      );
      dispatch({ type: "terminal.update", session: ended });
      setSessions((list) =>
        list.map((item) => (item.id === ended.id ? ended : item)),
      );
    } catch (err) {
      setError(workbenchError(err));
    } finally {
      setBusy(false);
    }
  };
  const processAction = async (id: string, action: "stop" | "restart") => {
    setBusy(true);
    setError("");
    try {
      await service.processAction(instanceId, generation, id, action);
      await sessionList();
    } catch (err) {
      setError(workbenchError(err));
    } finally {
      setBusy(false);
    }
  };
  const newBrowser = () => {
    setNewMenu(false);
    if (state.tabs.length >= MAX_WORKBENCH_TABS) {
      setError(t("workbench.sessionLimit"));
      return;
    }
    dispatch({ type: "browser.new", id: crypto.randomUUID() });
  };
  const message = !isRunning
    ? t("workbench.stopped")
    : caps.error
      ? workbenchError(caps.error)
      : caps.isLoading
        ? t("workbench.loading")
        : null;
  return (
    <section
      ref={rootRef}
      className={`wb-root cm-surface ${full ? "wb-fullscreen" : ""}`}
      aria-label={t("workbench.emptyTitle")}
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          setNewMenu(false);
          setShowSessions(false);
          setFull(false);
        }
      }}
    >
      <header className="wb-header">
        <div
          className="wb-panels"
          onKeyDown={navigateTabs}
          role="tablist"
          aria-label={t("workbench.emptyTitle")}
        >
          {(
            [
              { id: "files", icon: Files },
              { id: "git", icon: GitBranch },
              { id: "preview", icon: Globe },
            ] as const
          ).map(({ id, icon: Icon }) => (
            <button
              key={id}
              id={`wb-tab-${viewID}-${id}`}
              role="tab"
              aria-selected={state.panel === id}
              tabIndex={state.panel === id ? 0 : -1}
              aria-controls={`wb-panel-${viewID}-${id}`}
              className={state.panel === id ? "active" : ""}
              onClick={() => dispatch({ type: "panel", panel: id })}
            >
              <Icon size={14} />
              <span>{t(`workbench.${id}`)}</span>
            </button>
          ))}
        </div>
        <button
          className="wb-icon"
          title={t(full ? "workbench.collapse" : "workbench.expand")}
          aria-label={t(full ? "workbench.collapse" : "workbench.expand")}
          onClick={() => setFull(!full)}
        >
          {full ? <Minimize2 size={14} /> : <Maximize2 size={14} />}
        </button>
      </header>
      <div
        id={`wb-panel-${viewID}-files`}
        role="tabpanel"
        aria-labelledby={`wb-tab-${viewID}-files`}
        hidden={state.panel !== "files"}
        className="wb-panel wb-files"
      >
        <WorkspaceFileManager instanceId={instanceId} {...files} service={workspace} canWrite={canWrite} workspaceKey={workspaceKey} view={state.files} onViewChange={onFilesChange} />
      </div>
      <div
        id={`wb-panel-${viewID}-git`}
        role="tabpanel"
        aria-labelledby={`wb-tab-${viewID}-git`}
        hidden={state.panel !== "git"}
        className="wb-panel"
      >
        {message ? (
          <div className="wb-empty">
            <p>{message}</p>
            {isRunning && (
              <button className="wb-button" onClick={() => void caps.refetch()}>
                {t("workbench.retry")}
              </button>
            )}
          </div>
        ) : caps.data?.git ? (
          <GitProjects
            instanceId={instanceId}
            generation={generation}
            enabled={ready}
            visible={visible && rendered && state.panel === "git"}
            view={state.git}
            onChange={(patch) => dispatch({ type: "git.patch", patch })}
          />
        ) : (
          <div className="wb-empty">{t("workbench.upgrade")}</div>
        )}
      </div>
      <div
        id={`wb-panel-${viewID}-preview`}
        role="tabpanel"
        aria-labelledby={`wb-tab-${viewID}-preview`}
        hidden={state.panel !== "preview"}
        className="wb-panel wb-runtime"
      >
        <div className="wb-tabs-row">
          <div
            ref={programTabsRef}
            className="wb-program-tabs"
            onKeyDown={navigateTabs}
            role="tablist"
            aria-label={t("workbench.preview")}
          >
            {state.tabs
              .filter((tab) => !tab.hidden)
              .map((tab) => (
                <div
                  key={tab.id}
                  ref={selected?.id === tab.id ? activeTabRef : undefined}
                  className={`wb-program-tab ${selected?.id === tab.id ? "active" : ""}`}
                >
                  <button
                    id={`wb-program-tab-${viewID}-${tab.id}`}
                    aria-controls={`wb-program-panel-${viewID}-${tab.id}`}
                    role="tab"
                    aria-selected={selected?.id === tab.id}
                    tabIndex={selected?.id === tab.id ? 0 : -1}
                    onClick={() => dispatch({ type: "tab.select", id: tab.id })}
                    title={tab.title}
                  >
                    {tab.kind === "browser" ? (
                      <Globe size={13} />
                    ) : (
                      <Terminal size={13} />
                    )}
                    <span>{tab.title || t("workbench.browser")}</span>
                    {tab.kind === "terminal" && (
                      <i
                        className={
                          tab.stale || !tab.session || tab.session.exited ? "ended" : ""
                        }
                      />
                    )}
                  </button>
                  <button
                    className="wb-close-tab"
                    title={t("workbench.close")}
                    aria-label={`${t("workbench.close")} ${tab.title}`}
                    onClick={() => dispatch({ type: "tab.hide", id: tab.id })}
                  >
                    <X size={12} />
                  </button>
                </div>
              ))}
          </div>
          <div className="wb-create">
            <button
              className="wb-create-button"
              aria-expanded={newMenu}
              aria-controls={`wb-new-${viewID}`}
              disabled={busy || !ready}
              onClick={() => setNewMenu(!newMenu)}
            >
              <Plus size={14} />
              {t("workbench.newTab")}
              <ChevronDown size={12} />
            </button>
            {newMenu && (
              <div
                id={`wb-new-${viewID}`}
                className="wb-popover"
                role="group"
                aria-label={t("workbench.newTab")}
              >
                <button disabled={!caps.data?.preview} onClick={newBrowser}>
                  <Globe size={16} />
                  <span>{t("workbench.browser")}</span>
                </button>
                <button
                  disabled={!caps.data?.terminal}
                  onClick={() => void createTerminal()}
                >
                  <Terminal size={16} />
                  <span>{t("workbench.terminal")}</span>
                </button>
                <button
                  disabled={!ready || busy}
                  onClick={() => {
                    setShowSessions(!showSessions);
                    setNewMenu(false);
                  }}
                  aria-expanded={showSessions}
                >
                  <Activity size={16} />
                  <span>{t("workbench.sessions")}</span>
                </button>
                {selected?.kind === "terminal" &&
                  selected.session &&
                  !selected.session.exited &&
                  !selected.stale && (
                    <button
                      className="wb-button danger"
                      disabled={busy}
                      onClick={() => {
                        setNewMenu(false);
                        void endSession(selected.session!);
                      }}
                    >
                      <Square size={12} />
                      {t("workbench.end")}
                    </button>
                  )}
              </div>
            )}
          </div>
        </div>
        {(message || error) && (
          <div
            className={error ? "wb-error" : "wb-notice"}
            role={error ? "alert" : "status"}
          >
            {error || message}
            {caps.error && (
              <button className="wb-subtle" onClick={() => void caps.refetch()}>
                <RefreshCw size={12} />
                {t("workbench.retry")}
              </button>
            )}
          </div>
        )}
        {showSessions && (
          <div className="wb-session-drawer">
            <div className="wb-section-label">
              {t("workbench.sessions")}
              <button
                className="wb-icon"
                aria-label={t("workbench.refresh")}
                disabled={busy}
                onClick={() => void sessionList()}
              >
                <RefreshCw size={13} />
              </button>
              <button
                className="wb-icon"
                aria-label={t("workbench.cancel")}
                onClick={() => setShowSessions(false)}
              >
                <X size={14} />
              </button>
            </div>
            {hidden
              .filter(
                (tab) =>
                  tab.kind === "browser" ||
                  !sessions.some((session) => session.id === tab.id),
              )
              .map((tab) => (
                <div className="wb-session-row" key={tab.id}>
                  {tab.kind === "browser" ? (
                    <Globe size={14} />
                  ) : (
                    <Terminal size={14} />
                  )}
                  <span>{tab.title || t(`workbench.${tab.kind}`)}</span>
                  <button
                    className="wb-button"
                    onClick={() => {
                      dispatch({ type: "tab.restore", id: tab.id });
                      setShowSessions(false);
                    }}
                  >
                    {t("workbench.restore")}
                  </button>
                  <button
                    className="wb-icon"
                    aria-label={t("workbench.removeTab")}
                    onClick={() => dispatch({ type: "tab.remove", id: tab.id })}
                  >
                    <X size={13} />
                  </button>
                </div>
              ))}
            {sessions.map((session) => (
              <div className="wb-session-row" key={session.id}>
                <Terminal size={14} />
                <div>
                  <strong>{session.name}</strong>
                  <small>
                    {session.exited
                      ? t("workbench.ended")
                      : session.cwd || t("workbench.root")}
                  </small>
                </div>
                <button
                  className="wb-button"
                  onClick={() => {
                    dispatch({ type: "terminal.open", session });
                    setShowSessions(false);
                  }}
                >
                  {t("workbench.restore")}
                </button>
                {session.exited &&
                  state.tabs.some((tab) => tab.id === session.id) && (
                    <button
                      className="wb-icon"
                      aria-label={t("workbench.removeTab")}
                      onClick={() =>
                        dispatch({ type: "tab.remove", id: session.id })
                      }
                    >
                      <X size={13} />
                    </button>
                  )}
                {!session.exited && (
                  <button
                    className="wb-icon"
                    title={t("workbench.end")}
                    aria-label={t("workbench.end")}
                    disabled={busy}
                    onClick={() => void endSession(session)}
                  >
                    <Square size={13} />
                  </button>
                )}
              </div>
            ))}
            {!sessions.length && !hidden.length && (
              <p className="wb-muted">{t("workbench.noSessions")}</p>
            )}
            {caps.data?.processes && (
              <>
                <div className="wb-section-label">
                  {t("workbench.processes")}
                </div>
                {processes.map((process) => (
                  <div className="wb-session-row" key={process.id}>
                    <Activity size={14} />
                    <div>
                      <strong>{process.name}</strong>
                      <code title={process.command}>{process.command}</code>
                      {process.cwd && (
                        <code title={process.cwd}>{process.cwd}</code>
                      )}
                      <small>{process.state}</small>
                    </div>
                    <button
                      className="wb-icon"
                      title={t("workbench.stop")}
                      aria-label={t("workbench.stop")}
                      disabled={busy || !process.can_stop}
                      onClick={() => void processAction(process.id, "stop")}
                    >
                      <Square size={13} />
                    </button>
                    <button
                      className="wb-icon"
                      title={t(
                        process.can_restart
                          ? "workbench.restart"
                          : "workbench.restartUnavailable",
                      )}
                      aria-label={t("workbench.restart")}
                      disabled={busy || !process.can_restart}
                      onClick={() => void processAction(process.id, "restart")}
                    >
                      <RotateCcw size={13} />
                    </button>
                  </div>
                ))}
                {!processes.length && (
                  <p className="wb-muted">{t("workbench.noProcesses")}</p>
                )}
              </>
            )}
          </div>
        )}
        <div className="wb-live-views">
          {scopeAllowed && state.tabs.map((tab) => (
            <div
              key={tab.id}
              id={`wb-program-panel-${viewID}-${tab.id}`}
              role="tabpanel"
              aria-labelledby={`wb-program-tab-${viewID}-${tab.id}`}
              hidden={selected?.id !== tab.id || showSessions}
              className="wb-live-view"
            >
              {tab.kind === "browser" ? (
                <WorkbenchBrowser
                  instanceId={instanceId}
                  browserOrigin={caps.data}
                  metadataPending={!caps.data}
                  generation={generation}
                  tab={tab}
                  rendered={rendered}
                  enabled={ready && !!caps.data?.preview}
                  onNavigate={(address, url, current, preview) =>
                    dispatch({
                      type: "browser.navigate",
                      id: tab.id,
                      address,
                      url,
                      viewer_url: preview.viewer_url,
                      credentialless: preview.credentialless,
                      generation: current,
                    })
                  }
                />
              ) : (
                tab.session ? (
                  <WorkbenchTerminal
                    instanceId={instanceId}
                    session={tab.session}
                    visible={
                      visible &&
                      rendered &&
                      state.panel === "preview" &&
                      selected?.id === tab.id &&
                      !showSessions
                    }
                    stale={tab.stale || !isRunning}
                    onExit={(session) =>
                      dispatch({ type: "terminal.update", session })
                    }
                  />
                ) : (
                  <div className="wb-empty" role={terminalRestoration.error && tab.restoreTerminal ? "alert" : "status"}>
                    <Terminal size={32} />
                    <p>{tab.stale ? t("workbench.changed") : tab.restoreTerminal ? terminalRestoration.error ? workbenchError(terminalRestoration.error) : t("workbench.loading") : t("workbench.ended")}</p>
                    {tab.restoreTerminal && terminalRestoration.error && <button className="wb-button" disabled={!ready || !caps.data?.terminal || terminalRestoration.isFetching} onClick={() => void terminalRestoration.refetch()}>{t("workbench.reconnect")}</button>}
                  </div>
                )
              )}
            </div>
          ))}
          {!selected && !showSessions && (
            <div className="wb-empty">
              <Globe size={34} />
              <h3>{t("workbench.emptyTitle")}</h3>
              <p>{t("workbench.emptyDetail")}</p>
              <div className="wb-empty-actions">
                <button
                  className="wb-button"
                  disabled={!ready || !caps.data?.preview}
                  onClick={newBrowser}
                >
                  <Globe size={14} />
                  {t("workbench.newBrowser")}
                </button>
                <button
                  className="wb-button"
                  disabled={!ready || !caps.data?.terminal || busy}
                  onClick={() => void createTerminal()}
                >
                  <Terminal size={14} />
                  {t("workbench.newTerminal")}
                </button>
              </div>
            </div>
          )}
        </div>
        <footer className="wb-footer">
          {selected?.kind === "terminal"
            ? t("workbench.hideHint")
            : t("workbench.activityHint")}
        </footer>
      </div>
    </section>
  );
}
