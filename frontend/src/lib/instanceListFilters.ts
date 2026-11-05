import type { InstanceListFilters } from "../types/instance";

export const INSTANCE_STATUS_FILTERS = [
  "running",
  "auto_sleep",
  "stopped",
  "creating",
  "error",
  "deleting",
] as const;
export type InstanceStatusFilter = "all" | (typeof INSTANCE_STATUS_FILTERS)[number];
export const INSTANCE_TYPE_FILTERS = ["openclaw", "hermes", "opencode", "deepseek-harness"] as const;

export function instanceStatusFilterQuery(
  status: InstanceStatusFilter,
): Pick<InstanceListFilters, "status" | "auto_sleep"> {
  if (status === "all") return {};
  if (status === "auto_sleep") return { status: "stopped", auto_sleep: true };
  if (status === "stopped") return { status: "stopped", auto_sleep: false };
  return { status };
}
