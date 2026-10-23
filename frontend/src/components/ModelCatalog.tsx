import { Fragment, useMemo, useState } from 'react';
import { useI18n } from '../contexts/I18nContext';
import { groupCatalog, type CatalogModel } from '../lib/modelCatalog';
import SelectionCheckbox from './SelectionCheckbox';
import { selectedRows, toggleSelection } from '../lib/modelSelection';
import { modelToolSupport } from '../lib/modelToolSupport';
import { modelVisionSupport } from '../lib/modelVisionSupport';

interface Props<T extends CatalogModel> {
  models: T[];
  busy: boolean;
  editing: boolean;
  onEdit: (model: T) => void;
  onDelete: (model: T) => void;
  onSaveSelected: (models: T[]) => void;
  onDeleteSelected: (models: T[]) => void;
}

export default function ModelCatalog<T extends CatalogModel>({ models, busy, editing, onEdit, onDelete, onSaveSelected, onDeleteSelected }: Props<T>) {
  const { t } = useI18n();
  const label = (key: string) => t(`modelManagementPage.${key}`);
  const [grouping, setGrouping] = useState<'url' | 'tier'>('url');
  const [search, setSearch] = useState('');
  const [tier, setTier] = useState('all');
  const [status, setStatus] = useState('all');
  const [visibility, setVisibility] = useState('any');
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [limits, setLimits] = useState<Record<string, number>>({});
  const [selection, setSelection] = useState<Set<string>>(new Set());
  const groups = useMemo(() => groupCatalog(models.filter((model) => visibility === 'any' || (model.visibility_scope || 'all') === visibility), grouping, search, tier, status), [models, grouping, search, tier, status, visibility]);
  const filtered = groups.flatMap((group) => group.items);
  const eligible = filtered.filter((model) => !model.saving);
  const selected = selectedRows(eligible, selection, (model) => model.local_id);
  const pendingSelected = selected.filter((model) => model.isNew);
  const locked = busy || editing || models.some((model) => model.saving);
  const select = (items: T[], checked: boolean) => setSelection((current) => toggleSelection(current, items.filter((model) => !model.saving).map((model) => model.local_id), checked));
  const count = groups.reduce((sum, group) => sum + group.items.length, 0);
  const resetPages = () => { setLimits({}); setSelection(new Set()); };
  return <div className="mt-6 space-y-4">
    <div className="flex flex-wrap items-end gap-3 rounded-xl border border-gray-200 bg-gray-50 p-3">
      <label className="min-w-48 flex-1 text-xs text-gray-600">{label('catalogSearch')}
        <input type="search" className="app-input mt-1 w-full" value={search} placeholder={label('catalogSearchHint')}
          onChange={(e) => { setSearch(e.target.value); resetPages(); }} />
      </label>
      <label className="text-xs text-gray-600">{label('catalogGroup')}
        <select className="app-input mt-1 block" value={grouping} onChange={(e) => { setGrouping(e.target.value as 'url' | 'tier'); resetPages(); }}>
          <option value="url">{label('catalogByURL')}</option><option value="tier">{label('catalogByTier')}</option>
        </select>
      </label>
      <label className="text-xs text-gray-600">{label('qualityTier')}
        <select className="app-input mt-1 block" value={tier} onChange={(e) => { setTier(e.target.value); resetPages(); }}>
          <option value="all">{label('catalogAllTiers')}</option>
          {[0, 1, 2, 3, 4].map((value) => <option key={value} value={value}>{label(`qualityTier${value}`)}</option>)}
        </select>
      </label>
      <label className="text-xs text-gray-600">{label('catalogStatus')}
        <select className="app-input mt-1 block" value={status} onChange={(e) => { setStatus(e.target.value); resetPages(); }}>
          {['all', 'active', 'inactive', 'pending'].map((value) => <option key={value} value={value}>{label(`catalogStatus_${value}`)}</option>)}
        </select>
      </label>
      <label className="text-xs text-gray-600">可见范围
        <select className="app-input mt-1 block" value={visibility} onChange={(e) => { setVisibility(e.target.value); resetPages(); }}>
          <option value="any">全部范围</option><option value="all">全员可用</option><option value="selected_users">指定用户</option>
        </select>
      </label>
    </div>
    <div className="flex flex-wrap items-center gap-3 rounded-xl border border-sky-100 bg-sky-50 p-3 text-xs">
      <label className="flex items-center gap-2"><SelectionCheckbox label={label('selectFiltered')}
        checked={eligible.length > 0 && selected.length === eligible.length} mixed={selected.length > 0 && selected.length < eligible.length}
        disabled={locked || eligible.length === 0} onChange={(checked) => select(eligible, checked)} />{label('selectFiltered')}</label>
      <span role="status">{t('modelManagementPage.selectionCount', { count: selected.length, total: filtered.length })}</span>
      {busy && <span role="status" className="text-sky-800">{label('batchWorking')}</span>}
      <button type="button" disabled={locked || selected.length === 0} onClick={() => setSelection(new Set())} className="text-sky-700 disabled:opacity-40">{label('clearSelection')}</button>
      <button type="button" disabled={locked || pendingSelected.length === 0} onClick={() => onSaveSelected(pendingSelected)} className="rounded-lg border border-emerald-200 bg-white px-3 py-2 text-emerald-800 disabled:opacity-40">{t('modelManagementPage.saveSelected', { count: pendingSelected.length })}</button>
      <button type="button" disabled={locked || selected.length === 0} onClick={() => onDeleteSelected(selected)} className="rounded-lg border border-red-200 bg-white px-3 py-2 text-red-700 disabled:opacity-40">{t('modelManagementPage.deleteSelected', { count: selected.length })}</button>
      <p className="w-full text-gray-500">{label('catalogSelectionHelp')}</p>
    </div>
    <div className="flex flex-wrap justify-between gap-2 text-xs text-gray-500">
      <span>{t('modelManagementPage.catalogCount', { count, total: models.length, groups: groups.length })}</span>
      <div className="flex gap-3">
        <button type="button" onClick={() => setCollapsed(new Set())}>{label('catalogExpand')}</button>
        <button type="button" onClick={() => setCollapsed(new Set(groups.map((group) => `${grouping}:${group.key}`)))}>{label('catalogCollapse')}</button>
      </div>
    </div>
    {groups.length === 0 && <div className="rounded-xl border border-dashed p-6 text-center text-sm text-gray-500">
      <p>{label('catalogNoMatches')}</p>
      <button type="button" className="mt-2 text-sky-700" onClick={() => { setSearch(''); setTier('all'); setStatus('all'); setVisibility('any'); resetPages(); }}>{label('catalogClear')}</button>
    </div>}
    {groups.map(({ key, items }) => {
      const groupId = `${grouping}:${key}`;
      const isCollapsed = collapsed.has(groupId);
      const limit = limits[groupId] ?? 10;
      const title = grouping === 'url' ? key || label('catalogNoURL') : label(`qualityTier${key}`);
      const pending = items.filter((model) => model.isNew).length;
      return <section key={groupId} className="overflow-hidden rounded-xl border border-[#ead8cf] bg-white">
        <button type="button" className="flex w-full items-center gap-3 bg-[#fff8f5] px-4 py-3 text-left" aria-expanded={!isCollapsed}
          onClick={() => setCollapsed((current) => { const next = new Set(current); if (next.has(groupId)) next.delete(groupId); else next.add(groupId); return next; })}>
          <span aria-hidden="true">{isCollapsed ? '▸' : '▾'}</span>
          <span className="min-w-0 flex-1 break-all text-sm font-semibold text-gray-800">{title}</span>
          <span className="shrink-0 text-xs text-gray-500">{t('modelManagementPage.catalogGroupCount', { count: items.length })}</span>
          {pending > 0 && <span className="shrink-0 rounded-full bg-amber-100 px-2 py-1 text-xs text-amber-800">{t('modelManagementPage.catalogPendingCount', { count: pending })}</span>}
        </button>
        {!isCollapsed && <>
          <div className="overflow-x-auto"><table className="w-full min-w-[680px] text-left text-sm">
            <thead className="border-y border-gray-100 bg-gray-50 text-xs text-gray-500"><tr>
              <th scope="col" className="w-10 px-4 py-2"><SelectionCheckbox label={`${label('selectGroup')} ${title}`}
                checked={items.length > 0 && items.every((model) => selection.has(model.local_id))}
                mixed={items.some((model) => selection.has(model.local_id)) && !items.every((model) => selection.has(model.local_id))}
                disabled={locked} onChange={(checked) => select(items, checked)} /></th>
              {['providerModel', 'qualityTier', 'catalogStatus', 'catalogActions'].map((field) => <th key={field} scope="col" className="px-4 py-2 font-medium">{label(field)}</th>)}
            </tr></thead>
            <tbody className="divide-y divide-gray-100">{items.slice(0, limit).map((model) => <Fragment key={model.local_id}>
              <tr className="align-top hover:bg-gray-50/60">
                <td className="px-4 py-3"><SelectionCheckbox label={`${label('selectModel')} ${model.display_name || model.provider_model_name}`}
                  checked={selection.has(model.local_id)} disabled={locked || model.saving} onChange={(checked) => select([model], checked)} /></td>
                <td className="max-w-[360px] px-4 py-3">
                  <div className="break-words font-medium text-gray-900">{model.display_name || model.provider_model_name || '-'}</div>
                  <div className="mt-1 text-xs text-sky-700">可见范围：{model.visibility_scope === 'selected_users' ? '指定用户 · ' + (model.allowed_user_ids?.length ?? 0) + ' 人' : '全员可用'}</div>
                  <div className="mt-1 break-all text-xs text-gray-500">{grouping === 'tier' ? model.base_url : model.provider_model_name}</div>
                  <details className="mt-2 text-xs text-gray-500">
                    <summary className="w-fit cursor-pointer text-sky-700">{label('catalogDetails')}</summary>
                    <dl className="mt-2 grid gap-2 rounded-lg bg-gray-50 p-3">
                      {[
                        [label('baseUrl'), model.base_url], [label('providerModel'), model.provider_model_name],
                        [label('modelKind'), label(`modelKind_${model.model_kind || 'chat'}`)],
                        [label('imageGenerationEnabled'), label(model.image_generation_enabled ? 'visionSupported' : 'visionUnsupported')],
                        [label('imageEditEnabled'), label(model.image_edit_enabled ? 'visionSupported' : 'visionUnsupported')],
                        ...(model.model_kind === 'image' ? [[label('imagePricePerImage'), model.image_price_per_image == null ? label('imagePriceUnset') : `${model.image_price_per_image} USD`]] : []),
                        [label('modelGroup'), model.model_group], [label('deploymentPriority'), model.deployment_priority ?? 100],
                        [label('contextWindowTokens'), model.context_window_tokens ? model.context_window_tokens.toLocaleString() : label('contextWindowUnknown')],
                        [label('maxConcurrentRequests'), model.max_concurrent_requests || label('maxConcurrentUnlimited')],
                        [label('protocol'), model.protocol_type || model.provider_type],
                        [label('inputPriceShort'), `${model.input_price} ${model.currency}`], [label('outputPriceShort'), `${model.output_price} ${model.currency}`],
                          [label('reasoning'), model.supports_reasoning ? (model.reasoning_enabled ? label('reasoningEnabled') : label('reasoningDisabled')) : label('reasoningUnmanaged')],
                        [label('supportsTools'), label(modelToolSupport(model) === null ? 'toolsUnknown' : modelToolSupport(model) ? 'toolsSupported' : 'toolsUnsupported')],
                        [label('supportsVision'), label(modelVisionSupport(model) === null ? 'visionUnknown' : modelVisionSupport(model) ? 'visionSupported' : 'visionUnsupported')],
                        [t('common.description'), model.description || '-'],
                      ].map(([name, value]) => <div key={String(name)}><dt className="font-medium">{name}</dt><dd className="break-all whitespace-pre-wrap">{value || '-'}</dd></div>)}
                    </dl>
                  </details>
                  {model.error && <p role="alert" className="mt-2 text-xs text-red-700">{model.error}</p>}
                </td>
                <td className="px-4 py-3 text-xs text-gray-600">{model.model_kind && model.model_kind !== 'chat' ? label(`modelKind_${model.model_kind}`) : label(`qualityTier${model.quality_tier ?? 0}`)}</td>
                <td className="px-4 py-3"><div className="flex flex-wrap gap-1 text-xs">
                  {model.isNew && <span className="rounded-full bg-amber-50 px-2 py-1 text-amber-800">{label('catalogStatus_pending')}</span>}
                  <span className={`rounded-full px-2 py-1 ${model.is_active ? 'bg-green-50 text-green-700' : 'bg-gray-100 text-gray-600'}`}>{label(model.is_active ? 'active' : 'inactive')}</span>
                  {model.is_secure && <span className="rounded-full bg-emerald-50 px-2 py-1 text-emerald-700">{label('secure')}</span>}
                </div></td>
                <td className="px-4 py-3"><div className="flex gap-3 text-xs">
                  <button type="button" className="text-sky-700 disabled:opacity-40" disabled={locked} onClick={() => { setSelection(new Set()); onEdit(model); }}>{label('edit')}</button>
                  <button type="button" className="text-red-700 disabled:opacity-40" disabled={locked} onClick={() => onDelete(model)}>{t('common.delete')}</button>
                </div></td>
              </tr>
            </Fragment>)}</tbody>
          </table></div>
          {items.length > limit && <button type="button" className="w-full border-t py-3 text-xs text-sky-700 hover:bg-sky-50" onClick={() => setLimits((current) => ({ ...current, [groupId]: limit + 10 }))}>
            {t('modelManagementPage.catalogMore', { count: items.length - limit })}
          </button>}
        </>}
      </section>;
    })}
  </div>;
}
