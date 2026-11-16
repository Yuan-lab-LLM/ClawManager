import { useEffect, useRef } from "react";
import { Monitor } from "lucide-react";
import { formatInstanceType, type Instance } from "../types/instance";
import "./agent-state-icon.css";

const logos: Partial<Record<Instance["type"], string>> = {
  openclaw: "/openclaw.png",
  hermes: "/hermes.png",
  opencode: "/opencode.png",
  "deepseek-harness": "/deepseek-harness.svg",
  workbuddy: "/workbuddy.png",
  codex: "/codex.png",
  "claude-code": "/claude-code.png",
};

export type AgentStateIconProps = {
  type: Instance["type"];
  state: string;
  label?: string;
  size?: number;
  className?: string;
};

export default function AgentStateIcon({ type, state, label, size = 32, className = "" }: AgentStateIconProps) {
  const ref = useRef<HTMLSpanElement>(null);
  const displayState = state === "auto_sleep" ? "sleeping"
    : ["running", "stopped", "error", "sleeping"].includes(state) ? state : "unknown";
  const logo = logos[type];

  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    let visible = !("IntersectionObserver" in window);
    const update = () => {
      element.dataset.motionPaused = String(document.hidden || !visible);
    };
    const observer = "IntersectionObserver" in window ? new IntersectionObserver(([entry]) => {
      visible = entry.isIntersecting;
      update();
    }) : undefined;
    update();
    observer?.observe(element);
    document.addEventListener("visibilitychange", update);
    return () => {
      observer?.disconnect();
      document.removeEventListener("visibilitychange", update);
    };
  }, []);

  return <span ref={ref} className={`cm-agent-state-icon ${className}`.trim()}
    data-agent-state={displayState} role="img" aria-label={label ?? formatInstanceType(type)}
    style={{ width: size, height: size }}>
    <span className="cm-agent-state-art">
      {logo ? <img src={logo} alt="" width={size} height={size} draggable={false} />
        : <Monitor size={size} aria-hidden="true" />}
      {displayState === "sleeping" && type === "openclaw" && <svg className="cm-agent-state-sleep-eyes" viewBox="0 0 512 512" aria-hidden="true">
        <circle cx="192" cy="150" r="28" fill="#dc3639" />
        <circle cx="320" cy="150" r="28" fill="#d73538" />
        <path d="M176 148q16 15 32 0m96 0q16 15 32 0" fill="none" stroke="#25191a" strokeWidth="9" strokeLinecap="round" />
      </svg>}
    </span>
    {displayState === "error" && <svg className="cm-agent-state-warning" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M10.4 3.6a1.85 1.85 0 0 1 3.2 0l9 15.6a1.85 1.85 0 0 1-1.6 2.8H3a1.85 1.85 0 0 1-1.6-2.8Z" fill="#d20f2f" stroke="white" strokeWidth="1.8" strokeLinejoin="round" />
      <path d="M12 8v6" stroke="white" strokeWidth="2.3" strokeLinecap="round" />
      <circle cx="12" cy="17.3" r="1.2" fill="white" />
    </svg>}
    {displayState === "sleeping" && <svg className="cm-agent-state-sleep-sign" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M3 4.5h9L3 13h9M15 12h7l-7 7h7" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>}
  </span>;
}
