import type { RuntimeType } from '../types/runtimePool';

export interface RuntimeUpgradeIntent {
  stage: 'prepare' | 'preflight' | 'rollout';
  runtimeType: RuntimeType;
  targetImage: string;
  batchSize: number;
  maxUnavailable: number;
  preparationId?: string;
  protectedPreparation?: boolean;
  preflightId?: string;
  rolloutId?: number;
  submittedAt: string;
}

const intents = new Map<string, RuntimeUpgradeIntent>();
const cancellations = new Map<string, Promise<void>>();
const cancellationIds = new Map<string, Promise<void>>();
const key = (account: number | undefined) => `cm-runtime-upgrade-intent:${account ?? 'anonymous'}`;

export function readRuntimeUpgradeIntent(account: number | undefined): RuntimeUpgradeIntent | null {
  let saved: string | null = null;
  try { saved = sessionStorage.getItem(key(account)); } catch { /* The in-memory guard still blocks remounts. */ }
  if (saved) {
    try {
      const intent = JSON.parse(saved) as RuntimeUpgradeIntent;
      if (['prepare', 'preflight', 'rollout'].includes(intent.stage) && typeof intent.submittedAt === 'string') return intent;
    } catch { /* Keep the in-memory guard if browser storage was interrupted. */ }
  }
  return intents.get(key(account)) ?? null;
}

export function saveRuntimeUpgradeIntent(account: number | undefined, intent: RuntimeUpgradeIntent | null) {
  if (intent) {
    // Persist before dispatch. A storage failure must prevent the mutation.
    sessionStorage.setItem(key(account), JSON.stringify(intent));
    intents.set(key(account), intent);
  } else {
    sessionStorage.removeItem(key(account));
    intents.delete(key(account));
  }
}

export function cancelRuntimeUpgradePreparation(account: number | undefined, id: string, cancel: (id: string) => Promise<void>) {
  const operationKey = `${key(account)}:${id}`;
  const existing = cancellationIds.get(operationKey);
  if (existing) return existing;
  const pending = (cancellations.get(key(account)) ?? Promise.resolve()).then(() => cancel(id)).catch(() => {});
  cancellations.set(key(account), pending);
  cancellationIds.set(operationKey, pending);
  return pending;
}

export function waitForRuntimeUpgradeCancellation(account: number | undefined) {
  return cancellations.get(key(account));
}

export function isDefiniteRuntimeUpgradeRejection(error: unknown) {
  const status = (error as { response?: { status?: number } })?.response?.status;
  return typeof status === 'number' && status >= 400 && status < 500 && status !== 408;
}
