import { useRef, useState } from "react";
import axios from "axios";
import { useI18n } from "../contexts/I18nContext";
import { runtimeError } from "../lib/runtimeManagement";
import { runtimePoolService } from "../services/runtimePoolService";
import type { Instance } from "../types/instance";
import type { RuntimeAutoStopActivity } from "../types/runtimePool";

export default function InstanceAutoStopInfo({ instance }: { instance: Instance }) {
  const { t, locale } = useI18n();
  if (instance.instance_mode !== "lite") return null;
  const date = (value?: string) => value ? new Date(value).toLocaleString(locale) : t("autoStop.noActivity");
  return <div className="mt-2 space-y-1 whitespace-normal text-xs text-slate-500">
    {instance.stopped_reason && instance.status === "stopped" && <p>{t("autoStop.stoppedReason")}: {t(`autoStop.reason_${instance.stopped_reason}`)}</p>}
    <p>{t("autoStop.lastActivity")}: {date(instance.last_gateway_activity_at)}</p>
    <p>{t("autoStop.defaultsPolicy")}: {t(`autoStop.policy_${instance.defaults_policy_status === "configured_pending_verification" ? "configured" : instance.defaults_policy_status === "existing_source_unknown" ? "existing" : "unknown"}`)}</p>
    {instance.monitoring_since && <p>{t("autoStop.monitoringSince")}: {date(instance.monitoring_since)}</p>}
    {instance.auto_stop_disabled && <p>{t("autoStop.keepRunning")}</p>}
    {instance.auto_stop_phase && <p>{t("autoStop.phase")}: {instance.auto_stop_phase}</p>}
    {instance.auto_stop_blocker && <p className="break-words text-amber-700">{t("autoStop.blocker")}: {instance.auto_stop_blocker}</p>}
    {instance.stopped_reason === "idle_timeout" && instance.status === "stopped" && <p>{t("autoStop.startHelp")}</p>}
  </div>;
}

// Render only on the administrator instance page; the API also enforces admin authorization.
export function InstanceAutoStopControl({ instance, onChanged }: { instance: Instance; onChanged: (activity: RuntimeAutoStopActivity) => void }) {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const pending = useRef(false);
  if (instance.instance_mode !== "lite") return null;
  const change = async (disabled: boolean) => {
    if (pending.current) return;
    pending.current = true; setBusy(true); setError(undefined);
    try {
      const activity = await runtimePoolService.setInstanceAutoStopDisabled(instance.id, disabled);
      onChanged(activity);
    } catch (err) {
      const conflict = axios.isAxiosError(err) && err.response?.status === 409;
      setError(`${runtimeError(err)} ${t(conflict ? "autoStop.instanceConflict" : "autoStop.saveError")}`);
    } finally { pending.current = false; setBusy(false); }
  };
  return <div className="mt-2 text-xs">
    <label className="inline-flex items-center gap-2"><input type="checkbox" role="switch" checked={!!instance.auto_stop_disabled} disabled={busy || instance.status === "deleting"} onChange={event => void change(event.target.checked)} />{t("autoStop.keepRunning")}</label>
    {busy && <p role="status" className="text-slate-500">{t("autoStop.saving")}</p>}
    {error && <p role="alert" className="mt-1 whitespace-normal text-red-700">{error}</p>}
  </div>;
}
