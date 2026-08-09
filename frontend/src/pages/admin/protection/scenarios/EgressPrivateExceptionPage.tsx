import React, { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../../components/AdminLayout';
import { useI18n } from '../../../../contexts/I18nContext';
import { adminInstanceService } from '../../../../services/adminInstanceService';
import {
  egressPrivateExceptionService,
  type EgressPrivateException,
  type EgressPrivateScopeType,
} from '../../../../services/egressPrivateExceptionService';
import { userService } from '../../../../services/userService';

type Option = { id: number; label: string };

const DEFAULT_CIDR = '10.255.25.3/32';
const DEFAULT_PORT = 18080;

const EgressPrivateExceptionPage: React.FC = () => {
  const { t } = useI18n();
  const k = 'secplane.protection.egressPrivate';

  const [items, setItems] = useState<EgressPrivateException[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);

  const [scopeType, setScopeType] = useState<EgressPrivateScopeType>('instance');
  const [scopeId, setScopeId] = useState<number>(0);
  const [cidr, setCidr] = useState(DEFAULT_CIDR);
  const [port, setPort] = useState(DEFAULT_PORT);
  const [description, setDescription] = useState('');
  const [enabled, setEnabled] = useState(true);

  const [instances, setInstances] = useState<Option[]>([]);
  const [users, setUsers] = useState<Option[]>([]);

  const resetCreateForm = useCallback(() => {
    setEditingId(null);
    setScopeType('instance');
    setCidr(DEFAULT_CIDR);
    setPort(DEFAULT_PORT);
    setDescription('');
    setEnabled(true);
    setScopeId(instances[0]?.id ?? 0);
  }, [instances]);

  const loadItems = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setItems(await egressPrivateExceptionService.list());
    } catch (e) {
      const err = e as { message?: string };
      setError(err.message ?? t(`${k}.loadFail`));
    } finally {
      setLoading(false);
    }
  }, [k, t]);

  const loadOptions = useCallback(async () => {
    try {
      const [instanceResp, userResp] = await Promise.all([
        adminInstanceService.getInstances(1, 200),
        userService.getUsers(1, 200),
      ]);
      setInstances(
        (instanceResp.instances ?? []).map((item) => ({
          id: item.id,
          label: `${item.name} (#${item.id})`,
        })),
      );
      setUsers(
        (userResp.users ?? []).map((item) => ({
          id: item.id,
          label: `${item.username} (#${item.id})`,
        })),
      );
    } catch {
      setInstances([]);
      setUsers([]);
    }
  }, []);

  useEffect(() => {
    loadItems();
    loadOptions();
  }, [loadItems, loadOptions]);

  useEffect(() => {
    if (editingId !== null) return;
    const options = scopeType === 'instance' ? instances : users;
    if (options.length > 0 && !options.some((item) => item.id === scopeId)) {
      setScopeId(options[0].id);
    }
  }, [scopeType, instances, users, scopeId, editingId]);

  const onEdit = (item: EgressPrivateException) => {
    setEditingId(item.id);
    setScopeType(item.scope_type);
    setScopeId(item.scope_id);
    setCidr(item.cidr);
    setPort(item.port);
    setDescription(item.description ?? '');
    setEnabled(item.enabled);
    setError(null);
  };

  const onCancelEdit = () => {
    resetCreateForm();
    setError(null);
  };

  const onSubmit = async () => {
    if (!scopeId || !cidr.trim() || port <= 0) return;
    setSaving(true);
    setError(null);
    const payload = {
      scope_type: scopeType,
      scope_id: scopeId,
      cidr: cidr.trim(),
      port,
      enabled,
      description: description.trim() || undefined,
    };
    try {
      if (editingId !== null) {
        await egressPrivateExceptionService.update(editingId, payload);
        resetCreateForm();
      } else {
        await egressPrivateExceptionService.create(payload);
        setDescription('');
      }
      await loadItems();
    } catch (e) {
      const err = e as { response?: { data?: { error?: string } }; message?: string };
      setError(err.response?.data?.error ?? err.message ?? t(`${k}.saveFail`));
    } finally {
      setSaving(false);
    }
  };

  const onDelete = async (id: number) => {
    if (!window.confirm(t(`${k}.deleteConfirm`))) return;
    try {
      await egressPrivateExceptionService.remove(id);
      if (editingId === id) {
        resetCreateForm();
      }
      await loadItems();
    } catch (e) {
      const err = e as { response?: { data?: { error?: string } }; message?: string };
      setError(err.response?.data?.error ?? err.message ?? t(`${k}.deleteFail`));
    }
  };

  const scopeOptions = scopeType === 'instance' ? instances : users;
  const isEditing = editingId !== null;

  return (
    <AdminLayout>
      <div className="cm-content space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">{t(`${k}.breadcrumb1`)}</Link>
          <span>/</span>
          <Link to="/admin/secplane/cat-trust">{t(`${k}.breadcrumb2`)}</Link>
          <span>/</span>
          <span className="crumb-current">{t(`${k}.breadcrumb3`)}</span>
        </div>

        <div className="panel">
          <div className="hero-block">
            <div className="h-eyebrow">{t(`${k}.eyebrow`)}</div>
            <h2 className="h-title">{t(`${k}.title`)}</h2>
            <p className="h-subtitle">{t(`${k}.subtitle`)}</p>
          </div>
          <div className="grid grid-cols-2 gap-3 mt-5 max-w-xl">
            <div className="stat-card">
              <div className="stat-card-label">{t(`${k}.statEntries`)}</div>
              <div className="stat-card-value">{items.length}</div>
              <div className="stat-card-sub muted-strong">{t(`${k}.statEntriesSub`)}</div>
            </div>
          </div>
        </div>

        <div className="panel space-y-4">
          <div className="flex items-center justify-between gap-3 flex-wrap">
            <h3 className="section-title-lg">
              {isEditing ? t(`${k}.formTitleEdit`) : t(`${k}.formTitle`)}
            </h3>
            <button type="button" className="btn-secondary btn-sm" onClick={loadItems} disabled={loading}>
              {t(`${k}.refresh`)}
            </button>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <label className="text-xs space-y-1">
              <span className="muted-strong">{t(`${k}.scopeType`)}</span>
              <select
                className="w-full border rounded-lg px-3 py-2"
                value={scopeType}
                onChange={(e) => setScopeType(e.target.value as EgressPrivateScopeType)}
              >
                <option value="instance">{t(`${k}.scopeInstance`)}</option>
                <option value="user">{t(`${k}.scopeUser`)}</option>
              </select>
            </label>

            <label className="text-xs space-y-1">
              <span className="muted-strong">{t(`${k}.scopeId`)}</span>
              <select
                className="w-full border rounded-lg px-3 py-2"
                value={scopeId || ''}
                onChange={(e) => setScopeId(Number(e.target.value))}
              >
                <option value="">
                  {scopeType === 'instance' ? t(`${k}.pickInstance`) : t(`${k}.pickUser`)}
                </option>
                {scopeOptions.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.label}
                  </option>
                ))}
              </select>
            </label>

            <label className="text-xs space-y-1">
              <span className="muted-strong">{t(`${k}.cidr`)}</span>
              <input
                className="w-full border rounded-lg px-3 py-2"
                value={cidr}
                onChange={(e) => setCidr(e.target.value)}
                placeholder={t(`${k}.cidrHint`)}
              />
            </label>

            <label className="text-xs space-y-1">
              <span className="muted-strong">{t(`${k}.port`)}</span>
              <input
                type="number"
                min={1}
                max={65535}
                className="w-full border rounded-lg px-3 py-2"
                value={port}
                onChange={(e) => setPort(Number(e.target.value))}
              />
            </label>

            <label className="text-xs space-y-1 md:col-span-2">
              <span className="muted-strong">{t(`${k}.description`)}</span>
              <input
                className="w-full border rounded-lg px-3 py-2"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
            </label>
          </div>

          <div className="flex items-center gap-4 flex-wrap">
            <label className="text-xs flex items-center gap-2">
              <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
              {t(`${k}.enabled`)}
            </label>
            <button
              type="button"
              className="btn-primary btn-sm"
              onClick={onSubmit}
              disabled={saving || !scopeId}
            >
              {isEditing ? t(`${k}.save`) : t(`${k}.create`)}
            </button>
            {isEditing && (
              <button type="button" className="btn-secondary btn-sm" onClick={onCancelEdit} disabled={saving}>
                {t(`${k}.cancel`)}
              </button>
            )}
          </div>

          {error && <div className="text-sm tone-red">{error}</div>}
        </div>

        <div className="panel overflow-x-auto">
          {loading && <div className="text-xs muted">{t(`${k}.refresh`)}…</div>}
          {!loading && items.length === 0 && <div className="text-sm muted">{t(`${k}.empty`)}</div>}
          {items.length > 0 && (
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left muted-strong">
                  <th className="py-2 pr-3">{t(`${k}.colScope`)}</th>
                  <th className="py-2 pr-3">{t(`${k}.colCidr`)}</th>
                  <th className="py-2 pr-3">{t(`${k}.colPort`)}</th>
                  <th className="py-2 pr-3">{t(`${k}.colEnabled`)}</th>
                  <th className="py-2 pr-3">{t(`${k}.colDesc`)}</th>
                  <th className="py-2 pr-3">{t(`${k}.colActions`)}</th>
                </tr>
              </thead>
              <tbody>
                {items.map((item) => (
                  <tr key={item.id} className="border-t border-[#eadfd8]">
                    <td className="py-2 pr-3">
                      {item.scope_type}:{item.scope_id}
                    </td>
                    <td className="py-2 pr-3 font-mono text-xs">{item.cidr}</td>
                    <td className="py-2 pr-3">{item.port}</td>
                    <td className="py-2 pr-3">{item.enabled ? '✓' : '—'}</td>
                    <td className="py-2 pr-3">{item.description || '—'}</td>
                    <td className="py-2 pr-3">
                      <div className="flex items-center gap-2">
                        <button type="button" className="btn-secondary btn-sm" onClick={() => onEdit(item)}>
                          {t(`${k}.edit`)}
                        </button>
                        <button type="button" className="btn-secondary btn-sm" onClick={() => onDelete(item.id)}>
                          {t(`${k}.delete`)}
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </AdminLayout>
  );
};

export default EgressPrivateExceptionPage;
