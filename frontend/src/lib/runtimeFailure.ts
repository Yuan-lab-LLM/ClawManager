const descriptions: Record<string, string> = {
  MODEL_CONFIGURATION_REQUIRED: "没有可用模型，请检查模型设置",
  RUNTIME_CAPACITY_EXHAUSTED: "运行池容量不足，等待可用容量",
  RUNTIME_POOL_UNAVAILABLE: "当前没有符合条件的运行池",
  CONTROL_PLANE_UNAVAILABLE: "控制服务暂时不可用，请稍后重试",
  PROXY_POLICY_NOT_READY: "实例访问代理配置尚未就绪",
  GATEWAY_START_FAILED: "实例进程准备或启动失败，请查看详细原因",
};

export function runtimeFailureSummary(instance: { runtime_error_code?: string; runtime_error_message?: string }): string {
  return descriptions[instance.runtime_error_code || ""] || instance.runtime_error_message?.split(/\r?\n/).find(line => line.trim()) || "原因未记录";
}
