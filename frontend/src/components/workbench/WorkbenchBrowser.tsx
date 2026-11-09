import { useWorkbenchServices } from "./WorkbenchServices";
import { allowBrowserInstance, supportsCredentiallessPreview, trustedPreviewViewer, type BrowserOriginMetadata } from "../../lib/browserOrigin";
import type { WorkbenchPreview } from "../../types/workbench";
import { useRef, useState } from "react";
import { ExternalLink, Globe, RefreshCw, ArrowRight, ShieldAlert } from "lucide-react";
import { useRuntimeCertificateTrust } from "../../hooks/useRuntimeCertificateTrust";
import { useI18n } from "../../contexts/I18nContext";
import {
  workbenchPreviewError,
} from "../../services/instanceWorkbenchService";
import type { WorkbenchTab } from "./state";
export function WorkbenchBrowser({
  instanceId,
  generation,
  tab,
  enabled,
  rendered,
  onNavigate,
  browserOrigin,
  metadataPending = false,
}: {
  instanceId: number;
  generation: number;
  tab: WorkbenchTab;
  enabled: boolean;
  rendered: boolean;
  browserOrigin?: BrowserOriginMetadata;
  metadataPending?: boolean;
  onNavigate: (address: string, url: string, generation: number, preview: WorkbenchPreview) => void;
}) {
  const { t } = useI18n();
  const { service } = useWorkbenchServices();
  const [draft, setDraft] = useState({
    value: tab.address ?? "",
    loaded: tab.address ?? "",
  });
  if (draft.loaded !== (tab.address ?? ""))
    setDraft({ value: tab.address ?? "", loaded: tab.address ?? "" });
  const address = draft.value;
  const [busy, setBusy] = useState(false);
  const [requestError, setError] = useState("");
  const pending = busy || !!tab.previewPending;
  const error = busy ? "" : requestError || tab.previewError || "";
  const [reload, setReload] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const [activated, setActivated] = useState(false);
  if (rendered && !activated) setActivated(true);
  const isolated = browserOrigin?.browser_origin_mode === "chrome-single-host" || tab.credentialless === true;
  const supported = !isolated || supportsCredentiallessPreview();
  const scopeAllowed = allowBrowserInstance(instanceId, browserOrigin);
  const viewerUrl = isolated ? trustedPreviewViewer(tab.viewer_url, browserOrigin) : tab.url;
  const previewAllowed = !metadataPending && scopeAllowed && supported && (!isolated || tab.credentialless === true);
  const { frameUrl, checkingCertificate, certificateConfirmationRequired, certificateOrigin, confirmCertificate } =
    useRuntimeCertificateTrust(previewAllowed ? tab.url ?? null : null, activated && !isolated);
  return (
    <div className="wb-browser">
      <form
        className="wb-browser-toolbar"
        onSubmit={async (e) => {
          e.preventDefault();
          if (!enabled || !supported || !scopeAllowed || pending || !address.trim()) return;
          setBusy(true);
          setError("");
          try {
            const result = await service.preview(
              instanceId,
              generation,
              address.trim(),
            );
            onNavigate(address.trim(), result.url, result.generation, result);
          } catch (err) {
            setError(workbenchPreviewError(err, t));
            input.current?.focus();
          } finally {
            setBusy(false);
          }
        }}
      >
        <button
          type="button"
          className="wb-icon"
          disabled={!tab.url}
          aria-label={t("workbench.refresh")}
          onClick={() => setReload((n) => n + 1)}
        >
          <RefreshCw size={14} />
        </button>
        <label className="wb-address">
          <Globe size={13} />
          <span className="sr-only">{t("workbench.address")}</span>
          <input
            ref={input}
            autoFocus
            placeholder="http://localhost:3000"
            value={address}
            maxLength={8192}
            onChange={(e) =>
              setDraft((current) => ({ ...current, value: e.target.value }))
            }
            spellCheck={false}
            autoComplete="off"
          />
        </label>
        <button
          className="wb-icon"
          title={t("workbench.open")}
          aria-label={t("workbench.open")}
          disabled={!enabled || !supported || !scopeAllowed || pending || !address.trim()}
        >
          <ArrowRight size={16} />
        </button>
        {tab.url && viewerUrl && previewAllowed && (
          <a
            className="wb-icon"
            href={viewerUrl}
            target="_blank"
            rel="noopener noreferrer"
            title={t("workbench.openExternal")}
            aria-label={t("workbench.openExternal")}
          >
            <ExternalLink size={15} />
          </a>
        )}
      </form>
      {error && (
        <div role="alert" className="wb-error">
          <span>{error}</span>{" "}
          <button
            type="button"
            className="wb-button"
            disabled={!enabled || !supported || !scopeAllowed || pending || !address.trim()}
            onClick={() => input.current?.form?.requestSubmit()}
          >
            {t("workbench.retry")}
          </button>
        </div>
      )}
      {pending && (
        <div role="status" className="wb-loading">
          {t("workbench.loading")}
        </div>
      )}
      {!supported ? (
        <div className="wb-empty" role="alert">
          <ShieldAlert size={32} />
          <p>{t("workbench.credentiallessUnsupported")}</p>
        </div>
      ) : tab.url && activated && previewAllowed ? (frameUrl ? (
        <iframe
          key={reload}
          className="wb-preview-frame"
          src={frameUrl}
          {...(isolated ? { credentialless: "" } : {})}
          title={`${t("workbench.servicePreview")} · ${tab.address}`}
          sandbox={isolated
            ? "allow-scripts allow-forms allow-same-origin allow-modals allow-downloads"
            : "allow-scripts allow-forms allow-same-origin allow-modals allow-downloads allow-popups"}
          referrerPolicy="no-referrer"
        />
      ) : (
        <div className="wb-empty" role="status">
          <ShieldAlert size={32} />
          <h3>{t(certificateConfirmationRequired ? "instances.certificateConfirmationRequired" : "instances.checkingCertificate")}</h3>
          {certificateConfirmationRequired && <>
            <p className="max-w-lg break-words">{t("instances.certificateConfirmationDescription", { origin: certificateOrigin })}</p>
            <button type="button" className="wb-button primary" onClick={confirmCertificate} disabled={checkingCertificate}>
              {t("instances.continueCertificateConfirmation")}
            </button>
          </>}
        </div>
      )) : !pending && !error ? (
        <div className="wb-empty">
          <Globe size={32} />
          <h3>
            {tab.stale ? t("workbench.expired") : t("workbench.notStarted")}
          </h3>
          <p>{t("workbench.addressHint")}</p>
        </div>
      ) : null}
    </div>
  );
}
