import React, { useCallback, useEffect, useRef, useState } from "react";
import { useI18n } from "../contexts/I18nContext";

type OverlayCommand =
  | "start"
  | "stop"
  | "restart"
  | "collect-system-info"
  | "health-check";

export function OpenClawDesktopOverlay({
  gatewayStatus,
  canControl,
  actionLoading,
  onCommand,
}: {
  gatewayStatus: string;
  canControl: boolean;
  actionLoading: string | null;
  onCommand: (command: OverlayCommand) => void;
}) {
  const { t } = useI18n();
  const rootRef = useRef<HTMLDivElement | null>(null);
  const orbRef = useRef<HTMLButtonElement | null>(null);
  const dragOffsetRef = useRef({ x: 0, y: 0 });
  const pointerStartRef = useRef({ x: 0, y: 0 });
  const hideTimerRef = useRef<number | null>(null);
  const movedRef = useRef(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const [position, setPosition] = useState({ x: 18, y: 18 });
  const [isDragging, setIsDragging] = useState(false);

  const orbSize = 62;
  const ballTone = gatewayBallTone(gatewayStatus);

  const clampPosition = useCallback((x: number, y: number) => {
    const root = rootRef.current;
    if (!root) {
      return { x, y };
    }
    const maxX = Math.max(root.clientWidth - orbSize - 12, 12);
    const maxY = Math.max(root.clientHeight - orbSize - 12, 12);
    return {
      x: Math.min(Math.max(12, x), maxX),
      y: Math.min(Math.max(12, y), maxY),
    };
  }, []);

  useEffect(() => {
    const handleResize = () => {
      setPosition((current) => clampPosition(current.x, current.y));
    };

    window.addEventListener("resize", handleResize);
    return () => {
      window.removeEventListener("resize", handleResize);
    };
  }, [clampPosition]);

  useEffect(() => {
    return () => {
      if (hideTimerRef.current !== null) {
        window.clearTimeout(hideTimerRef.current);
      }
    };
  }, []);

  const scheduleHide = () => {
    if (hideTimerRef.current !== null) {
      window.clearTimeout(hideTimerRef.current);
    }
    hideTimerRef.current = window.setTimeout(() => {
      setMenuOpen(false);
    }, 360);
  };

  const cancelHide = () => {
    if (hideTimerRef.current !== null) {
      window.clearTimeout(hideTimerRef.current);
      hideTimerRef.current = null;
    }
  };

  const handlePointerDown = (event: React.PointerEvent<HTMLButtonElement>) => {
    const orb = orbRef.current;
    if (!orb) {
      return;
    }

    movedRef.current = false;
    pointerStartRef.current = {
      x: event.clientX,
      y: event.clientY,
    };
    const rect = orb.getBoundingClientRect();
    dragOffsetRef.current = {
      x: event.clientX - rect.left,
      y: event.clientY - rect.top,
    };
    setIsDragging(true);
    orb.setPointerCapture(event.pointerId);
  };

  const handlePointerMove = (event: React.PointerEvent<HTMLButtonElement>) => {
    const root = rootRef.current;
    const orb = orbRef.current;
    if (!root || !orb || !orb.hasPointerCapture(event.pointerId)) {
      return;
    }

    const rootRect = root.getBoundingClientRect();
    const nextX = event.clientX - rootRect.left - dragOffsetRef.current.x;
    const nextY = event.clientY - rootRect.top - dragOffsetRef.current.y;
    const next = clampPosition(nextX, nextY);
    const movedDistance = Math.hypot(
      event.clientX - pointerStartRef.current.x,
      event.clientY - pointerStartRef.current.y,
    );
    if (movedDistance > 3) {
      movedRef.current = true;
    }
    setPosition(next);
  };

  const handlePointerUp = (event: React.PointerEvent<HTMLButtonElement>) => {
    const orb = orbRef.current;
    if (orb?.hasPointerCapture(event.pointerId)) {
      orb.releasePointerCapture(event.pointerId);
    }
    setIsDragging(false);

    setPosition((current) => {
      const root = rootRef.current;
      if (!root) {
        return current;
      }
      const maxX = Math.max(root.clientWidth - orbSize - 12, 12);
      const snappedX =
        current.x + orbSize / 2 < root.clientWidth / 2 ? 12 : maxX;
      return { ...current, x: snappedX };
    });

    if (!movedRef.current) {
      setMenuOpen((current) => !current);
    }
    window.setTimeout(() => {
      movedRef.current = false;
    }, 0);
  };

  const actionVectors =
    position.x > 260
      ? [
          { x: -84, y: -12 },
          { x: -56, y: -92 },
          { x: -30, y: 66 },
        ]
      : [
          { x: 84, y: -12 },
          { x: 56, y: -92 },
          { x: 30, y: 66 },
        ];

  const actions = [
    {
      key: "start",
      label: t("common.start"),
      icon: <StartIcon />,
      vector: actionVectors[0],
      disabled:
        !canControl ||
        gatewayStatus === "running" ||
        actionLoading === "runtime-start",
      loading: actionLoading === "runtime-start",
      onClick: () => {
        setMenuOpen(false);
        onCommand("start");
      },
    },
    {
      key: "stop",
      label: t("common.stop"),
      icon: <StopIcon />,
      vector: actionVectors[1],
      disabled:
        !canControl ||
        gatewayStatus === "stopped" ||
        actionLoading === "runtime-stop",
      loading: actionLoading === "runtime-stop",
      onClick: () => {
        setMenuOpen(false);
        onCommand("stop");
      },
    },
    {
      key: "restart",
      label: t("common.restart"),
      icon: <RestartIcon />,
      vector: actionVectors[2],
      disabled: !canControl || actionLoading === "runtime-restart",
      loading: actionLoading === "runtime-restart",
      onClick: () => {
        setMenuOpen(false);
        onCommand("restart");
      },
    },
  ];

  return (
    <div ref={rootRef} className="pointer-events-none absolute inset-0 z-20">
      <button
        ref={orbRef}
        type="button"
        aria-label={t("instances.openClawGatewayControls")}
        onMouseEnter={cancelHide}
        onMouseLeave={scheduleHide}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerUp}
        onPointerCancel={() => setIsDragging(false)}
        style={{ transform: `translate(${position.x}px, ${position.y}px)` }}
        className={`pointer-events-auto absolute flex h-[62px] w-[62px] items-center justify-center rounded-full border-4 border-white/90 text-white shadow-[0_20px_40px_-18px_rgba(15,23,42,0.82)] ${
          isDragging
            ? "scale-100 cursor-grabbing"
            : "cursor-grab transition-[transform,box-shadow] duration-300 ease-out hover:scale-[1.03] active:scale-100"
        }`}
      >
        <span
          className="absolute inset-0 rounded-full"
          style={{ background: ballTone.gradient }}
        />
        <span className="absolute inset-[7px] rounded-full border border-white/35 bg-[radial-gradient(circle_at_30%_26%,rgba(255,255,255,0.42),rgba(255,255,255,0.08)_42%,rgba(0,0,0,0.08)_100%)] shadow-[inset_0_1px_0_rgba(255,255,255,0.35),inset_0_-8px_14px_rgba(0,0,0,0.12)] backdrop-blur-md" />
        <span className="absolute inset-x-[15px] top-[10px] h-[12px] rounded-full bg-white/28 blur-[2px]" />
        <span className="relative flex h-10 w-10 items-center justify-center rounded-full border border-white/28 bg-[linear-gradient(180deg,rgba(255,255,255,0.28)_0%,rgba(255,255,255,0.08)_100%)] shadow-[0_8px_18px_-10px_rgba(15,23,42,0.8)] backdrop-blur-md">
          <svg
            className="h-7 w-7"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.7"
            aria-hidden="true"
          >
            <rect x="6.5" y="7.5" width="11" height="9" rx="3" />
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              d="M12 4.75v2.75M9 18h6M8.8 11.6h.01M15.2 11.6h.01M10 14.1c.55.4 1.22.6 2 .6s1.45-.2 2-.6"
            />
          </svg>
        </span>
      </button>

      {menuOpen ? (
        <div
          style={{ transform: `translate(${position.x}px, ${position.y}px)` }}
          className="pointer-events-none absolute"
        >
          {actions.map((action, index) => (
            <OrbActionButton
              key={action.key}
              label={action.label}
              icon={action.icon}
              disabled={action.disabled}
              loading={action.loading}
              onClick={action.onClick}
              vector={action.vector}
              delayMs={index * 36}
              menuOpen={menuOpen}
              onMouseEnter={cancelHide}
              onMouseLeave={scheduleHide}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}

function OrbActionButton({
  icon,
  label,
  disabled,
  loading,
  onClick,
  vector,
  delayMs,
  menuOpen,
  onMouseEnter,
  onMouseLeave,
}: {
  icon: React.ReactNode;
  label: string;
  disabled: boolean;
  loading: boolean;
  onClick: () => void;
  vector: { x: number; y: number };
  delayMs: number;
  menuOpen: boolean;
  onMouseEnter: () => void;
  onMouseLeave: () => void;
}) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      onClick={onClick}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
      disabled={disabled}
      style={{
        transform: menuOpen
          ? `translate(${vector.x}px, ${vector.y}px) scale(1)`
          : "translate(0px, 0px) scale(0.72)",
        transitionDelay: menuOpen ? `${delayMs}ms` : "0ms",
      }}
      className="pointer-events-auto absolute left-0 top-0 flex h-[54px] w-[54px] items-center justify-center rounded-full border border-white/18 bg-[radial-gradient(circle_at_top,rgba(255,255,255,0.42),rgba(255,255,255,0.08)_38%),linear-gradient(180deg,#ffffff_0%,#eef2f7_100%)] text-[#182131] opacity-100 shadow-[0_18px_36px_-18px_rgba(15,23,42,0.9)] transition-[transform,opacity] duration-300 [transition-timing-function:cubic-bezier(0.22,1.22,0.36,1)] hover:scale-105 disabled:cursor-not-allowed disabled:opacity-45"
    >
      <span className="flex h-9 w-9 items-center justify-center rounded-full bg-[#f3f5f8] shadow-inner">
        {loading ? (
          <span className="h-4 w-4 animate-spin rounded-full border-2 border-[#c6ccd7] border-t-[#182131]" />
        ) : (
          icon
        )}
      </span>
    </button>
  );
}

function StartIcon() {
  return (
    <svg className="h-4.5 w-4.5" viewBox="0 0 24 24" fill="currentColor">
      <path d="M8 5.14v13.72L19 12 8 5.14z" />
    </svg>
  );
}

function StopIcon() {
  return (
    <svg className="h-4.5 w-4.5" viewBox="0 0 24 24" fill="currentColor">
      <path d="M7 7h10v10H7z" />
    </svg>
  );
}

function RestartIcon() {
  return (
    <svg
      className="h-4.5 w-4.5"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
    >
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M20 12a8 8 0 10-2.34 5.66M20 12v-6m0 6h-6"
      />
    </svg>
  );
}

function gatewayBallTone(status: string) {
  const normalized = status.trim().toLowerCase();
  switch (normalized) {
    case "running":
    case "started":
    case "online":
      return {
        gradient:
          "radial-gradient(circle at 32% 28%, rgba(255,255,255,0.44), rgba(255,255,255,0.04) 35%), linear-gradient(145deg, #34d399 0%, #16a34a 100%)",
      };
    case "starting":
    case "stopping":
    case "configuring":
    case "pending":
      return {
        gradient:
          "radial-gradient(circle at 32% 28%, rgba(255,255,255,0.46), rgba(255,255,255,0.05) 35%), linear-gradient(145deg, #fde68a 0%, #eab308 100%)",
      };
    default:
      return {
        gradient:
          "radial-gradient(circle at 32% 28%, rgba(255,255,255,0.44), rgba(255,255,255,0.04) 35%), linear-gradient(145deg, #fb7185 0%, #dc2626 100%)",
      };
  }
}
