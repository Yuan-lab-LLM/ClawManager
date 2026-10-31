import { useEffect, useRef, useState } from "react";
import { useAuth } from "../contexts/AuthContext";
import { useI18n } from "../contexts/I18nContext";
import { instanceService } from "../services/instanceService";
import type { RuntimeMaintenanceStatus } from "../types/instance";

// Mounted with an instance key: operations and late responses cannot move to
// another instance when navigating. No image/stop API is called by this panel.
export default function RuntimeMaintenancePanel({ instanceId }: { instanceId: number }) {
  const { user } = useAuth();
  const { locale } = useI18n();
  const zh = locale.startsWith("zh");
  const admin = user?.role === "admin";
  const [open, setOpen] = useState(false);
  const [state, setState] = useState<RuntimeMaintenanceStatus>();
  const [supported, setSupported] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const alive = useRef(true);
  const sequence = useRef(0);
  useEffect(() => { alive.current = true; return () => { alive.current = false; sequence.current++; }; }, []);
  useEffect(() => {
    if (!open || !admin) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      const ticket = sequence.current;
      if (!document.hidden && !busy) {
        try {
          const [value, protocol] = await Promise.all([
            instanceService.getRuntimeMaintenance(instanceId), instanceService.getRuntimeGovernance(instanceId),
          ]);
          if (!cancelled && ticket === sequence.current) {
            setState(value);
            setSupported(protocol.mode === "governed" && protocol.capabilities.includes("maintenance_drain"));
            setError("");
          }
        } catch {
          if (!cancelled && ticket === sequence.current) {
            setState(undefined); setSupported(false);
            setError(zh ? "无法确认排空状态，请勿停止或升级实例。" : "Drain status unavailable. Do not stop or upgrade this instance.");
          }
        }
      }
      if (!cancelled) timer = setTimeout(poll, 2000);
    };
    void poll();
    return () => { cancelled = true; clearTimeout(timer); };
  }, [open, admin, instanceId, busy, zh]);
  const act = async (cancel: boolean) => {
    if (busy || state?.operation?.upgrade_fenced) return;
    const operation = state?.operation?.operation_id;
    if (cancel && !operation) return;
    sequence.current++;
    setBusy(true); setError("");
    try {
      if (cancel) await instanceService.cancelRuntimeMaintenance(instanceId, operation!);
      else await instanceService.beginRuntimeMaintenance(instanceId);
      if (alive.current) setState(undefined);
    } catch {
      if (alive.current) setError(zh ? "操作未确认：运行时可能不支持排空、状态已变化或连接失败，请刷新确认。" : "Operation unconfirmed: unsupported runtime, changed state or connection failure. Refresh to verify.");
    } finally { if (alive.current) setBusy(false); }
  };
  if (!admin) return null;
  const reported = state?.draining && state.operation?.reported_at;
  const fenced = !!state?.operation?.upgrade_fenced;
  return <details className="w-full rounded border border-slate-200 bg-white p-2 text-xs" onToggle={event => setOpen(event.currentTarget.open)}>
    <summary className="cursor-pointer">{zh ? "升级前排空检查（管理员）" : "Pre-upgrade drain check (admin)"}</summary>
    {fenced ? <p role="alert" className="my-2 text-amber-700">{zh ? "此实例已被持久化升级隔离保护，不会因排空超时自动恢复接单，不能在此取消。请由升级恢复流程确认后解除；本页面不会停止进程或修改镜像。" : "This instance has a durable upgrade fence. Drain timeout cannot reopen admission and cancellation is disabled here. Upgrade recovery must explicitly release it; this panel does not stop processes or change images."}</p> : <p className="my-2">{zh ? "仅检查，不升级：暂停接收新任务，已接收任务继续执行。连接正常时，5 分钟后自动恢复，或手动取消。不强杀任务，不修改镜像。" : "Check only: pauses new tasks while accepted work finishes. Reopens after 5 minutes when control connectivity is healthy, or cancel manually. No process kill or image change."}</p>}
    <p>{!state ? (zh ? "状态尚未确认" : "Status unconfirmed") : state.drained ? (zh ? "运行时报告已排空；尚不能自动升级或据此备份。" : "Runtime reports drained; automatic upgrade/backup is not available.") : state.draining ? (zh ? "正在等待排空" : "Waiting for drain") : (zh ? "未处于排空状态（未开始、已取消或已超时）" : "Not draining (not started, cancelled or expired)")}</p>
    {reported && <p className="my-2">{zh ? "执行 / 接单 / 后台 / 待处理：" : "Execution / admission / jobs / queued: "}{state.operation!.active_executions} / {state.operation!.active_admissions} / {state.operation!.active_jobs} / {state.operation!.queued_inputs}</p>}
    {!supported && <p className="my-2 text-amber-700">{zh ? "当前运行时尚未确认排空能力；旧实例可继续使用，不会被强制升级。" : "Drain capability is unconfirmed. Existing instances remain usable and are not force-upgraded."}</p>}
    {error && <p role="alert" className="my-2 text-red-600">{error}</p>}
    <div className="mt-2 flex gap-2">
      <button type="button" className="app-button-secondary" disabled={busy || fenced || !state || !supported || state.draining} onClick={() => void act(false)}>{zh ? "开始排空检查" : "Begin drain check"}</button>
      <button type="button" className="app-button-secondary" disabled={busy || fenced || !state?.draining || !state.operation} onClick={() => void act(true)}>{zh ? "取消排空，恢复接单" : "Cancel drain"}</button>
    </div>
  </details>;
}
