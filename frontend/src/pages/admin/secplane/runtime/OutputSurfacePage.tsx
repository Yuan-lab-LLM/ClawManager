import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../../components/AdminLayout';
import ApplyDispatchButton from '../../../../components/secplane/ApplyDispatchButton';
import { useInstanceHealth } from './useInstanceHealth';
import { useSurfaceBackend } from './useSurfaceBackend';
import { FEATURES } from '../../../../config/features';
import { useI18n } from '../../../../contexts/I18nContext';

// Output Surface Protection (scenario d)
// Backend: defense.outputRedaction toggle + apply + real-time redaction alerts

const ALERT_PREFIXES = ['defense.outputRedaction', 'output_redaction'];

type Tone = 'red' | 'orange' | 'amber' | 'blue' | 'purple' | 'green' | 'slate';

type Category = 'credential' | 'pii' | 'pci' | 'network';

const PRIVACY_RULES: Array<[string, string, Category, string, Tone, string, string]> = [
  ['api-key',     'apiKey',     'credential',  'OpenAI / Anthropic / AWS / GCP API keys',     'red',    'critical', 'sk-***  /  sk-ant-***'],
  ['jwt',         'jwt',   'credential',  'JSON Web Token (3-segment base64)',                 'red',    'critical', 'eyJ***.***.***'],
  ['aws-secret',  'awsSecret',  'credential',  'aws_access_key_id / aws_secret_access_key',    'red',    'critical', 'AKIA***'],
  ['ssh-key',     'sshKey',     'credential',  'Private key header / passphrase',                         'red',    'critical', '-----BEGIN ***-----'],
  ['id-card',     'idCard',     'pii',  'Chinese mainland 18-digit ID (incl. check digit)',                'red',    'critical', '310***********X'],
  ['email',       'email',       'pii',   'Email address (incl. username / domain)',                    'orange', 'high',   '***@***.com'],
  ['phone',       'phone',       'pii',   'Chinese / international phone numbers',                              'orange', 'high',   '138****5678'],
  ['credit-card', 'creditCard', 'pci',   'Visa / Master / Amex / UnionPay card numbers (Luhn check)',   'red',    'critical', '****-****-****-1234'],
  ['ip-addr',     'ipAddr',     'network',  'Internal IP / Public IP / IPv6',                      'amber',  'medium',   '10.***.***.***'],
];

const CRED_ALERTS: Array<[string, string, string, Tone, string]> = [
  ['openclaw-prod-east-12', '/etc/openclaw/config.yaml:42', 'AWS Secret', 'red', 'critical'],
  ['openclaw-finance-svc', '~/.openai-config.json:8', 'API Key', 'red', 'critical'],
  ['openclaw-finance-svc', 'skill-finance/handler.js:87', 'API Key', 'red', 'critical'],
  ['openclaw-ops-bot-3', 'secret/db-conn.env:12', 'DB Password', 'orange', 'high'],
  ['openclaw-staging-7', 'skills/qa-bot/keys.txt:1', 'JWT', 'red', 'critical'],
];

const catBadge = (c: Category) => (c === 'credential' || c === 'pci' ? 'badge-red' : c === 'pii' ? 'badge-orange' : 'badge-slate');

