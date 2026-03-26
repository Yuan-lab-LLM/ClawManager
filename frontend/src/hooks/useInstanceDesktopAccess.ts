import { useCallback, useEffect, useRef, useState } from 'react';
import { instanceService } from '../services/instanceService';

interface RefreshAccessOptions {
  forceReload?: boolean;
  silent?: boolean;
}

interface UseInstanceDesktopAccessOptions {
  instanceId: number | null;
  isRunning: boolean;
  resolveEmbedUrl: (url: string | null) => string | null;
  failedMessage: string;
  refreshLeadMs?: number;
  retryDelayMs?: number;
}

const DEFAULT_REFRESH_LEAD_MS = 5 * 60 * 1000;
const DEFAULT_RETRY_DELAY_MS = 5000;
const FRAME_ERROR_PATTERN = /(access token expired or invalid|access token required|token does not match instance|failed to proxy request)/i;

export function useInstanceDesktopAccess({
  instanceId,
  isRunning,
  resolveEmbedUrl,
  failedMessage,
  refreshLeadMs = DEFAULT_REFRESH_LEAD_MS,
  retryDelayMs = DEFAULT_RETRY_DELAY_MS,
}: UseInstanceDesktopAccessOptions) {
  const [embedUrl, setEmbedUrl] = useState<string | null>(null);
  const [expiresAt, setExpiresAt] = useState<Date | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [frameKey, setFrameKey] = useState(0);
  const [reconnecting, setReconnecting] = useState(false);
  const requestIdRef = useRef(0);
  const embedUrlRef = useRef<string | null>(null);
  const expiresAtRef = useRef<Date | null>(null);
  const retryTimeoutRef = useRef<number | null>(null);
  const refreshTimeoutRef = useRef<number | null>(null);
  const refreshAccessRef = useRef<((options?: RefreshAccessOptions) => Promise<void>) | null>(null);

  useEffect(() => {
    embedUrlRef.current = embedUrl;
  }, [embedUrl]);

  useEffect(() => {
    expiresAtRef.current = expiresAt;
  }, [expiresAt]);

  const clearRetryTimeout = useCallback(() => {
    if (retryTimeoutRef.current !== null) {
      window.clearTimeout(retryTimeoutRef.current);
      retryTimeoutRef.current = null;
    }
  }, []);

  const clearRefreshTimeout = useCallback(() => {
    if (refreshTimeoutRef.current !== null) {
      window.clearTimeout(refreshTimeoutRef.current);
      refreshTimeoutRef.current = null;
    }
  }, []);

  const clearAccessState = useCallback(() => {
    clearRetryTimeout();
    clearRefreshTimeout();
    requestIdRef.current += 1;
    embedUrlRef.current = null;
    expiresAtRef.current = null;
    setEmbedUrl(null);
    setExpiresAt(null);
    setError(null);
    setLoading(false);
    setReconnecting(false);
    setFrameKey(0);
  }, [clearRefreshTimeout, clearRetryTimeout]);

  const scheduleRetry = useCallback(() => {
    clearRetryTimeout();
    if (!instanceId || !isRunning || document.hidden) {
      return;
    }

    retryTimeoutRef.current = window.setTimeout(() => {
      retryTimeoutRef.current = null;
      void refreshAccessRef.current?.({ forceReload: !embedUrlRef.current, silent: true });
    }, retryDelayMs);
  }, [clearRetryTimeout, instanceId, isRunning, retryDelayMs]);

  const refreshAccess = useCallback(async ({ forceReload = false, silent = false }: RefreshAccessOptions = {}) => {
    if (!instanceId || !isRunning) {
      clearAccessState();
      return;
    }

    const requestId = requestIdRef.current + 1;
    requestIdRef.current = requestId;
    clearRetryTimeout();
    if (silent) {
      setReconnecting(true);
    } else {
      setLoading(true);
    }

    try {
      const data = await instanceService.generateAccessToken(instanceId);
      if (requestId !== requestIdRef.current) {
        return;
      }

      const nextEmbedUrl = resolveEmbedUrl(data.proxy_url || data.access_url);
      const nextExpiresAt = new Date(data.expires_at);
      const previousEmbedUrl = embedUrlRef.current;

      expiresAtRef.current = nextExpiresAt;
      setExpiresAt(nextExpiresAt);
      setError(null);

      if (!previousEmbedUrl || forceReload) {
        embedUrlRef.current = nextEmbedUrl;
        setEmbedUrl(nextEmbedUrl);
        setFrameKey((current) => current + 1);
      } else {
        setEmbedUrl(previousEmbedUrl);
      }
    } catch (err: any) {
      if (requestId !== requestIdRef.current) {
        return;
      }

      setError(err.response?.data?.error || failedMessage);
      if (!embedUrlRef.current) {
        setEmbedUrl(null);
        setExpiresAt(null);
      }
      scheduleRetry();
    } finally {
      if (requestId === requestIdRef.current) {
        setLoading(false);
        setReconnecting(false);
      }
    }
  }, [clearAccessState, clearRetryTimeout, failedMessage, instanceId, isRunning, resolveEmbedUrl, scheduleRetry]);

  useEffect(() => {
    refreshAccessRef.current = refreshAccess;
  }, [refreshAccess]);

  useEffect(() => {
    if (!instanceId || !isRunning) {
      clearAccessState();
      return;
    }

    void refreshAccess({ forceReload: true });

    return () => {
      clearRetryTimeout();
      clearRefreshTimeout();
    };
  }, [clearAccessState, clearRefreshTimeout, clearRetryTimeout, instanceId, isRunning, refreshAccess]);

  useEffect(() => {
    if (!instanceId || !isRunning || !expiresAt) {
      clearRefreshTimeout();
      return;
    }

    const remainingMs = expiresAt.getTime() - Date.now();
    const delay = remainingMs <= refreshLeadMs
      ? Math.max(remainingMs - 30 * 1000, 0)
      : remainingMs - refreshLeadMs;

    refreshTimeoutRef.current = window.setTimeout(() => {
      refreshTimeoutRef.current = null;
      void refreshAccessRef.current?.({ silent: true });
    }, delay);

    return clearRefreshTimeout;
  }, [clearRefreshTimeout, expiresAt, instanceId, isRunning, refreshLeadMs]);

  useEffect(() => {
    const maybeReconnect = () => {
      if (!instanceId || !isRunning) {
        return;
      }

      const currentExpiry = expiresAtRef.current?.getTime() ?? 0;
      const hasActiveFrame = Boolean(embedUrlRef.current);
      const isNearExpiry = currentExpiry === 0 || currentExpiry - Date.now() <= refreshLeadMs;

      if (!hasActiveFrame) {
        void refreshAccessRef.current?.({ forceReload: true, silent: true });
        return;
      }

      if (isNearExpiry) {
        void refreshAccessRef.current?.({ silent: true });
      }
    };

    const handleVisibilityChange = () => {
      if (document.hidden) {
        return;
      }

      maybeReconnect();
    };

    const handleFocus = () => {
      maybeReconnect();
    };

    document.addEventListener('visibilitychange', handleVisibilityChange);
    window.addEventListener('focus', handleFocus);

    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange);
      window.removeEventListener('focus', handleFocus);
    };
  }, [instanceId, isRunning, refreshLeadMs]);

  const handleFrameLoad = useCallback((frame: HTMLIFrameElement | null) => {
    if (!frame) {
      return;
    }

    window.setTimeout(() => {
      try {
        const frameText = frame.contentDocument?.body?.textContent?.trim() ?? '';
        if (frameText && FRAME_ERROR_PATTERN.test(frameText)) {
          void refreshAccessRef.current?.({ forceReload: true, silent: true });
        }
      } catch (frameError) {
        console.error('Failed to inspect desktop frame state', frameError);
      }
    }, 0);
  }, []);

  return {
    embedUrl,
    expiresAt,
    loading,
    error,
    frameKey,
    reconnecting,
    refreshAccess,
    handleFrameLoad,
  };
}
