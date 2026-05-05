import { useCallback, useEffect, useRef, useState } from "react";
import { instanceService } from "../services/instanceService";
import type { InstanceAccessMode } from "../services/instanceService";

interface RefreshAccessOptions {
  forceReload?: boolean;
  silent?: boolean;
}

interface UseInstanceDesktopAccessOptions {
  instanceId: number | null;
  isRunning: boolean;
  accessMode?: InstanceAccessMode;
  retainSessionOnStop?: boolean;
  resolveEmbedUrl: (url: string | null) => string | null;
  failedMessage: string;
  refreshLeadMs?: number;
  retryDelayMs?: number;
}

const DEFAULT_REFRESH_LEAD_MS = 5 * 60 * 1000;
const DEFAULT_RETRY_DELAY_MS = 5000;
const FRAME_ERROR_PATTERN =
  /(access token expired or invalid|access token required|token does not match instance|failed to proxy request)/i;

type DesktopSessionSnapshot = {
  embedUrl: string | null;
  expiresAt: number | null;
  hasEstablishedSession: boolean;
};

const desktopSessionStore = new Map<string, DesktopSessionSnapshot>();
const accessSessionStoreKey = (
  instanceId: number,
  accessMode: InstanceAccessMode,
) => `${accessMode}:${instanceId}`;

function accessErrorMessage(error: unknown, fallback: string): string {
  if (typeof error === "object" && error !== null && "response" in error) {
    const response = (error as { response?: { data?: { error?: unknown } } })
      .response;
    if (typeof response?.data?.error === "string") {
      return response.data.error;
    }
  }

  return fallback;
}

