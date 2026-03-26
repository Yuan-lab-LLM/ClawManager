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

const AIAuditPage: React.FC = () => {
  const { t } = useI18n();
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
  const syncedTraceRef = useRef<string | null>(null);
  const closingTraceRef = useRef(false);
  const detailRequestRef = useRef(0);
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
      completed: listState.items.filter((item) => item.status === 'completed').length,
      blocked: listState.items.filter((item) => item.status === 'blocked').length,
      failed: listState.items.filter((item) => item.status === 'failed').length,
      tokens: listState.items.reduce((sum, item) => sum + item.total_tokens, 0),
    };
  }, [listState.items]);

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

  const isSplitView = detailPanelMounted || isDetailVisible;

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
    if (!selectedTrace) {
      return null;
    }

    return (
      <div className="mt-6 space-y-6 transition-all duration-300 ease-out">
        <div className="rounded-2xl border border-[#eadfd8] bg-[#fffaf7] p-4">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <div className="text-sm text-[#8f8681]">{t('aiAuditPage.trace')}</div>
              <div className="mt-1 font-medium text-[#171212]">{selectedTrace.trace_id}</div>
              <div className="mt-2 text-sm text-[#8f8681]">{t('aiAuditPage.userLabel', { user: selectedTrace.username || '-' })}</div>
              {selectedSessionId && (
                <div className="mt-1 text-sm text-[#8f8681]">{t('aiAuditPage.sessionLabel', { session: selectedSessionId })}</div>
              )}
            </div>
            <div className="flex items-center gap-3">
              <button
                type="button"
                onClick={() => void copyTrace(selectedTrace.trace_id)}
                className="app-button-secondary"
              >
                {copyState === selectedTrace.trace_id ? t('aiAuditPage.copied') : t('aiAuditPage.copyTraceId')}
              </button>
            </div>
          </div>
        </div>

        <div className="rounded-2xl border border-[#eadfd8] bg-white p-4">
          <div className="flex items-center justify-between gap-3">
            <div>
              <h3 className="text-lg font-semibold text-[#171212]">{t('aiAuditPage.executionFlow')}</h3>
              <p className="mt-1 text-sm text-[#8f8681]">
                {t('aiAuditPage.executionFlowSubtitle')}
              </p>
            </div>
          </div>
          <div className="mt-4">
            {selectedTrace.flow_nodes.length === 0 ? (
              <div className="text-sm text-[#8f8681]">{t('aiAuditPage.noFlowNodes')}</div>
            ) : (
              <div className="space-y-4">
                {selectedTrace.flow_nodes.map((node, index) => (
                  <div key={node.id} className="relative pl-8">
                    {index < selectedTrace.flow_nodes.length - 1 && (
                      <div className="absolute left-[11px] top-7 h-[calc(100%-0.5rem)] w-px bg-[#eadfd8]" />
                    )}
                    <div className={`absolute left-0 top-4 h-6 w-6 rounded-full border ${flowKindTone(node.kind)}`} />
                    <div className="rounded-2xl border border-[#f1e7e1] bg-[#fffaf7] p-4">
                      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                        <div>
                          <div className={`inline-flex rounded-full border px-2.5 py-1 text-xs font-medium ${flowKindTone(node.kind)}`}>
                            {flowKindLabel(node.kind, t)}
                          </div>
                          <div className="mt-3 text-base font-semibold text-[#171212]">{node.title || flowKindLabel(node.kind, t)}</div>
                          {node.summary && (
                            <div className="mt-2 text-sm text-[#5f5957]">{node.summary}</div>
                          )}
                          <div className="mt-3 flex flex-wrap gap-x-4 gap-y-2 text-xs text-[#8f8681]">
                            {node.request_id && <span>{t('aiAuditPage.requestIdLabel')}: {node.request_id}</span>}
                            {node.model && <span>{t('aiAuditPage.modelLabel')}: {node.model}</span>}
                            {node.status && <span>{t('common.status')}: {node.status}</span>}
                          </div>
                        </div>
                        <div className="text-xs text-[#8f8681]">{new Date(node.created_at).toLocaleString()}</div>
                      </div>

                      {(node.input_payload || node.output_payload) && (
                        <div className="mt-4 grid gap-4 xl:grid-cols-2">
                          {node.input_payload && (
                            <div>
                              <div className="mb-2 text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">
                                {t('aiAuditPage.nodeInput')}
                              </div>
                              <pre className="max-h-72 overflow-auto rounded-lg bg-[#171212] p-3 text-[11px] leading-5">
                                {renderCodeBlock(node.input_payload)}
                              </pre>
                            </div>
                          )}
                          {node.output_payload && (
                            <div>
                              <div className="mb-2 text-xs font-semibold uppercase tracking-[0.12em] text-[#8f8681]">
                                {t('aiAuditPage.nodeOutput')}
                              </div>
                              <pre className="max-h-72 overflow-auto rounded-lg bg-[#171212] p-3 text-[11px] leading-5">
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
            )}
          </div>
        </div>

        <div className="grid gap-6 xl:grid-cols-3">
          <div className="rounded-2xl border border-[#eadfd8] bg-white p-4">
            <h3 className="text-lg font-semibold text-[#171212]">{t('aiAuditPage.invocations')}</h3>
            <div className="mt-4 space-y-4">
              {selectedTrace.invocations.length === 0 ? (
                <div className="text-sm text-[#8f8681]">
                  {t('aiAuditPage.noInvocation')}
                </div>
              ) : (
                selectedTrace.invocations.map((invocation) => (
                  <div key={`${invocation.id || invocation.trace_id}-${invocation.request_id}`} className="rounded-xl border border-[#f1e7e1] bg-[#fffaf7] p-3">
                    <div className="font-medium text-[#171212]">{invocation.requested_model}</div>
                    <div className="mt-1 text-xs text-[#8f8681]">{invocation.actual_provider_model}</div>
                    <div className="mt-2 text-xs text-[#8f8681]">Status: {invocation.status}</div>
                    <div className="mt-2 text-xs text-[#8f8681]">Tokens: {invocation.total_tokens}</div>
                    <details className="mt-3">
                      <summary className="cursor-pointer text-xs font-medium text-[#b46c50]">{t('aiAuditPage.payloads')}</summary>
                      <pre className="mt-2 max-h-56 overflow-auto rounded-lg bg-[#171212] p-3 text-[11px] leading-5">{renderCodeBlock(invocation.request_payload)}</pre>
                      <pre className="mt-2 max-h-56 overflow-auto rounded-lg bg-[#171212] p-3 text-[11px] leading-5">{renderCodeBlock(invocation.response_payload)}</pre>
                    </details>
                  </div>
                ))
              )}
            </div>
          </div>

          <div className="rounded-2xl border border-[#eadfd8] bg-white p-4">
            <h3 className="text-lg font-semibold text-[#171212]">{t('aiAuditPage.auditEvents')}</h3>
            <div className="mt-4 space-y-3">
              {selectedTrace.audit_events.map((event) => (
                <div key={event.id} className="rounded-xl border border-[#f1e7e1] bg-[#fffaf7] p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div className="font-medium text-[#171212]">{event.event_type}</div>
                    <span className="rounded-full bg-[#fff1ea] px-2.5 py-1 text-xs font-medium text-[#b46c50]">
                      {event.severity}
                    </span>
                  </div>
                  <div className="mt-2 text-sm text-[#5f5957]">{event.message}</div>
                  {event.details && (
                    <pre className="mt-3 max-h-40 overflow-auto rounded-lg bg-[#171212] p-3 text-[11px] leading-5">{renderCodeBlock(event.details)}</pre>
                  )}
                  <div className="mt-2 text-xs text-[#8f8681]">{new Date(event.created_at).toLocaleString()}</div>
                </div>
              ))}
            </div>
          </div>

          <div className="rounded-2xl border border-[#eadfd8] bg-white p-4">
            <h3 className="text-lg font-semibold text-[#171212]">{t('aiAuditPage.costRecords')}</h3>
            <div className="mt-4 space-y-3">
              {selectedTrace.cost_records.length === 0 ? (
                <div className="text-sm text-[#8f8681]">
                  {t('aiAuditPage.noCostRecords')}
                </div>
              ) : (
                selectedTrace.cost_records.map((record) => (
                  <div key={record.id} className="rounded-xl border border-[#f1e7e1] bg-[#fffaf7] p-3">
                    <div className="font-medium text-[#171212]">{record.model_name}</div>
                    <div className="mt-1 text-xs text-[#8f8681]">{record.provider_type}</div>
                    <div className="mt-2 text-sm text-[#5f5957]">
                      {record.total_tokens} tokens
                    </div>
                    <div className="mt-1 text-sm text-[#5f5957]">
                      Estimated: {record.estimated_cost.toFixed(8)} {record.currency}
                    </div>
                    <div className="mt-1 text-sm text-[#5f5957]">
                      Internal: {record.internal_cost.toFixed(8)} {record.currency}
                    </div>
                    <div className="mt-2 text-xs text-[#8f8681]">{new Date(record.recorded_at).toLocaleString()}</div>
                  </div>
                ))
              )}
            </div>
          </div>
        </div>

        <div className="rounded-2xl border border-[#eadfd8] bg-white p-4">
          <h3 className="text-lg font-semibold text-[#171212]">{t('aiAuditPage.riskHits')}</h3>
          <div className="mt-4 space-y-3">
            {selectedTrace.risk_hits.length === 0 ? (
              <div className="text-sm text-[#8f8681]">{t('aiAuditPage.noRiskHits')}</div>
            ) : (
              selectedTrace.risk_hits.map((hit) => (
                <div key={hit.id} className="rounded-xl border border-[#f1e7e1] bg-[#fffaf7] p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div className="font-medium text-[#171212]">{hit.rule_name}</div>
                    <div className="flex items-center gap-2">
                      <span className="rounded-full bg-[#fff1ea] px-2.5 py-1 text-xs font-medium text-[#b46c50]">
                        {hit.severity}
                      </span>
                      <span className="rounded-full bg-[#eef7ff] px-2.5 py-1 text-xs font-medium text-[#356a9f]">
                        {hit.action}
                      </span>
                    </div>
                  </div>
                  <div className="mt-2 text-sm text-[#5f5957]">{hit.match_summary}</div>
                  <div className="mt-2 text-xs text-[#8f8681]">{new Date(hit.created_at).toLocaleString()}</div>
                </div>
              ))
            )}
          </div>
        </div>

        <div className="rounded-2xl border border-[#eadfd8] bg-white p-4">
          <h3 className="text-lg font-semibold text-[#171212]">{t('aiAuditPage.messages')}</h3>
          <div className="mt-4 space-y-3">
            {selectedTrace.messages.length === 0 ? (
              <div className="text-sm text-[#8f8681]">{t('aiAuditPage.noMessages')}</div>
            ) : (
              selectedTrace.messages.map((message) => (
                <div key={message.id} className="rounded-xl border border-[#f1e7e1] bg-[#fffaf7] p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div className="font-medium capitalize text-[#171212]">{message.role}</div>
                    <div className="text-xs text-[#8f8681]">#{message.sequence_no}</div>
                  </div>
                  <pre className="mt-3 whitespace-pre-wrap break-words rounded-lg bg-white p-3 text-sm text-[#5f5957]">{message.content}</pre>
                  <div className="mt-2 text-xs text-[#8f8681]">{new Date(message.created_at).toLocaleString()}</div>
                </div>
              ))
            )}
          </div>
        </div>
      </div>
    );
  };

  return (
    <AdminLayout title={t('nav.aiAudit')}>
      <div className="space-y-6">
        <section className="app-panel p-6">
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

          <div className="mt-6 overflow-hidden rounded-2xl border border-[#eadfd8] bg-white">
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
                          <div className="text-xs text-[#8f8681]">{new Date(item.created_at).toLocaleString()}</div>
                        </button>
                      );
                    })}
                  </div>
                  {renderPagination()}
                </div>

                <div
                  className={`min-w-0 bg-[#fffdfb] px-4 py-5 transition-all duration-300 ease-out sm:px-5 xl:px-6 ${
                    isDetailVisible ? 'translate-x-0 opacity-100' : 'pointer-events-none translate-x-4 opacity-0'
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
                            <div className="mt-1 text-xs text-[#8f8681]">{new Date(item.created_at).toLocaleString()}</div>
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
                          </td>
                          <td className="px-4 py-4 align-top">
                            <div className="font-medium text-[#171212]">{item.total_tokens} tokens</div>
                            <div className="mt-1 text-xs text-[#8f8681]">
                              {item.prompt_tokens} in / {item.completion_tokens} out
                            </div>
                          </td>
                          <td className="px-4 py-4 align-top">
                            <span className={`rounded-full px-2.5 py-1 text-xs font-medium ${
                              item.status === 'completed'
                                ? 'bg-green-100 text-green-800'
                                : item.status === 'failed'
                                  ? 'bg-red-100 text-red-700'
                                  : 'bg-yellow-100 text-yellow-800'
                            }`}>
                              {item.status}
                            </span>
                            {item.error_message && (
                              <div className="mt-2 max-w-[280px] text-xs text-red-600">{item.error_message}</div>
                            )}
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
