import React from 'react';
import { type DispatchResult } from '../../../../services/secplaneService';
import { useI18n } from '../../../../contexts/I18nContext';

// Honest dispatch banner: a DispatchResult only means rows were inserted into
// instance_commands. Pod-side agent has to poll/execute/ack for the policy
// to actually take effect. If the OpenClaw pod is unreachable, rows stay
// `pending` indefinitely. We surface per-target status here so operators
// don't read "dispatch succeeded" and assume the policy is live on the pod.

interface Props {
  result: DispatchResult;
}

const DispatchResultBanner: React.FC<Props> = ({ result }) => {
  const { t } = useI18n();
  const counts: Record<string, number> = {};
  for (const tgt of result.targets) {
    const s = (tgt.status || 'unknown').toLowerCase();
    counts[s] = (counts[s] || 0) + 1;
  }

  const total = result.targets.length;
  const failed = counts['failed'] || 0;
  const succeeded = counts['succeeded'] || 0;
  const pending = counts['pending'] || 0;
  const dispatched = counts['dispatched'] || 0;
  const others = total - failed - succeeded - pending - dispatched;

  // tone: failure dominates; all-succeeded is the only green; mixed/pending → warning
  let alertClass = 'alert alert-warning';
  let headline = t('secplane.runtime.dispatchResultBanner.queued');
  if (failed > 0 && succeeded === 0 && pending === 0 && dispatched === 0) {
    alertClass = 'alert alert-danger';
    headline = t('secplane.runtime.dispatchResultBanner.allFailed');
  } else if (succeeded === total && total > 0) {
    alertClass = 'alert alert-success';
    headline = t('secplane.runtime.dispatchResultBanner.allSucceeded');
  } else if (failed > 0) {
    alertClass = 'alert alert-danger';
    headline = t('secplane.runtime.dispatchResultBanner.partialFailed', { failed, total });
  }

  const failedTargets = result.targets.filter((tgt) => (tgt.status || '').toLowerCase() === 'failed');

  return (
    <div className={alertClass} style={{ flexDirection: 'column', alignItems: 'stretch', gap: 8 }}>
      <div className="flex items-center justify-between gap-3">
        <strong>{headline}</strong>
        <span className="text-xs muted-strong" style={{ fontFamily: 'ui-monospace, monospace' }}>
          revision {result.revision} · sha {result.sha256.slice(0, 16)}
        </span>
      </div>
      <div className="text-xs flex flex-wrap gap-x-4 gap-y-1">
        <span dangerouslySetInnerHTML={{ __html: t('secplane.runtime.dispatchResultBanner.totalInstances', { total }) ?? '' }} />
        {pending > 0 && <span dangerouslySetInnerHTML={{ __html: t('secplane.runtime.dispatchResultBanner.pendingNote', { count: pending }) ?? '' }} />}
        {dispatched > 0 && <span dangerouslySetInnerHTML={{ __html: t('secplane.runtime.dispatchResultBanner.dispatchedNote', { count: dispatched }) ?? '' }} />}
        {succeeded > 0 && <span dangerouslySetInnerHTML={{ __html: t('secplane.runtime.dispatchResultBanner.succeededNote', { count: succeeded }) ?? '' }} />}
        {failed > 0 && <span dangerouslySetInnerHTML={{ __html: t('secplane.runtime.dispatchResultBanner.failedNote', { count: failed }) ?? '' }} />}
        {others > 0 && <span dangerouslySetInnerHTML={{ __html: t('secplane.runtime.dispatchResultBanner.otherNote', { count: others }) ?? '' }} />}
      </div>
      {failedTargets.length > 0 && (
        <div className="text-xs">
          {t('secplane.runtime.dispatchResultBanner.failedInstances')}
          {failedTargets.map((tgt, i) => (
            <span key={tgt.instance_id}>
              {i > 0 && '、'}
              <code className="font-mono">#{tgt.instance_id}</code>
              {tgt.error && <span className="muted ml-1">({tgt.error})</span>}
            </span>
          ))}
        </div>
      )}
      {(pending > 0 || dispatched > 0) && (
        <div className="text-xs muted">
          {t('secplane.runtime.dispatchResultBanner.note')}
        </div>
      )}
    </div>
  );
};

export default DispatchResultBanner;
