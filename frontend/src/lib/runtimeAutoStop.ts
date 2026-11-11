// Matches Go's maximum time.Duration measured in 24-hour days.
export const MAX_AUTO_STOP_DAYS = 106751;

export function parseAutoStopDays(value: string): number | undefined {
  if (!/^\d+$/.test(value)) return undefined;
  const days = Number(value);
  return Number.isSafeInteger(days) && days >= 1 && days <= MAX_AUTO_STOP_DAYS ? days : undefined;
}

// Floor of Go's maximum time.Duration in seconds; remains an exact JS integer.
export const MAX_AUTO_STOP_SECONDS = 9223372036;
export const AUTO_STOP_UNITS = { second: 1, minute: 60, hour: 3600, day: 86400 } as const;
export type AutoStopUnit = keyof typeof AUTO_STOP_UNITS;

export function parseAutoStopSeconds(value: string, unit: AutoStopUnit): number | undefined {
  if (!/^\d+$/.test(value) || !Object.hasOwn(AUTO_STOP_UNITS, unit)) return undefined;
  const seconds = Number(value) * AUTO_STOP_UNITS[unit];
  return Number.isSafeInteger(seconds) && seconds >= 1 && seconds <= MAX_AUTO_STOP_SECONDS ? seconds : undefined;
}

export function autoStopDuration(seconds: number): { value: string; unit: AutoStopUnit } {
  for (const unit of ['day', 'hour', 'minute', 'second'] as const) {
    if (seconds % AUTO_STOP_UNITS[unit] === 0) return { value: String(seconds / AUTO_STOP_UNITS[unit]), unit };
  }
  return { value: String(seconds), unit: 'second' };
}