export function useInstanceDesktopAccess({
  instanceId,
  isRunning,
  accessMode = "desktop",
  retainSessionOnStop = false,
  resolveEmbedUrl,
  failedMessage,
  refreshLeadMs = DEFAULT_REFRESH_LEAD_MS,
  retryDelayMs = DEFAULT_RETRY_DELAY_MS,
}: UseInstanceDesktopAccessOptions) {
  const initialCachedSession = instanceId
    ? desktopSessionStore.get(accessSessionStoreKey(instanceId, accessMode))
    : null;

  const [embedUrl, setEmbedUrl] = useState<string | null>(
    initialCachedSession?.embedUrl ?? null,
  );
  const [expiresAt, setExpiresAt] = useState<Date | null>(
    initialCachedSession?.expiresAt
      ? new Date(initialCachedSession.expiresAt)
      : null,
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [reconnecting, setReconnecting] = useState(false);

  const requestIdRef = useRef(0);
  const embedUrlRef = useRef<string | null>(initialCachedSession?.embedUrl ?? null);
  const expiresAtRef = useRef<Date | null>(
    initialCachedSession?.expiresAt
      ? new Date(initialCachedSession.expiresAt)
      : null,
  );
  const retryTimeoutRef = useRef<number | null>(null);
  const refreshTimeoutRef = useRef<number | null>(null);
  const refreshAccessRef = useRef<
    ((options?: RefreshAccessOptions) => Promise<void>) | null
  >(null);
  const hasEstablishedSessionRef = useRef(
    initialCachedSession?.hasEstablishedSession ?? false,
  );

  const syncSessionStore = useCallback(
    (patch: Partial<DesktopSessionSnapshot>) => {
      if (!instanceId) {
        return;
      }

      const storeKey = accessSessionStoreKey(instanceId, accessMode);
      const current = desktopSessionStore.get(storeKey) ?? {
        embedUrl: embedUrlRef.current,
        expiresAt: expiresAtRef.current?.getTime() ?? null,
        hasEstablishedSession: hasEstablishedSessionRef.current,
      };

      desktopSessionStore.set(storeKey, {
        embedUrl:
          patch.embedUrl !== undefined ? patch.embedUrl : current.embedUrl,
        expiresAt:
          patch.expiresAt !== undefined ? patch.expiresAt : current.expiresAt,
        hasEstablishedSession:
          patch.hasEstablishedSession !== undefined
            ? patch.hasEstablishedSession
            : current.hasEstablishedSession,
      });
    },
    [accessMode, instanceId],
  );

  useEffect(() => {
    embedUrlRef.current = embedUrl;
    syncSessionStore({ embedUrl });
  }, [embedUrl, syncSessionStore]);

  useEffect(() => {
    expiresAtRef.current = expiresAt;
    syncSessionStore({ expiresAt: expiresAt?.getTime() ?? null });
  }, [expiresAt, syncSessionStore]);

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
    hasEstablishedSessionRef.current = false;
    if (instanceId) {
      desktopSessionStore.delete(accessSessionStoreKey(instanceId, accessMode));
    }
    setEmbedUrl(null);
    setExpiresAt(null);
    setError(null);
    setLoading(false);
    setReconnecting(false);
  }, [accessMode, clearRefreshTimeout, clearRetryTimeout, instanceId]);

  const shouldPreserveSession = useCallback(() => {
    return (
      retainSessionOnStop &&
      (hasEstablishedSessionRef.current || Boolean(embedUrlRef.current))
    );
  }, [retainSessionOnStop]);

  const scheduleRetry = useCallback(() => {
    clearRetryTimeout();
    if (shouldPreserveSession()) {
      return;
    }
    if (!instanceId || !isRunning || document.hidden) {
      return;
    }

    retryTimeoutRef.current = window.setTimeout(() => {
      retryTimeoutRef.current = null;
      void refreshAccessRef.current?.({
        forceReload: !embedUrlRef.current,
        silent: true,
      });
    }, retryDelayMs);
  }, [clearRetryTimeout, instanceId, isRunning, retryDelayMs, shouldPreserveSession]);

  const refreshAccess = useCallback(
    async ({ forceReload = false, silent = false }: RefreshAccessOptions = {}) => {
      if (!instanceId || !isRunning) {
        if (shouldPreserveSession()) {
          return;
        }
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
        const data = await instanceService.generateAccessToken(
          instanceId,
          accessMode,
        );
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
        } else {
          setEmbedUrl(previousEmbedUrl);
        }

        syncSessionStore({
          embedUrl: !previousEmbedUrl || forceReload ? nextEmbedUrl : previousEmbedUrl,
          expiresAt: nextExpiresAt.getTime(),
        });
      } catch (error: unknown) {
        if (requestId !== requestIdRef.current) {
          return;
        }

        setError(accessErrorMessage(error, failedMessage));
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
    },
    [
      clearAccessState,
      clearRetryTimeout,
      accessMode,
      failedMessage,
      instanceId,
      isRunning,
      resolveEmbedUrl,
      scheduleRetry,
      shouldPreserveSession,
      syncSessionStore,
    ],
  );

  useEffect(() => {
    refreshAccessRef.current = refreshAccess;
  }, [refreshAccess]);

  useEffect(() => {
    if (!instanceId) {
      clearAccessState();
      return;
    }

    if (!isRunning) {
      if (shouldPreserveSession() || embedUrlRef.current) {
        clearRetryTimeout();
        clearRefreshTimeout();
        return;
      }

      clearAccessState();
      return;
    }

    if (!embedUrlRef.current) {
      void refreshAccess({ forceReload: true });
    }

    return () => {
      clearRetryTimeout();
      clearRefreshTimeout();
    };
  }, [
    clearAccessState,
    clearRefreshTimeout,
    clearRetryTimeout,
    instanceId,
    isRunning,
    refreshAccess,
    shouldPreserveSession,
  ]);

  useEffect(() => {
    if (!instanceId || !isRunning || !expiresAt) {
      clearRefreshTimeout();
      return;
    }

    if (shouldPreserveSession()) {
      clearRefreshTimeout();
      return;
    }

    const remainingMs = expiresAt.getTime() - Date.now();
    const delay =
      remainingMs <= refreshLeadMs
        ? Math.max(remainingMs - 30 * 1000, 0)
        : remainingMs - refreshLeadMs;

    refreshTimeoutRef.current = window.setTimeout(() => {
      refreshTimeoutRef.current = null;
      void refreshAccessRef.current?.({ silent: true });
    }, delay);

    return clearRefreshTimeout;
  }, [
    clearRefreshTimeout,
    expiresAt,
    instanceId,
    isRunning,
    refreshLeadMs,
    shouldPreserveSession,
  ]);

  useEffect(() => {
    if (shouldPreserveSession()) {
      return;
    }

    const maybeReconnect = () => {
      if (!instanceId || !isRunning) {
        return;
      }

      const currentExpiry = expiresAtRef.current?.getTime() ?? 0;
      const hasActiveFrame = Boolean(embedUrlRef.current);
      const isNearExpiry =
        currentExpiry === 0 || currentExpiry - Date.now() <= refreshLeadMs;

      if (!hasActiveFrame) {
        void refreshAccessRef.current?.({ forceReload: true, silent: true });
        return;
      }

      if (isNearExpiry) {
        void refreshAccessRef.current?.({ silent: true });
      }
    };

    const handleVisibilityChange = () => {
      if (!document.hidden) {
        maybeReconnect();
      }
    };

    const handleFocus = () => {
      maybeReconnect();
    };

    document.addEventListener("visibilitychange", handleVisibilityChange);
    window.addEventListener("focus", handleFocus);

    return () => {
      document.removeEventListener("visibilitychange", handleVisibilityChange);
      window.removeEventListener("focus", handleFocus);
    };
  }, [instanceId, isRunning, refreshLeadMs, shouldPreserveSession]);

  const handleFrameLoad = useCallback(
    (frame: HTMLIFrameElement | null) => {
      if (!frame) {
        return;
      }

      window.setTimeout(() => {
        try {
          const frameText = frame.contentDocument?.body?.textContent?.trim() ?? "";
          if (frameText && FRAME_ERROR_PATTERN.test(frameText)) {
            if (!hasEstablishedSessionRef.current) {
              void refreshAccessRef.current?.({ forceReload: true, silent: true });
            }
            return;
          }

          hasEstablishedSessionRef.current = true;
          syncSessionStore({ hasEstablishedSession: true });
        } catch (frameError) {
          console.error("Failed to inspect desktop frame state", frameError);
        }
      }, 0);
    },
    [syncSessionStore],
  );

  const handleFrameError = useCallback(() => {
    if (!hasEstablishedSessionRef.current) {
      void refreshAccessRef.current?.({ forceReload: true, silent: true });
    }
  }, []);

  return {
    embedUrl,
    expiresAt,
    loading,
    error,
    reconnecting,
    refreshAccess,
    handleFrameLoad,
    handleFrameError,
  };
}
