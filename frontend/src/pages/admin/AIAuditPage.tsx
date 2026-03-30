import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import AdminLayout from '../../components/AdminLayout';
import {
  adminService,
  type AIAuditListResponse,
  type AuditTraceDetail,
} from '../../services/adminService';
import { useI18n } from '../../contexts/I18nContext';

const jsonTokenClassMap: Record<string, string> = {
  key: 'text-[#f5c07a]',
  string: 'text-[#98c379]',
  number: 'text-[#d19a66]',
  boolean: 'text-[#56b6c2]',
  null: 'text-[#c678dd]',
  punctuation: 'text-[#abb2bf]',
};

function renderCodeBlock(content?: string) {
  const normalized = (content ?? '').trim();
  if (!normalized) {
    return <span className="text-[#8b949e]">-</span>;
  }

  try {
    const parsed = JSON.parse(normalized);
    const pretty = JSON.stringify(parsed, null, 2);
    const lines = pretty.split('\n');

    return (
      <>
        {lines.map((line, index) => (
          <div key={`${index}-${line}`} className="whitespace-pre-wrap break-words">
            {tokenizeJsonLine(line).map((token, tokenIndex) => (
              <span key={`${index}-${tokenIndex}`} className={jsonTokenClassMap[token.type]}>
                {token.value}
              </span>
            ))}
          </div>
        ))}
      </>
    );
  } catch {
    return <span className="whitespace-pre-wrap break-words text-[#f7f4f2]">{content}</span>;
  }
}

function tokenizeJsonLine(line: string): Array<{ value: string; type: keyof typeof jsonTokenClassMap }> {
  const tokens: Array<{ value: string; type: keyof typeof jsonTokenClassMap }> = [];
  const pattern = /("(?:\\.|[^"\\])*")|(\b-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b)|(\btrue\b|\bfalse\b)|(\bnull\b)|([{}\[\]:,])/g;

  let lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(line)) !== null) {
    if (match.index > lastIndex) {
      tokens.push({ value: line.slice(lastIndex, match.index), type: 'punctuation' });
    }

    const [fullMatch, stringValue, numberValue, booleanValue, nullValue, punctuationValue] = match;
    if (stringValue) {
      const nextChar = line.slice(pattern.lastIndex).trimStart().charAt(0);
      const tokenType: keyof typeof jsonTokenClassMap = nextChar === ':' ? 'key' : 'string';
      tokens.push({ value: fullMatch, type: tokenType });
    } else if (numberValue) {
      tokens.push({ value: fullMatch, type: 'number' });
    } else if (booleanValue) {
      tokens.push({ value: fullMatch, type: 'boolean' });
    } else if (nullValue) {
      tokens.push({ value: fullMatch, type: 'null' });
    } else if (punctuationValue) {
      tokens.push({ value: fullMatch, type: 'punctuation' });
    }

    lastIndex = pattern.lastIndex;
  }

  if (lastIndex < line.length) {
    tokens.push({ value: line.slice(lastIndex), type: 'punctuation' });
  }

  return tokens;
}

function flowKindLabel(kind: string, t: (key: string, vars?: any) => string) {
  const keyMap: Record<string, string> = {
    user_message: 'aiAuditPage.flowUserMessage',
    llm_call: 'aiAuditPage.flowLlmCall',
    tool_call: 'aiAuditPage.flowToolCall',
    tool_output: 'aiAuditPage.flowToolOutput',
    assistant_response: 'aiAuditPage.flowAssistantResponse',
    assistant_message: 'aiAuditPage.flowAssistantMessage',
  };

  return t(keyMap[kind] || 'aiAuditPage.flowNode');
}

function flowKindTone(kind: string) {
  switch (kind) {
    case 'user_message':
      return 'bg-[#eef7ff] text-[#356a9f] border-[#d9e8f8]';
    case 'llm_call':
      return 'bg-[#fff3ec] text-[#b46c50] border-[#f3d7ca]';
    case 'tool_call':
      return 'bg-[#f6f0ff] text-[#6f4ea5] border-[#e3d7f4]';
    case 'tool_output':
      return 'bg-[#eefbf2] text-[#2f7a45] border-[#d2ecd9]';
    default:
      return 'bg-[#f5f1ec] text-[#6d645f] border-[#eadfd8]';
  }
}

function parseTimestamp(value?: string) {
  if (!value) {
    return null;
  }

  const parsed = new Date(value);
  const timestamp = parsed.getTime();
  return Number.isNaN(timestamp) ? null : timestamp;
}

function formatAbsoluteTimestamp(value: string | undefined, locale: string) {
  const timestamp = parseTimestamp(value);
  if (timestamp === null) {
    return '-';
  }

  return new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'medium',
  }).format(timestamp);
}

function formatRelativeTimestamp(value: string | undefined, locale: string) {
  const timestamp = parseTimestamp(value);
  if (timestamp === null) {
    return '-';
  }

  const diff = timestamp - Date.now();
  const absDiff = Math.abs(diff);
  const formatter = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
  const ranges: Array<{ limit: number; divisor: number; unit: Intl.RelativeTimeFormatUnit }> = [
    { limit: 60_000, divisor: 1_000, unit: 'second' },
    { limit: 3_600_000, divisor: 60_000, unit: 'minute' },
    { limit: 86_400_000, divisor: 3_600_000, unit: 'hour' },
    { limit: 604_800_000, divisor: 86_400_000, unit: 'day' },
    { limit: 2_592_000_000, divisor: 604_800_000, unit: 'week' },
    { limit: 31_536_000_000, divisor: 2_592_000_000, unit: 'month' },
    { limit: Number.POSITIVE_INFINITY, divisor: 31_536_000_000, unit: 'year' },
  ];

  for (const range of ranges) {
    if (absDiff < range.limit) {
      return formatter.format(Math.round(diff / range.divisor), range.unit);
    }
  }

  return formatter.format(0, 'second');
}

function formatDuration(durationMs?: number | null) {
  if (durationMs == null || Number.isNaN(durationMs) || durationMs < 0) {
    return '-';
  }

  if (durationMs < 1_000) {
    return `${Math.round(durationMs)} ms`;
  }

  if (durationMs < 60_000) {
    const seconds = durationMs / 1_000;
    return `${seconds.toFixed(seconds < 10 ? 1 : 0)} s`;
  }

  if (durationMs < 3_600_000) {
    const minutes = Math.floor(durationMs / 60_000);
    const seconds = Math.round((durationMs % 60_000) / 1_000);
    return `${minutes}m ${seconds}s`;
  }

  if (durationMs < 86_400_000) {
    const hours = Math.floor(durationMs / 3_600_000);
    const minutes = Math.round((durationMs % 3_600_000) / 60_000);
    return `${hours}h ${minutes}m`;
  }

  const days = Math.floor(durationMs / 86_400_000);
  const hours = Math.round((durationMs % 86_400_000) / 3_600_000);
  return `${days}d ${hours}h`;
}

