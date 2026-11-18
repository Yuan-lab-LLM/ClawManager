// The lifecycle stays stopped; the saved reason only changes its display label.
export function getInstanceDisplayStatus(status: string, stoppedReason?: string): string {
  const normalized = status.trim().toLowerCase();
  return normalized === "stopped" && stoppedReason === "idle_timeout" ? "auto_sleep" : normalized;
}
