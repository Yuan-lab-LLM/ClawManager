import { useEffect, useState } from 'react';
import { useI18n } from '../contexts/I18nContext';
import { instanceService } from '../services/instanceService';
import { modelCatalogSyncPresentation, type ModelCatalogSyncStatus } from '../lib/modelCatalogSync';

export default function ModelCatalogSyncBadge({ instanceId, generation, status }: { instanceId: number; generation: number; status: string }) {
  const { locale } = useI18n();
  const [report, setReport] = useState<{ id: number; generation: number; status: string; value: ModelCatalogSyncStatus }>();
  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const poll = async () => {
      if (!document.hidden) {
        try {
          const value = await instanceService.getModelCatalogSync(instanceId);
          if (!cancelled) setReport({ id: instanceId, generation, status, value });
        } catch {
          if (!cancelled) setReport({ id: instanceId, generation, status, value: { state: 'unknown', reason: 'request_failed', generation, model_count: 0 } });
        }
      }
      if (!cancelled) timer = setTimeout(poll, 15000);
    };
    void poll();
    return () => { cancelled = true; clearTimeout(timer); };
  }, [instanceId, generation, status]);
  const value = report?.id === instanceId && report.generation === generation && report.status === status && report.value.generation === generation ? report.value : undefined;
  const presentation = modelCatalogSyncPresentation(value, locale);
  return <span className="whitespace-nowrap text-xs text-slate-500" tabIndex={0} title={presentation.title}>{presentation.label}</span>;
}