function formatDurationBetween(start?: string, end?: string) {
  const startTimestamp = parseTimestamp(start);
  const endTimestamp = parseTimestamp(end);
  if (startTimestamp === null || endTimestamp === null) {
    return '-';
  }

  return formatDuration(endTimestamp - startTimestamp);
}

function formatNumberValue(value: number, locale: string) {
  return new Intl.NumberFormat(locale).format(value);
}

function formatCurrencyValue(value: number, currency: string, locale: string) {
  const absolute = Math.abs(value);
  const maximumFractionDigits = absolute !== 0 && absolute < 0.01 ? 8 : absolute !== 0 && absolute < 1 ? 4 : 2;
  const minimumFractionDigits = absolute !== 0 && absolute < 0.01 ? 4 : 2;

  try {
    return new Intl.NumberFormat(locale, {
      style: 'currency',
      currency,
      currencyDisplay: 'narrowSymbol',
      minimumFractionDigits,
      maximumFractionDigits,
    }).format(value);
  } catch {
    return `${currency} ${new Intl.NumberFormat(locale, {
      minimumFractionDigits,
      maximumFractionDigits,
    }).format(value)}`;
  }
}

function statusTone(status?: string) {
  switch ((status ?? '').toLowerCase()) {
    case 'completed':
    case 'success':
    case 'succeeded':
      return 'border-[#cfe6d8] bg-[#eefaf2] text-[#246b3f]';
    case 'failed':
    case 'error':
      return 'border-[#f2c8c5] bg-[#fff1f0] text-[#b23b32]';
    case 'blocked':
      return 'border-[#f4ddbf] bg-[#fff7e9] text-[#9f5e16]';
    default:
      return 'border-[#d9e8f8] bg-[#eef7ff] text-[#356a9f]';
  }
}

function normalizeAuditStatus(status?: string) {
  const normalized = (status ?? '').trim().toLowerCase();
  if (
    normalized === 'completed' ||
    normalized === 'success' ||
    normalized === 'succeeded' ||
    normalized.includes('complete') ||
    normalized.includes('success')
  ) {
    return 'completed';
  }

  if (normalized === 'blocked' || normalized.includes('block')) {
    return 'blocked';
  }

  if (
    normalized === 'failed' ||
    normalized === 'error' ||
    normalized.includes('fail') ||
    normalized.includes('error')
  ) {
    return 'failed';
  }

  return 'pending';
}

function auditStatusLabel(status: string, t: (key: string, variables?: Record<string, string | number>) => string) {
  switch (normalizeAuditStatus(status)) {
    case 'completed':
      return t('aiAuditPage.completed');
    case 'blocked':
      return t('aiAuditPage.blocked');
    case 'failed':
      return t('aiAuditPage.failed');
    default:
      return t('aiAuditPage.pending');
  }
}

function unwrapAuditErrorMessage(errorMessage?: string) {
  const trimmed = (errorMessage ?? '').trim();
  if (!trimmed) {
    return '';
  }

  const prefix = 'provider returned non-success status:';
  const payload = trimmed.toLowerCase().startsWith(prefix)
    ? trimmed.slice(prefix.length).trim()
    : trimmed;

  let resolved = payload;
  try {
    const parsed = JSON.parse(payload);
    if (typeof parsed === 'string') {
      resolved = parsed;
    } else if (parsed && typeof parsed === 'object') {
      const record = parsed as {
        message?: unknown;
        error?: {
          message?: unknown;
        };
      };
      if (typeof record.error?.message === 'string' && record.error.message.trim()) {
        resolved = record.error.message.trim();
      } else if (typeof record.message === 'string' && record.message.trim()) {
        resolved = record.message.trim();
      }
    }
  } catch {
    resolved = payload;
  }

  return resolved.replace(/^do request failed:\s*/i, '').trim();
}

function severityTone(severity?: string) {
  switch ((severity ?? '').toLowerCase()) {
    case 'critical':
    case 'high':
      return 'border-[#f2c8c5] bg-[#fff1f0] text-[#b23b32]';
    case 'medium':
      return 'border-[#f4ddbf] bg-[#fff7e9] text-[#9f5e16]';
    case 'low':
      return 'border-[#cfe6d8] bg-[#eefaf2] text-[#246b3f]';
    default:
      return 'border-[#d9e8f8] bg-[#eef7ff] text-[#356a9f]';
  }
}

