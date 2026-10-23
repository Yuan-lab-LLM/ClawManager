import type { LLMModel } from '../services/modelService';

export interface CatalogModel extends LLMModel {
  local_id: string;
  isNew?: boolean;
  saving?: boolean;
  error?: string | null;
}

export function catalogSource(baseURL: string): string {
  const value = baseURL.trim();
  try {
    const url = new URL(value);
    url.pathname = url.pathname.replace(/\/+$/, '') || '/';
    return url.toString().replace(/\/$/, '');
  } catch { return value.replace(/\/+$/, ''); }
}

export function groupCatalog<T extends CatalogModel>(models: T[], grouping: 'url' | 'tier', search: string, tier: string, status: string) {
  const query = search.trim().toLowerCase();
  const groups = new Map<string, T[]>();
  for (const model of models) {
    if (tier !== 'all' && (model.quality_tier ?? 0) !== Number(tier)) continue;
    if (status === 'pending' && !model.isNew) continue;
    if (status === 'active' && !model.is_active) continue;
    if (status === 'inactive' && model.is_active) continue;
    if (query && ![model.display_name, model.provider_model_name, model.base_url, model.model_group].some((value) => value?.toLowerCase().includes(query))) continue;
    const key = grouping === 'url' ? catalogSource(model.base_url) : String(model.quality_tier ?? 0);
    const items = groups.get(key) ?? [];
    items.push(model);
    groups.set(key, items);
  }
  return [...groups].sort(([a], [b]) => a.localeCompare(b, undefined, { numeric: true })).map(([key, items]) => ({ key, items }));
}
