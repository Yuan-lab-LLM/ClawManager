// Matches Go's maximum time.Duration measured in 24-hour days.
export const MAX_AUTO_STOP_DAYS = 106751;

export function parseAutoStopDays(value: string): number | undefined {
  if (!/^\d+$/.test(value)) return undefined;
  const days = Number(value);
  return Number.isSafeInteger(days) && days >= 1 && days <= MAX_AUTO_STOP_DAYS ? days : undefined;
}
