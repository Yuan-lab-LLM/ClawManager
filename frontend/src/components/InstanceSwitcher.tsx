import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { ChevronLeft, ChevronRight, LoaderCircle, PanelLeftClose, Plus, Search, SlidersHorizontal, X } from "lucide-react";
import { useI18n } from "../contexts/I18nContext";
import { getInstanceDisplayStatus } from "../lib/instanceStatus";
import { readInstanceList, readInstanceListView, rememberInstanceList, rememberInstanceTarget, rememberLastInstance, writeInstanceListView } from "../lib/instanceNavigation";
import { instanceService } from "../services/instanceService";
import { useAuthStore } from "../stores/authStore";
import { formatInstanceType, type Instance } from "../types/instance";
import { instanceWorkspaceText } from "./instanceWorkspaceText";
import InstanceOverviewDialog from "./InstanceOverviewDialog";
import AgentStateIcon from "./AgentStateIcon";
import "./instance-switcher.css";

const currentLabels = {
  en: "Current instance", zh: "当前实例", ja: "現在のインスタンス",
  ko: "현재 인스턴스", de: "Aktuelle Instanz",
};

type Props = { current: Instance; onClose?: () => void; onHideList?: () => void; listId?: string };

export default function InstanceSwitcher(props: Props) {
  const userId = useAuthStore((auth) => auth.isAuthenticated ? auth.user?.id : undefined);
  // Restore each account's own view without retaining another account's state.
  return <InstanceSwitcherList key={userId ?? "signed-out"} {...props} userId={userId} />;
}

