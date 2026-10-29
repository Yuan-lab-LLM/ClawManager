import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import axios from "axios";
import { useI18n } from "../../contexts/I18nContext";
import { runtimeError, summarizePod } from "../../lib/runtimeManagement";
import { runtimePoolService } from "../../services/runtimePoolService";
import { formatInstanceType } from "../../types/instance";
import type { RuntimeManagedPool, RuntimeScaleInPlan, RuntimeSchedulingOperation, RuntimeType } from "../../types/runtimePool";
import { buttonClass, primaryClass, inputClass, ErrorMessage, RuntimeDialog } from "./RuntimeManagementUI";

const runtimeTypes: RuntimeType[] = ["openclaw", "hermes", "opencode", "deepseek-harness"];
export default function RuntimeMaintenanceDialog({ onClose, onChanged, onViewScheduling }: { onClose: () => void; onChanged: () => void; onViewScheduling: () => void }) {
  const { t } = useI18n();
  const [section, setSection] = useState<"create" | "scale-in" | "manage">("create");
  const scaleOptions = useQuery({ queryKey: ["runtime-pod-scale-options"], queryFn: ({ signal }) => runtimePoolService.listScaleOptions(signal) });
  const pools = useQuery({ queryKey: ["runtime-pod-pools"], queryFn: ({ signal }) => runtimePoolService.listManagedPools(signal), enabled: section === "manage" });
  const inventory = useQuery({ queryKey: ["runtime-pods", "all"], queryFn: ({ signal }) => runtimePoolService.listPods(undefined, signal), enabled: section === "manage", refetchInterval: 3000 });
  const [type, setType] = useState<RuntimeType | "">("");
  const [podCount, setPodCount] = useState("1");
  const [plan, setPlan] = useState<RuntimeScaleInPlan>();
  const [acknowledged, setAcknowledged] = useState(false);
  const [operation, setOperation] = useState<RuntimeSchedulingOperation>();
  const planRef = useRef<RuntimeScaleInPlan | undefined>(undefined);
  const mutationPending = useRef(false);
  const resetPlan = () => { setPlan(undefined); planRef.current = undefined; setAcknowledged(false); };

  const currentType = useRef<RuntimeType | "">("");
  const [deleting, setDeleting] = useState<RuntimeManagedPool>();
  const [confirmName, setConfirmName] = useState("");
  const [confirmUid, setConfirmUid] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const selected = scaleOptions.data?.items.find(item => item.runtime_type === type);
  const unavailable = scaleOptions.data?.unavailable?.find(item => item.runtime_type === type);
  const minPods = scaleOptions.data?.limits.min_pods ?? 1;
  const maxPods = scaleOptions.data?.limits.max_pods ?? 10;
  const count = Number(podCount);
  const validCount = podCount.trim() !== "" && Number.isInteger(count) && count >= minPods && count <= maxPods;
  const validCreate = !!selected && !scaleOptions.isError && validCount;
  const validScaleIn = validCreate && count < selected!.replicas;
  const canScaleIn = validScaleIn && !!plan && plan.runtime_type === type &&
    plan.removed_pods === count && plan.allowed && !!plan.plan_fingerprint && acknowledged;
  const previewScaleIn = async () => {
    if (mutationPending.current || !validScaleIn || !selected || selected.runtime_type !== currentType.current) return;
    mutationPending.current = true; setBusy(true); setError(undefined); setNotice(undefined); resetPlan();
    try {
      const result = await runtimePoolService.preflightScaleIn({ runtime_type: selected.runtime_type, pod_count: count });
      if (result.runtime_type !== selected.runtime_type || result.removed_pods !== count ||
        !Array.isArray(result.source_pods) || !Array.isArray(result.items) || !Array.isArray(result.blockers) ||
         (result.allowed && (!result.plan_fingerprint || !result.namespace || !result.deployment_name || !result.deployment_uid ||
          result.replicas < 1 || result.previous_replicas - result.replicas !== count))) throw new Error(t("runtimePods.scaleInPlanIncomplete"));
      setPlan(result); planRef.current = result;
    } catch (err) { reportError(err); } finally { mutationPending.current = false; setBusy(false); }
  };
  const scaleIn = async () => {
    if (mutationPending.current || !canScaleIn || !plan || planRef.current !== plan) return;
    mutationPending.current = true; setBusy(true); setError(undefined); resetPlan();
    try {
      const result = await runtimePoolService.scaleIn({ runtime_type: plan.runtime_type, pod_count: plan.removed_pods, plan_fingerprint: plan.plan_fingerprint });
      setOperation(result); refresh();
    } catch (err) { reportError(err); onChanged(); } finally { mutationPending.current = false; setBusy(false); }
  };
  const poolPods = (pool: RuntimeManagedPool) => (inventory.data ?? []).filter(pod => pod.namespace === pool.namespace && pod.deployment_name === pool.deployment_name);
  const canDelete = (pool: RuntimeManagedPool) => !!pool.deployment_uid && inventory.isSuccess && !pools.isError &&
    !!pools.data?.items.some(item => item.namespace === pool.namespace && item.deployment_name === pool.deployment_name && item.deployment_uid === pool.deployment_uid) &&
    poolPods(pool).every(pod => !summarizePod(pod).occupied && (pod.id <= 0 || !!pod.pod_uid));
  const reportError = (err: unknown) => setError(
    axios.isAxiosError(err) && !err.response && ["ERR_NETWORK", "ECONNABORTED", "ETIMEDOUT"].includes(err.code ?? "")
      ? t("runtimePods.maintenanceOutcomeUnknown") : runtimeError(err),
  );
  const refresh = () => {
    onChanged();
    void scaleOptions.refetch({ cancelRefetch: false });
    if (section === "manage") {
      void pools.refetch({ cancelRefetch: false });
      void inventory.refetch({ cancelRefetch: false });
    }
  };
  const create = async () => {
    if (mutationPending.current || !validCreate || !selected || selected.runtime_type !== currentType.current) return;
    mutationPending.current = true;
    setBusy(true); setError(undefined); setNotice(undefined);
    try {
      const result = await runtimePoolService.createPod({ runtime_type: selected.runtime_type, pod_count: count });
      setNotice(t("runtimePods.podsAdded", { count: result.added_pods, total: result.replicas }));
      refresh();
    } catch (err) {
      reportError(err);
      onChanged();
      await scaleOptions.refetch({ cancelRefetch: false }).catch(() => undefined);
    } finally { mutationPending.current = false; setBusy(false); }
  };
  const remove = async () => {
    if (mutationPending.current || !deleting || !canDelete(deleting) || confirmName !== deleting.deployment_name || confirmUid !== deleting.deployment_uid) return;
    mutationPending.current = true;
    setBusy(true); setError(undefined); setNotice(undefined);
    try {
      const registered = poolPods(deleting).find(pod => pod.id > 0);
      if (registered) await runtimePoolService.deletePod(registered.id, registered.pod_uid!);
      else await runtimePoolService.deletePodPool(deleting.deployment_name, deleting.namespace, deleting.deployment_uid);
      setNotice(t("runtimePods.poolDeletionRequested", { name: deleting.deployment_name }));
      setDeleting(undefined); setConfirmName(""); setConfirmUid(""); refresh();
    } catch (err) { reportError(err); } finally { mutationPending.current = false; setBusy(false); }
  };
  return (
    <RuntimeDialog title={t("runtimePods.maintenance")} onClose={() => { if (!mutationPending.current) onClose(); }} wide closeOnBackdrop={false}
      footer={<>
        {section === "manage" && deleting ? <>
          <button type="button" className={buttonClass} disabled={busy} onClick={() => setDeleting(undefined)}>{t("runtimePods.cancel")}</button>
          <button type="submit" form="runtime-delete-pool" className={`${primaryClass} !border-red-600 !bg-red-600`} disabled={busy || !canDelete(deleting) || confirmName !== deleting.deployment_name || confirmUid !== deleting.deployment_uid}>{t("runtimePods.deleteEmptyPool")}</button>
        </> : <>
          <button type="button" className={buttonClass} disabled={busy} onClick={onClose}>{t("runtimePods.close")}</button>
          {section === "scale-in" && !operation && <button type="button" className={primaryClass} disabled={busy || !canScaleIn} onClick={() => void scaleIn()}>{t("runtimePods.confirmScaleIn")}</button>}
          {section === "create" && !operation && <button type="submit" form="runtime-create-pod" className={primaryClass} disabled={busy || !validCreate}>{t("runtimePods.addPods")}</button>}
        </>}
      </>}>
      <nav aria-label={t("runtimePods.maintenance")} className="flex gap-2 border-b border-slate-100 pb-3">
        <button type="button" className={section === "create" ? primaryClass : buttonClass} aria-pressed={section === "create"} disabled={busy} onClick={() => { setSection("create"); resetPlan(); }}>{t("runtimePods.addPods")}</button>
        <button type="button" className={section === "scale-in" ? primaryClass : buttonClass} aria-pressed={section === "scale-in"} disabled={busy} onClick={() => { setSection("scale-in"); resetPlan(); }}>{t("runtimePods.scaleIn")}</button>
        <button type="button" className={section === "manage" ? primaryClass : buttonClass} aria-pressed={section === "manage"} disabled={busy} onClick={() => { setSection("manage"); resetPlan(); }}>{t("runtimePods.managedPools")}{pools.data ? ` (${pools.data.items.length})` : ""}</button>
      </nav>
      {error && <ErrorMessage>{error}</ErrorMessage>}
      {notice && <p role="status" className="rounded bg-emerald-50 p-3 text-xs text-emerald-700">{notice}</p>}
      {section !== "manage" && scaleOptions.isError && <ErrorMessage>{runtimeError(scaleOptions.error)} <button type="button" onClick={() => void scaleOptions.refetch({ cancelRefetch: false })}>{t("runtimePods.retry")}</button></ErrorMessage>}
      {section !== "manage" && !scaleOptions.data && !scaleOptions.isError && <p role="status">{t("runtimePods.loading")}</p>}
      {section !== "manage" && !operation && <form id="runtime-create-pod" aria-label={t(section === "scale-in" ? "runtimePods.scaleIn" : "runtimePods.addPods")} className="space-y-4" onSubmit={event => { event.preventDefault(); if (section === "create") void create(); else void previewScaleIn(); }}>
        <p className="text-xs leading-5 text-slate-500">{t(section === "scale-in" ? "runtimePods.scaleInHelp" : "runtimePods.scaleHelp")}</p>
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="space-y-1 text-xs"><span>{t("runtimePods.typeLabel")}</span>
            <select aria-label={t("runtimePods.runtimeType")} className={`${inputClass} w-full`} value={type} disabled={busy} onChange={event => {
              currentType.current = event.target.value as RuntimeType | ""; setType(currentType.current); resetPlan(); setError(undefined); setNotice(undefined);
            }}>
              <option value="">{t("runtimePods.chooseRuntimeType")}</option>
              {runtimeTypes.map(value => <option key={value} value={value} disabled={!!scaleOptions.data && !scaleOptions.data.items.some(item => item.runtime_type === value)}>{formatInstanceType(value)}</option>)}
            </select>
          </label>
          <label className="space-y-1 text-xs"><span>{t(section === "scale-in" ? "runtimePods.removedPods" : "runtimePods.additionalPods")}</span><input aria-label={t(section === "scale-in" ? "runtimePods.removedPods" : "runtimePods.additionalPods")} className={`${inputClass} w-full`} type="number" min={minPods} max={maxPods} step={1} required value={podCount} disabled={busy} onChange={event => { setPodCount(event.target.value); resetPlan(); setError(undefined); }} /></label>
        </div>
        {selected && section === "create" && <p className="rounded bg-slate-50 p-3 text-xs leading-5 text-slate-600">{t("runtimePods.scaleSummary", { slots: selected.slot_count, count: validCount ? count : "—", added: validCount ? count * selected.slot_count : "—", total: validCount ? selected.replicas + count : "—" })}</p>}
        {type && scaleOptions.data && !selected && <ErrorMessage>{unavailable?.reason || t("runtimePods.scaleUnavailable")}</ErrorMessage>}
        {!!scaleOptions.data?.unavailable?.length && !type && <ul className="space-y-1 text-xs text-slate-500">{scaleOptions.data.unavailable.map(item => <li key={item.runtime_type}>{formatInstanceType(item.runtime_type)}: {item.reason}</li>)}</ul>}
        {section === "scale-in" && <>
          {selected && <p className="text-xs text-slate-600">{t("runtimePods.scaleInCount", { before: selected.replicas, after: validCount ? Math.max(0, selected.replicas - count) : "—" })}</p>}
          {selected && validCount && !validScaleIn && <ErrorMessage>{t("runtimePods.scaleInKeepOne")}</ErrorMessage>}
          <button type="button" className={buttonClass} disabled={busy || !validScaleIn} onClick={() => void previewScaleIn()}>{t("runtimePods.previewScaleIn")}</button>
          {plan && <section className="space-y-3 rounded border border-slate-200 p-3 text-xs">
            {plan.deployment_name && <p className="break-all font-medium">{plan.namespace}/{plan.deployment_name}</p>}
            <p>{plan.previous_replicas > 0 && t("runtimePods.scaleInCount", { before: plan.previous_replicas, after: plan.replicas })} · {t("runtimePods.scaleInMoves", { count: plan.items.length })}</p>
            <table className="w-full text-left"><thead className="text-slate-500"><tr><th>Pod</th><th>{t("runtimePods.node")}</th><th>{t("runtimePods.instances")}</th></tr></thead><tbody>{plan.source_pods.map(pod => <tr key={pod.pod_uid}><td className="break-all py-2 pr-2">{pod.pod_name}</td><td>{pod.node_name || "—"}</td><td>{pod.instance_count}</td></tr>)}</tbody></table>
            {plan.items.length > 0 && <details className="rounded bg-slate-50 p-2"><summary className="cursor-pointer font-medium">{t("runtimePods.scaleInMoves", { count: plan.items.length })}</summary><ul className="mt-2 max-h-40 space-y-1 overflow-y-auto">{plan.items.map(item => <li key={item.instance_id}>#{item.instance_id} · Pod #{item.source_pod_id ?? "—"} → Pod #{item.target_pod_id ?? "—"} / {t("runtimePods.slot")} {item.target_slot ?? "—"}</li>)}</ul></details>}
            {plan.blockers.map((blocker, index) => <ErrorMessage key={index}>{blocker}</ErrorMessage>)}
            <label className="flex items-start gap-2 leading-5"><input type="checkbox" aria-label={t("runtimePods.scaleInAcknowledge")} checked={acknowledged} disabled={busy || !plan.allowed} onChange={event => setAcknowledged(event.target.checked)} />{t("runtimePods.scaleInAcknowledge")}</label>
          </section>}
        </>}
      </form>}
      {operation && <div role="status" className="space-y-3 rounded bg-emerald-50 p-3 text-xs text-emerald-800"><p>{t("runtimePods.scaleInSubmitted", { id: operation.id })}</p><button type="button" className={buttonClass} onClick={onViewScheduling}>{t("runtimePods.viewScheduling")}</button></div>}
      {section === "manage" && <section className="space-y-3">
        <h3 className="text-sm font-semibold">{t("runtimePods.managedPools")}</h3>
        {pools.isError && <ErrorMessage>{runtimeError(pools.error)} <button type="button" onClick={() => void pools.refetch({ cancelRefetch: false })}>{t("runtimePods.retry")}</button></ErrorMessage>}
        {!pools.data && !pools.isError && <p role="status">{t("runtimePods.loading")}</p>}
        <p className="text-xs leading-5 text-slate-500">{t("runtimePods.deletePoolHelp")}</p>
        {inventory.isError && <ErrorMessage>{runtimeError(inventory.error)} <button type="button" onClick={() => void inventory.refetch({ cancelRefetch: false })}>{t("runtimePods.retry")}</button></ErrorMessage>}
        {pools.data?.items.length === 0 && <p className="text-xs text-slate-500">{t("runtimePods.noManagedPools")}</p>}
        {pools.data?.items.map(pool => <div key={`${pool.namespace}/${pool.deployment_uid}`} className="flex items-start justify-between gap-3 rounded border border-slate-200 p-3 text-xs">
          <div className="min-w-0 break-all"><p>{pool.namespace}/{pool.deployment_name} · {pool.replicas} Pod</p><p className="mt-1 text-slate-500">UID: {pool.deployment_uid}</p></div>
          <button type="button" className={`${buttonClass} shrink-0 text-red-700`} disabled={busy || !canDelete(pool)} onClick={() => { setDeleting(pool); setConfirmName(""); setConfirmUid(""); setError(undefined); setNotice(undefined); }}>{t("runtimePods.deletePool")}</button>
        </div>)}
      </section>}
      {section === "manage" && deleting && <form id="runtime-delete-pool" aria-label={t("runtimePods.deleteEmptyPool")} className="space-y-3 rounded border border-red-200 bg-red-50/30 p-3" onSubmit={event => { event.preventDefault(); void remove(); }}>
        <p className="break-all text-xs font-medium">{deleting.namespace}/{deleting.deployment_name} · UID: {deleting.deployment_uid}</p>
        <p className="text-xs leading-5 text-slate-500">{t("runtimePods.confirmPoolDeletion")}</p>
        <label className="block space-y-1 text-xs"><span>{t("runtimePods.confirmDeploymentName")}</span><input aria-label={t("runtimePods.confirmDeploymentName")} className={`${inputClass} w-full`} value={confirmName} disabled={busy} onChange={event => setConfirmName(event.target.value)} /></label>
        <label className="block space-y-1 text-xs"><span>{t("runtimePods.confirmDeploymentUid")}</span><input aria-label={t("runtimePods.confirmDeploymentUid")} className={`${inputClass} w-full`} value={confirmUid} disabled={busy} onChange={event => setConfirmUid(event.target.value)} /></label>
      </form>}
    </RuntimeDialog>
  );
}
