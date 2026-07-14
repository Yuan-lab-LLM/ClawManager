import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { secplaneService, type SecplaneRule } from '../../../../services/secplaneService';
import { useI18n } from '../../../../contexts/I18nContext';

// Manages add / remove for one kind of protected_* rule (path / skill / plugin).
// Backed by the existing /policy/rules endpoints — same shape as the old
// InputDetectionPage uses, just packaged for reuse from the new scenario pages.

type Kind = 'protected_path' | 'protected_skill' | 'protected_plugin';
type Prefix = 'pp' | 'psk' | 'ppl';

const KIND_TO_PREFIX: Record<Kind, Prefix> = {
  protected_path: 'pp',
  protected_skill: 'psk',
  protected_plugin: 'ppl',
};

// rule_id slug: <prefix>.<safe-slug>. Mirrors slugifyResource() in the legacy
// InputDetectionPage so rules created from both UIs share the same namespace.
function slugifyResource(prefix: Prefix, value: string): string {
  const trimmed = value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._/-]+/g, '-')
    .replace(/^-+|-+$/g, '');
  return `${prefix}.${trimmed || Date.now().toString(36)}`;
}

interface Props {
  kind: Kind;
  title: string;
  placeholder: string;
  helpText?: string;
}

const ProtectedResourceList: React.FC<Props> = ({ kind, title, placeholder, helpText }) => {
  const { t } = useI18n();
  const [rules, setRules] = useState<SecplaneRule[]>([]);
  const [draft, setDraft] = useState('');
  const [busyId, setBusyId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const prefix = KIND_TO_PREFIX[kind];

  const load = useCallback(async () => {
    try {
      const items = await secplaneService.listRules(kind);
      setRules(items);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [kind]);

  useEffect(() => {
    load();
  }, [load]);

  const visible = useMemo(() => rules.filter((r) => r.is_enabled), [rules]);

  const handleAdd = async () => {
    const value = draft.trim();
    if (!value) return;
    const ruleID = slugifyResource(prefix, value);
    const next: SecplaneRule = {
      rule_id: ruleID,
      kind,
      display_name: value,
      pattern: value,
      target: 'user_input',
      severity: 'high',
      action: 'block',
      mode: 'enforce',
      is_enabled: true,
      sort_order: 700,
    };
    setBusyId(ruleID);
    setError(null);
    try {
      const saved = await secplaneService.saveRule(next);
      setRules((prev) => {
        const without = prev.filter((r) => r.rule_id !== saved.rule_id);
        return [...without, saved];
      });
      setDraft('');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyId(null);
    }
  };

  const handleRemove = async (ruleId: string) => {
    setBusyId(ruleId);
    setError(null);
    try {
      await secplaneService.disableRule(ruleId);
      setRules((prev) => prev.filter((r) => r.rule_id !== ruleId));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyId(null);
    }
  };

  return (
    <div className="panel-warm" style={{ padding: 18 }}>
      <div className="flex items-center justify-between mb-2">
        <div className="section-title">{title}</div>
        <span className="muted text-xs">{t('secplane.runtime.shared.effectiveItems', { count: visible.length })}</span>
      </div>
      {helpText && <div className="muted text-xs mb-3">{helpText}</div>}
      <div className="flex gap-2 mb-3">
        <input
          type="text"
          className="input"
          value={draft}
          placeholder={placeholder}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') handleAdd();
          }}
        />
        <button
          type="button"
          className="btn-primary btn-sm"
          disabled={!draft.trim() || busyId !== null}
          onClick={handleAdd}
        >
          {t('secplane.runtime.protectedResourceList.add')}
        </button>
      </div>
      {error && (
        <div className="alert alert-danger mb-3" style={{ padding: '8px 12px', fontSize: 12 }}>
          {error}
        </div>
      )}
      <ul className="space-y-1">
        {visible.map((r) => (
          <li
            key={r.rule_id}
            className="flex items-center gap-2 rounded-lg border border-[#eadfd8] bg-white px-3 py-2"
          >
            <code className="flex-1 truncate font-mono text-xs text-[#171212]" title={r.pattern}>
              {r.pattern}
            </code>
            <button
              type="button"
              className="text-xs hover:underline"
              style={{ color: '#b42318', background: 'none', border: 'none', cursor: 'pointer', padding: 0 }}
              disabled={busyId === r.rule_id}
              onClick={() => handleRemove(r.rule_id)}
            >
              {busyId === r.rule_id ? t('secplane.runtime.protectedResourceList.removing') : t('secplane.runtime.protectedResourceList.remove')}
            </button>
          </li>
        ))}
        {visible.length === 0 && <li className="muted text-xs">{t('secplane.runtime.protectedResourceList.noItems')}</li>}
      </ul>
      <div className="muted-strong text-xs mt-3" style={{ fontFamily: 'ui-monospace, monospace' }}>
        {t('secplane.runtime.protectedResourceList.ruleIdPrefix')}<code>{prefix}.*</code>
      </div>
    </div>
  );
};

export default ProtectedResourceList;
