import { useWorkbenchServices } from "./WorkbenchServices";
import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { Unicode11Addon } from "@xterm/addon-unicode11";
import "@xterm/xterm/css/xterm.css";
import { useI18n } from "../../contexts/I18nContext";
import {
  workbenchError,
} from "../../services/instanceWorkbenchService";
import type { TerminalSession } from "../../types/workbench";
interface Props {
  instanceId: number;
  session: TerminalSession;
  visible: boolean;
  stale: boolean;
  onExit: (session: TerminalSession) => void;
}
export function WorkbenchTerminal({
  instanceId,
  session,
  visible,
  stale,
  onExit,
}: Props) {
  const { t } = useI18n();
  const { service } = useWorkbenchServices();
  const host = useRef<HTMLDivElement>(null);
  const terminal = useRef<Terminal | null>(null);
  const fit = useRef<FitAddon | null>(null);
  const cursor = useRef(0);
  // Each view must replay retained output, even if the session already ended.
  const outputComplete = useRef(false);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  const exitCallback = useRef(onExit);
  useEffect(() => {
    exitCallback.current = onExit;
  }, [onExit]);
  useEffect(() => {
    if (!host.current) return;
    const term = new Terminal({
      cursorBlink: true,
      screenReaderMode: true,
      allowProposedApi: true,
      convertEol: false,
      fontSize: 12,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
      scrollback: 8000,
      theme: {
        background: "#0f172a",
        foreground: "#e2e8f0",
        cursor: "#f8fafc",
      },
    });
    const addon = new FitAddon();
    term.loadAddon(addon);
    term.loadAddon(new Unicode11Addon());
    term.unicode.activeVersion = "11";
    term.open(host.current);
    terminal.current = term;
    fit.current = addon;
    return () => {
      terminal.current = null;
      fit.current = null;
      term.dispose();
    };
  }, []);
  useEffect(() => {
    const term = terminal.current;
    if (!term || stale || session.exited) return;
    let pending = "",
      timer: ReturnType<typeof setTimeout> | undefined;
    let chain = Promise.resolve();
    let disposed = false;
    const flush = () => {
      timer = undefined;
      const data = pending;
      pending = "";
      if (!data || disposed) return;
      chain = chain
        .then(() =>
          service.input(instanceId, session.generation, session.id, data),
        )
        .catch((err) => {
          if (!disposed) setError(workbenchError(err));
        });
    };
    const subscription = term.onData((data) => {
      if (pending.length + data.length > 16000) flush();
      pending += data;
      if (!timer) timer = setTimeout(flush, 40);
    });
    return () => {
      disposed = true;
      if (timer) clearTimeout(timer);
      subscription.dispose();
    };
  }, [service, instanceId, session.id, session.generation, session.exited, stale]);
  useEffect(() => {
    if (!visible || stale || !host.current) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const resize = () => {
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => {
        try {
          fit.current?.fit();
          const term = terminal.current;
          if (term && !session.exited) {
            void service
              .resize(
                instanceId,
                session.generation,
                session.id,
                term.cols,
                term.rows,
              )
              .catch((err) => setError(workbenchError(err)));
          }
        } catch {
          /* Hidden terminal has no measurable size. */
        }
      }, 100);
    };
    const observer = new ResizeObserver(resize);
    observer.observe(host.current);
    resize();
    terminal.current?.focus();
    return () => {
      observer.disconnect();
      if (timer) clearTimeout(timer);
    };
  }, [
    service,
    visible,
    stale,
    session.id,
    session.generation,
    session.exited,
    instanceId,
  ]);
  useEffect(() => {
    if (!visible || stale || (session.exited && outputComplete.current)) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const read = async () => {
      try {
        const output = await service.output(
          instanceId,
          session.generation,
          session.id,
          cursor.current,
          controller.signal,
        );
        if (controller.signal.aborted) return;
        if (output.truncated)
          terminal.current?.writeln(`\r\n[${t("workbench.outputLost")}]`);
        if (output.data) {
          const bytes = Uint8Array.from(atob(output.data), (c) =>
            c.charCodeAt(0),
          );
          terminal.current?.write(bytes);
        }
        cursor.current = output.cursor;
        if (output.exited || session.exited) {
          outputComplete.current = true;
          if (!session.exited) exitCallback.current({ ...session, exited: true });
          return;
        }
        timer = setTimeout(read, document.hidden ? 2500 : 300);
      } catch (err) {
        if (!controller.signal.aborted) setError(workbenchError(err));
      }
    };
    void read();
    return () => {
      controller.abort();
      if (timer) clearTimeout(timer);
    };
  }, [
    service,
    instanceId,
    session.id,
    session.generation,
    session.exited,
    visible,
    stale,
    retry,
    t,
    session,
  ]);
  return (
    <div className="wb-terminal">
      <div ref={host} className="wb-terminal-screen" />
      {(stale || session.exited || error) && (
        <div className="wb-terminal-message" role={error ? "alert" : "status"}>
          {stale ? t("workbench.changed") : error || t("workbench.ended")}
          {error && !stale && !session.exited && (
            <button
              onClick={() => {
                setError("");
                setRetry((n) => n + 1);
              }}
            >
              {t("workbench.reconnect")}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