function InstanceSwitcherList({ current, onClose, onHideList, listId, userId }: Props & { userId?: number }) {
  const { t, locale } = useI18n();
  const navigate = useNavigate();
  const location = useLocation();
  const [initialView] = useState(() => userId ? readInstanceListView(userId) : { query: "", page: 1, scrollTop: 0 });
  const [{ query, page }, setSearch] = useState(() => ({ query: initialView.query, page: initialView.page }));
  const [pendingId, setPendingId] = useState<number | null>(null);
  const [navigationError, setNavigationError] = useState<string | null>(null);
  const search = query.trim();
  const snapshot = useMemo(() => userId ? readInstanceList(userId, search, page) : null, [userId, search, page]);
  const overviewSearch = new URLSearchParams(location.search);
  if (search) overviewSearch.set("q", search);
  const rowsRef = useRef<HTMLDivElement>(null);
  const scrollPosition = useRef(initialView.scrollTop);
  const restoreScroll = useRef(true);
  const previousView = useRef({ query, page });
  const mounted = useRef(true);
  const navigationRequest = useRef(0);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; navigationRequest.current += 1; };
  }, []);
  const result = useQuery({
    queryKey: ["instance-switcher", userId, search, page],
    queryFn: async ({ signal }) => {
      const data = await instanceService.getInstances(page, 50, { query: search || undefined }, signal);
      const auth = useAuthStore.getState();
      const activeView = userId ? readInstanceListView(userId) : null;
      // Persist only a successful live response for the still authenticated
      // account. Placeholder rows and failed/aborted requests never renew it.
      if (userId && !signal.aborted && auth.isAuthenticated && auth.user?.id === userId
        && activeView?.query.trim() === search && activeView.page === page) {
        rememberInstanceList(userId, search, data);
      }
      return data;
    },
    enabled: !!userId,
    initialData: snapshot?.data,
    initialDataUpdatedAt: snapshot?.savedAt,
    // Retain pagination controls while loading a page, never a different search/account.
    placeholderData: (previous, previousQuery) => previousQuery?.queryKey[1] === userId && previousQuery?.queryKey[2] === search ? previous : undefined,
    refetchInterval: 5000,
    // Native-origin isolation still reloads the document; restore its last
    // real rows immediately and let the existing poll update them in place.
    refetchOnMount: false,
    refetchOnWindowFocus: "always",
    refetchOnReconnect: "always",
    retry: 1,
  });
  const instances = result.data?.instances ?? [];
  const totalPages = Math.max(1, Math.ceil((result.data?.total ?? 0) / (result.data?.limit || 50)));
  useLayoutEffect(() => {
    if (previousView.current.query !== query || previousView.current.page !== page) {
      previousView.current = { query, page };
      scrollPosition.current = 0;
      restoreScroll.current = true;
    }
    // A reload may first render an empty query cache. Wait for actual rows so
    // the browser does not clamp the saved position to the loading screen.
    if (restoreScroll.current && result.data && !result.isPlaceholderData && rowsRef.current) {
      rowsRef.current.scrollTop = scrollPosition.current;
      restoreScroll.current = false;
    }
  }, [query, page, result.data, result.isPlaceholderData]);
  useEffect(() => {
    if (userId) writeInstanceListView(userId, { query, page, scrollTop: scrollPosition.current });
  }, [userId, query, page]);
  // A live inventory can shrink while the user is on its last page.
  if (result.data && !result.isPlaceholderData && page > totalPages) {
    setSearch(current => ({ ...current, page: totalPages }));
  }
  const showCurrent = !!userId && !instances.some((instance) => instance.id === current.id);

  const selectInstance = async (target: Instance) => {
    const request = ++navigationRequest.current;
    setNavigationError(null);
    if (!userId) return;
    if (target.id === current.id) {
      setPendingId(null);
      onClose?.();
      return;
    }
    setPendingId(target.id);
    const isCurrentRequest = () => {
      const auth = useAuthStore.getState();
      return mounted.current && request === navigationRequest.current
        && auth.isAuthenticated && auth.user?.id === userId;
    };
    try {
      // Fetch through the owner API before handing metadata to a new document;
      // a list row alone does not establish that access is still authorized.
      const instance = await instanceService.getInstance(target.id);
      if (!isCurrentRequest()) return;
      if (instance.id !== target.id) throw new Error(t("instances.failedToLoad"));
      rememberInstanceTarget(userId, instance);
      rememberLastInstance(userId, instance.id);
      navigate(`/instances/${instance.id}`);
      onClose?.();
    } catch {
      if (isCurrentRequest()) setNavigationError(t("instances.failedToLoad"));
    } finally {
      if (isCurrentRequest()) setPendingId(null);
    }
  };

  const renderRow = (entry: Instance) => {
    // Detail polling and a successful rename are fresher than the list cache.
    const instance = entry.id === current.id ? current : entry;
    const selected = instance.id === current.id;
    const status = getInstanceDisplayStatus(instance.status, instance.stopped_reason);
    const statusKey = `status.${status}`;
    const statusLabel = t(statusKey);
    const displayStatus = statusLabel === statusKey ? instance.status : statusLabel;
    const typeLabel = formatInstanceType(instance.type);
    return <li key={instance.id}>
      <Link to={`/instances/${instance.id}`} onClick={event => {
        if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        event.preventDefault();
        void selectInstance(instance);
      }} className="cm-instance-switcher-row" data-status={status}
        title={`${instance.name} · ${displayStatus}`}
        aria-label={`${instance.name}, ${typeLabel}, ${displayStatus}`}
        aria-busy={pendingId === instance.id || undefined} aria-current={selected ? "page" : undefined}>
        <span className="cm-instance-switcher-logo">
          <AgentStateIcon type={instance.type} state={status} label={`${typeLabel} · ${displayStatus}`} />
        </span>
        <span className="cm-instance-switcher-identity">
          <span className="cm-instance-switcher-name" title={instance.name}>{instance.name}</span>
          <span className="cm-instance-switcher-type" title={typeLabel}>{typeLabel}</span>
        </span>
        {pendingId === instance.id && <LoaderCircle size={16} className="cm-instance-switcher-pending animate-spin" aria-hidden="true" />}
      </Link>
    </li>;
  };

  return <aside className="cm-instance-switcher" aria-label={t("nav.instances")}>
    <InstanceOverviewDialog initialOpen={!onClose && ["q", "status", "type", "mode", "availability", "page"].some(key => new URLSearchParams(location.search).has(key))} initialSearch={`?${overviewSearch.toString()}`} onSelectInstance={instance => { void selectInstance(instance); }} renderTrigger={open => <>
      <header className="cm-instance-switcher-header">
        <h2 title={t("nav.instances")}>{t("nav.instances")}</h2>
        <div className="cm-instance-switcher-actions">
          {onHideList && <button type="button" className="cm-icon-button" data-instance-list-hide aria-label={instanceWorkspaceText(locale).hideList} title={instanceWorkspaceText(locale).hideList} aria-controls={listId} aria-expanded="true" onClick={onHideList}><PanelLeftClose size={16} aria-hidden="true" /></button>}
          {onClose && <button type="button" className="cm-icon-button" aria-label={t("common.close")} onClick={onClose}><X size={18} aria-hidden="true" /></button>}
        </div>
      </header>
      <nav className="cm-instance-switcher-shortcuts">
        <Link to="/instances/new" className="cm-instance-shortcut cm-instance-shortcut-create" title={t("instances.createInstance")}><Plus size={18} aria-hidden="true" /><span>{t("instances.createInstance")}</span></Link>
        <button type="button" className="cm-instance-shortcut" title={instanceWorkspaceText(locale).manage} aria-haspopup="dialog" onClick={open}><SlidersHorizontal size={18} aria-hidden="true" /><span>{instanceWorkspaceText(locale).manage}</span></button>
      </nav>
    </>} />
    <div className="cm-instance-switcher-search">
      <Search size={16} aria-hidden="true" />
      <input type="search" value={query} aria-label={t("instances.searchLabel")}
        placeholder={t("instances.searchPlaceholder")}
        onChange={(event) => setSearch({ query: event.target.value, page: 1 })} />
    </div>
    {pendingId !== null && <span className="sr-only" role="status">{t("common.loading")}</span>}
    {navigationError && <p className="cm-instance-switcher-message cm-instance-switcher-error" role="alert">{navigationError}</p>}
    {showCurrent && <section className="cm-instance-switcher-current" aria-label={currentLabels[locale] ?? currentLabels.en}>
      <h3>{currentLabels[locale] ?? currentLabels.en}</h3>
      <ul>{renderRow(current)}</ul>
    </section>}
    <div ref={rowsRef} className="cm-instance-switcher-scroll" aria-busy={result.isFetching} onScroll={event => {
      if (!userId || restoreScroll.current) return;
      scrollPosition.current = event.currentTarget.scrollTop;
      writeInstanceListView(userId, { query, page, scrollTop: scrollPosition.current });
    }}>
      {(result.isPending || result.isPlaceholderData) && <p className="cm-instance-switcher-message" role="status">{t("common.loading")}</p>}
      {result.isError && <div className="cm-instance-switcher-message cm-instance-switcher-error" role="status">
        <p>{t("instances.failedToLoad")}</p>
        <button type="button" className="app-button-secondary" disabled={result.isFetching} onClick={() => void result.refetch()}>{t("common.retry")}</button>
      </div>}
      {result.data && instances.length === 0 && <p className="cm-instance-switcher-message" role="status">
        {t(search ? "instances.noMatchingInstances" : "instances.noInstances")}
      </p>}
      <ul className="cm-instance-switcher-rows">{instances.map(renderRow)}</ul>
    </div>
    {result.data && totalPages > 1 && <nav className="cm-instance-switcher-pagination" aria-label={t("instances.pageOf", { page, totalPages })}>
      <button type="button" className="cm-icon-button" aria-label={t("instances.previous")}
        disabled={page <= 1 || result.isFetching} onClick={() => setSearch({ query, page: page - 1 })}><ChevronLeft size={18} aria-hidden="true" /></button>
      <span>{page} / {totalPages}</span>
      <button type="button" className="cm-icon-button" aria-label={t("instances.nextPage")}
        disabled={page >= totalPages || result.isFetching} onClick={() => setSearch({ query, page: page + 1 })}><ChevronRight size={18} aria-hidden="true" /></button>
    </nav>}
  </aside>;
}
