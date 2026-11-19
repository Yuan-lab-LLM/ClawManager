// API and runtime contracts use tokens; the administrator enters K tokens.
export const TOKENS_PER_K = 1_024;
export const DEFAULT_CONTEXT_WINDOW_TOKENS = 256 * TOKENS_PER_K;
export const MAX_CONTEXT_WINDOW_TOKENS = 2_147_483_647;

export function contextWindowTokensOrDefault(tokens?: number): number {
  return tokens == null || tokens === 0 ? DEFAULT_CONTEXT_WINDOW_TOKENS : tokens;
}

export function contextWindowInputTokens(value: string): number | undefined {
  return value.trim() === '' ? undefined : Math.round(Number(value) * TOKENS_PER_K);
}

export function isValidContextWindow(tokens?: number): boolean {
  const value = contextWindowTokensOrDefault(tokens);
  return Number.isSafeInteger(value) && value > 0 && value <= MAX_CONTEXT_WINDOW_TOKENS;
}
