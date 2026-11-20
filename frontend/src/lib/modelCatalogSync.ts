export interface ModelCatalogSyncStatus {
  state: 'applied' | 'empty' | 'syncing' | 'failed' | 'not_running' | 'unknown';
  reason?: string;
  generation: number;
  revision?: string;
  model_count: number;
  observed_at?: string;
}

export function modelCatalogSyncPresentation(status: ModelCatalogSyncStatus | undefined, locale: string) {
  const zh = locale.startsWith('zh');
  const state = status?.state ?? 'unknown';
  const labels = zh
    ? { applied: `启动模型：已同步 (${status?.model_count ?? 0})`, empty: '启动模型：暂无可用模型', syncing: '启动模型：同步中', failed: '启动模型：同步失败', not_running: '启动模型：待启动', unknown: '启动模型：未确认' }
    : { applied: `Startup models: synced (${status?.model_count ?? 0})`, empty: 'Startup models: none available', syncing: 'Startup models: syncing', failed: 'Startup models: sync failed', not_running: 'Startup models: pending start', unknown: 'Startup models: unconfirmed' };
  const explanation = zh
    ? state === 'applied' || state === 'empty' ? '运行时已确认本次启动的配置写入；这不是实时模型健康探测。'
      : state === 'failed' ? '本次启动的模型配置同步未完成，请检查运行时启动错误；不会据此宣称模型均不可用。'
        : state === 'not_running' ? '实例下次启动时检查并更新托管模型配置。'
          : state === 'syncing' ? '正在准备本次启动的模型配置，尚未确认写入成功。'
            : status?.reason === 'identity_mismatch' ? '回执不属于当前实例、所有者或启动代次，未作为成功证据。'
              : status?.reason === 'runtime_unavailable' || status?.reason === 'request_failed' ? '暂时无法读取同步状态；不代表目录为空或模型不可用，请稍后刷新。'
                : status?.reason === 'invalid_report' ? '运行时回执不完整或不合法，未按同步成功处理。'
              : '未取得有效的运行时写入回执；旧镜像或独立运行时可能尚未上报，不代表模型不可用。'
    : state === 'applied' || state === 'empty' ? 'The runtime confirmed the startup configuration write; this is not a live health probe.'
      : state === 'failed' ? 'Startup model configuration did not complete; inspect runtime startup errors.'
        : 'No current startup write has been confirmed. Older or standalone runtimes may not report it; this does not prove models are unavailable.';
  const metadata = [status?.revision && /^[a-f0-9]{64}$/i.test(status.revision) ? `${zh ? '版本' : 'Revision'}: ${status.revision.slice(0, 12)}` : '', status?.observed_at && !Number.isNaN(Date.parse(status.observed_at)) ? new Date(status.observed_at).toLocaleString(locale) : ''].filter(Boolean);
  return { label: labels[state] ?? labels.unknown, title: [explanation, ...metadata].join('\n') };
}
