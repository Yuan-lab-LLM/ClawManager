import { useI18n } from "../contexts/I18nContext";
import type { InstanceAvailability } from "../types/instance";

const statusClasses: Record<string, string> = {
  running: "border-emerald-200 bg-emerald-50 text-emerald-700",
  error: "border-red-200 bg-red-50 text-red-700",
};
const pendingStatuses = new Set(["creating", "starting", "stopping", "deleting", "restarting", "resetting", "upgrading", "recovering", "migrating"]);
const knownStatuses = new Set(["running", "stopped", "error", ...pendingStatuses]);

export default function InstanceLifecycleStatus({ status, availability }: {
  status: string;
  availability?: InstanceAvailability;
}) {
  const { t } = useI18n();
  const normalized = status.trim().toLowerCase();
  const color = statusClasses[normalized] ?? (pendingStatuses.has(normalized)
    ? "border-amber-200 bg-amber-50 text-amber-700"
    : "border-slate-200 bg-slate-50 text-slate-600");
  return <span className="inline-flex flex-col items-start gap-1">
    <span className={`inline-flex rounded-md border px-2 py-1 text-xs font-medium ${color}`}>
      {knownStatuses.has(normalized) ? t(`status.${normalized}`) : status}
    </span>
    {availability && <span className="text-xs text-slate-500">
      {t(availability === "available" ? "instances.accessAvailable" : availability === "starting" ? "instances.accessStarting" : "instances.accessUnavailable")}
    </span>}
  </span>;
}
