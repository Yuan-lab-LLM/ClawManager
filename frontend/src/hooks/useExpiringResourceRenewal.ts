import { useEffect, useRef } from "react";

interface UseExpiringResourceRenewalOptions {
  expiresAt?: string | null;
  renew: () => Promise<void>;
  leadTimeMs?: number;
  retryDelayMs?: number;
  fallbackDelayMs?: number;
}

const DEFAULT_LEAD_TIME_MS = 5 * 60_000;
const DEFAULT_RETRY_DELAY_MS = 30_000;
const DEFAULT_FALLBACK_DELAY_MS = 30 * 60_000;

/**
 * Renews an expiring browser resource without tying the timer lifecycle to the
 * callback identity. Failed renewals are retried at a fixed interval, and a
 * suspended background tab gets another chance as soon as it becomes active.
 */
export function useExpiringResourceRenewal({
  expiresAt,
  renew,
  leadTimeMs = DEFAULT_LEAD_TIME_MS,
  retryDelayMs = DEFAULT_RETRY_DELAY_MS,
  fallbackDelayMs = DEFAULT_FALLBACK_DELAY_MS,
}: UseExpiringResourceRenewalOptions) {
  const renewRef = useRef(renew);
  const renewalInFlightRef = useRef(false);

  useEffect(() => {
    renewRef.current = renew;
  }, [renew]);

  useEffect(() => {
    if (!expiresAt) return;

    let cancelled = false;
    let timer: number | undefined;
    const expiresAtMs = Date.parse(expiresAt);

    const clearTimer = () => {
      if (timer !== undefined) {
        window.clearTimeout(timer);
        timer = undefined;
      }
    };

    async function runRenewal() {
      if (cancelled || renewalInFlightRef.current) return;
      renewalInFlightRef.current = true;
      try {
        await renewRef.current();
      } catch {
        if (!cancelled) schedule(retryDelayMs);
      } finally {
        renewalInFlightRef.current = false;
      }
    }

    function schedule(delayMs: number) {
      clearTimer();
      timer = window.setTimeout(() => void runRenewal(), Math.max(0, delayMs));
    }

    const initialDelay = Number.isFinite(expiresAtMs)
      ? Math.max(retryDelayMs, expiresAtMs - Date.now() - leadTimeMs)
      : fallbackDelayMs;
    schedule(initialDelay);

    const renewWhenActive = () => {
      if (document.hidden) return;
      if (!Number.isFinite(expiresAtMs) || expiresAtMs - Date.now() <= leadTimeMs) {
        clearTimer();
        void runRenewal();
      }
    };
    const handleVisibilityChange = () => {
      if (!document.hidden) renewWhenActive();
    };

    document.addEventListener("visibilitychange", handleVisibilityChange);
    window.addEventListener("focus", renewWhenActive);

    return () => {
      cancelled = true;
      clearTimer();
      document.removeEventListener("visibilitychange", handleVisibilityChange);
      window.removeEventListener("focus", renewWhenActive);
    };
  }, [expiresAt, fallbackDelayMs, leadTimeMs, retryDelayMs]);
}
