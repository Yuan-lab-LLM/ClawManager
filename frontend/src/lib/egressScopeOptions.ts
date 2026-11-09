export interface ScopeOptionsPage<T> {
  items: T[];
  total: number;
}

// Respect the server's total even when it caps the requested page size.
// Reject incomplete/repeated pages instead of silently hiding scope choices.
export async function loadAllScopeOptions<T extends { id: number }>(
  fetchPage: (page: number, limit: number) => Promise<ScopeOptionsPage<T>>,
): Promise<T[]> {
  const items = new Map<number, T>();
  for (let page = 1; ; page += 1) {
    const response = await fetchPage(page, 200);
    if (!Number.isFinite(response.total) || response.total < 0) {
      throw new Error('Invalid scope options response');
    }
    const previousSize = items.size;
    response.items.forEach((item) => items.set(item.id, item));
    if (items.size >= response.total) return [...items.values()];
    if (items.size === previousSize) throw new Error('Incomplete scope options response');
  }
}
