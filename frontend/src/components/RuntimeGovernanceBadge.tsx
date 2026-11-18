import { useEffect, useState } from "react";
import { useI18n } from "../contexts/I18nContext";
import { instanceService } from "../services/instanceService";
import type { RuntimeGovernanceStatus } from "../types/instance";

export default function RuntimeGovernanceBadge({ instanceId }: { instanceId: number }) {
  const { locale } = useI18n();
  const zh = locale.startsWith("zh");
  const [state, setState] = useState<{ id: number; value?: RuntimeGovernanceStatus; failed?: boolean }>();
  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const poll = async () => {
      if (!document.hidden) {
        try {
          const value = await instanceService.getRuntimeGovernance(instanceId);
          if (!cancelled) setState({ id: instanceId, value });
        } catch {
          // Never label an unreadable enrollment state as legacy/compatible.
          if (!cancelled) setState({ id: instanceId, failed: true });
        }
      }
      if (!cancelled) timer = setTimeout(poll, 10000);
    };
    void poll();
    return () => { cancelled = true; clearTimeout(timer); };
  }, [instanceId]);
  const current = state?.id === instanceId ? state : undefined;
  const mode = current?.value?.mode;
  const reason = current?.value?.reason;
  const reasons = zh ? {
    not_enrolled: "尚未完成新版协议协商，继续使用兼容模式。",
    connected: "当前代次的完整能力报告有效。",
    unsupported_protocol: "协议版本不受当前服务端支持，请核对运行时与服务端版本。",
    owner_changed: "能力报告属于旧所有者，等待当前实例重新协商。",
    generation_changed: "能力报告属于旧运行代次，等待重启后的运行时重新协商。",
    runtime_not_running: "实例尚未运行，不能确认当前能力已连接。",
    report_clock_skew: "报告时间晚于服务端时间，请检查时钟同步。",
    report_expired: "能力报告已过期，等待运行时更新报告。",
    invalid_report: "能力报告不完整或版本字段无效，不能认定支持完整治理。",
  } : {
    not_enrolled: "No new-protocol enrollment; legacy compatibility remains active.",
    connected: "A complete report for the current generation is valid.",
    unsupported_protocol: "Unsupported protocol; check runtime and server versions.",
    owner_changed: "The report belongs to the previous owner; re-enrollment is required.",
    generation_changed: "The report belongs to a previous runtime generation.",
    runtime_not_running: "The instance is not running; readiness is unconfirmed.",
    report_clock_skew: "The report is in the future; check clock synchronization.",
    report_expired: "The report expired; awaiting a runtime refresh.",
    invalid_report: "Incomplete capabilities or invalid version fields; full governance is unconfirmed.",
  };
  const labels = zh ? {
    legacy: "治理：兼容模式", governed: "治理：完整协议已连接", awaiting_runtime: "治理：等待运行时确认",
  } : { legacy: "Governance: legacy compatible", governed: "Governance: protocol connected", awaiting_runtime: "Governance: awaiting runtime" };
  const descriptions = zh ? {
    legacy: "旧实例可继续使用，模型权限和 Auto 过滤仍然生效；尚未接入整轮取消和实时模型目录。",
    governed: "运行时已上报任务身份、整轮取消及目录同步能力。具体任务是否已取消，以取消回执为准。",
    awaiting_runtime: "实例已接入治理，但当前运行时尚未确认或报告已过期；不会自动降级绕过任务检查。",
  } : {
    legacy: "Existing instances remain usable. Model authorization and Auto filtering apply; whole-turn cancellation and live catalog are not connected.",
    governed: "The runtime reports task identity, cancellation and live catalog support. Individual cancellations require a receipt.",
    awaiting_runtime: "Enrollment remains enforced while the runtime report is missing or stale. No automatic downgrade.",
  };
  return <span className="text-xs text-slate-500" tabIndex={0}
    title={mode ? [descriptions[mode], reason && reasons[reason]].filter(Boolean).join(" ") : zh ? "暂不能确认治理状态，不代表实例不可用。" : "Governance status is unconfirmed; this does not imply the instance is unavailable."}>
    {mode ? labels[mode] : current?.failed ? (zh ? "治理：状态暂不可用" : "Governance: status unavailable") : (zh ? "治理：加载中" : "Governance: loading")}
  </span>;
}
