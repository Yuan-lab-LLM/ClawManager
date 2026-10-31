import { useCallback, useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { ownerTeamService, type OwnerTeamCapabilities, type OwnerTeamSummary } from '../services/ownerTeamService';

export function useOwnerTeams(owner?: string) {
  const [params, setParams] = useSearchParams();
  const [capabilities, setCapabilities] = useState<OwnerTeamCapabilities | null>(null);
  const [teams, setTeams] = useState<OwnerTeamSummary[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [page, setPage] = useState(1);
  const [loadedOwner, setLoadedOwner] = useState(owner);
  const sequence = useRef(0);
  const query = params.get('team_q') || '';
  const positiveID = (key: string) => { const n = Number(params.get(key)); return Number.isSafeInteger(n) && n > 0 ? n : null; };
  const selectedID = positiveID('selected_team_id');
  const expandedID = positiveID('expanded_team_id');
  const leftTab = params.get('workspace_tab') === 'teams' ? 'teams' : 'instances';
  const setPresentation = useCallback((values: Record<string, string | null>) => {
    const next = new URLSearchParams(window.location.search);
    Object.entries(values).forEach(([key, value]) => value === null ? next.delete(key) : next.set(key, value));
    next.delete('token'); setParams(next, { replace: true });
  }, [setParams]);
  const refresh = useCallback(async (signal?: AbortSignal) => {
    if (!owner) return;
    const request = ++sequence.current;
    setLoading(true);
    try {
      const caps = await ownerTeamService.capabilities(signal);
      if (request !== sequence.current || signal?.aborted) return;
      setCapabilities(caps);
      if (!caps.enabled) { setTeams([]); setTotal(0); setError(''); return; }
      const result = await ownerTeamService.list(1, 20, query, signal);
      for (let currentPage=2; currentPage<=page && result.teams.length<result.total; currentPage++) {
        const more = await ownerTeamService.list(currentPage, 20, query, signal);
        result.teams.push(...more.teams);
      }
      if (request !== sequence.current || signal?.aborted) return;
      setTeams(result.teams || []); setTotal(result.total); setError('');
    } catch (error) { if (request === sequence.current && !signal?.aborted) setError(error instanceof Error ? error.message : '团队列表读取失败'); }
    finally { if (request === sequence.current && !signal?.aborted) setLoading(false); }
  }, [owner, page, query]);
  useEffect(() => { sequence.current++; setLoadedOwner(owner); setCapabilities(null); setTeams([]); setTotal(0); setError(''); setPage(1); }, [owner]);
  useEffect(() => { setPage(1); }, [query]);
  useEffect(() => {
    const controller = new AbortController(); void refresh(controller.signal);
    const timer = window.setInterval(() => { if (!document.hidden) void refresh(controller.signal); }, 10_000);
    return () => { controller.abort(); sequence.current++; clearInterval(timer); };
  }, [refresh]);
  const currentOwner = loadedOwner === owner;
  return { owner: owner || '', capabilities: currentOwner ? capabilities : null, enabled: currentOwner && capabilities?.enabled === true, teams: currentOwner ? teams : [], total: currentOwner ? total : 0, loading, error: currentOwner ? error : '', query, selectedID: selectedID || (currentOwner ? teams[0]?.id : null) || null, expandedID, leftTab,
    setQuery: (q: string) => setPresentation({ team_q: q || null }),
    setLeftTab: (tab: 'instances' | 'teams') => setPresentation({ workspace_tab: tab }),
    select: (id: number) => setPresentation({ selected_team_id: String(id), dashboard_tab: 'team', view: 'overview' }),
    expand: (id: number) => setPresentation({ expanded_team_id: expandedID === id ? null : String(id), selected_team_id: String(id), dashboard_tab: 'team', view: 'overview' }),
    refresh: () => refresh(), loadMore: () => setPage(p => p + 1), hasMore: teams.length < total,
  };
}
export type OwnerTeamsState = ReturnType<typeof useOwnerTeams>;
