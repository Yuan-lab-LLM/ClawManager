import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import axios from "axios";
import { useI18n } from "../../contexts/I18nContext";
import { AUTO_STOP_UNITS, MAX_AUTO_STOP_SECONDS, autoStopDuration, parseAutoStopSeconds, type AutoStopUnit } from "../../lib/runtimeAutoStop";
import { runtimeError } from "../../lib/runtimeManagement";
import { runtimePoolService } from "../../services/runtimePoolService";
import type { RuntimeAutoStopSettings } from "../../types/runtimePool";
import { buttonClass, primaryClass, inputClass, ErrorMessage, RuntimeDialog } from "./RuntimeManagementUI";

export default function RuntimeAutoStopDialog({ onClose }: { onClose: () => void }) {
  const { t } = useI18n();
  const query = useQuery({ queryKey: ["runtime-auto-stop"], queryFn: ({ signal }) => runtimePoolService.getAutoStopSettings(signal), staleTime: 0, retry: false, refetchOnWindowFocus: false, refetchOnReconnect: false });
  if (!query.data || query.isError) return <RuntimeDialog title={t("autoStop.title")} onClose={onClose}>
    {query.isError ? <ErrorMessage>{runtimeError(query.error)} <button type="button" className={buttonClass} onClick={() => void query.refetch()}>{t("runtimePods.retry")}</button></ErrorMessage> : <p role="status">{t("runtimePods.loading")}</p>}
  </RuntimeDialog>;
  return <SettingsForm key={query.dataUpdatedAt} settings={query.data} onClose={onClose} onRefresh={() => void query.refetch()} refreshing={query.isFetching} />;
}

function SettingsForm({ settings, onClose, onRefresh, refreshing }: { settings: RuntimeAutoStopSettings; onClose: () => void; onRefresh: () => void; refreshing: boolean }) {
  const { t, locale } = useI18n();
  const [saved, setSaved] = useState(settings);
  const [enabled, setEnabled] = useState(settings.enabled);
  const initialInterval = autoStopDuration(settings.check_interval_seconds ?? settings.check_interval_days * 86400);
  const initialTimeout = autoStopDuration(settings.idle_timeout_seconds ?? settings.idle_timeout_days * 86400);
  const [interval, setInterval] = useState(initialInterval.value);
  const [intervalUnit, setIntervalUnit] = useState<AutoStopUnit>(initialInterval.unit);
  const [timeout, setTimeoutValue] = useState(initialTimeout.value);
  const [timeoutUnit, setTimeoutUnit] = useState<AutoStopUnit>(initialTimeout.unit);
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState(false);
  const [conflict, setConflict] = useState(false);
  const checkSeconds = parseAutoStopSeconds(interval, intervalUnit);
  const idleSeconds = parseAutoStopSeconds(timeout, timeoutUnit);
  const valid = checkSeconds !== undefined && idleSeconds !== undefined;
  const save = async () => {
    if (pending.current || refreshing || !valid || conflict) return;
    pending.current = true; setBusy(true); setError(undefined); setNotice(false);
    try {
      const result = await runtimePoolService.updateAutoStopSettings({ enabled, check_interval_seconds: checkSeconds, idle_timeout_seconds: idleSeconds, version: saved.version });
      setSaved(result); setNotice(true);
    } catch (err) {
      const stale = axios.isAxiosError(err) && err.response?.status === 409;
      setConflict(stale);
      setError(`${runtimeError(err)} ${t(stale ? "autoStop.conflict" : "autoStop.saveError")}`);
    } finally { pending.current = false; setBusy(false); }
  };
  const changed = () => { setNotice(false); if (!conflict) setError(undefined); };
  return <RuntimeDialog title={t("autoStop.title")} onClose={() => { if (!pending.current) onClose(); }} closeOnBackdrop={false}
    footer={<>
      <button type="button" className={buttonClass} disabled={busy || refreshing} onClick={onRefresh}>{t("common.refresh")}</button>
      <button type="button" className={buttonClass} disabled={busy} onClick={onClose}>{t("runtimePods.close")}</button>
      <button type="submit" form="runtime-auto-stop-settings" className={primaryClass} disabled={busy || refreshing || !valid || conflict}>{t(busy ? "autoStop.saving" : "autoStop.save")}</button>
    </>}>
    <p className="text-xs leading-5 text-slate-500">{t("autoStop.help")}</p>
    {error && <ErrorMessage>{error}</ErrorMessage>}
    {notice && <p role="status" className="rounded bg-emerald-50 p-3 text-xs text-emerald-700">{t("autoStop.saved")}</p>}
    <form id="runtime-auto-stop-settings" className="space-y-4" onSubmit={event => { event.preventDefault(); void save(); }}>
      <label className="flex items-center gap-2 text-sm"><input type="checkbox" role="switch" checked={enabled} disabled={busy || refreshing} onChange={event => { setEnabled(event.target.checked); changed(); }} />{t("autoStop.enabled")}</label>
      <div className="grid gap-3 sm:grid-cols-2">
        {([
          { label: "autoStop.checkInterval", unitLabel: "autoStop.checkIntervalUnit", value: interval, unit: intervalUnit, seconds: checkSeconds, setValue: setInterval, setUnit: setIntervalUnit },
          { label: "autoStop.idleTimeout", unitLabel: "autoStop.idleTimeoutUnit", value: timeout, unit: timeoutUnit, seconds: idleSeconds, setValue: setTimeoutValue, setUnit: setTimeoutUnit },
        ]).map(field => <fieldset key={field.label} className="min-w-0 space-y-1 text-xs">
          <legend>{t(field.label)}</legend>
          <div className="flex gap-2">
            <input className={`${inputClass} min-w-0 flex-1`} aria-label={t(field.label)} type="number" min={1} max={Math.floor(MAX_AUTO_STOP_SECONDS / AUTO_STOP_UNITS[field.unit])} step={1} required value={field.value} disabled={busy || refreshing} aria-invalid={field.seconds === undefined} aria-describedby={field.seconds === undefined ? "auto-stop-duration-error" : undefined} onChange={event => { field.setValue(event.target.value); changed(); }} />
            <select className={inputClass} aria-label={t(field.unitLabel)} value={field.unit} disabled={busy || refreshing} onChange={event => { field.setUnit(event.target.value as AutoStopUnit); changed(); }}>
              {(Object.keys(AUTO_STOP_UNITS) as AutoStopUnit[]).map(unit => <option key={unit} value={unit}>{t(`autoStop.unit_${unit}`)}</option>)}
            </select>
          </div>
        </fieldset>)}
      </div>
      {!valid && <p id="auto-stop-duration-error" role="alert" className="text-xs text-red-700">{t("autoStop.invalidDuration")}</p>}
      <p className="text-xs text-slate-600">{t("autoStop.nextCheck")}: {saved.next_check_at ? new Date(saved.next_check_at).toLocaleString(locale, { timeZone: "Asia/Shanghai" }) : "—"} · Asia/Shanghai{!saved.enabled && ` · ${t("autoStop.paused")}`}</p>
    </form>
  </RuntimeDialog>;
}
