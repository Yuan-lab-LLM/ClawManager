import { useEffect, useState } from "react";
import { useI18n } from "../contexts/I18nContext";
import { instanceService } from "../services/instanceService";
import type { RuntimeShutdownAuditHistory } from "../types/instance";

// Read-only diagnostics. This component must never invoke stop, unlock or
// upgrade APIs, and a failed read must not turn into a successful cleanup label.
export default function RuntimeShutdownAuditPanel({ instanceId }: { instanceId: number }) {
  const { locale } = useI18n();
  const zh = locale.startsWith("zh");
  const [open, setOpen] = useState(false);
  const [cursor, setCursor] = useState<number>();
  const [refresh, setRefresh] = useState(0);
  const requestKey = `${instanceId}:${cursor ?? 0}:${refresh}`;
  const [result, setResult] = useState<{ requestKey: string; data?: RuntimeShutdownAuditHistory; error?: boolean }>();
  const current = result?.requestKey === requestKey ? result : undefined;
  const state = { data: current?.data, error: current?.error, loading: !current };
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    const controller = new AbortController();
    void instanceService.getRuntimeShutdownAudits(instanceId, cursor, controller.signal).then(data => {
      if (!cancelled) setResult({ requestKey, data });
    }).catch(() => { if (!cancelled) setResult({ requestKey, error: true }); });
    return () => { cancelled = true; controller.abort(); };
  }, [instanceId, open, cursor, requestKey]);
  const labels = zh ? {
    cleanup_unconfirmed: "清理未确认",
    cleanup_confirmed_release_unconfirmed: "已上报清理完成；占用释放未确认",
    cleanup_confirmed_release_observed: "已上报清理完成，并观察到占用释放",
  } : {
    cleanup_unconfirmed: "Cleanup unconfirmed",
    cleanup_confirmed_release_unconfirmed: "Cleanup reported; release unconfirmed",
    cleanup_confirmed_release_observed: "Cleanup reported; release observed",
  };
  return <details className="w-full rounded border border-slate-200 bg-white p-2 text-xs" onToggle={event => { setOpen(event.currentTarget.open); if (event.currentTarget.open) setRefresh(value => value + 1); }}>
    <summary className="cursor-pointer">{zh ? "运行时停机观察（只读）" : "Runtime shutdown observations (read-only)"}</summary>
    <p className="my-2 text-slate-500">{zh ? "以下是管理器上报的历史观察，不是平台独立验签结果，不代表当前占用空闲，不能据此解锁、确认任务取消或自动升级。未显示记录不等于安全。" : "Historical manager observations, not independent platform proof. They do not authorize unlocking, task cancellation or upgrades. Missing records do not imply safety."}</p>
    {state.loading && <p>{zh ? "加载中…" : "Loading…"}</p>}
    {state.error && <p role="alert" className="text-red-600">{zh ? "停机观察读取失败，当前清理状态未知。" : "History unavailable; cleanup state is unknown."}</p>}
    {state.data && <>
      <p>{zh ? "实例当前代次：" : "Current instance generation: "}{state.data.generation}</p>
      {!state.data.items.length && <p className="my-2">{zh ? "没有可展示的停机观察。旧运行时、未捕获占用身份或尚未上报时可能没有记录。" : "No observations. Older runtimes, missing claim identity or pending reports may leave no records."}</p>}
      <ul className="my-2 space-y-2">
        {state.data.items.map(item => <li key={item.id} className="rounded border border-slate-100 p-2">
          <p>{labels[item.outcome] ?? (zh ? "未知观察结果" : "Unknown outcome")}</p>
          <p className="text-slate-500">{zh ? "历史观察代次" : "Observed generation"} {item.generation} · {item.source === "pro_agent" ? "Pro" : "Lite"} · {zh ? "平台接收时间" : "Received"} {item.received_at}</p>
          <p className="text-slate-500" title={item.claim_id}>{zh ? "占用标识：" : "Claim: "}{item.claim_id.slice(0, 12)}…</p>
        </li>)}
      </ul>
    </>}
    <div className="mt-2 flex gap-2">
      <button type="button" className="app-button-secondary" disabled={state.loading} onClick={() => { setCursor(undefined); setRefresh(value => value + 1); }}>{zh ? "刷新最新记录" : "Refresh latest"}</button>
      <button type="button" className="app-button-secondary" disabled={state.loading || !state.data?.next_before_id} onClick={() => setCursor(state.data!.next_before_id)}>{zh ? "更早记录" : "Older records"}</button>
    </div>
  </details>;
}
