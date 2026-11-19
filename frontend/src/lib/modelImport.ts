import type { LLMModel } from '../services/modelService';

type Connection = Pick<LLMModel, 'provider_type' | 'protocol_type' | 'base_url' | 'api_key' | 'api_key_secret_ref'>;

function normalizedURL(value: string): string {
  try { return new URL(value.trim()).toString().replace(/\/+$/, ''); }
  catch { return value.trim().replace(/\/+$/, ''); }
}

// Credentials are compared only in memory, never used as visible row IDs.
function sameConnection(left: Connection, right: Connection): boolean {
  return left.provider_type === right.provider_type && left.protocol_type === right.protocol_type &&
    normalizedURL(left.base_url) === normalizedURL(right.base_url) &&
    (left.api_key?.trim() ?? '') === (right.api_key?.trim() ?? '') &&
    (left.api_key_secret_ref?.trim() ?? '') === (right.api_key_secret_ref?.trim() ?? '');
}

export function importableModels<T extends { id: string }>(source: Connection, discovered: T[], existing: (Connection & { provider_model_name: string })[]): T[] {
  const seen = new Set(existing.filter((row) => sameConnection(row, source)).map((row) => row.provider_model_name.trim()));
  return discovered.filter((row) => {
    const id = row.id.trim();
    if (seen.has(id)) return false;
    seen.add(id);
    return true;
  });
}
