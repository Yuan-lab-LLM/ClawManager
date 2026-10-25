import { Maximize2, Minimize2, PanelRightClose, PanelRightOpen, RefreshCw } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useI18n } from "../contexts/I18nContext";

interface InstanceServiceFrameShellProps {
  instanceName: string;
  children: ReactNode;
  onRefresh?: () => void;
  refreshing?: boolean;
  workspaceVisible?: boolean;
  onWorkspaceVisibilityChange?: (visible: boolean) => void;
}

export function InstanceServiceFrameShell({
  instanceName,
  children,
  onRefresh,
  refreshing = false,
  workspaceVisible,
  onWorkspaceVisibilityChange,
}: InstanceServiceFrameShellProps) {
  const { t } = useI18n();
  const containerRef = useRef<HTMLElement | null>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);

  useEffect(() => {
    const handleChange = () => setIsFullscreen(document.fullscreenElement === containerRef.current);
    document.addEventListener("fullscreenchange", handleChange);
    return () => document.removeEventListener("fullscreenchange", handleChange);
  }, []);

  const toggleFullscreen = () => {
    const element = containerRef.current;
    if (!element) return;
    if (document.fullscreenElement === element) {
      void document.exitFullscreen().catch(() => undefined);
    } else {
      void element.requestFullscreen().catch(() => undefined);
    }
  };

  return (
    <section
      ref={containerRef}
      className="cm-surface relative isolate flex h-full min-h-0 min-w-0 flex-col overflow-hidden bg-white max-xl:min-h-[360px]"
      style={isFullscreen ? { height: "100vh", width: "100vw", borderRadius: 0 } : undefined}
    >
      <div className="relative z-20 flex h-12 shrink-0 items-center justify-between gap-2 border-b border-slate-200 bg-white px-3">
        <div className="min-w-0 truncate text-sm font-medium text-slate-950">{instanceName}</div>
        <div className="relative z-20 flex shrink-0 items-center gap-2">
          {typeof workspaceVisible === "boolean" && onWorkspaceVisibilityChange && (
            <button
              type="button"
              onClick={() => onWorkspaceVisibilityChange(!workspaceVisible)}
              className="cm-icon-button"
              title={workspaceVisible ? t("instances.hideWorkspace") : t("instances.showWorkspace")}
              aria-label={workspaceVisible ? t("instances.hideWorkspace") : t("instances.showWorkspace")}
            >
              {workspaceVisible ? <PanelRightClose className="h-4 w-4" /> : <PanelRightOpen className="h-4 w-4" />}
            </button>
          )}
          {onRefresh && (
            <button type="button" onClick={onRefresh} className="cm-icon-button" title={t("common.refresh")} aria-label={t("common.refresh")}>
              <RefreshCw className={`h-4 w-4 ${refreshing ? "animate-spin" : ""}`} />
            </button>
          )}
          <button
            type="button"
            onClick={toggleFullscreen}
            className="cm-icon-button"
            title={isFullscreen ? t("instances.exitFullscreen") : t("instances.enterFullscreen")}
            aria-label={isFullscreen ? t("instances.exitFullscreen") : t("instances.enterFullscreen")}
          >
            {isFullscreen ? <Minimize2 className="h-4 w-4" /> : <Maximize2 className="h-4 w-4" />}
          </button>
        </div>
      </div>
      {children}
    </section>
  );
}
