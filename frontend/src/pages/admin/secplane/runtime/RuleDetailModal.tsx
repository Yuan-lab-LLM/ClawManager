import React from 'react';
import { useI18n } from '../../../../contexts/I18nContext';

// Static reference data for the "View Rule" modal. The actual regexes live in
// the ClawAegisEx plugin source (rules.ts etc.); we surface representative
// samples here so operators can see what each defense matches. Kept in sync
// with the plugin manually — when a new rule is added there, update the
// corresponding entry in the page's RULE_MODAL_DATA map.

export type RuleTone = 'red' | 'orange' | 'amber' | 'slate';

export interface RuleCategory {
  flag: string;
  name: string;
  tone: RuleTone;
  hits?: number;
  regex: string[];
  examples?: string[];
  maskExample?: string;       // for output-redaction patterns
}

export interface RuleSection {
  name: string;
  items: string[];
}

export type RuleModalData =
  | { title: string; subtitle: string; type: 'patterns'; categories: RuleCategory[] }
  | { title: string; subtitle: string; type: 'injectedText'; sections: RuleSection[] };

const TONE_TO_BADGE: Record<RuleTone, string> = {
  red: 'badge-red',
  orange: 'badge-orange',
  amber: 'badge-amber',
  slate: 'badge-slate',
};

interface Props {
  data: RuleModalData;
  onClose: () => void;
}

const RuleDetailModal: React.FC<Props> = ({ data, onClose }) => {
  const { t } = useI18n();

  return (
    <div className="secp-modal-root">
      <div className="secp-modal-backdrop" onClick={onClose} />
      <div className="secp-modal-content">
        <div className="secp-modal-header">
          <div>
            <div className="eyebrow">{t('secplane.runtime.ruleDetailModal.eyebrow')}</div>
            <h3 className="secp-modal-title">{data.title}</h3>
            <div className="muted text-xs mt-1">{data.subtitle}</div>
          </div>
          <button type="button" className="icon-btn" onClick={onClose} aria-label={t('secplane.runtime.ruleDetailModal.close') ?? 'Close'}>
            ×
          </button>
        </div>
        <div className="secp-modal-body">
          {data.type === 'patterns'
            ? data.categories.map((c, idx) => (
                <div key={c.flag} style={{ marginBottom: idx === data.categories.length - 1 ? 0 : 20 }}>
                  <div className="flex items-center justify-between mb-2">
                    <div className="flex items-center gap-2">
                      <code className="text-xs font-bold text-[#171212]">{c.flag}</code>
                      <span className="text-sm text-[#171212]">{c.name}</span>
                    </div>
                    {c.hits !== undefined && (
                      <span className={`badge ${TONE_TO_BADGE[c.tone]}`}>{c.hits} hits / 24h</span>
                    )}
                  </div>
                  <div className="muted-strong text-xs mb-1">{t('secplane.runtime.ruleDetailModal.regexLabel', { count: c.regex.length })}</div>
                  <div className="flex flex-col gap-1 mb-3">
                    {c.regex.map((r, i) => (
                      <code
                        key={i}
                        className="block text-xs rounded-md px-3 py-1.5"
                        style={{ background: '#fdf6f1', color: '#7a4a30', wordBreak: 'break-all' }}
                      >
                        {r}
                      </code>
                    ))}
                  </div>
                  {c.maskExample && (
                    <>
                      <div className="muted-strong text-xs mb-1">{t('secplane.runtime.ruleDetailModal.maskExample')}</div>
                      <code
                        className="block text-xs rounded-md px-3 py-1.5 mb-3"
                        style={{ background: '#fdf6f1', color: '#171212' }}
                      >
                        {c.maskExample}
                      </code>
                    </>
                  )}
                  {c.examples && c.examples.length > 0 && (
                    <>
                      <div className="muted-strong text-xs mb-1">{t('secplane.runtime.ruleDetailModal.hitExample')}</div>
                      <div className="flex flex-col gap-1">
                        {c.examples.map((e, i) => (
                          <div
                            key={i}
                            className="muted text-xs italic px-3 py-1"
                            style={{ borderLeft: '2px solid #eadfd8' }}
                          >
                            "{e}"
                          </div>
                        ))}
                      </div>
                    </>
                  )}
                  {idx !== data.categories.length - 1 && <div className="divider" />}
                </div>
              ))
            : data.sections.map((s, idx) => (
                <div key={s.name} style={{ marginBottom: idx === data.sections.length - 1 ? 0 : 16 }}>
                  <div className="text-sm font-semibold text-[#171212] mb-2">{s.name}</div>
                  <div className="flex flex-col gap-1.5">
                    {s.items.map((it, i) => (
                      <div
                        key={i}
                        className="text-xs text-[#171212] rounded-md px-3 py-2"
                        style={{ background: '#fdf6f1', lineHeight: 1.6 }}
                      >
                        <span className="muted-strong mr-2">{i + 1}.</span>
                        {it}
                      </div>
                    ))}
                  </div>
                </div>
              ))}
        </div>
        <div className="secp-modal-footer">
          <button type="button" className="btn-secondary btn-sm" onClick={onClose}>
            {t('secplane.runtime.ruleDetailModal.close')}
          </button>
        </div>
      </div>
    </div>
  );
};

export default RuleDetailModal;
