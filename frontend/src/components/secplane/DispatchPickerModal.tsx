import React, { useEffect, useMemo, useState } from 'react';
import { instanceService } from '../../services/instanceService';
import type { Instance } from '../../types/instance';
import { useI18n } from '../../contexts/I18nContext';

// DispatchPickerModal is the shared instance picker UI used by every
// secplane sub-page that needs to push config to running OpenClaw pods.
// It owns its own instance-list state — parent only supplies the
// `onDispatch` callback (called with selected IDs, or null = dispatch to
// all) and `dispatching` flag (drives the loading state of the action
// buttons).
//
// Behavior:
//   - Lazy-loads /instances on first open; cached for subsequent opens
//   - Search filters across name / id / status / pod / type substring
//   - "Select All Visible" only selects currently-filtered rows
//   - "Dispatch to All" calls onDispatch(null), letting the backend resolve
//   - Closing on backdrop / X / Esc / cancel; parent decides when to set
//     `open` back to false after a successful dispatch (typically inside
//     the parent's runDispatch handler).

export interface DispatchPickerModalProps {
  open: boolean;
  onClose: () => void;
  onDispatch: (instanceIDs: number[] | null) => void | Promise<void>;
  dispatching: boolean;
  title?: string;
  hint?: string;
}

const DispatchPickerModal: React.FC<DispatchPickerModalProps> = ({
  open,
  onClose,
  onDispatch,
  dispatching,
  title,
  hint,
}) => {
  const { t } = useI18n();
  const _title = title ?? t('secplane.runtime.dispatchPicker.defaultTitle');
  const _hint = hint ?? t('secplane.runtime.dispatchPicker.defaultHint');
  const [instances, setInstances] = useState<Instance[]>([]);
  const [instancesLoading, setInstancesLoading] = useState(false);
  const [instancesError, setInstancesError] = useState<string | null>(null);
  const [instanceFilter, setInstanceFilter] = useState('');
  const [selectedInstanceIDs, setSelectedInstanceIDs] = useState<Set<number>>(new Set());

  const loadInstances = async () => {
    setInstancesLoading(true);
    setInstancesError(null);
    try {
      const resp = await instanceService.getInstances(1, 200);
      setInstances(resp.instances);
    } catch (e: any) {
      setInstancesError(e?.response?.data?.error ?? e?.message ?? 'failed to load instances');
    } finally {
      setInstancesLoading(false);
    }
  };

  // Lazy-load on first open; reset filter each time the modal opens so the
  // user doesn't see leftover search state from a previous session.
  useEffect(() => {
    if (!open) return;
    setInstanceFilter('');
    if (instances.length === 0) {
      void loadInstances();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const filteredInstances = useMemo(() => {
    const q = instanceFilter.trim().toLowerCase();
    if (!q) return instances;
    return instances.filter((inst) =>
      inst.name.toLowerCase().includes(q) ||
      String(inst.id).includes(q) ||
      inst.status.toLowerCase().includes(q) ||
      (inst.pod_name ?? '').toLowerCase().includes(q) ||
      inst.type.toLowerCase().includes(q),
    );
  }, [instances, instanceFilter]);

  const toggleInstanceSelected = (id: number) => {
    setSelectedInstanceIDs((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const selectAllVisible = () => {
    setSelectedInstanceIDs((prev) => {
      const next = new Set(prev);
      for (const inst of filteredInstances) next.add(inst.id);
      return next;
    });
  };

  const clearSelection = () => setSelectedInstanceIDs(new Set());

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <div className="flex max-h-[85vh] w-full max-w-3xl flex-col rounded-xl bg-white shadow-xl">
        <div className="flex items-center justify-between border-b border-gray-200 px-5 py-3">
          <div>
            <div className="text-base font-semibold text-gray-900">{_title}</div>
            <div className="text-xs text-gray-500">{_hint}</div>
          </div>
          <button
            onClick={onClose}
            className="rounded p-1 text-gray-500 hover:bg-gray-100 hover:text-gray-800"
            aria-label={t('secplane.runtime.dispatchPicker.close') ?? 'Close'}
          >
            ✕
          </button>
        </div>

        <div className="border-b border-gray-200 px-5 py-3">
          <div className="flex flex-wrap items-center gap-3">
            <div className="relative flex-1 min-w-[200px]">
              <input
                type="text"
                value={instanceFilter}
                onChange={(e) => setInstanceFilter(e.target.value)}
                placeholder={t('secplane.runtime.dispatchPicker.searchPlaceholder') ?? ''}
                className="w-full rounded border border-gray-300 px-3 py-2 text-sm"
              />
            </div>
            <button
              onClick={selectAllVisible}
              disabled={filteredInstances.length === 0}
              className="rounded border border-gray-300 bg-white px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-50 disabled:opacity-60"
            >
              {t('secplane.runtime.dispatchPicker.selectAll')} ({filteredInstances.length})
            </button>
            <button
              onClick={clearSelection}
              disabled={selectedInstanceIDs.size === 0}
              className="rounded border border-gray-300 bg-white px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-50 disabled:opacity-60"
            >
              {t('secplane.runtime.dispatchPicker.clear')} ({selectedInstanceIDs.size})
            </button>
            <button
              onClick={loadInstances}
              className="rounded border border-gray-300 bg-white px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-50"
            >
              {t('secplane.runtime.shared.refresh')}
            </button>
          </div>
          <div className="mt-2 text-xs text-gray-500">
            {t('secplane.runtime.dispatchPicker.summary', { total: instances.length, filtered: filteredInstances.length, selected: selectedInstanceIDs.size })}
          </div>
        </div>

        <div className="flex-1 overflow-y-auto">
          {instancesLoading && <div className="p-6 text-center text-sm text-gray-500">{t('secplane.runtime.dispatchPicker.loading')}</div>}
          {instancesError && (
            <div className="m-5 rounded border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700">{instancesError}</div>
          )}
          {!instancesLoading && filteredInstances.length === 0 && (
            <div className="p-6 text-center text-sm text-gray-500">
              {instances.length === 0 ? t('secplane.runtime.dispatchPicker.noInstances') : t('secplane.runtime.dispatchPicker.noMatch')}
            </div>
          )}
          <ul className="divide-y divide-gray-100">
            {filteredInstances.map((inst) => {
              const checked = selectedInstanceIDs.has(inst.id);
              const statusTone: Record<string, string> = {
                running: 'bg-emerald-100 text-emerald-700 border-emerald-200',
                stopped: 'bg-gray-100 text-gray-600 border-gray-200',
                creating: 'bg-sky-100 text-sky-700 border-sky-200',
                deleting: 'bg-amber-100 text-amber-700 border-amber-200',
                error: 'bg-rose-100 text-rose-700 border-rose-200',
              };
              const tone = statusTone[inst.status] ?? 'bg-gray-100 text-gray-600 border-gray-200';
              const notRunning = inst.status !== 'running';
              return (
                <li key={inst.id}>
                  <label className={`flex cursor-pointer items-center gap-3 px-5 py-3 hover:bg-gray-50 ${checked ? 'bg-indigo-50/50' : ''}`}>
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() => toggleInstanceSelected(inst.id)}
                      className="h-4 w-4 rounded border-gray-300"
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="truncate font-medium text-gray-900">{inst.name}</span>
                        <span className="text-xs text-gray-500">#{inst.id}</span>
                        <span className={`rounded-full border px-2 py-0.5 text-[10px] uppercase ${tone}`}>{inst.status}</span>
                        <span className="rounded bg-gray-100 px-2 py-0.5 text-[10px] text-gray-600">{inst.type}</span>
                        {notRunning && <span className="text-[10px] text-amber-600" title={t('secplane.runtime.dispatchPicker.notRunningTitle') ?? ''}>{t('secplane.runtime.dispatchPicker.notRunning')}</span>}
                      </div>
                      {inst.pod_name && (
                        <div className="mt-0.5 text-xs text-gray-500">
                          <span className="font-mono">{inst.pod_namespace}/{inst.pod_name}</span>
                          {inst.pod_ip && <span className="ml-2">{inst.pod_ip}</span>}
                        </div>
                      )}
                    </div>
                  </label>
                </li>
              );
            })}
          </ul>
        </div>

        <div className="flex items-center justify-between border-t border-gray-200 px-5 py-3">
          <div className="text-xs text-gray-500">
            {selectedInstanceIDs.size > 0
              ? t('secplane.runtime.dispatchPicker.willDispatchSelected', { count: selectedInstanceIDs.size })
              : t('secplane.runtime.dispatchPicker.noneSelectedHint')}
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={onClose}
              className="rounded border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50"
            >
              {t('secplane.runtime.dispatchPicker.cancel')}
            </button>
            <button
              onClick={() => onDispatch(null)}
              disabled={dispatching || instances.length === 0}
              className="rounded border border-indigo-300 bg-white px-3 py-1.5 text-sm text-indigo-700 hover:bg-indigo-50 disabled:opacity-60"
              title={t('secplane.runtime.dispatchPicker.sendToAllTitle') ?? ''}
            >
              {dispatching ? t('secplane.runtime.dispatchPicker.busyLabel') : t('secplane.runtime.dispatchPicker.sendToAll', { count: instances.length })}
            </button>
            <button
              onClick={() => onDispatch(Array.from(selectedInstanceIDs))}
              disabled={dispatching || selectedInstanceIDs.size === 0}
              className="rounded bg-indigo-600 px-3 py-1.5 text-sm text-white hover:bg-indigo-700 disabled:opacity-60"
            >
              {dispatching ? t('secplane.runtime.dispatchPicker.busyLabel') : t('secplane.runtime.dispatchPicker.sendToSelected', { count: selectedInstanceIDs.size })}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};

export default DispatchPickerModal;
