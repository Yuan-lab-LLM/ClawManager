import { useEffect, useId, useState } from "react";
import { useI18n } from "../contexts/I18nContext";
import { instanceService } from "../services/instanceService";
import type { InstanceSessionUsageSummary } from "../types/instance";

// This is instance-wide gateway telemetry, not an inference about the iframe's
// currently selected session. Keep that distinction visible in the details.
export default function InstanceModelHint({ instanceId, active }: {
  instanceId: number;
  active: boolean;
}) {
  const { locale } = useI18n();
  const zh = locale.startsWith("zh");
  const detailsId = useId();
  const [open, setOpen] = useState(false);
  const [state, setState] = useState<{
    instanceId: number;
    summary?: InstanceSessionUsageSummary;
    failed?: boolean;
  } | null>(null);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    setState(null);
    setOpen(false);
    if (!active) return;
    const poll = async () => {
      if (!document.hidden) {
        try {
          const result = await instanceService.getInstanceSessionUsage(instanceId, { page: 1, limit: 1 });
          if (!cancelled) setState({ instanceId, summary: result.summary });
        } catch {
          if (!cancelled) setState({ instanceId, failed: true });
        }
      }
      // Schedule after completion: slow requests must not overlap or arrive
      // out of order. Cleanup fences responses from the previous instance.
      if (!cancelled) timer = setTimeout(poll, 4000);
    };
    void poll();
    return () => { cancelled = true; clearTimeout(timer); };
  }, [instanceId, active]);

  const current = active && state?.instanceId === instanceId ? state : null;
  const summary = current?.summary;
  const model = summary?.latest_actual_model;
  const completed = summary?.latest_model_status === "completed";
  const retrying = summary?.latest_model_status === "retrying";
  const statusLabels: Record<string, string> = zh ? {
    completed: "已完成", failed: "本次尝试失败（不代表整轮任务失败）",
    request_failed: "本次请求已失败，自动切换已结束", retrying: "已切换模型，等待结果",
    retry_unknown: "已发生切换，最终结果暂未确认",
  } : {
    completed: "Completed", failed: "Attempt failed (not necessarily the whole task)",
    request_failed: "Request failed; no further retry", retrying: "Switched model; awaiting result",
    retry_unknown: "Failover recorded; final outcome unconfirmed",
  };
  const label = model
    ? `${zh ? (retrying ? "已切换，等待结果" : completed ? "最近回答" : "最近调用") : (retrying ? "Retrying" : completed ? "Latest reply" : "Latest call")} · ${model}`
    : !active ? (zh ? "模型记录：实例未就绪" : "Model: instance not ready")
    : current?.failed ? (zh ? "模型记录：暂不可用" : "Model record unavailable")
    : !current ? (zh ? "模型记录：加载中" : "Loading model record")
    : (zh ? "最近回答：暂无记录" : "No reply recorded");
  const time = summary?.latest_model_at ? new Date(summary.latest_model_at) : null;

  return (
    <div className="group relative min-w-0" onKeyDown={(event) => {
      if (event.key === "Escape") { setOpen(false); event.currentTarget.querySelector("button")?.blur(); }
    }}>
      <button type="button" aria-label={label} aria-describedby={detailsId}
        onClick={() => setOpen(!open)} onBlur={() => setOpen(false)}
        className="flex max-w-[140px] items-center gap-1.5 rounded-md px-2 py-1 text-xs text-slate-500 hover:bg-slate-50 focus-visible:outline-indigo-500 sm:max-w-[240px]">
        <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${retrying ? "animate-pulse bg-amber-400" : model && completed ? "bg-emerald-400" : "bg-slate-300"}`} />
        <span className="truncate">{label}</span>
      </button>
      <div id={detailsId} role="tooltip"
        className={`pointer-events-none absolute right-0 top-full z-50 mt-2 w-64 max-w-[80vw] rounded-lg border border-slate-200 bg-white p-3 text-xs text-slate-600 shadow-lg ${open ? "visible" : "invisible group-hover:visible group-focus-within:visible"}`}>
        <p className="font-medium text-slate-900">{zh ? "实例最近一次模型调用" : "Latest instance model call"}</p>
        <dl className="mt-2 space-y-1 break-words">
          <div><dt className="inline">{zh ? "请求模式：" : "Requested: "}</dt><dd className="inline">{summary?.latest_requested_model || "—"}</dd></div>
          <div><dt className="inline">{zh ? "实际模型：" : "Actual model: "}</dt><dd className="inline">{model || "—"}</dd></div>
          <div><dt className="inline">{zh ? "状态：" : "Status: "}</dt><dd className="inline">{statusLabels[summary?.latest_model_status || ""] || summary?.latest_model_status || "—"}</dd></div>
          <div><dt className="inline">{zh ? "时间：" : "Time: "}</dt><dd className="inline">{time && !Number.isNaN(time.getTime()) ? time.toLocaleString(locale) : "—"}</dd></div>
          {summary?.latest_latency_ms != null && <div>{zh ? "响应耗时：" : "Latency: "}{(summary.latest_latency_ms / 1000).toFixed(2)} s</div>}
          {summary?.latest_ttft_ms != null && <div>{zh ? "首个有效输出（含思考）：" : "First output (including reasoning): "}{summary.latest_ttft_ms} ms</div>}
        </dl>
        <p className="mt-2 text-slate-400">{zh ? "每 4 秒刷新；包含此实例的其他会话或后台调用，不与当前气泡逐条绑定。历史模型请查看下方会话用量详情。" : "Refreshes every 4 seconds. Includes other sessions and background calls on this instance; not tied to the current message. See session usage details for history."}</p>
      </div>
    </div>
  );
}
