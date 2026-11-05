import { useRef, useState } from "react";
import { ExternalLink, Globe, RefreshCw, ArrowRight } from "lucide-react";
import { useI18n } from "../../contexts/I18nContext";
import {
  instanceWorkbenchService as service,
  workbenchError,
} from "../../services/instanceWorkbenchService";
import type { WorkbenchTab } from "./state";
export function WorkbenchBrowser({
  instanceId,
  generation,
  tab,
  enabled,
  rendered,
  onNavigate,
}: {
  instanceId: number;
  generation: number;
  tab: WorkbenchTab;
  enabled: boolean;
  rendered: boolean;
  onNavigate: (address: string, url: string, generation: number) => void;
}) {
  const { t } = useI18n();
  const [draft, setDraft] = useState({
    value: tab.address ?? "",
    loaded: tab.address ?? "",
  });
  if (draft.loaded !== (tab.address ?? ""))
    setDraft({ value: tab.address ?? "", loaded: tab.address ?? "" });
  const address = draft.value;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [reload, setReload] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const [activated, setActivated] = useState(false);
  if (rendered && !activated) setActivated(true);
  return (
    <div className="wb-browser">
      <form
        className="wb-browser-toolbar"
        onSubmit={async (e) => {
          e.preventDefault();
          if (!address.trim()) return;
          setBusy(true);
          setError("");
          try {
            const result = await service.preview(
              instanceId,
              generation,
              address.trim(),
            );
            onNavigate(address.trim(), result.url, result.generation);
          } catch (err) {
            setError(workbenchError(err));
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
          disabled={!enabled || busy || !address.trim()}
        >
          <ArrowRight size={16} />
        </button>
        {tab.url && (
          <a
            className="wb-icon"
            href={tab.url}
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
          {error}
        </div>
      )}
      {busy && (
        <div role="status" className="wb-loading">
          {t("workbench.loading")}
        </div>
      )}
      {tab.url && activated ? (
        <iframe
          key={reload}
          className="wb-preview-frame"
          src={tab.url}
          title={`${t("workbench.servicePreview")} · ${tab.address}`}
          sandbox="allow-scripts allow-forms allow-same-origin allow-modals allow-downloads allow-popups"
          referrerPolicy="no-referrer"
        />
      ) : (
        <div className="wb-empty">
          <Globe size={32} />
          <h3>
            {tab.stale ? t("workbench.expired") : t("workbench.notStarted")}
          </h3>
          <p>{t("workbench.addressHint")}</p>
        </div>
      )}
    </div>
  );
}
