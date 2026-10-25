import { useCallback, useEffect, useMemo, useState } from "react";

const CERTIFICATE_CHECK_PATH = "/__clawmanager_cert_check";
const CERTIFICATE_TRUST_PATH = "/__clawmanager_cert_trust";
const PENDING_TRUST_STORAGE_KEY = "clawmanager.runtime-certificate-trust.pending.v1";
const PENDING_TRUST_TTL_MS = 10 * 60 * 1000;

interface CertificateTrustTarget {
  origin: string;
  checkUrl: string;
  trustUrl: string;
}

interface PendingCertificateTrust {
  origin: string;
  startedAt: number;
}

const trustedRuntimeOrigins = new Set<string>();

function resolveCertificateTrustTarget(
  frameUrl: string | null,
  enabled: boolean,
): CertificateTrustTarget | null {
  if (!enabled || !frameUrl) {
    return null;
  }

  try {
    const parsed = new URL(frameUrl, window.location.href);
    if (parsed.protocol !== "https:" || parsed.origin === window.location.origin) {
      return null;
    }

    return {
      origin: parsed.origin,
      checkUrl: new URL(CERTIFICATE_CHECK_PATH, parsed.origin).toString(),
      trustUrl: new URL(CERTIFICATE_TRUST_PATH, parsed.origin).toString(),
    };
  } catch {
    return null;
  }
}

function readPendingCertificateTrust(): PendingCertificateTrust | null {
  try {
    const raw = window.sessionStorage.getItem(PENDING_TRUST_STORAGE_KEY);
    if (!raw) {
      return null;
    }

    const pending = JSON.parse(raw) as Partial<PendingCertificateTrust>;
    if (
      typeof pending.origin !== "string" ||
      typeof pending.startedAt !== "number" ||
      Date.now() - pending.startedAt > PENDING_TRUST_TTL_MS
    ) {
      window.sessionStorage.removeItem(PENDING_TRUST_STORAGE_KEY);
      return null;
    }

    return pending as PendingCertificateTrust;
  } catch {
    return null;
  }
}

function rememberPendingCertificateTrust(origin: string) {
  try {
    window.sessionStorage.setItem(
      PENDING_TRUST_STORAGE_KEY,
      JSON.stringify({ origin, startedAt: Date.now() } satisfies PendingCertificateTrust),
    );
  } catch {
    // A disabled sessionStorage must not prevent the browser confirmation flow.
  }
}

function clearPendingCertificateTrust(origin: string) {
  try {
    if (readPendingCertificateTrust()?.origin === origin) {
      window.sessionStorage.removeItem(PENDING_TRUST_STORAGE_KEY);
    }
  } catch {
    // Ignore storage failures after a successful certificate probe.
  }
}

export function useRuntimeCertificateTrust(
  frameUrl: string | null,
  enabled: boolean,
) {
  const target = useMemo(
    () => resolveCertificateTrustTarget(frameUrl, enabled),
    [enabled, frameUrl],
  );
  const [approvedOrigin, setApprovedOrigin] = useState<string | null>(null);
  const [requiredOrigin, setRequiredOrigin] = useState<string | null>(null);
  const [probeRevision, setProbeRevision] = useState(0);

  useEffect(() => {
    if (!target) {
      return;
    }

    if (trustedRuntimeOrigins.has(target.origin)) {
      clearPendingCertificateTrust(target.origin);
      return;
    }

    let cancelled = false;

    void fetch(target.checkUrl, {
      method: "GET",
      mode: "no-cors",
      credentials: "omit",
      cache: "no-store",
      referrerPolicy: "no-referrer",
    })
      .then(() => {
        if (cancelled) {
          return;
        }
        trustedRuntimeOrigins.add(target.origin);
        clearPendingCertificateTrust(target.origin);
        setApprovedOrigin(target.origin);
        setRequiredOrigin(null);
      })
      .catch(() => {
        if (cancelled) {
          return;
        }
        setRequiredOrigin(target.origin);
        const pending = readPendingCertificateTrust();
        if (pending?.origin === target.origin) {
          return;
        }

        rememberPendingCertificateTrust(target.origin);
        window.location.assign(target.trustUrl);
      });

    return () => {
      cancelled = true;
    };
  }, [probeRevision, target]);

  useEffect(() => {
    if (!target) {
      return;
    }

    const handlePageShow = () => {
      if (readPendingCertificateTrust()?.origin === target.origin) {
        setProbeRevision((revision) => revision + 1);
      }
    };

    window.addEventListener("pageshow", handlePageShow);
    return () => window.removeEventListener("pageshow", handlePageShow);
  }, [target]);

  const confirmCertificate = useCallback(() => {
    if (!target) {
      return;
    }

    rememberPendingCertificateTrust(target.origin);
    window.location.assign(target.trustUrl);
  }, [target]);

  const isReady =
    !target ||
    approvedOrigin === target.origin ||
    trustedRuntimeOrigins.has(target.origin);

  return {
    frameUrl: isReady ? frameUrl : null,
    checkingCertificate:
      Boolean(target) && !isReady && requiredOrigin !== target?.origin,
    certificateConfirmationRequired:
      Boolean(target) && !isReady && requiredOrigin === target?.origin,
    confirmCertificate,
  };
}
