import { useRef, useState } from "react";
import axios from "axios";
import { useI18n } from "../../contexts/I18nContext";
import { runtimeError } from "../../lib/runtimeManagement";
import { runtimePoolService } from "../../services/runtimePoolService";
import { formatInstanceType } from "../../types/instance";
import type { RuntimeBalancePlan, RuntimeType } from "../../types/runtimePool";
import { buttonClass, primaryClass, inputClass, ErrorMessage, RuntimeDialog } from "./RuntimeManagementUI";

const runtimeTypes: Array<RuntimeType | "all"> = ["all", "openclaw", "hermes", "opencode", "deepseek-harness"];

export default function RuntimeBalanceDialog({ onClose, onSubmitted }: { onClose: () => void; onSubmitted: () => void }) {
  const { t } = useI18n();
  const [type, setType] = useState<RuntimeType | "all">("all");
  const [plan, setPlan] = useState<RuntimeBalancePlan>();
  const planRef = useRef<RuntimeBalancePlan | undefined>(undefined);
  const version = useRef(0);
  const [acknowledged, setAcknowledged] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const canExecute = !!plan && plan.runtime_type === type && plan.allowed && plan.moves.length > 0 && !!plan.plan_fingerprint && acknowledged;
  const preview = async () => {
    if (busy) return;
    const current = version.current;
    setBusy(true); setError(undefined); setPlan(undefined); planRef.current = undefined; setAcknowledged(false);
    try {
      const result = await runtimePoolService.planBalance(type);
      if (current !== version.current) return;
      if (result.runtime_type !== type || !result.plan_fingerprint) throw new Error(t("runtimePods.balancePlanIncomplete"));
      setPlan(result); planRef.current = result;
    } catch (err) { if (current === version.current) setError(runtimeError(err)); }
    finally { if (current === version.current) setBusy(false); }
  };
  const execute = async () => {
    if (busy || !canExecute || !plan || planRef.current !== plan) return;
    planRef.current = undefined;
    setBusy(true); setError(undefined);
    try {
      await runtimePoolService.executeBalance(type, plan.plan_fingerprint);
      onSubmitted();
    } catch (err) {
      setPlan(undefined); planRef.current = undefined; setAcknowledged(false);
      setError(axios.isAxiosError(err) && !err.response && ["ERR_NETWORK", "ECONNABORTED", "ETIMEDOUT"].includes(err.code ?? "")
        ? t("runtimePods.evictionOutcomeUnknown")
        : `${runtimeError(err)} ${t("runtimePods.balanceReplan")}`);
    } finally { setBusy(false); }
  };
  return (
    <RuntimeDialog title={t("runtimePods.balance")} onClose={onClose}>
      {error && <ErrorMessage>{error}</ErrorMessage>}
      <p className="text-xs leading-5 text-slate-500">{t("runtimePods.balanceHelp")}</p>
      <div className="flex items-end gap-3">
        <label className="flex-1 space-y-1 text-xs"><span>{t("runtimePods.runtimeType")}</span>
          <select aria-label={t("runtimePods.runtimeType")} className={`${inputClass} w-full`} value={type} disabled={busy} onChange={event => {
            const next = event.target.value as RuntimeType | "all";
            if (!runtimeTypes.includes(next)) return;
            version.current++; setType(next); setPlan(undefined); planRef.current = undefined; setAcknowledged(false); setError(undefined); setBusy(false);
          }}>
            {runtimeTypes.map(value => <option key={value} value={value}>{value === "all" ? t("common.all") : formatInstanceType(value)}</option>)}
          </select>
        </label>
        <button type="button" className={buttonClass} disabled={busy} onClick={() => void preview()}>{t("runtimePods.previewBalance")}</button>
      </div>
      {plan && <>
        {plan.moves.length === 0 && <p role="status" className="rounded bg-slate-50 p-3 text-xs text-slate-600">{t(plan.blocked.length === 0 && plan.groups.every(group => group.balanced) ? "runtimePods.alreadyBalanced" : "runtimePods.noBalanceMoves")}</p>}
        <section className="space-y-3">
          <h3 className="text-sm font-semibold">{t("runtimePods.balanceDistribution")}</h3>
          {plan.groups.map((group, index) => <div key={`${group.runtime_type}/${group.pod_ids.join(",")}/${index}`} className="rounded border border-slate-200 p-3 text-xs">
            <p className="mb-2 font-medium">{formatInstanceType(group.runtime_type)} · {t("runtimePods.compatibilityGroup", { count: index + 1 })}</p>
            <table className="w-full text-left"><thead className="text-slate-500"><tr><th className="pb-1">Pod</th><th>{t("runtimePods.before")}</th><th>{t("runtimePods.after")}</th><th>{t("runtimePods.capacity")}</th></tr></thead>
              <tbody>{group.before.map(item => <tr key={item.pod_id}><td className="py-1">#{item.pod_id}</td><td>{item.count}</td><td>{group.after.find(after => after.pod_id === item.pod_id)?.count ?? "—"}</td><td>{item.capacity}</td></tr>)}</tbody>
            </table>
          </div>)}
        </section>
        <section className="space-y-2"><h3 className="text-sm font-semibold">{t("runtimePods.balanceMoves", { count: plan.moves.length })}</h3>
          {plan.moves.map(move => <div key={move.instance_id} className="rounded border border-slate-200 p-3 text-xs"><p className="font-medium">{move.instance_name || `#${move.instance_id}`} · #{move.instance_id} · {formatInstanceType(move.runtime_type)}</p><p className="mt-1 text-slate-500">Pod #{move.source_pod_id} → Pod #{move.target_pod_id} / {t("runtimePods.slot")} {move.target_slot}</p></div>)}
        </section>
        {plan.blocked.length > 0 && <section className="space-y-2"><h3 className="text-sm font-semibold">{t("runtimePods.balanceBlocked")}</h3>{plan.blocked.map((item, index) => <p key={index} className="rounded bg-amber-50 p-3 text-xs text-amber-700">{formatInstanceType(item.runtime_type)}{item.instance_id ? ` · #${item.instance_id}` : item.pod_id ? ` · Pod #${item.pod_id}` : ""} · {item.reason}</p>)}</section>}
        {plan.moves.length > 0 && <label className="flex items-start gap-2 text-xs leading-5"><input type="checkbox" checked={acknowledged} disabled={busy} onChange={event => setAcknowledged(event.target.checked)} />{t("runtimePods.balanceAcknowledge")}</label>}
      </>}
      <div className="flex justify-end gap-2 border-t border-slate-100 pt-4"><button type="button" className={buttonClass} disabled={busy} onClick={onClose}>{t("runtimePods.cancel")}</button><button type="button" className={primaryClass} disabled={busy || !canExecute} onClick={() => void execute()}>{t("runtimePods.executeBalance")}</button></div>
    </RuntimeDialog>
  );
}
