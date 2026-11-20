import type { IEISystemInstance, IEISystemInstanceAccess } from '../services/ieiSystemService';

type Dependencies = {
  startInstance(id: number): Promise<void>;
  getInstance(id: number): Promise<IEISystemInstance>;
  generateAccess(id: number): Promise<IEISystemInstanceAccess>;
};

// Navigation can observe an accepted start, but only an explicit stopped-card
// click may dispatch one. Polling never sends Start/Restart/Reset again.
export async function connectOwnerInstance(
  initial: IEISystemInstance, requestStart: boolean, service: Dependencies,
  observe: (instance: IEISystemInstance) => void, current: () => boolean,
  sleep = (ms: number) => new Promise<void>(resolve => window.setTimeout(resolve, ms)),
  now = () => Date.now(), timeoutMs = 15 * 60_000,
): Promise<IEISystemInstanceAccess> {
  let instance = initial;
  const waiting = requestStart || initial.status === 'creating';
  const deadline = now() + timeoutMs;
  const check = () => { if (!current()) throw new Error('Instance navigation changed'); };
  check();
  if (requestStart && instance.status === 'stopped') {
    try { await service.startInstance(instance.id); }
    catch (failure) {
      check();
      // A lost HTTP response is not permission to submit Start twice.
      const observed = await service.getInstance(instance.id);
      check(); observe(observed); instance = observed;
      if (!['creating', 'running'].includes(instance.status)) throw failure;
    }
    check(); instance = await service.getInstance(instance.id); check(); observe(instance);
  }
  for (;;) {
    check();
    if (instance.status === 'running') {
      try { const access = await service.generateAccess(instance.id); check(); return access; }
      catch (failure) {
        check();
        const status = (failure as {response?: {status?: number}})?.response?.status;
        if (!waiting || [401,403,404].includes(status ?? 0)) throw failure;
      }
    } else if (instance.status !== 'creating') {
      throw new Error(instance.status === 'stopped' ? '实例已停止，可点击“启动实例”恢复使用，原会话和文件已保留。' : '实例运行异常，请查看状态或联系管理员。');
    }
    if (now() >= deadline) throw new Error('实例仍在启动，后台任务会继续。请稍后刷新访问；不会重复提交启动。');
    await sleep(2_000); check();
    instance = await service.getInstance(instance.id); check(); observe(instance);
  }
}
