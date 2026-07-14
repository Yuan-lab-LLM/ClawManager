// Frontend feature flags — flip to true when the backend / UX is real.
// Each flag should carry a one-line comment about what gets shown.
//
// Convention: types are widened to `boolean` (via the FeatureFlags shape) so
// flipping a flag never trips TS's "always-false condition" narrowing.

export interface FeatureFlags {
  // Output Surface scenario — "存量凭据巡检 / 本地存储明文凭据告警" panel.
  // Currently mock UI (CRED_ALERTS + 立即扫描 + 标记为已处理); no backend.
  credentialInventory: boolean;

  // Asset Protection scenario — "智能体资产基线漂移监控" panel.
  // 30-min 周期校验, 现 DRIFT_CHECKS 全 mock; 立即校验按钮未接后端.
  assetDriftMonitor: boolean;

  // Asset Protection scenario — "智能体记忆资产实时漂移告警" panel.
  // 4-级告警分级, 现 MEMORY_ALERTS 全 mock; 无实时事件源.
  memoryDriftAlerts: boolean;

  // Asset Protection scenario — "受保护的智能体核心资产 · 记忆 / 技能 / 插件 / 凭据" panel.
  // 8 项内置资产 + secureclaw 校验/监听机制展示, 现 CORE_ASSETS 全 mock; 无 inventory 后端.
  coreAssetsInventory: boolean;

  // State Surface scenario — "记忆完整性监控" panel.
  // INTEGRITY_EVENTS hard-coded + alert banner 数字硬编码; 无 secureclaw 校验后端.
  memoryIntegrityCheck: boolean;

  // Approval scenario (G) — 人因审批中心 entire page body.
  // CASES + TABS + 拒绝/允许 buttons all mock; no approval queue / workflow backend.
  approvalCenter: boolean;
}

export const FEATURES: FeatureFlags = {
  credentialInventory: false,
  assetDriftMonitor: false,
  memoryDriftAlerts: false,
  coreAssetsInventory: false,
  memoryIntegrityCheck: false,
  approvalCenter: false,
};