const OutputSurfacePage: React.FC = () => {
  const { t } = useI18n();
  const { alerts, dispatching, dispatchMsg, modeOf, setMode, dispatchApply } = useSurfaceBackend(ALERT_PREFIXES);
  const { instances, healthy } = useInstanceHealth();
  const enabled = modeOf('defense.outputRedaction', 'enforce') !== 'off';
  const toggleEnabled = () => setMode('defense.outputRedaction', enabled ? 'off' : 'enforce');
  const [resolved, setResolved] = useState<Set<number>>(new Set());

  const exportJsonl = () => {
    const text = alerts.map((a) => JSON.stringify(a)).join('\n');
    const blob = new Blob([text], { type: 'application/jsonl' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `output-surface-alerts-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '')}.jsonl`;
    link.click();
    URL.revokeObjectURL(url);
  };

  return (
    <AdminLayout title={t('secplane.runtime.shared.crumbSecurity')}>
      <div className="cm-content space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">{t('secplane.runtime.shared.crumbSecurity')}</Link>
          <span>/</span>
          <Link to="/admin/secplane/runtime">{t('secplane.runtime.shared.crumbRuntime')}</Link>
          <span>/</span>
          <span className="crumb-current">{t('secplane.runtime.outputSurface.crumbCurrent')}</span>
        </div>

        <div className="panel">
          <div className="hero-block">
            <div className="h-eyebrow">{t('secplane.runtime.outputSurface.heroEyebrow')}</div>
            <h2 className="h-title">{t('secplane.runtime.outputSurface.heroTitle')}</h2>
            <p className="h-subtitle">{t('secplane.runtime.outputSurface.heroSubtitle')}</p>
          </div>
          <div className="grid grid-cols-4 gap-3 mt-5">
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.runtime.outputSurface.redactionToggle')}</div>
              <div className={`stat-card-value ${enabled ? 'tone-green' : 'tone-orange'}`}>{enabled ? t('secplane.runtime.shared.enabled') : t('secplane.runtime.shared.modeOff')}</div>
              <div className="stat-card-sub muted-strong">outputRedaction · before_message_write</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.runtime.shared.statAlerts')}</div>
              <div className={`stat-card-value ${alerts.length > 0 ? 'tone-red' : 'tone-green'}`}>{alerts.length}</div>
              <div className="stat-card-sub muted-strong">{t('secplane.runtime.shared.statAlertsSub')}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.runtime.shared.statInstances')}</div>
              <div className="stat-card-value">{instances.length}</div>
              <div className="stat-card-sub muted-strong">{healthy.length} running</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">{t('secplane.runtime.shared.statChannel')}</div>
              <div className="stat-card-value" style={{ fontSize: '1rem' }}>install_skill</div>
              <div className="stat-card-sub muted-strong">hot-reload via mtime</div>
            </div>
          </div>
        </div>

        <div className="panel">
          <div className="flex items-center justify-between mb-4 gap-4">
            <div>
              <div className="eyebrow">{t('secplane.runtime.outputSurface.rulesEyebrow')}</div>
              <h3 className="section-title-lg mt-1">{t('secplane.runtime.outputSurface.rulesTitle')}</h3>
            </div>
            <div className="flex items-center gap-3">
              <span className="text-xs muted-strong">{t('secplane.runtime.outputSurface.redactionToggleLabel')}</span>
              <button
                role="switch"
                aria-checked={enabled}
                onClick={toggleEnabled}
                style={{ width: 38, height: 22, borderRadius: 11, background: enabled ? '#2563eb' : '#cbd5e1', position: 'relative', cursor: 'pointer', flexShrink: 0, transition: 'background .15s', border: 'none' }}
              >
                <div style={{ width: 18, height: 18, borderRadius: 9, background: 'white', position: 'absolute', top: 2, left: enabled ? 18 : 2, transition: 'left .15s', boxShadow: '0 1px 3px rgba(0,0,0,0.15)' }} />
              </button>
              <ApplyDispatchButton onDispatch={dispatchApply} busy={dispatching} className="btn-primary btn-sm" triggerLabel={t('secplane.runtime.shared.saveApply')} />
              {dispatchMsg && <span className="text-xs muted ml-2">{dispatchMsg}</span>}
            </div>
          </div>
          <div className="space-y-2.5">
            {PRIVACY_RULES.map(([key, ruleKey, category, _desc, tone, sevKey, mask]) => (
              <div key={key} className="flex items-center gap-4 p-4 rounded-2xl border border-[#eadfd8] bg-white">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1 flex-wrap">
                    <span className="font-semibold text-[#171212]">{t(`secplane.runtime.outputSurface.privacyRules.${ruleKey}.name`)}</span>
                    <span className={`badge ${catBadge(category)} text-[9px]`}>{t(`secplane.runtime.shared.${category}`)}</span>
                    <span className={`badge badge-${tone} text-[9px]`}>{t(`secplane.runtime.shared.${sevKey}`)}</span>
                  </div>
                  <div className="text-xs muted mb-1">{t(`secplane.runtime.outputSurface.privacyRules.${ruleKey}.desc`)}</div>
                  <code className="block text-[10px] muted-strong bg-[#fdf6f1] px-2 py-1 rounded font-mono truncate" style={{ maxWidth: 420 }}>
                    {t('secplane.runtime.outputSurface.maskExample')}{mask}
                  </code>
                </div>
              </div>
            ))}
          </div>
        </div>

        {FEATURES.credentialInventory && <div className="panel">
          <div className="flex items-center justify-between mb-4">
            <div>
              <div className="eyebrow">{t('secplane.runtime.outputSurface.credInventoryEyebrow')}</div>
              <h3 className="section-title-lg mt-1">{t('secplane.runtime.outputSurface.credInventoryTitle')}</h3>
            </div>
            <button className="btn-primary btn-sm">{t('secplane.runtime.outputSurface.scanNow')}</button>
          </div>
          <div className="grid grid-cols-2 gap-2">
            {CRED_ALERTS.map(([inst, loc, type, tone, sevKey], i) => {
              const isResolved = resolved.has(i);
              return (
                <div
                  key={i}
                  className="p-3 rounded-xl border border-[#f4b6b3] bg-[#fdeded]"
                  style={isResolved ? { opacity: 0.55, background: '#f5f5f4', textDecoration: 'line-through' } : {}}
                >
                  <div className="flex items-center gap-2 mb-1">
                    <span className="text-[9px] muted-strong tracking-wider">{t('secplane.runtime.outputSurface.instance')}</span>
                    <code className="text-[11px] font-mono text-[#7a4a30]">{inst}</code>
                  </div>
                  <code className="text-xs text-[#b42318] font-mono break-all block">{loc}</code>
                  <div className="flex items-center justify-between mt-2">
                    <div className="flex items-center gap-1.5">
                      <span className={`badge badge-${tone} text-[9px]`}>{t(`secplane.runtime.shared.${sevKey}`)}</span>
                      <span className="text-xs muted-strong">{type}</span>
                    </div>
                    <button
                      className="text-xs tone-red font-semibold hover:underline"
                      onClick={() =>
                        setResolved((s) => {
                          const n = new Set(s);
                          if (n.has(i)) n.delete(i);
                          else n.add(i);
                          return n;
                        })
                      }
                      style={isResolved ? { color: '#059669', textDecoration: 'none' } : {}}
                    >
                      {isResolved ? t('secplane.runtime.outputSurface.resolvedUndo') : t('secplane.runtime.outputSurface.markResolved')}
                    </button>
                  </div>
                </div>
              );
            })}
          </div>
        </div>}

        <div className="panel">
          <div className="flex items-center justify-between mb-4">
            <div>
              <div className="eyebrow">{t('secplane.runtime.outputSurface.eventsEyebrow')}</div>
              <h3 className="section-title-lg mt-1">{t('secplane.runtime.outputSurface.eventsTitle')}</h3>
            </div>
            <button
              className="btn-secondary btn-sm"
              onClick={exportJsonl}
              disabled={alerts.length === 0}
            >
              {t('secplane.runtime.shared.exportJsonl')}
            </button>
          </div>
          <table className="tbl">
            <thead>
              <tr>
                <th style={{ width: 80 }}>{t('secplane.runtime.shared.colTime')}</th>
                <th>{t('secplane.runtime.shared.colInstance')}</th>
                <th>{t('secplane.runtime.shared.colRule')}</th>
                <th>{t('secplane.runtime.shared.colOriginal')}</th>
                <th style={{ width: 80 }}>{t('secplane.runtime.shared.colAction')}</th>
              </tr>
            </thead>
            <tbody>
              {alerts.length === 0 && (
                <tr>
                  <td colSpan={5} className="text-xs muted" style={{ textAlign: 'center', padding: 20 }}>
                    {t('secplane.runtime.outputSurface.noRedactionEvents')}
                  </td>
                </tr>
              )}
              {alerts.slice(0, 50).map((a) => (
                <tr key={a.id}>
                  <td><span className="muted-strong text-xs">{a.ts?.replace('T', ' ').slice(11, 19)}</span></td>
                  <td><span className="font-mono text-xs">{a.agent_id ?? '—'}</span></td>
                  <td><span className="badge badge-red">{a.rule_name ?? a.rule_id ?? '—'}</span></td>
                  <td><code className="text-xs text-[#171212] truncate inline-block" style={{ maxWidth: 340 }}>{a.evidence ?? '—'}</code></td>
                  <td><span className={`badge badge-${a.action === 'block' ? 'red' : a.action === 'redact' ? 'orange' : 'slate'}`}>{a.action}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
          {alerts.length > 0 && (
            <div className="text-xs muted mt-3 text-center">
              {t('secplane.runtime.shared.totalRows', { count: alerts.length })}
            </div>
          )}
        </div>
      </div>

    </AdminLayout>
  );
};

export default OutputSurfacePage;
