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
    title={mode ? descriptions[mode] : zh ? "暂不能确认治理状态，不代表实例不可用。" : "Governance status is unconfirmed; this does not imply the instance is unavailable."}>
    {mode ? labels[mode] : current?.failed ? (zh ? "治理：状态暂不可用" : "Governance: status unavailable") : (zh ? "治理：加载中" : "Governance: loading")}
  </span>;
}
