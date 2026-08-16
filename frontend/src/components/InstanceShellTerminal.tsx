import { useCallback, useEffect, useRef, useState } from "react";
import { FitAddon } from "@xterm/addon-fit";
import { Unicode11Addon } from "@xterm/addon-unicode11";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useI18n } from "../contexts/I18nContext";

interface InstanceShellTerminalProps {
  instanceId: number;
  instanceName: string;
  isRunning: boolean;
  autoConnect?: boolean;
  heightClassName?: string;
  className?: string;
}

type ShellConnectionState =
  | "idle"
  | "connecting"
  | "connected"
  | "disconnected"
  | "error";

const textEncoder = new TextEncoder();

export function InstanceShellTerminal({
  instanceId,
  instanceName,
  isRunning,
  autoConnect = false,
  heightClassName = "h-[54vh] min-h-[420px] max-h-[720px] md:h-[58vh] xl:h-[60vh]",
  className = "",
}: InstanceShellTerminalProps) {
  const { t } = useI18n();
  const [connectionState, setConnectionState] =
    useState<ShellConnectionState>("idle");
  const [error, setError] = useState<string | null>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [terminalElement, setTerminalElement] =
    useState<HTMLDivElement | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const resizeFrameRef = useRef<number | null>(null);
  const manuallyDisconnectedRef = useRef(false);

  const buildShellUrl = useCallback(() => {
    const token = localStorage.getItem("access_token");
    if (!token) {
      return null;
    }

    const explicitOrigin = import.meta.env.VITE_BACKEND_ORIGIN as
      | string
      | undefined;
    const base = explicitOrigin || window.location.origin;
    const url = new URL(`/api/v1/instances/${instanceId}/shell`, base);
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    if (!explicitOrigin && window.location.port === "9002") {
      url.port = "9001";
    }
    url.searchParams.set("token", token);
    return url.toString();
  }, [instanceId]);

  const fitAndSendResize = useCallback(() => {
    const terminal = terminalRef.current;
    const fitAddon = fitAddonRef.current;
    if (!terminal || !fitAddon) {
      return;
    }

    try {
      fitAddon.fit();
    } catch {
      return;
    }

    const socket = socketRef.current;
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      return;
    }

    socket.send(
      JSON.stringify({
        type: "resize",
        cols: terminal.cols,
        rows: terminal.rows,
      }),
    );
  }, []);

  const scheduleFitAndResize = useCallback(() => {
    if (resizeFrameRef.current != null) {
      window.cancelAnimationFrame(resizeFrameRef.current);
    }
    resizeFrameRef.current = window.requestAnimationFrame(() => {
      resizeFrameRef.current = null;
      fitAndSendResize();
    });
  }, [fitAndSendResize]);

  const disconnect = useCallback((manual = true) => {
    if (manual) {
      manuallyDisconnectedRef.current = true;
    }
    socketRef.current?.close();
    socketRef.current = null;
    setConnectionState((current) =>
      current === "connected" || current === "connecting"
        ? "disconnected"
        : current,
    );
  }, []);

  const toggleFullscreen = useCallback(async () => {
    const container = containerRef.current;
    if (!container) {
      return;
    }

    try {
      if (document.fullscreenElement === container) {
        await document.exitFullscreen();
      } else {
        if (document.fullscreenElement) {
          await document.exitFullscreen();
        }
        await container.requestFullscreen();
      }
    } catch (fullscreenError) {
      console.error("Failed to toggle shell fullscreen", fullscreenError);
    }
  }, []);

  const connect = useCallback(() => {
    if (!isRunning) {
      return;
    }

    const existingSocket = socketRef.current;
    if (
      existingSocket &&
      (existingSocket.readyState === WebSocket.OPEN ||
        existingSocket.readyState === WebSocket.CONNECTING)
    ) {
      return;
    }

    const shellUrl = buildShellUrl();
    if (!shellUrl) {
      setError(t("instances.shellMissingToken"));
      setConnectionState("error");
      return;
    }

    const terminal = terminalRef.current;
    terminal?.reset();
    terminal?.clear();

    setConnectionState("connecting");
    setError(null);

    const socket = new WebSocket(shellUrl);
    socket.binaryType = "arraybuffer";
    socketRef.current = socket;

    socket.onopen = () => {
      setConnectionState("connected");
      terminalRef.current?.focus();
      scheduleFitAndResize();
    };

    socket.onmessage = (event) => {
      const currentTerminal = terminalRef.current;
      if (!currentTerminal) {
        return;
      }

      if (event.data instanceof ArrayBuffer) {
        currentTerminal.write(new Uint8Array(event.data));
        return;
      }

      if (event.data instanceof Blob) {
        void event.data.arrayBuffer().then((buffer) => {
          terminalRef.current?.write(new Uint8Array(buffer));
        });
        return;
      }

      if (typeof event.data === "string") {
        currentTerminal.write(event.data);
      }
    };

    socket.onerror = () => {
      setError(t("instances.shellConnectionFailed"));
      setConnectionState("error");
    };

    socket.onclose = () => {
      if (socketRef.current === socket) {
        socketRef.current = null;
      }
      setConnectionState((current) =>
        current === "error" ? current : "disconnected",
      );
    };
  }, [buildShellUrl, isRunning, scheduleFitAndResize, t]);

  useEffect(() => {
    manuallyDisconnectedRef.current = false;
  }, [instanceId]);

  // OpenCode Lite uses this terminal as its primary UI. Connect as soon as
  // its xterm surface exists, while preserving a user's explicit disconnect.
  useEffect(() => {
    if (!autoConnect || !isRunning || !terminalElement || manuallyDisconnectedRef.current) {
      return;
    }
    connect();
  }, [autoConnect, connect, instanceId, isRunning, terminalElement]);

  useEffect(() => {
    if (!terminalElement) {
      return;
    }

    const terminal = new Terminal({
      allowProposedApi: true,
      allowTransparency: false,
      convertEol: false,
      cursorBlink: true,
      cursorStyle: "block",
      drawBoldTextInBrightColors: true,
      fontFamily:
        '"Cascadia Mono", "JetBrains Mono", "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace',
      fontSize: 13,
      ignoreBracketedPasteMode: false,
      lineHeight: 1.18,
      scrollback: 100000,
      theme: {
        background: "#0b1020",
        foreground: "#d7e1f2",
        cursor: "#ffffff",
        cursorAccent: "#0b1020",
        selectionBackground: "#334155",
        black: "#0b1020",
        red: "#ef4444",
        green: "#22c55e",
        yellow: "#f59e0b",
        blue: "#3b82f6",
        magenta: "#a855f7",
        cyan: "#14b8a6",
        white: "#e5e7eb",
        brightBlack: "#64748b",
        brightRed: "#f87171",
        brightGreen: "#4ade80",
        brightYellow: "#fbbf24",
        brightBlue: "#60a5fa",
        brightMagenta: "#c084fc",
        brightCyan: "#2dd4bf",
        brightWhite: "#f8fafc",
      },
    });
    const fitAddon = new FitAddon();
    const unicode11Addon = new Unicode11Addon();

    terminal.loadAddon(fitAddon);
    terminal.loadAddon(unicode11Addon);
    terminal.unicode.activeVersion = "11";
    terminal.open(terminalElement);

    terminalRef.current = terminal;
    fitAddonRef.current = fitAddon;

    const dataDisposable = terminal.onData((data) => {
      const socket = socketRef.current;
      if (!socket || socket.readyState !== WebSocket.OPEN) {
        return;
      }
      socket.send(textEncoder.encode(data));
    });

    scheduleFitAndResize();

    return () => {
      dataDisposable.dispose();
      if (resizeFrameRef.current != null) {
        window.cancelAnimationFrame(resizeFrameRef.current);
        resizeFrameRef.current = null;
      }
      socketRef.current?.close();
      socketRef.current = null;
      terminal.dispose();
      terminalRef.current = null;
      fitAddonRef.current = null;
    };
  }, [scheduleFitAndResize, terminalElement]);

  useEffect(() => {
    if (!terminalElement || typeof ResizeObserver === "undefined") {
      return;
    }

    const observer = new ResizeObserver(() => scheduleFitAndResize());
    observer.observe(terminalElement);
    return () => observer.disconnect();
  }, [scheduleFitAndResize, terminalElement]);

  useEffect(() => {
    const handleFullscreenChange = () => {
      setIsFullscreen(document.fullscreenElement === containerRef.current);
      window.setTimeout(() => {
        scheduleFitAndResize();
        terminalRef.current?.focus();
      }, 50);
    };

    document.addEventListener("fullscreenchange", handleFullscreenChange);
    return () => {
      document.removeEventListener("fullscreenchange", handleFullscreenChange);
    };
  }, [scheduleFitAndResize]);

  useEffect(() => {
    if (!isRunning) {
      disconnect(false);
    }
  }, [disconnect, isRunning]);

  const isConnecting = connectionState === "connecting";
  const isConnected = connectionState === "connected";
  const statusLabel = isConnected
    ? t("instances.shellConnectedStatus")
    : isConnecting
      ? t("instances.shellConnecting")
      : error
        ? error
        : t("instances.shellReady");
  const fullscreenLabel = isFullscreen
    ? t("instances.exitFullscreen")
    : t("instances.enterFullscreen");
  const containerFrameClass = isFullscreen
    ? "h-screen min-h-0 max-h-none rounded-none border-0 shadow-none"
    : `rounded-lg border border-[#1f2937] shadow-[0_30px_90px_-56px_rgba(17,24,39,0.9)] ${heightClassName}`;

  if (!isRunning && !isConnected) {
    return (
      <div
        className={`app-panel flex flex-col items-center justify-center border-dashed p-12 text-center ${heightClassName} ${className}`}
      >
        <h3 className="text-sm font-medium text-gray-900">
          {t("instances.startTheInstance")}
        </h3>
        <p className="mt-1 text-sm text-gray-500">
          {t("instances.startToAccessShell")}
        </p>
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      className={`relative flex flex-col overflow-hidden bg-[#0b1020] ${containerFrameClass} ${className}`}
    >
      <div className="flex items-center justify-between border-b border-[#202a3b] bg-[#111827] px-4 py-3 text-white">
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold">{instanceName}</p>
          <p
            className={`mt-1 truncate text-xs ${
              error ? "text-red-300" : "text-[#aab4c4]"
            }`}
          >
            {statusLabel}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={toggleFullscreen}
            aria-label={fullscreenLabel}
            title={fullscreenLabel}
            className="flex h-8 w-8 items-center justify-center rounded-md bg-[#243041] text-gray-200 hover:bg-[#31415a] hover:text-white"
          >
            {isFullscreen ? (
              <svg
                className="h-4 w-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                aria-hidden="true"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M6 18L18 6M6 6l12 12"
                />
              </svg>
            ) : (
              <svg
                className="h-4 w-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                aria-hidden="true"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M4 8V4m0 0h4M4 4l5 5m11-1V4m0 0h-4m4 0l-5 5M4 16v4m0 0h4m-4 0l5-5m11 5l-5-5m5 5v-4m0 4h-4"
                />
              </svg>
            )}
          </button>
          {isConnected ? (
            <button
              type="button"
              onClick={() => disconnect()}
              className="rounded-md bg-[#243041] px-3 py-1.5 text-xs font-medium text-gray-200 hover:bg-[#31415a]"
            >
              {t("instances.disconnectShell")}
            </button>
          ) : (
            <button
              type="button"
              onClick={connect}
              disabled={isConnecting}
              className="rounded-md bg-indigo-500 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-600 disabled:cursor-wait disabled:opacity-70"
            >
              {isConnecting
                ? t("instances.shellConnecting")
                : t("instances.connectShell")}
            </button>
          )}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-hidden p-4">
        <div
          ref={setTerminalElement}
          className="h-full w-full overflow-hidden [&_.xterm]:h-full [&_.xterm-screen]:outline-none [&_.xterm-viewport]:bg-[#0b1020]"
        />
      </div>
    </div>
  );
}
