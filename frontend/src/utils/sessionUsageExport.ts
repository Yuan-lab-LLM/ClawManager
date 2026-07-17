import type { InstanceSessionUsageItem } from "../types/instance";

export type SessionUsageTimeRange = "all" | "24h" | "7d" | "30d";

export function resolveSessionUsageSince(range: SessionUsageTimeRange): string | undefined {
  if (range === "all") {
    return undefined;
  }
  const now = Date.now();
  const offsets: Record<Exclude<SessionUsageTimeRange, "all">, number> = {
    "24h": 24 * 60 * 60 * 1000,
    "7d": 7 * 24 * 60 * 60 * 1000,
    "30d": 30 * 24 * 60 * 60 * 1000,
  };
  return new Date(now - offsets[range]).toISOString();
}

function escapeCsvValue(value: string | number | undefined | null): string {
  const normalized = value == null ? "" : String(value);
  if (/[",\n]/.test(normalized)) {
    return `"${normalized.replace(/"/g, '""')}"`;
  }
  return normalized;
}

export function buildSessionUsageCsv(items: InstanceSessionUsageItem[], currency: string): string {
  const header = [
    "session_id",
    "session_key",
    "title",
    "prompt_tokens",
    "completion_tokens",
    "total_tokens",
    "estimated_cost",
    "currency",
    "invocation_count",
    "first_seen_at",
    "last_seen_at",
  ];
  const rows = items.map((item) => [
    item.session_id,
    item.session_key,
    item.title ?? "",
    item.prompt_tokens,
    item.completion_tokens,
    item.total_tokens,
    item.estimated_cost,
    item.currency || currency,
    item.invocation_count,
    item.first_seen_at,
    item.last_seen_at,
  ]);
  return [header, ...rows]
    .map((row) => row.map((cell) => escapeCsvValue(cell)).join(","))
    .join("\n");
}

export function downloadSessionUsageCsv(content: string, filename: string) {
  const blob = new Blob(["\uFEFF", content], { type: "text/csv;charset=utf-8;" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.click();
  URL.revokeObjectURL(url);
}
