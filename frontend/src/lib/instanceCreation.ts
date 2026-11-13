import type { CreateInstanceRequest, InstanceMode } from '../types/instance';
import type { SystemImageSetting } from '../services/systemSettingsService';

export const createAgentTypes = [
  { id: 'openclaw', label: 'OpenClaw', icon: '/openclaw.png' },
  { id: 'hermes', label: 'Hermes', icon: '/hermes.png' },
  { id: 'opencode', label: 'OpenCode', icon: '/opencode.png' },
  { id: 'deepseek-harness', label: 'DeepSeek Harness', icon: '/deepseek-harness.svg' },
  { id: 'custom', label: 'Custom', icon: '' },
] as const;
export function defaultCreateImage(images: SystemImageSetting[], type: string, mode: InstanceMode) {
  if (type === 'custom' && mode === 'lite') return undefined;
  return images.find(image => image.is_enabled !== false && image.instance_type === type &&
    (image.runtime_type === 'gateway' ? 'lite' : 'pro') === mode);
}
export const supportsCreateImport = (type: string) => type === 'openclaw' || type === 'hermes';
export const supportsCreateSkills = (type: string) => type !== 'custom';
export function parseCreateEnvironment(text: string): { values: Record<string, string>; error?: { line: number; duplicate?: string } } {
  const values: Record<string, string> = Object.create(null);
  for (const [index, line] of text.split(/\r?\n/).entries()) {
    if (!line.trim() || line.trimStart().startsWith('#')) continue;
    const equal = line.indexOf('=');
    const key = equal < 0 ? '' : line.slice(0, equal).trim();
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) return { values, error: { line: index + 1 } };
    if (Object.hasOwn(values, key)) return { values, error: { line: index + 1, duplicate: key } };
    values[key] = line.slice(equal + 1);
  }
  return { values };
}
export function createNames(name: string, batch: boolean, count: number, start: number): string[] {
  const prefix = name.trim();
  if (!prefix || (batch && (prefix.length > 40 || !Number.isInteger(count) || count < 2 || count > 100 || !Number.isInteger(start) || start < 1 || start > 9999))) return [];
  return batch ? Array.from({ length: count }, (_, index) => `${prefix}-${String(start + index).padStart(3, '0')}`) : [prefix];
}
export function validCreateNames(names: string[]) {
  return names.length > 0 && names.every(name => name.length >= 3 && new TextEncoder().encode(name).length <= 50);
}
export const maskCreateEnvironment = (key: string, value: string) => /token|secret|password|passwd|api[_-]?key|private[_-]?key|authorization|credential/i.test(key) ? '••••••••' : value;
export type CreationResult = { name: string; status: 'created' | 'failed'; instanceId?: number; error?: string; importPending?: boolean; retryBlocked?: boolean };
export type CreationSnapshot = { payload: CreateInstanceRequest; file: File | null };