const AIAuditPage: React.FC = () => {
  const { locale, t } = useI18n();
  const [searchParams, setSearchParams] = useSearchParams();
  const [listState, setListState] = useState<AIAuditListResponse>({
    items: [],
    total: 0,
    page: 1,
    limit: 20,
  });
  const [selectedTrace, setSelectedTrace] = useState<AuditTraceDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState('');
  const [status, setStatus] = useState('');
  const [modelFilter, setModelFilter] = useState('');
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(20);
  const [copyState, setCopyState] = useState<string | null>(null);
  const [loadingTraceId, setLoadingTraceId] = useState<string | null>(null);
  const [detailPanelMounted, setDetailPanelMounted] = useState(false);
  const [activeFlowNodeId, setActiveFlowNodeId] = useState('');
  const syncedTraceRef = useRef<string | null>(null);
  const closingTraceRef = useRef(false);
  const detailRequestRef = useRef(0);
  const flowNodeRefs = useRef<Record<string, HTMLDivElement | null>>({});
  const minimapScrollRef = useRef<HTMLDivElement | null>(null);
  const minimapItemRefs = useRef<Record<string, HTMLButtonElement | null>>({});
  const selectedTraceId = selectedTrace?.trace_id ?? '';
  const selectedSessionId = useMemo(() => {
    if (!selectedTrace) {
      return '';
    }
    const invocationSession = selectedTrace.invocations.find((item) => item.session_id)?.session_id;
    if (invocationSession) {
      return invocationSession;
    }
    return selectedTrace.messages[0]?.session_id ?? '';
  }, [selectedTrace]);
  const traceQuery = searchParams.get('trace')?.trim() || '';

  useEffect(() => {
    void loadAudit();
  }, [page, limit]);

  useEffect(() => {
    if (loading) {
      return;
    }

    if (closingTraceRef.current) {
      return;
    }

    if (traceQuery) {
      if (syncedTraceRef.current && syncedTraceRef.current !== traceQuery) {
        return;
      }
      if (selectedTraceId !== traceQuery && loadingTraceId !== traceQuery && syncedTraceRef.current !== traceQuery) {
        void loadTraceDetail(traceQuery, false);
      }
    }
  }, [loading, traceQuery, selectedTraceId, loadingTraceId]);

  useEffect(() => {
    if (!traceQuery) {
      closingTraceRef.current = false;
    }
  }, [traceQuery]);

  const loadAudit = async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await adminService.getAIAudit({
        page,
        limit,
        search: search.trim() || undefined,
        status: status || undefined,
        model: modelFilter.trim() || undefined,
      });
      setListState(data);
    } catch (err: any) {
      setError(err.response?.data?.error || t('aiAuditPage.loadFailed'));
    } finally {
      setLoading(false);
    }
  };

  const applyFilters = async () => {
    setPage(1);
    try {
      setLoading(true);
      setError(null);
      const data = await adminService.getAIAudit({
        page: 1,
        limit,
        search: search.trim() || undefined,
        status: status || undefined,
        model: modelFilter.trim() || undefined,
      });
      setListState(data);
    } catch (err: any) {
      setError(err.response?.data?.error || t('aiAuditPage.loadFailed'));
    } finally {
      setLoading(false);
    }
  };

  const loadTraceDetail = async (traceId: string, replaceHistory: boolean = false) => {
    if (!traceId || loadingTraceId === traceId) {
      return;
    }

    const requestId = detailRequestRef.current + 1;
    detailRequestRef.current = requestId;

    try {
      closingTraceRef.current = false;
      setDetailLoading(true);
      setLoadingTraceId(traceId);
      syncedTraceRef.current = traceId;
      setError(null);
      const detail = await adminService.getAITraceDetail(traceId);
      if (detailRequestRef.current !== requestId || closingTraceRef.current) {
        return;
      }
      setSelectedTrace(detail);
      if (searchParams.get('trace') !== traceId) {
        setSearchParams((current) => {
          const next = new URLSearchParams(current);
          next.set('trace', traceId);
          return next;
        }, { replace: replaceHistory });
      }
    } catch (err: any) {
      if (detailRequestRef.current !== requestId) {
        return;
      }
      if (syncedTraceRef.current === traceId) {
        syncedTraceRef.current = null;
      }
      setError(err.response?.data?.error || t('aiAuditPage.detailLoadFailed'));
    } finally {
      if (detailRequestRef.current === requestId) {
        setDetailLoading(false);
        setLoadingTraceId(null);
      }
    }
  };

  const totalPages = useMemo(() => {
    return Math.max(1, Math.ceil((listState.total || 0) / (listState.limit || limit || 1)));
  }, [listState.total, listState.limit, limit]);

  const listSummary = useMemo(() => {
    return {
      completed: listState.items.filter((item) => normalizeAuditStatus(item.status) === 'completed').length,
      blocked: listState.items.filter((item) => normalizeAuditStatus(item.status) === 'blocked').length,
      failed: listState.items.filter((item) => normalizeAuditStatus(item.status) === 'failed').length,
      tokens: listState.items.reduce((sum, item) => sum + item.total_tokens, 0),
    };
  }, [listState.items]);

  const traceSummary = useMemo(() => {
    if (!selectedTrace) {
      return null;
    }

    const activityEntries = [
      ...selectedTrace.flow_nodes.map((node) => node.created_at),
      ...selectedTrace.invocations.flatMap((item) => [item.created_at, item.completed_at].filter(Boolean) as string[]),
      ...selectedTrace.audit_events.map((event) => event.created_at),
      ...selectedTrace.cost_records.map((record) => record.recorded_at),
      ...selectedTrace.risk_hits.map((hit) => hit.created_at),
      ...selectedTrace.messages.map((message) => message.created_at),
    ]
      .map((value) => {
        const parsed = parseTimestamp(value);
        return parsed === null ? null : { value, parsed };
      })
      .filter((entry): entry is { value: string; parsed: number } => entry !== null)
      .sort((a, b) => a.parsed - b.parsed);

    const completedEntries = selectedTrace.invocations
      .map((item) => {
        const value = item.completed_at;
        const parsed = parseTimestamp(value);
        return value && parsed !== null ? { value, parsed } : null;
      })
      .filter((entry): entry is { value: string; parsed: number } => entry !== null)
      .sort((a, b) => a.parsed - b.parsed);

    const tokenSource = selectedTrace.invocations.length > 0 ? selectedTrace.invocations : selectedTrace.cost_records;

    return {
      startedAt: activityEntries[0]?.value,
      latestAt: activityEntries[activityEntries.length - 1]?.value,
      completedAt: completedEntries[completedEntries.length - 1]?.value,
      totalTokens: tokenSource.reduce((sum, item) => sum + item.total_tokens, 0),
      promptTokens: tokenSource.reduce((sum, item) => sum + item.prompt_tokens, 0),
      completionTokens: tokenSource.reduce((sum, item) => sum + item.completion_tokens, 0),
      totalEstimatedCost: selectedTrace.cost_records.reduce((sum, item) => sum + item.estimated_cost, 0),
      totalInternalCost: selectedTrace.cost_records.reduce((sum, item) => sum + item.internal_cost, 0),
      currency: selectedTrace.cost_records[0]?.currency ?? 'USD',
    };
  }, [selectedTrace]);

  const copyTrace = async (traceId: string) => {
    try {
      await navigator.clipboard.writeText(traceId);
      setCopyState(traceId);
      window.setTimeout(() => setCopyState((current) => current === traceId ? null : current), 1800);
    } catch {
      setError(t('aiAuditPage.copyFailed'));
    }
  };

  const clearSelectedTrace = () => {
    closingTraceRef.current = true;
    detailRequestRef.current += 1;
    setSelectedTrace(null);
    setDetailLoading(false);
    setLoadingTraceId(null);
    syncedTraceRef.current = null;
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      next.delete('trace');
      return next;
    }, { replace: true });
  };

  const isDetailVisible = detailLoading || Boolean(selectedTrace);

  useEffect(() => {
    if (isDetailVisible) {
      setDetailPanelMounted(true);
      return;
    }

    const timeoutId = window.setTimeout(() => {
      setDetailPanelMounted(false);
    }, 220);

    return () => window.clearTimeout(timeoutId);
  }, [isDetailVisible]);

  useEffect(() => {
    if (!selectedTrace || selectedTrace.flow_nodes.length === 0) {
      setActiveFlowNodeId('');
      return;
    }

    const nodes = selectedTrace.flow_nodes
      .map((node) => flowNodeRefs.current[node.id])
      .filter((node): node is HTMLDivElement => Boolean(node));

    if (nodes.length === 0) {
      setActiveFlowNodeId(selectedTrace.flow_nodes[0]?.id ?? '');
      return;
    }

    setActiveFlowNodeId((current) => current || selectedTrace.flow_nodes[0]?.id || '');

    const observer = new IntersectionObserver(
      (entries) => {
        const visibleEntries = entries
          .filter((entry) => entry.isIntersecting)
          .sort((left, right) => {
            if (right.intersectionRatio !== left.intersectionRatio) {
              return right.intersectionRatio - left.intersectionRatio;
            }

            return left.boundingClientRect.top - right.boundingClientRect.top;
          });

        const nextActiveNode = visibleEntries[0]?.target.getAttribute('data-flow-node-id') ?? '';
        if (nextActiveNode) {
          setActiveFlowNodeId(nextActiveNode);
        }
      },
      {
        rootMargin: '-14% 0px -56% 0px',
        threshold: [0.2, 0.45, 0.7],
      },
    );

    nodes.forEach((node) => observer.observe(node));

    return () => observer.disconnect();
  }, [detailPanelMounted, selectedTrace]);

  useEffect(() => {
    if (!activeFlowNodeId) {
      return;
    }

    const container = minimapScrollRef.current;
    const item = minimapItemRefs.current[activeFlowNodeId];
    if (!container || !item) {
      return;
    }

    const itemTop = item.offsetTop;
    const itemBottom = itemTop + item.offsetHeight;
    const viewportTop = container.scrollTop;
    const viewportBottom = viewportTop + container.clientHeight;

    if (itemTop >= viewportTop && itemBottom <= viewportBottom) {
      return;
    }

    const nextTop = Math.max(0, itemTop - container.clientHeight / 2 + item.offsetHeight / 2);
    container.scrollTo({ top: nextTop, behavior: 'smooth' });
  }, [activeFlowNodeId]);

  const isSplitView = detailPanelMounted || isDetailVisible;

  const jumpToFlowNode = (nodeId: string) => {
    const target = flowNodeRefs.current[nodeId];
    if (!target) {
      return;
    }

    setActiveFlowNodeId(nodeId);
    target.scrollIntoView({ behavior: 'smooth', block: 'center' });
  };

  const renderPagination = () => (
    !loading && listState.total > 0 && (
      <div className="flex items-center justify-between gap-3 border-t border-[#f1e7e1] px-4 py-4 text-xs text-[#8f8681] sm:px-5 sm:text-sm">
        <div className="truncate">
          {t('admin.pageSummary', { page, total: totalPages })}
        </div>
        <div className="flex shrink-0 items-center gap-2 whitespace-nowrap">
          <button
            type="button"
            onClick={() => setPage((current) => Math.max(1, current - 1))}
            disabled={page <= 1}
            className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
          >
            {t('admin.prev')}
          </button>
          <button
            type="button"
            onClick={() => setPage((current) => Math.min(totalPages, current + 1))}
            disabled={page >= totalPages}
            className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
          >
            {t('admin.nextPage')}
          </button>
        </div>
      </div>
    )
  );

  const renderTraceDetailContent = () => {
    if (!selectedTrace || !traceSummary) {
      return null;
    }

    return (
      <div className="mt-6 space-y-6 transition-all duration-300 ease-out">
        <div className="overflow-hidden rounded-[28px] border border-[#eadfd8] bg-[linear-gradient(135deg,#fff7f1_0%,#fffdfb_55%,#fffaf5_100%)] shadow-[0_34px_90px_-70px_rgba(96,58,24,0.55)]">
          <div className="border-b border-[#efe3db] px-5 py-5 sm:px-6">
            <div className="flex flex-col gap-6 xl:flex-row xl:items-start xl:justify-between">
              <div className="min-w-0 flex-1">
                <div className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b46c50]">{t('aiAuditPage.trace')}</div>
                <div className="mt-3 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0">
                    <div className="break-all text-[1.55rem] font-semibold tracking-[-0.04em] text-[#171212]">
                      {selectedTrace.trace_id}
                    </div>
                    <div className="mt-4 flex flex-wrap gap-2">
                      <MetaPill>{t('aiAuditPage.userLabel', { user: selectedTrace.username || '-' })}</MetaPill>
                      {selectedSessionId && (
                        <MetaPill tone="info">{t('aiAuditPage.sessionLabel', { session: selectedSessionId })}</MetaPill>
                      )}
                    </div>
                  </div>
                  <div className="flex items-center gap-3">
                    <button
                      type="button"
                      onClick={() => void copyTrace(selectedTrace.trace_id)}
                      className="app-button-secondary shrink-0 border-[#d9ccc4] bg-white/90 text-[#171212] shadow-sm"
                    >
                      {copyState === selectedTrace.trace_id ? t('aiAuditPage.copied') : t('aiAuditPage.copyTraceId')}
                    </button>
                  </div>
                </div>
              </div>
              <div className="grid gap-3 sm:grid-cols-2 xl:w-[380px]">
                <div className="rounded-2xl border border-[#eadfd8] bg-white/88 p-4">
                  <TimestampStack label={t('common.createdAt')} value={traceSummary.startedAt} locale={locale} />
                </div>
                <div className="rounded-2xl border border-[#eadfd8] bg-white/88 p-4">
                  <TimestampStack label={t('common.lastUpdated')} value={traceSummary.latestAt} locale={locale} />
                </div>
              </div>
            </div>
          </div>

          <div className="grid gap-3 px-5 py-5 sm:px-6 md:grid-cols-2 xl:grid-cols-5">
            <DetailMetricCard
              label={t('aiAuditPage.auditEvents')}
              value={formatNumberValue(selectedTrace.audit_events.length, locale)}
              hint={`${formatNumberValue(selectedTrace.risk_hits.length, locale)} ${t('aiAuditPage.riskHits')}`}
              tone="warm"
            />
            <DetailMetricCard
              label={t('aiAuditPage.executionFlow')}
              value={formatNumberValue(selectedTrace.flow_nodes.length, locale)}
              hint={selectedSessionId || '-'}
              tone="slate"
            />
            <DetailMetricCard
              label={t('costsPage.tokens')}
              value={formatNumberValue(traceSummary.totalTokens, locale)}
              hint={`${formatNumberValue(traceSummary.promptTokens, locale)} / ${formatNumberValue(traceSummary.completionTokens, locale)}`}
              tone="sunset"
            />
            <DetailMetricCard
              label={t('costsPage.estimatedSpend')}
              value={formatCurrencyValue(traceSummary.totalEstimatedCost, traceSummary.currency, locale)}
              hint={traceSummary.currency}
              tone="gold"
            />
            <DetailMetricCard
              label={t('costsPage.internalCost')}
              value={formatCurrencyValue(traceSummary.totalInternalCost, traceSummary.currency, locale)}
              hint={traceSummary.completedAt ? formatDurationBetween(traceSummary.startedAt, traceSummary.completedAt) : '-'}
              tone="emerald"
            />
          </div>
        </div>

        <TraceSection
          title={t('aiAuditPage.executionFlow')}
          subtitle={t('aiAuditPage.executionFlowSubtitle')}
          badge={formatNumberValue(selectedTrace.flow_nodes.length, locale)}
          stickyFriendly
        >
          {selectedTrace.flow_nodes.length === 0 ? (
            <div className="text-sm text-[#8f8681]">{t('aiAuditPage.noFlowNodes')}</div>
          ) : (
            <div className="grid gap-5 xl:grid-cols-[260px_minmax(0,1fr)]">
              <aside className="self-start xl:sticky xl:top-6">
                <div className="overflow-hidden rounded-[24px] border border-[#efe3db] bg-[#fcfaf8] shadow-[0_24px_60px_-52px_rgba(88,54,24,0.4)]">
                  <div className="border-b border-[#f1e7e1] px-4 py-4">
                    <div className="text-sm font-semibold text-[#171212]">{t('aiAuditPage.flowMinimap')}</div>
                    <p className="mt-1 text-xs leading-5 text-[#8f8681]">{t('aiAuditPage.flowMinimapSubtitle')}</p>
                  </div>
                  <div ref={minimapScrollRef} className="max-h-[70vh] overflow-auto p-3">
                    <div className="space-y-2">
                      {selectedTrace.flow_nodes.map((node, index) => {
                        const isActive = node.id === activeFlowNodeId;

                        return (
                          <button
                            key={node.id}
                            ref={(element) => {
                              if (element) {
                                minimapItemRefs.current[node.id] = element;
                                return;
                              }

                              delete minimapItemRefs.current[node.id];
                            }}
                            type="button"
                            onClick={() => jumpToFlowNode(node.id)}
                            className={`w-full rounded-2xl border px-3 py-3 text-left transition ${
                              isActive
                                ? 'border-[#ef6b4a] bg-[#fff3ec] shadow-[0_18px_42px_-32px_rgba(180,108,80,0.55)]'
                                : 'border-transparent bg-white hover:border-[#eadfd8] hover:bg-[#fffaf7]'
                            }`}
                          >
                            <div className="flex items-start gap-3">
                              <span className={`inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-full border text-[11px] font-semibold ${flowKindTone(node.kind)}`}>
                                {index + 1}
                              </span>
                              <div className="min-w-0">
                                <div className="truncate text-sm font-semibold text-[#171212]">
                                  {node.title || flowKindLabel(node.kind, t)}
                                </div>
                                <div className="mt-1 text-[11px] text-[#7a6d66]">{flowKindLabel(node.kind, t)}</div>
                                <div className="mt-1 text-[11px] text-[#9a8f89]">{formatRelativeTimestamp(node.created_at, locale)}</div>
                              </div>
                            </div>
                          </button>
                        );
                      })}
                    </div>
                  </div>
                </div>
              </aside>

              <div className="space-y-4">
                {selectedTrace.flow_nodes.map((node, index) => (
                  <div
                    key={node.id}
                    ref={(element) => {
                      if (element) {
                        flowNodeRefs.current[node.id] = element;
                        return;
                      }

                      delete flowNodeRefs.current[node.id];
                    }}
                    data-flow-node-id={node.id}
                    className="relative scroll-mt-28 pl-8"
                  >
                    {index < selectedTrace.flow_nodes.length - 1 && (
                      <div className="absolute left-[13px] top-8 h-[calc(100%-0.75rem)] w-px bg-[#eadfd8]" />
                    )}
                    <div className={`absolute left-0 top-4 h-7 w-7 rounded-full border shadow-sm ${flowKindTone(node.kind)}`} />
                    <div className={`rounded-[24px] border bg-[#fffaf7] p-4 shadow-[0_24px_60px_-52px_rgba(88,54,24,0.45)] transition ${
                      node.id === activeFlowNodeId ? 'border-[#ef6b4a]' : 'border-[#efe3db]'
                    }`}>
                      <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <div className={`inline-flex rounded-full border px-2.5 py-1 text-xs font-medium ${flowKindTone(node.kind)}`}>
                              {flowKindLabel(node.kind, t)}
                            </div>
                            {node.status && (
                              <span className={`inline-flex rounded-full border px-2.5 py-1 text-xs font-medium ${statusTone(node.status)}`}>
                                {node.status}
                              </span>
                            )}
                          </div>
                          <div className="mt-3 text-base font-semibold text-[#171212]">
                            {node.title || flowKindLabel(node.kind, t)}
                          </div>
                          {node.summary && (
                            <div className="mt-2 text-sm leading-6 text-[#5f5957]">{node.summary}</div>
                          )}
                          <div className="mt-4 flex flex-wrap gap-2 text-xs text-[#7a6d66]">
                            {node.request_id && (
                              <MetaPill tone="soft">{`${t('aiAuditPage.requestIdLabel')}: ${node.request_id}`}</MetaPill>
                            )}
                            {node.model && (
                              <MetaPill tone="info">{`${t('aiAuditPage.modelLabel')}: ${node.model}`}</MetaPill>
                            )}
                          </div>
                        </div>
                        <div className="xl:w-[220px]">
                          <TimestampStack value={node.created_at} locale={locale} align="right" />
                        </div>
                      </div>

                      {(node.input_payload || node.output_payload) && (
                        <div className="mt-4 grid gap-4 xl:grid-cols-2">
                          {node.input_payload && (
                            <div className="rounded-2xl border border-[#eadfd8] bg-white p-4">
                              <div className="mb-3 text-[11px] font-semibold uppercase tracking-[0.18em] text-[#8f8681]">
                                {t('aiAuditPage.nodeInput')}
                              </div>
                              <pre className="max-h-80 overflow-auto rounded-xl bg-[#171212] p-3 text-[11px] leading-5">
                                {renderCodeBlock(node.input_payload)}
                              </pre>
                            </div>
                          )}
                          {node.output_payload && (
                            <div className="rounded-2xl border border-[#eadfd8] bg-white p-4">
                              <div className="mb-3 text-[11px] font-semibold uppercase tracking-[0.18em] text-[#8f8681]">
                                {t('aiAuditPage.nodeOutput')}
                              </div>
                              <pre className="max-h-80 overflow-auto rounded-xl bg-[#171212] p-3 text-[11px] leading-5">
                                {renderCodeBlock(node.output_payload)}
                              </pre>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </TraceSection>

        <TraceSection
          title={t('aiAuditPage.costRecords')}
          badge={formatNumberValue(selectedTrace.cost_records.length, locale)}
        >
          {selectedTrace.cost_records.length === 0 ? (
            <div className="text-sm text-[#8f8681]">{t('aiAuditPage.noCostRecords')}</div>
          ) : (
            <div className="space-y-4">
              {selectedTrace.cost_records.map((record) => (
                <div key={record.id} className="rounded-[24px] border border-[#efe3db] bg-[#fffaf7] p-4 shadow-[0_24px_60px_-52px_rgba(88,54,24,0.45)]">
                  <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <MetaPill tone="info">{record.provider_type}</MetaPill>
                        {record.username && <MetaPill>{record.username}</MetaPill>}
                        {record.instance_name && <MetaPill tone="soft">{record.instance_name}</MetaPill>}
                      </div>
                      <div className="mt-3 text-lg font-semibold text-[#171212]">{record.model_name}</div>
                      <div className="mt-2 text-sm text-[#7a6d66]">{selectedTrace.trace_id}</div>
                    </div>
                    <div className="xl:w-[220px]">
                      <TimestampStack value={record.recorded_at} locale={locale} align="right" />
                    </div>
                  </div>

                  <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                    <DetailMetricCard
                      label={t('costsPage.estimated')}
                      value={formatCurrencyValue(record.estimated_cost, record.currency, locale)}
                      hint={record.currency}
                      tone="gold"
                    />
                    <DetailMetricCard
                      label={t('costsPage.internal')}
                      value={formatCurrencyValue(record.internal_cost, record.currency, locale)}
                      hint={record.currency}
                      tone="emerald"
                    />
                    <DetailMetricCard
                      label={t('costsPage.tokens')}
                      value={formatNumberValue(record.total_tokens, locale)}
                      hint={`${formatNumberValue(record.prompt_tokens, locale)} in / ${formatNumberValue(record.completion_tokens, locale)} out`}
                      tone="neutral"
                    />
                    <DetailMetricCard
                      label={t('costsPage.recorded')}
                      value={formatRelativeTimestamp(record.recorded_at, locale)}
                      hint={formatAbsoluteTimestamp(record.recorded_at, locale)}
                      tone="slate"
                    />
                  </div>
                </div>
              ))}
            </div>
          )}
        </TraceSection>

        <TraceSection
          title={t('aiAuditPage.riskHits')}
          badge={formatNumberValue(selectedTrace.risk_hits.length, locale)}
        >
          {selectedTrace.risk_hits.length === 0 ? (
            <div className="text-sm text-[#8f8681]">{t('aiAuditPage.noRiskHits')}</div>
          ) : (
            <div className="space-y-3">
              {selectedTrace.risk_hits.map((hit) => (
                <div key={hit.id} className="rounded-2xl border border-[#f1e7e1] bg-[#fffaf7] p-4">
                  <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className={`inline-flex rounded-full border px-2.5 py-1 text-xs font-medium ${severityTone(hit.severity)}`}>
                          {hit.severity}
                        </span>
                        <MetaPill tone="info">{hit.action}</MetaPill>
                      </div>
                      <div className="mt-3 text-base font-semibold text-[#171212]">{hit.rule_name}</div>
                      <div className="mt-2 text-sm leading-6 text-[#5f5957]">{hit.match_summary}</div>
                    </div>
                    <div className="sm:w-[220px]">
                      <TimestampStack value={hit.created_at} locale={locale} align="right" />
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </TraceSection>
      </div>
    );
  };


  return (
    <AdminLayout title={t('nav.aiAudit')}>
      <div className="space-y-6">
        <section className={`app-panel p-6 ${isSplitView ? 'overflow-visible' : ''}`}>
          <div>
            <h2 className="text-xl font-semibold text-[#171212]">{t('aiAuditPage.title')}</h2>
            <p className="mt-1 text-sm text-[#8f8681]">
              {t('aiAuditPage.subtitle')}
            </p>
          </div>

          {error && (
            <div className="mt-4 rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-red-700">
              {error}
            </div>
          )}

          {!loading && listState.total > 0 && (
            <div className="mt-5 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
              <SummaryCard label={t('aiAuditPage.completed')} value={String(listSummary.completed)} tone="green" />
              <SummaryCard label={t('aiAuditPage.blocked')} value={String(listSummary.blocked)} tone="amber" />
              <SummaryCard label={t('aiAuditPage.failed')} value={String(listSummary.failed)} tone="red" />
              <SummaryCard label={t('aiAuditPage.tokensOnPage')} value={listSummary.tokens.toLocaleString()} />
            </div>
          )}

          <div className={`mt-6 rounded-2xl border border-[#eadfd8] bg-white ${isSplitView ? 'overflow-visible' : 'overflow-hidden'}`}>
            <div className="border-b border-[#f1e7e1] px-4 py-4 sm:px-5">
              <div className="flex flex-col gap-4 xl:flex-row xl:items-end xl:justify-between">
                <div>
                  <h3 className="text-lg font-semibold text-[#171212]">{t('aiAuditPage.traceTable')}</h3>
                  <p className="mt-1 text-sm text-[#8f8681]">
                    {t('aiAuditPage.traceTableSubtitle')}
                  </p>
                </div>
                <div className="flex flex-col gap-3 lg:flex-row">
                  <input
                    type="text"
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                    placeholder={t('aiAuditPage.searchPlaceholder')}
                    className="app-input min-w-[280px]"
                  />
                  <select
                    value={status}
                    onChange={(event) => setStatus(event.target.value)}
                    className="app-input"
                  >
                    <option value="">{t('aiAuditPage.allStatuses')}</option>
                    <option value="completed">{t('aiAuditPage.completed')}</option>
                    <option value="failed">{t('aiAuditPage.failed')}</option>
                    <option value="blocked">{t('aiAuditPage.blocked')}</option>
                    <option value="pending">{t('aiAuditPage.pending')}</option>
                  </select>
                  <input
                    type="text"
                    value={modelFilter}
                    onChange={(event) => setModelFilter(event.target.value)}
                    placeholder={t('aiAuditPage.modelPlaceholder')}
                    className="app-input min-w-[220px]"
                  />
                  <select
                    value={limit}
                    onChange={(event) => setLimit(Number(event.target.value))}
                    className="app-input"
                  >
                    <option value={10}>{t('costsPage.pageSize10')}</option>
                    <option value={20}>{t('costsPage.pageSize20')}</option>
                    <option value={50}>{t('costsPage.pageSize50')}</option>
                  </select>
                  <button onClick={applyFilters} className="app-button-secondary">
                    {t('common.refresh')}
                  </button>
                </div>
              </div>
            </div>

            {loading ? (
              <div className="px-5 py-6 text-sm text-[#8f8681]">{t('aiAuditPage.loading')}</div>
            ) : listState.items.length === 0 ? (
              <div className="px-5 py-6 text-sm text-[#8f8681]">{t('aiAuditPage.empty')}</div>
            ) : isSplitView ? (
              <div className="grid gap-0 xl:grid-cols-[320px_minmax(0,1fr)]">
                <div className="border-b border-[#f1e7e1] bg-[#fcfaf8] xl:border-b-0 xl:border-r">
                  <div className="border-b border-[#f1e7e1] px-5 py-4">
                    <div className="text-sm font-semibold text-[#171212]">{t('aiAuditPage.trace')}</div>
                    <div className="mt-1 text-xs text-[#8f8681]">{t('aiAuditPage.traceTableSubtitle')}</div>
                  </div>
                  <div className="divide-y divide-[#f7efe9]">
                    {listState.items.map((item) => {
                      const isActive = selectedTraceId === item.trace_id || loadingTraceId === item.trace_id;
                      return (
                        <button
                          key={`${item.trace_id}-${item.request_id}`}
                          type="button"
                          onClick={() => void loadTraceDetail(item.trace_id)}
                          className={`flex w-full flex-col gap-2 px-5 py-4 text-left transition ${
                            isActive ? 'bg-[#fff3ec]' : 'hover:bg-[#fffaf7]'
                          }`}
                        >
                          <div className="truncate text-sm font-medium text-[#171212]">{item.trace_id}</div>
                          <div className="text-xs font-medium text-[#5f5957]">{formatRelativeTimestamp(item.created_at, locale)}</div>
                          <div className="text-[11px] text-[#9a8f89]">{formatAbsoluteTimestamp(item.created_at, locale)}</div>
                        </button>
                      );
                    })}
                  </div>
                  {renderPagination()}
                </div>

                <div
                  className={`min-w-0 bg-[#fffdfb] px-4 py-5 transition-all duration-300 ease-out sm:px-5 xl:px-6 ${
                    isDetailVisible ? 'opacity-100' : 'pointer-events-none translate-x-4 opacity-0'
                  }`}
                >
                  <div className="flex items-center justify-between gap-3 border-b border-[#f1e7e1] pb-4">
                    <div>
                      <h3 className="text-lg font-semibold text-[#171212]">{t('aiAuditPage.detailTitle')}</h3>
                      <p className="mt-1 text-sm text-[#8f8681]">
                        {t('aiAuditPage.detailSubtitle')}
                      </p>
                    </div>
                    <div className="flex items-center gap-3">
                      {detailLoading && <div className="text-sm text-[#8f8681]">{t('aiAuditPage.detailLoading')}</div>}
                      <button
                        type="button"
                        onClick={clearSelectedTrace}
                        className="app-button-secondary border-[#d9ccc4] bg-white text-[#171212] shadow-sm"
                      >
                        {t('common.close')}
                      </button>
                    </div>
                  </div>

                  {selectedTrace ? (
                    renderTraceDetailContent()
                  ) : (
                    <div className="mt-6 rounded-2xl border border-dashed border-[#eadfd8] bg-white px-5 py-8 text-sm text-[#8f8681]">
                      {t('aiAuditPage.detailLoading')}
                    </div>
                  )}
                </div>
              </div>
            ) : (
              <>
                <div className="overflow-x-auto">
                  <table className="min-w-full divide-y divide-[#f1e7e1]">
                    <thead className="bg-[#fcfaf8]">
                      <tr>
                        <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('aiAuditPage.trace')}</th>
                        <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('aiAuditPage.user')}</th>
                        <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('aiAuditPage.requested')}</th>
                        <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('aiAuditPage.providerResult')}</th>
                        <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('aiAuditPage.usage')}</th>
                        <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('common.status')}</th>
                        <th className="px-4 py-3 text-right text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{t('aiAuditPage.action')}</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-[#f7efe9]">
                      {listState.items.map((item) => (
                        (() => {
                          const normalizedStatus = normalizeAuditStatus(item.status);
                          const errorSummary = unwrapAuditErrorMessage(item.error_message);

                          return (
                            <tr
                              key={`${item.trace_id}-${item.request_id}`}
                              className={`${selectedTraceId === item.trace_id ? 'bg-[#fff3ec]' : 'hover:bg-[#fffaf7]'}`}
                            >
                              <td className="px-4 py-4 align-top">
                                <div className="flex items-center gap-2">
                                  <div className="font-medium text-[#171212]">{item.trace_id}</div>
                                  <button
                                    type="button"
                                    onClick={() => void copyTrace(item.trace_id)}
                                    className="rounded-full border border-[#eadfd8] bg-white px-2 py-1 text-[11px] text-[#8f8681] hover:text-[#171212]"
                                  >
                                    {copyState === item.trace_id ? t('aiAuditPage.copied') : t('aiAuditPage.copy')}
                                  </button>
                                </div>
                                <div className="mt-1 text-xs font-medium text-[#5f5957]">{formatRelativeTimestamp(item.created_at, locale)}</div>
                                <div className="mt-1 text-[11px] text-[#9a8f89]">{formatAbsoluteTimestamp(item.created_at, locale)}</div>
                              </td>
                              <td className="px-4 py-4 align-top">
                                <div className="font-medium text-[#171212]">{item.username || '-'}</div>
                                <div className="mt-1 text-xs text-[#8f8681]">{item.request_id}</div>
                              </td>
                              <td className="px-4 py-4 align-top">
                                <div className="font-medium text-[#171212]">{item.requested_model}</div>
                                <div className="mt-1 text-xs text-[#8f8681]">{item.provider_type}</div>
                              </td>
                              <td className="px-4 py-4 align-top">
                                <div className="font-medium text-[#171212]">{item.actual_provider_model}</div>
                                <div className="mt-1 text-xs text-[#8f8681]">{item.latency_ms ? `${item.latency_ms} ms` : t('aiAuditPage.noLatency')}</div>
                                {errorSummary && (
                                  <div
                                    className="mt-2 max-w-[320px] break-words text-xs text-red-600"
                                    title={item.error_message}
                                  >
                                    {t('aiAuditPage.failureReason')}: {errorSummary}
                                  </div>
                                )}
                              </td>
                              <td className="px-4 py-4 align-top">
                                <div className="font-medium text-[#171212]">{item.total_tokens} tokens</div>
                                <div className="mt-1 text-xs text-[#8f8681]">
                                  {item.prompt_tokens} in / {item.completion_tokens} out
                                </div>
                              </td>
                              <td className="px-4 py-4 align-top">
                                <span className={`rounded-full border px-2.5 py-1 text-xs font-medium ${statusTone(normalizedStatus)}`}>
                                  {auditStatusLabel(normalizedStatus, t)}
                                </span>
                              </td>
                              <td className="px-4 py-4 text-right align-top">
                                <button
                                  type="button"
                                  onClick={() => void loadTraceDetail(item.trace_id)}
                                  className="app-button-secondary"
                                >
                                  {t('admin.inspect')}
                                </button>
                              </td>
                            </tr>
                          );
                        })()
                      ))}
                    </tbody>
                  </table>
                </div>
                {renderPagination()}
              </>
            )}
          </div>
        </section>
      </div>
    </AdminLayout>
  );
};

const TimestampStack: React.FC<{
  value?: string;
  locale: string;
  label?: string;
  align?: 'left' | 'right';
}> = ({ value, locale, label, align = 'left' }) => {
  const isRightAligned = align === 'right';

  return (
    <div className={isRightAligned ? 'text-left sm:text-right' : 'text-left'}>
      {label && (
        <div className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b09d93]">{label}</div>
      )}
      <div className={label ? 'mt-2 text-sm font-semibold text-[#171212]' : 'text-sm font-semibold text-[#171212]'}>
        {formatRelativeTimestamp(value, locale)}
      </div>
      <div className="mt-1 text-xs text-[#8f8681]">{formatAbsoluteTimestamp(value, locale)}</div>
    </div>
  );
};

const TraceSection: React.FC<{
  title: string;
  subtitle?: string;
  badge?: string;
  stickyFriendly?: boolean;
  children: React.ReactNode;
}> = ({ title, subtitle, badge, stickyFriendly = false, children }) => {
  return (
    <section className={`${stickyFriendly ? 'overflow-visible' : 'overflow-hidden'} rounded-[28px] border border-[#eadfd8] bg-white shadow-[0_26px_72px_-60px_rgba(85,52,26,0.45)]`}>
      <div className="flex flex-col gap-3 border-b border-[#f1e7e1] px-5 py-5 sm:flex-row sm:items-start sm:justify-between sm:px-6">
        <div>
          <h3 className="text-lg font-semibold text-[#171212]">{title}</h3>
          {subtitle && <p className="mt-1 text-sm text-[#8f8681]">{subtitle}</p>}
        </div>
        {badge && (
          <span className="inline-flex items-center rounded-full border border-[#eadfd8] bg-[#fffaf7] px-3 py-1 text-xs font-semibold text-[#7a6d66]">
            {badge}
          </span>
        )}
      </div>
      <div className={`${stickyFriendly ? 'overflow-visible' : ''} px-5 py-5 sm:px-6`}>{children}</div>
    </section>
  );
};

const MetaPill: React.FC<{
  children: React.ReactNode;
  tone?: 'neutral' | 'soft' | 'info';
}> = ({ children, tone = 'neutral' }) => {
  const toneClass = tone === 'info'
    ? 'border-[#d9e8f8] bg-[#eef7ff] text-[#356a9f]'
    : tone === 'soft'
      ? 'border-[#eadfd8] bg-white text-[#7a6d66]'
      : 'border-[#f4ddbf] bg-[#fff7e9] text-[#9f5e16]';

  return (
    <span className={`inline-flex max-w-full items-center rounded-full border px-2.5 py-1 text-xs font-medium ${toneClass}`}>
      <span className="truncate">{children}</span>
    </span>
  );
};

const DetailMetricCard: React.FC<{
  label: string;
  value: string;
  hint?: string;
  tone?: 'neutral' | 'warm' | 'slate' | 'sunset' | 'gold' | 'emerald';
}> = ({ label, value, hint, tone = 'neutral' }) => {
  const toneClass = tone === 'warm'
    ? 'border-[#f4ddbf] bg-[#fff8ee]'
    : tone === 'slate'
      ? 'border-[#d9e8f8] bg-[#f5fafe]'
      : tone === 'sunset'
        ? 'border-[#f0d8cc] bg-[#fff3ec]'
        : tone === 'gold'
          ? 'border-[#f1e0b6] bg-[#fff9e9]'
          : tone === 'emerald'
            ? 'border-[#d3ead8] bg-[#f3fff6]'
            : 'border-[#eadfd8] bg-white';

  return (
    <div className={`rounded-2xl border px-4 py-4 ${toneClass}`}>
      <div className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#8f8681]">{label}</div>
      <div className="mt-3 break-words text-xl font-semibold tracking-[-0.03em] text-[#171212]">{value}</div>
      {hint && <div className="mt-2 text-xs text-[#7a6d66]">{hint}</div>}
    </div>
  );
};
const SummaryCard: React.FC<{
  label: string;
  value: string;
  tone?: 'green' | 'amber' | 'red';
}> = ({ label, value, tone }) => {
  const toneClass = tone === 'green'
    ? 'border-[#d9ead3] bg-[#f3fff0]'
    : tone === 'amber'
      ? 'border-amber-200 bg-amber-50'
      : tone === 'red'
        ? 'border-red-200 bg-red-50'
        : 'border-[#eadfd8] bg-white';

  return (
    <div className={`rounded-2xl border px-4 py-4 ${toneClass}`}>
      <div className="text-xs font-semibold uppercase tracking-[0.14em] text-[#8f8681]">{label}</div>
      <div className="mt-3 text-2xl font-semibold tracking-[-0.04em] text-[#171212]">{value}</div>
    </div>
  );
};

export default AIAuditPage;
