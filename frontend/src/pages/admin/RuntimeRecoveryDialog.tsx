import { useState } from "react";
import { useI18n } from "../../contexts/I18nContext";
import { runtimePoolService } from "../../services/runtimePoolService";
import { runtimeError } from "../../lib/runtimeManagement";
import type { RuntimePod, RuntimeRecoveryInspection } from "../../types/runtimePool";
import { buttonClass, primaryClass, inputClass, ErrorMessage, RuntimeDialog } from "./RuntimeManagementUI";

export default function RuntimeRecoveryDialog({ pods, onClose, onChanged, onEvict }: {
  pods: RuntimePod[];
  onClose: () => void;
  onChanged: () => void;
  onEvict?: (pod: RuntimePod, ids?: number[], verifiedRecoveryIds?: number[]) => void;
}) {
  const { t } = useI18n();
  const [id, setId] = useState("");
  const [info, setInfo] = useState<RuntimeRecoveryInspection>();
  const [podId, setPodId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [message, setMessage] = useState<string>();
  const number = Number(id);
  const validId = Number.isSafeInteger(number) && number > 0;
  const pod = pods.find(item => item.id === Number(podId)) ?? (info?.pod?.id === Number(podId) ? info.pod : undefined);
  const canRetry = info && ["creating", "error", "running"].includes(info.status) && (info.binding?.state !== "running" || !!info.failover);
  const act = async (action: "inspect" | "recheck" | "retry") => {
    if (busy || (action !== "recheck" && !validId)) return;
    setBusy(true); setError(undefined); setMessage(undefined);
    try {
      if (action === "recheck") {
        if (!pod?.pod_uid) return;
        await runtimePoolService.recheckPod(pod.id, pod.pod_uid);
      } else if (action === "retry") {
        if (!info || info.instance_id !== number) return;
        await runtimePoolService.retryInstanceRecovery(info.instance_id, info.generation);
      }
      if (validId) {
        const result = await runtimePoolService.inspectInstanceRecovery(number);
        setInfo(result);
        if (action === "inspect" && result.pod) setPodId(String(result.pod.id));
      }
      if (action !== "inspect") { setMessage(t("runtimePods.recoveryRequested")); onChanged(); }
    } catch (err) {
      setError(runtimeError(err));
      if (validId) {
        try { setInfo(await runtimePoolService.inspectInstanceRecovery(number)); } catch { /* Retain the failed request and last inspected identity. */ }
      }
      onChanged();
    } finally { setBusy(false); }
  };
  const options = info?.pod && !pods.some(item => item.id === info.pod!.id) ? [...pods, info.pod] : pods;
  return (
    <RuntimeDialog title={t("runtimePods.recoveryTitle")} onClose={() => { if (!busy) onClose(); }} closeOnBackdrop={!busy}>
      <p className="text-sm leading-6 text-slate-600">{t("runtimePods.recoveryHelp")}</p>
      <form className="mt-4 flex flex-wrap items-end gap-3" onSubmit={event => { event.preventDefault(); void act("inspect"); }}>
        <label className="flex min-w-0 flex-1 flex-col gap-2 text-sm">
          {t("runtimePods.recoveryInstanceId")}
          <input type="number" min="1" step="1" className={inputClass} value={id} disabled={busy} onChange={event => { setId(event.target.value); setInfo(undefined); setError(undefined); setMessage(undefined); }} />
        </label>
        <button type="submit" className={buttonClass} disabled={busy || !validId}>{t("runtimePods.recoveryInspect")}</button>
      </form>
      {info && <section className="mt-4 space-y-3" aria-label={t("runtimePods.recoveryInspect")}>
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          <dt className="text-slate-600">{t("runtimePods.status")}</dt><dd>{t(`runtimePods.${info.status}`)} · G{info.generation}</dd>
          <dt className="text-slate-600">{t("runtimePods.recoveryAttempts")}</dt><dd>{info.failover?.attempts ?? info.recovery?.attempts ?? 0} / {info.failover ? 5 : 3}</dd>
          {info.recovery?.phase && <><dt className="text-slate-600">{t("runtimePods.recoveryPhase")}</dt><dd>{t(info.recovery.phase === "source_stopped" ? "runtimePods.recoverySourceStopped" : "runtimePods.recoveryPlacing")}</dd></>}
        </dl>
        {info.blocker && <p className="text-sm leading-6 text-amber-800">{info.blocker_code ? t(`runtimePods.recoveryBlocker_${info.blocker_code}`) : info.blocker}</p>}
        {info.recovery?.reason && <p className="break-words text-sm text-slate-600">{info.recovery.reason}</p>}
        <button type="button" className={primaryClass} disabled={busy || !canRetry} onClick={() => void act("retry")}>{t("runtimePods.recoveryRetry")}</button>
      </section>}
      <section className="mt-6 space-y-3 border-t border-slate-200 pt-4">
        <label className="flex flex-col gap-2 text-sm">{t("runtimePods.recoveryPod")}
          <select className={`${inputClass} w-full`} value={podId} disabled={busy} onChange={event => setPodId(event.target.value)}>
            <option value="">{t("runtimePods.recoveryChoosePod")}</option>
            {options.map(item => <option key={item.id} value={item.id}>{item.pod_name} · #{item.id}</option>)}
          </select>
        </label>
        <button type="button" className={buttonClass} disabled={busy || !pod || !onEvict} onClick={() => { if (pod) { const verified = info && info.binding?.runtime_pod_id === pod.id && Number.isSafeInteger(info.instance_id) && info.instance_id > 0 ? [info.instance_id] : undefined; onEvict?.(pod, verified, verified); } }}>{t("runtimePods.faultRecovery")}</button>
        <button type="button" className={buttonClass} disabled={busy || !pod?.pod_uid} onClick={() => void act("recheck")}>{t("runtimePods.recoveryRecheck")}</button>
      </section>
      {busy && <p className="mt-3 text-sm text-slate-600" role="status">{t("runtimePods.recoveryChecking")}</p>}
      {error && <div className="mt-3"><ErrorMessage>{error}</ErrorMessage></div>}
      {message && <p className="mt-3 text-sm text-slate-700" role="status">{message}</p>}
    </RuntimeDialog>
  );
}
