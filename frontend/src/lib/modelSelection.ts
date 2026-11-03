import type { LLMModel } from '../services/modelService';

// Resolve against the current rows: stale IDs must never become operation targets.
export function selectedRows<T>(rows: T[], selected: ReadonlySet<string>, id: (row: T) => string): T[] {
  return rows.filter((row) => selected.has(id(row)));
}

export function toggleSelection(current: ReadonlySet<string>, ids: string[], checked: boolean): Set<string> {
  const next = new Set(current);
  for (const id of ids) {
    if (checked) next.add(id);
    else next.delete(id);
  }
  return next;
}

interface BatchResult {
  saved: LLMModel[];
  failed: Array<{ display_name: string; provider_model_name: string; error: string }>;
}

// The API returns successful records and per-item errors separately. Never mark
// an item persisted without its server ID, or touch an unsubmitted local row.
export function mergeBatchSave<T extends LLMModel & { local_id: string }>(
  rows: T[], submitted: ReadonlySet<string>, result: BatchResult, fallbackError: string,
): T[] {
  const key = (row: { display_name: string; provider_model_name: string }) => JSON.stringify([row.display_name.trim(), row.provider_model_name.trim()]);
  const saved = new Map(result.saved.map((row) => [key(row), row]));
  const failed = new Map(result.failed.map((row) => [key(row), row.error]));
  return rows.map((row) => {
    if (!submitted.has(row.local_id)) return row;
    const record = saved.get(key(row));
    if (failed.has(key(row)) || !record?.id) return { ...row, saving: false, error: failed.get(key(row)) || fallbackError };
    return {
      ...row, ...record, local_id: row.local_id,
      description: record.description ?? '', api_key: record.api_key ?? '', api_key_secret_ref: record.api_key_secret_ref ?? '',
      isNew: false, isEditing: false, saving: false, error: null, edit_snapshot: undefined,
    };
  });
}
