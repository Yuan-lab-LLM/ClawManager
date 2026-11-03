import type { Instance, InstanceListFilters, InstanceListResponse } from "../types/instance";

export const unavailableSelectionFilters: InstanceListFilters = { instance_mode: "lite", availability: "unavailable" };

// Materialize an explicit selection, never a live predicate that can silently
// add newly unavailable instances to an already confirmed destructive action.
export async function collectUnavailableLiteIds(
  fetchPage: (page: number, limit: number, filters: InstanceListFilters) => Promise<InstanceListResponse>,
  excluded: ReadonlySet<number>,
  cancelled: () => boolean,
): Promise<number[]> {
  const ids: number[] = [];
  const seen = new Set<number>();
  let total: number | undefined;
  for (let page = 1; ; page++) {
    if (cancelled()) throw new Error("选择已取消");
    const result = await fetchPage(page, 100, unavailableSelectionFilters);
    if (cancelled()) throw new Error("选择已取消");
    if (total !== undefined && total !== result.total) throw new Error("实例列表发生变化，请重新勾选全选。");
    total = result.total;
    for (const instance of result.instances) {
      if (seen.has(instance.id)) throw new Error("实例列表发生变化，请重新勾选全选。");
      seen.add(instance.id);
      if (isUnavailableLiteCandidate(instance) && !excluded.has(instance.id)) ids.push(instance.id);
    }
    if (page * 100 >= total) break;
    if (!result.instances.length) throw new Error("实例列表不完整，请重新勾选全选。");
  }
  if (seen.size !== total) throw new Error("实例列表不完整，请重新勾选全选。");
  return ids;
}

export function isUnavailableLiteCandidate(instance: Instance): boolean {
  return instance.instance_mode === "lite"
    && ["openclaw", "hermes", "opencode", "deepseek-harness"].includes(instance.type)
    && ["error", "stopped"].includes(instance.status)
    && !instance.name.startsWith("cleanup-pending-") && !instance.name.startsWith("reset-");
}

export function pruneVisibleSelection(ids: number[], visible: Instance[]): number[] {
  const byId = new Map(visible.map(instance => [instance.id, instance]));
  return ids.filter(id => {
    const instance = byId.get(id);
    return !instance || ((instance.instance_mode === "lite" || instance.runtime_type === "gateway") && instance.status !== "deleting");
  });
}
