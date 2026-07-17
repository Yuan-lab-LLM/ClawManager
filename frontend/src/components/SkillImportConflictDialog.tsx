import React, { useEffect, useMemo, useState } from 'react';
import { useI18n } from '../contexts/I18nContext';
import type { SkillImportDecision, SkillImportPreviewItem } from '../types/skill';

type SkillImportConflictDialogProps = {
  open: boolean;
  items: SkillImportPreviewItem[];
  loading?: boolean;
  onConfirm: (decisions: SkillImportDecision[]) => void;
  onCancel: () => void;
};

const SkillImportConflictDialog: React.FC<SkillImportConflictDialogProps> = ({
  open,
  items,
  loading = false,
  onConfirm,
  onCancel,
}) => {
  const { t } = useI18n();
  const conflicts = useMemo(
    () => items.filter((item) => item.conflict_type === 'content_changed'),
    [items],
  );
  const [choices, setChoices] = useState<Record<string, 'new_version' | 'save_as_new' | 'skip'>>({});

  useEffect(() => {
    if (!open) {
      return;
    }
    const initial: Record<string, 'new_version' | 'save_as_new' | 'skip'> = {};
    conflicts.forEach((item) => {
      initial[item.directory_name] = 'new_version';
    });
    setChoices(initial);
  }, [conflicts, open]);

  if (!open || conflicts.length === 0) {
    return null;
  }

  const single = conflicts.length === 1 ? conflicts[0] : null;

  const handleConfirm = () => {
    const decisions: SkillImportDecision[] = items.map((item) => {
      if (item.conflict_type === 'unchanged') {
        return { directory_name: item.directory_name, action: 'skip' };
      }
      if (item.conflict_type === 'none') {
        return { directory_name: item.directory_name, action: 'new_version' };
      }
      const action = choices[item.directory_name] || 'new_version';
      if (action === 'save_as_new') {
        return {
          directory_name: item.directory_name,
          action,
          skill_key: item.suggested_skill_key,
        };
      }
      return { directory_name: item.directory_name, action };
    });
    onConfirm(decisions);
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4">
      <div className="w-full max-w-xl rounded-[24px] border border-[#efe2d8] bg-[#fffaf7] p-6 shadow-[0_30px_80px_-40px_rgba(72,44,24,0.55)]">
        <h3 className="text-lg font-semibold text-[#1d1713]">{t('skillHubPage.importConflict.title')}</h3>

        {single ? (
          <p className="mt-3 text-sm leading-6 text-[#6f6158]">
            {t('skillHubPage.importConflict.message', {
              name: single.existing_name || single.skill_key,
              version: single.current_version_no ?? 1,
            })}
          </p>
        ) : (
          <p className="mt-3 text-sm leading-6 text-[#6f6158]">{t('skillHubPage.importConflict.batchMessage')}</p>
        )}

        {single ? (
          <div className="mt-5 flex flex-col gap-2 sm:flex-row sm:flex-wrap">
            <button
              type="button"
              className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
              disabled={loading}
              onClick={() => onConfirm([
                ...items.filter((i) => i.conflict_type !== 'content_changed').map((item) => ({
                  directory_name: item.directory_name,
                  action: item.conflict_type === 'unchanged' ? 'skip' as const : 'new_version' as const,
                })),
                { directory_name: single.directory_name, action: 'new_version' },
              ])}
            >
              {t('skillHubPage.importConflict.newVersion')}
            </button>
            <button
              type="button"
              className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
              disabled={loading}
              onClick={() => onConfirm([
                ...items.filter((i) => i.conflict_type !== 'content_changed').map((item) => ({
                  directory_name: item.directory_name,
                  action: item.conflict_type === 'unchanged' ? 'skip' as const : 'new_version' as const,
                })),
                {
                  directory_name: single.directory_name,
                  action: 'save_as_new',
                  skill_key: single.suggested_skill_key,
                },
              ])}
            >
              {t('skillHubPage.importConflict.saveAsNew', {
                key: single.suggested_skill_key || `${single.skill_key}-2`,
              })}
            </button>
            <button type="button" className="app-button-secondary" disabled={loading} onClick={onCancel}>
              {t('skillHubPage.importConflict.cancel')}
            </button>
          </div>
        ) : (
          <>
            <div className="mt-4 space-y-3">
              {conflicts.map((item) => (
                <div
                  key={item.directory_name}
                  className="rounded-[18px] border border-[#efe2d8] bg-white px-4 py-3"
                >
                  <div className="text-sm font-medium text-[#1d1713]">{item.existing_name || item.skill_key}</div>
                  <div className="mt-1 text-xs text-[#7a6d66]">
                    {t('skillHubPage.importConflict.currentVersion', {
                      version: item.current_version_no ?? 1,
                    })}
                  </div>
                  <select
                    className="mt-3 w-full rounded-xl border border-[#ead8cf] bg-[#fffaf7] px-3 py-2 text-sm text-[#1d1713]"
                    value={choices[item.directory_name] || 'new_version'}
                    onChange={(event) =>
                      setChoices((current) => ({
                        ...current,
                        [item.directory_name]: event.target.value as 'new_version' | 'save_as_new' | 'skip',
                      }))
                    }
                  >
                    <option value="new_version">{t('skillHubPage.importConflict.newVersion')}</option>
                    <option value="save_as_new">
                      {t('skillHubPage.importConflict.saveAsNew', {
                        key: item.suggested_skill_key || `${item.skill_key}-2`,
                      })}
                    </option>
                    <option value="skip">{t('skillHubPage.importConflict.skip')}</option>
                  </select>
                </div>
              ))}
            </div>
            <div className="mt-5 flex flex-wrap gap-2">
              <button
                type="button"
                className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
                disabled={loading}
                onClick={handleConfirm}
              >
                {t('skillHubPage.importConflict.confirmBatch')}
              </button>
              <button type="button" className="app-button-secondary" disabled={loading} onClick={onCancel}>
                {t('skillHubPage.importConflict.cancel')}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
};

export default SkillImportConflictDialog;
