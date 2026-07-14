import { useCallback, useEffect, useState } from 'react';
import {
  secplaneService,
  type SecplaneRule,
  type SecplaneAlert,
  type RuleMode,
  type DispatchResult,
} from '../../../../services/secplaneService';

// 共享 hook：每个 surface 页 (state/decision/output/asset) 用它接入真实后端
// - 加载 defense_toggle 规则 (全部 14 项，由调用方按需读取它关心的 rule_id)
// - 加载 aegis 告警（按 rule_id 前缀筛选）
// - mode selector 变更 → PUT 保存
// - "应用" → dispatchAegisApply
export function useSurfaceBackend(alertRulePrefixes: string[] = []) {
  const [rules, setRules] = useState<SecplaneRule[]>([]);
  const [alerts, setAlerts] = useState<SecplaneAlert[]>([]);
  const [loading, setLoading] = useState(false);
  const [dispatching, setDispatching] = useState(false);
  const [dispatchMsg, setDispatchMsg] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [rs, as] = await Promise.all([
        secplaneService.listRules('defense_toggle'),
        secplaneService.listAlerts({ source: 'aegis', limit: 50 }),
      ]);
      setRules(rs);
      const filtered = alertRulePrefixes.length === 0
        ? as
        : as.filter((a) => a.rule_id && alertRulePrefixes.some((p) => a.rule_id!.startsWith(p)));
      setAlerts(filtered);
    } catch {
      // fail open — UI 显示 mock 数据
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [alertRulePrefixes.join(',')]);

  useEffect(() => {
    load();
  }, [load]);

  // 给定 rule_id 找到当前规则；不存在时返回 null
  const ruleOf = useCallback(
    (ruleId: string) => rules.find((r) => r.rule_id === ruleId) ?? null,
    [rules],
  );

  // 当前 rule 的"显示模式"：is_enabled=false → off, 否则按 rule.mode
  const modeOf = useCallback(
    (ruleId: string, defaultMode: RuleMode = 'enforce'): RuleMode => {
      const r = ruleOf(ruleId);
      if (!r) return defaultMode;
      if (!r.is_enabled) return 'off';
      return (r.mode ?? defaultMode) as RuleMode;
    },
    [ruleOf],
  );

  // 修改某 rule 的模式：'off' → is_enabled=false；其他 → is_enabled=true + mode=...
  const setMode = useCallback(
    async (ruleId: string, mode: RuleMode) => {
      const cur = ruleOf(ruleId);
      if (!cur) return; // 不存在的话需要前后端先 seed
      const next: SecplaneRule = {
        ...cur,
        is_enabled: mode !== 'off',
        mode: mode === 'off' ? cur.mode : mode,
      };
      try {
        const saved = await secplaneService.saveRule(next);
        setRules((rs) => rs.map((r) => (r.rule_id === ruleId ? saved : r)));
      } catch {
        // ignore — UI 状态可能短暂不同步，刷新即可
      }
    },
    [ruleOf],
  );

  const dispatchApply = useCallback(async (instanceIds?: number[] | null) => {
    setDispatching(true);
    setDispatchMsg(null);
    try {
      const ids = instanceIds && instanceIds.length > 0 ? instanceIds : undefined;
      const res: DispatchResult = await secplaneService.dispatchAegisApply(ids);
      const targets = res.targets ?? [];
      const errCount = targets.filter((t) => t.status === 'error' || !!t.error).length;
      const okCount = targets.length - errCount;
      if (targets.length === 0) {
        setDispatchMsg('下发完成，但没有 running 状态的实例可派发');
      } else if (errCount === 0) {
        setDispatchMsg(`已派发到 ${okCount} 个实例（pending → agent 拉取后即生效）`);
      } else {
        setDispatchMsg(`派发到 ${okCount} 个成功，${errCount} 个失败`);
      }
      load();
    } catch (e) {
      const err = e as { response?: { data?: { error?: string } }; message?: string };
      setDispatchMsg('下发失败：' + (err.response?.data?.error ?? err.message ?? '未知错误'));
    } finally {
      setDispatching(false);
    }
  }, [load]);

  return { rules, alerts, loading, dispatching, dispatchMsg, ruleOf, modeOf, setMode, dispatchApply, reload: load };
}
