import { useState } from 'react';
import api from '../services/api';
import { useI18n } from '../contexts/I18nContext';

// Each action downloads one bounded page; the next cursor remains explicit.
export default function ArchiveHistoryExport({ instanceId, admin = false }: { instanceId?: number; admin?: boolean }) {
  const { t } = useI18n();
  const [since, setSince] = useState(() => new Date(Date.now() - 120 * 86400000).toISOString().slice(0, 10));
  const [until, setUntil] = useState(() => new Date(Date.now() - 90 * 86400000).toISOString().slice(0, 10));
  const [cursor, setCursor] = useState('');
  const [hasMore, setHasMore] = useState(false);
  const [completed, setCompleted] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const reset = () => { setCursor(''); setHasMore(false); setCompleted(false); setError(''); };
  const download = async () => {
    const from = Date.parse(`${since}T00:00:00Z`), to = Date.parse(`${until}T00:00:00Z`);
    if (!Number.isFinite(from) || !Number.isFinite(to) || to <= from || to - from > 90 * 86400000) { setError(t('aiAuditPage.archiveWindow')); return; }
    setBusy(true); setError('');
    try {
      const response = await api.get(`${admin ? '/admin' : ''}/history/model_invocations/export`, {
        params: { since: new Date(from).toISOString(), until: new Date(to).toISOString(), instance_id: instanceId, limit: 200, after_id: cursor || undefined, include_body: false }, responseType: 'blob',
      });
      const url = URL.createObjectURL(response.data); const link = document.createElement('a');
      link.href = url; link.download = `invocation-history-${since}-${until}-${cursor || 'first'}.ndjson`; link.click(); URL.revokeObjectURL(url);
      const more = response.headers['x-history-has-more'] === 'true';
      setCursor(response.headers['x-history-next-cursor'] || ''); setHasMore(more); setCompleted(!more);
    } catch { setError(t('aiAuditPage.archiveFailed')); } finally { setBusy(false); }
  };
  return <details className="rounded-lg border border-[var(--cm-border)] p-3 text-sm">
    <summary className="cursor-pointer font-medium">{t('aiAuditPage.archiveHistory')}</summary>
    <p className="my-3 text-[var(--cm-muted)]">{t('aiAuditPage.archiveDescription')}</p>
    <div className="flex flex-wrap items-end gap-3">
      <label className="grid gap-1">{t('aiAuditPage.archiveFrom')}<input className="app-input" type="date" value={since} disabled={busy} onChange={event => { setSince(event.target.value); reset(); }} /></label>
      <label className="grid gap-1">{t('aiAuditPage.archiveUntil')}<input className="app-input" type="date" value={until} disabled={busy} onChange={event => { setUntil(event.target.value); reset(); }} /></label>
      <button type="button" className="app-button-secondary" disabled={busy || completed} onClick={() => void download()}>{busy ? t('common.loading') : hasMore ? t('aiAuditPage.archiveNext') : t('aiAuditPage.archiveDownload')}</button>
    </div>
    <div aria-live="polite" className="mt-2">{error && <p role="alert" className="text-red-700">{error}</p>}{completed && <p>{t('aiAuditPage.archiveComplete')}</p>}</div>
  </details>;
}
