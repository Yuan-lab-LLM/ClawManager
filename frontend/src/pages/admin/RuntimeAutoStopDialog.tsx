import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import axios from "axios";
import { useI18n } from "../../contexts/I18nContext";
import { MAX_AUTO_STOP_DAYS, parseAutoStopDays } from "../../lib/runtimeAutoStop";
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
  const [interval, setInterval] = useState(String(settings.check_interval_days ?? 1));
  const [timeout, setTimeoutDays] = useState(String(settings.idle_timeout_days ?? 1));
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState(false);
  const [conflict, setConflict] = useState(false);
  const checkDays = parseAutoStopDays(interval);
  const idleDays = parseAutoStopDays(timeout);
  const valid = checkDays !== undefined && idleDays !== undefined;
  const save = async () => {
    if (pending.current || refreshing || !valid || conflict) return;
    pending.current = true; setBusy(true); setError(undefined); setNotice(false);
    try {
      const result = await runtimePoolService.updateAutoStopSettings({ enabled, check_interval_days: checkDays, idle_timeout_days: idleDays, version: saved.version });
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
        <label className="space-y-1 text-xs"><span>{t("autoStop.checkInterval")}</span><input className={`${inputClass} w-full`} type="number" min={1} max={MAX_AUTO_STOP_DAYS} step={1} required value={interval} disabled={busy || refreshing} aria-invalid={checkDays === undefined} aria-describedby={!valid ? "auto-stop-days-error" : undefined} onChange={event => { setInterval(event.target.value); changed(); }} /></label>
        <label className="space-y-1 text-xs"><span>{t("autoStop.idleTimeout")}</span><input className={`${inputClass} w-full`} type="number" min={1} max={MAX_AUTO_STOP_DAYS} step={1} required value={timeout} disabled={busy || refreshing} aria-invalid={idleDays === undefined} aria-describedby={!valid ? "auto-stop-days-error" : undefined} onChange={event => { setTimeoutDays(event.target.value); changed(); }} /></label>
      </div>
      {!valid && <p id="auto-stop-days-error" role="alert" className="text-xs text-red-700">{t("autoStop.invalidDays", { max: MAX_AUTO_STOP_DAYS })}</p>}
      <p className="text-xs text-slate-600">{t("autoStop.nextCheck")}: {saved.next_check_at ? new Date(saved.next_check_at).toLocaleString(locale, { timeZone: "Asia/Shanghai" }) : "—"} · Asia/Shanghai{!saved.enabled && ` · ${t("autoStop.paused")}`}</p>
    </form>
  </RuntimeDialog>;
}
