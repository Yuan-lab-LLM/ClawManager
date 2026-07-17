import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import SkillImportConflictDialog from '../../components/SkillImportConflictDialog';
import UserLayout from '../../components/UserLayout';
import { useAuth } from '../../contexts/AuthContext';
import { useI18n } from '../../contexts/I18nContext';
import { instanceService } from '../../services/instanceService';
import { skillHubService } from '../../services/skillHubService';
import type { Instance } from '../../types/instance';
import type {
  Skill,
  SkillHubTag,
  SkillImportDecision,
  SkillImportPreviewItem,
  SkillImportResultItem,
} from '../../types/skill';

type HubTab = 'catalog' | 'mine' | 'admin';

const SkillHubPage: React.FC = () => {
  const { user } = useAuth();
  const { t } = useI18n();
  const [tab, setTab] = useState<HubTab>('catalog');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [tags, setTags] = useState<SkillHubTag[]>([]);
  const [catalogSkills, setCatalogSkills] = useState<Skill[]>([]);
  const [mySkills, setMySkills] = useState<Skill[]>([]);
  const [adminSkills, setAdminSkills] = useState<Skill[]>([]);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [search, setSearch] = useState('');
  const [selectedTag, setSelectedTag] = useState('');
  const [uploadFiles, setUploadFiles] = useState<File[]>([]);
  const [publishSkillId, setPublishSkillId] = useState<number | null>(null);
  const [editTagsSkillId, setEditTagsSkillId] = useState<number | null>(null);
  const [selectedTagIds, setSelectedTagIds] = useState<number[]>([]);
  const [installSkillId, setInstallSkillId] = useState<number | null>(null);
  const [selectedInstanceId, setSelectedInstanceId] = useState<number | ''>('');
  const [actionLoading, setActionLoading] = useState('');
  const [importPreviewItems, setImportPreviewItems] = useState<SkillImportPreviewItem[]>([]);
  const [importDialogOpen, setImportDialogOpen] = useState(false);
  const [pendingUploadFile, setPendingUploadFile] = useState<File | null>(null);

  const tagLabel = useCallback(
    (tag: SkillHubTag) => t(`skillHubPage.tags.${tag.tag_key}`) || tag.name,
    [t],
  );

  const loadBase = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [tagItems, instanceResponse] = await Promise.all([
        skillHubService.listTags(),
        instanceService.getInstances(1, 100),
      ]);
      setTags(tagItems);
      setInstances(instanceResponse.instances || []);
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : t('skillHubPage.errors.load');
      setError(message);
    } finally {
      setLoading(false);
    }
  }, [t]);

  const loadCatalog = useCallback(async () => {
    try {
      const result = await skillHubService.listCatalog({
        q: search || undefined,
        tag_keys: selectedTag ? [selectedTag] : undefined,
        page: 1,
        page_size: 100,
      });
      setCatalogSkills(result.items || []);
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : t('skillHubPage.errors.load');
      setError(message);
    }
  }, [search, selectedTag, t]);

  const loadMine = useCallback(async () => {
    try {
      const items = await skillHubService.listMine();
      setMySkills(items);
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : t('skillHubPage.errors.load');
      setError(message);
    }
  }, [t]);

  const loadAdmin = useCallback(async () => {
    if (user?.role !== 'admin') {
      return;
    }
    try {
      const items = await skillHubService.listAdminSkills();
      setAdminSkills(items);
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : t('skillHubPage.errors.load');
      setError(message);
    }
  }, [t, user?.role]);

  const refreshAll = useCallback(async () => {
    await loadBase();
    await Promise.all([loadCatalog(), loadMine(), loadAdmin()]);
  }, [loadAdmin, loadBase, loadCatalog, loadMine]);

  useEffect(() => {
    void loadBase();
    void loadMine();
  }, [loadBase, loadMine]);

  useEffect(() => {
    if (tab === 'catalog') {
      void loadCatalog();
    } else if (tab === 'mine') {
      void loadMine();
    } else if (tab === 'admin') {
      void loadAdmin();
    }
  }, [tab, loadAdmin, loadCatalog, loadMine]);

  useEffect(() => {
    const onVisibilityChange = () => {
      if (!document.hidden) {
        void refreshAll();
      }
    };
    document.addEventListener('visibilitychange', onVisibilityChange);
    return () => {
      document.removeEventListener('visibilitychange', onVisibilityChange);
    };
  }, [refreshAll]);

  const visibleTags = useMemo(
    () => tags.filter((tag) => !tag.admin_only || user?.role === 'admin'),
    [tags, user?.role],
  );

  const currentSkills = tab === 'catalog' ? catalogSkills : tab === 'mine' ? mySkills : adminSkills;

  const toggleTag = (tagId: number) => {
    setSelectedTagIds((current) =>
      current.includes(tagId) ? current.filter((id) => id !== tagId) : [...current, tagId],
    );
  };

  const buildDefaultDecisions = (preview: SkillImportPreviewItem[]): SkillImportDecision[] =>
    preview.map((item) => {
      if (item.conflict_type === 'unchanged') {
        return { directory_name: item.directory_name, action: 'skip' };
      }
      return { directory_name: item.directory_name, action: 'new_version' };
    });

  const formatImportNotice = (results: SkillImportResultItem[]): string => {
    const messages = results.map((item) => {
      const name = item.skill.name || item.skill.skill_key;
      switch (item.action) {
        case 'created':
          return t('skillHubPage.notices.created', { name });
        case 'versioned':
          return t('skillHubPage.notices.versioned', {
            name,
            version: item.skill.current_version_no ?? (item.previous_version_no ?? 0) + 1,
          });
        case 'unchanged':
          return t('skillHubPage.notices.unchanged', { name });
        case 'saved_as_new':
          return t('skillHubPage.notices.savedAsNew', { key: item.skill.skill_key });
        default:
          return t('skillHubPage.notices.uploaded');
      }
    });
    return messages.join(' · ');
  };

  const finalizeImport = async (file: File, decisions: SkillImportDecision[]) => {
    return skillHubService.importSkills(file, decisions);
  };

  const importSingleArchive = async (file: File): Promise<SkillImportResultItem[]> => {
    const preview = await skillHubService.previewImportSkills(file);
    const conflicts = preview.filter((item) => item.conflict_type === 'content_changed');
    if (conflicts.length === 0) {
      return finalizeImport(file, buildDefaultDecisions(preview));
    }
    return new Promise((resolve, reject) => {
      setPendingUploadFile(file);
      setImportPreviewItems(preview);
      setImportDialogOpen(true);
      pendingImportResolverRef.current = { resolve, reject };
    });
  };

  const pendingImportResolverRef = useRef<{
    resolve: (value: SkillImportResultItem[]) => void;
    reject: (reason?: unknown) => void;
  } | null>(null);

  const handleUpload = async () => {
    if (uploadFiles.length === 0) {
      return;
    }
    try {
      setActionLoading('upload');
      setError(null);
      const allResults: SkillImportResultItem[] = [];
      const errors: string[] = [];
      for (const file of uploadFiles) {
        try {
          const results = await importSingleArchive(file);
          allResults.push(...results);
        } catch (err: unknown) {
          errors.push(`${file.name}: ${(err as { response?: { data?: { error?: string } } })?.response?.data?.error || t('skillHubPage.errors.upload')}`);
        }
      }
      if (allResults.length > 0) {
        setUploadFiles([]);
        setNotice(formatImportNotice(allResults));
        setTab('mine');
        await loadMine();
      }
      if (errors.length > 0) {
        setError(errors.join(' · '));
      }
    } catch (err: unknown) {
      setError((err as { response?: { data?: { error?: string } } })?.response?.data?.error || t('skillHubPage.errors.upload'));
    } finally {
      setActionLoading('');
    }
  };

  const handleImportConflictConfirm = async (decisions: SkillImportDecision[]) => {
    if (!pendingUploadFile) {
      return;
    }
    try {
      setActionLoading('upload');
      setError(null);
      const results = await finalizeImport(pendingUploadFile, decisions);
      pendingImportResolverRef.current?.resolve(results);
      pendingImportResolverRef.current = null;
      setPendingUploadFile(null);
      setImportPreviewItems([]);
      setImportDialogOpen(false);
    } catch (err: unknown) {
      pendingImportResolverRef.current?.reject(err);
      pendingImportResolverRef.current = null;
      setError((err as { response?: { data?: { error?: string } } })?.response?.data?.error || t('skillHubPage.errors.upload'));
    } finally {
      setActionLoading('');
    }
  };

  const handleImportConflictCancel = () => {
    pendingImportResolverRef.current?.reject(new Error('import_cancelled'));
    pendingImportResolverRef.current = null;
    setImportDialogOpen(false);
    setPendingUploadFile(null);
    setImportPreviewItems([]);
  };

  const handlePublish = async () => {
    if (!publishSkillId || selectedTagIds.length === 0) {
      setError(t('skillHubPage.errors.tagsRequired'));
      return;
    }
    try {
      setActionLoading(`publish-${publishSkillId}`);
      setError(null);
      await skillHubService.publishSkill(publishSkillId, selectedTagIds);
      setPublishSkillId(null);
      setSelectedTagIds([]);
      setNotice(t('skillHubPage.notices.published'));
      await refreshAll();
    } catch (err: unknown) {
      setError((err as { response?: { data?: { error?: string } } })?.response?.data?.error || t('skillHubPage.errors.publish'));
    } finally {
      setActionLoading('');
    }
  };

  const handleUpdateTags = async () => {
    if (!editTagsSkillId || selectedTagIds.length === 0) {
      setError(t('skillHubPage.errors.tagsRequired'));
      return;
    }
    try {
      setActionLoading(`edit-tags-${editTagsSkillId}`);
      setError(null);
      await skillHubService.updateTags(editTagsSkillId, selectedTagIds);
      setEditTagsSkillId(null);
      setSelectedTagIds([]);
      setNotice(t('skillHubPage.notices.tagsUpdated'));
      await refreshAll();
    } catch (err: unknown) {
      setError((err as { response?: { data?: { error?: string } } })?.response?.data?.error || t('skillHubPage.errors.updateTags'));
    } finally {
      setActionLoading('');
    }
  };

  const handleUnpublish = async (skillId: number) => {
    try {
      setActionLoading(`unpublish-${skillId}`);
      await skillHubService.unpublishSkill(skillId);
      setNotice(t('skillHubPage.notices.unpublished'));
      await refreshAll();
    } catch (err: unknown) {
      setError((err as { response?: { data?: { error?: string } } })?.response?.data?.error || t('skillHubPage.errors.publish'));
    } finally {
      setActionLoading('');
    }
  };

  const handleDownload = async (skill: Skill) => {
    try {
      setActionLoading(`download-${skill.id}`);
      setError(null);
      const blob = await skillHubService.downloadSkill(skill.id);
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `${skill.skill_key}.zip`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(url);
    } catch (err: unknown) {
      setError((err as { response?: { data?: { error?: string } } })?.response?.data?.error || t('skillHubPage.errors.download'));
    } finally {
      setActionLoading('');
    }
  };

  const handleDelete = async (skillId: number) => {
    try {
      setActionLoading(`delete-${skillId}`);
      await skillHubService.deleteSkill(skillId);
      setNotice(t('skillHubPage.notices.deleted'));
      await refreshAll();
    } catch (err: unknown) {
      setError((err as { response?: { data?: { error?: string } } })?.response?.data?.error || t('skillHubPage.errors.delete'));
    } finally {
      setActionLoading('');
    }
  };

  const handleInstall = async () => {
    if (!installSkillId || selectedInstanceId === '') {
      return;
    }
    try {
      setActionLoading(`install-${installSkillId}`);
      await skillHubService.installSkill(installSkillId, Number(selectedInstanceId));
      setInstallSkillId(null);
      setSelectedInstanceId('');
      setNotice(t('skillHubPage.notices.installed'));
    } catch (err: unknown) {
      setError((err as { response?: { data?: { error?: string } } })?.response?.data?.error || t('skillHubPage.errors.install'));
    } finally {
      setActionLoading('');
    }
  };

  const blockReasonLabel = (reason?: string) => {
    if (!reason) {
      return null;
    }
    const key = `skillHubPage.blockReasons.${reason}`;
    const label = t(key);
    return label === key ? reason : label;
  };

  const renderSkillCard = (skill: Skill, options?: { showOwner?: boolean; adminView?: boolean }) => {
    const isOwner = skill.user_id === user?.id;
    const canPublishManage = isOwner;
    const canDelete = isOwner || (options?.adminView === true && user?.role === 'admin');
    return (
      <div key={skill.id} className="rounded-[22px] border border-[#efe2d8] bg-[#fffaf7] px-5 py-4">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="text-base font-semibold text-[#1d1713]">{skill.name}</h3>
              {skill.current_version_no ? (
                <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.12em] text-emerald-800">
                  {t('skillHubPage.versionLabel', { version: skill.current_version_no })}
                </span>
              ) : null}
              <span className="rounded-full border border-[#ead8cf] bg-white px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.12em] text-[#8f776b]">
                {skill.visibility === 'public' ? t('skillHubPage.visibility.public') : t('skillHubPage.visibility.private')}
              </span>
              <span className="rounded-full border border-[#ead8cf] bg-white px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.12em] text-[#8f776b]">
                {skill.risk_level}
              </span>
              {skill.scan_status ? (
                <span className="rounded-full border border-[#ead8cf] bg-white px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.12em] text-[#8f776b]">
                  {skill.scan_status}
                </span>
              ) : null}
            </div>
            <p className="mt-2 text-sm text-[#6f6158]">{skill.skill_key}</p>
            {options?.showOwner && skill.owner_username ? (
              <p className="mt-1 text-sm text-[#6f6158]">{t('skillHubPage.owner')}: {skill.owner_username}</p>
            ) : null}
            {skill.published_at ? (
              <p className="mt-1 text-sm text-[#6f6158]">
                {t('skillHubPage.publishedAt')}: {new Date(skill.published_at).toLocaleString()}
              </p>
            ) : null}
            <div className="mt-3 flex flex-wrap gap-2">
              {(skill.tags || []).map((tag) => (
                <span key={tag.id} className="rounded-full bg-[#f6ece4] px-2.5 py-1 text-xs text-[#7a6d66]">
                  {tagLabel(tag)}
                </span>
              ))}
            </div>
            <p className="mt-2 text-xs text-[#9a877c]">
              {skill.publishable ? t('skillHubPage.publishable') : t('skillHubPage.notPublishable')}
              {!skill.publishable && skill.publish_blocked_reason ? (
                <> · {blockReasonLabel(skill.publish_blocked_reason)}</>
              ) : null}
              {' · '}
              {t('skillHubPage.instances')}: {skill.instance_count}
            </p>
            {skill.package_collect_error ? (
              <p className="mt-1 text-xs leading-5 text-amber-800">{skill.package_collect_error}</p>
            ) : null}
            {!skill.publishable && skill.source_type === 'discovered' ? (
              <p className="mt-1 text-xs text-[#9a877c]">{t('skillHubPage.discoveredPublishHint')}</p>
            ) : null}
          </div>
          <div className="flex flex-wrap gap-2">
            {tab === 'catalog' ? (
              <>
                <button type="button" className="app-button-primary" onClick={() => setInstallSkillId(skill.id)}>
                  {t('skillHubPage.install')}
                </button>
                <button
                  type="button"
                  className="app-button-secondary"
                  disabled={actionLoading === `download-${skill.id}`}
                  onClick={() => void handleDownload(skill)}
                >
                  {t('skillHubPage.download')}
                </button>
              </>
            ) : null}
            {(tab === 'mine' || tab === 'admin') && skill.visibility === 'public' ? (
              <button
                type="button"
                className="app-button-secondary"
                disabled={actionLoading === `download-${skill.id}`}
                onClick={() => void handleDownload(skill)}
              >
                {t('skillHubPage.download')}
              </button>
            ) : null}
            {canPublishManage && skill.visibility !== 'public' ? (
              <button
                type="button"
                className="app-button-secondary"
                disabled={!skill.publishable || actionLoading === `publish-${skill.id}`}
                onClick={() => {
                  setPublishSkillId(skill.id);
                  setSelectedTagIds((skill.tags || []).filter((tag) => !tag.admin_only).map((tag) => tag.id));
                }}
              >
                {t('skillHubPage.publish')}
              </button>
            ) : null}
            {canPublishManage && skill.visibility === 'public' ? (
              <>
                <button
                  type="button"
                  className="app-button-secondary"
                  disabled={actionLoading === `edit-tags-${skill.id}`}
                  onClick={() => {
                    setEditTagsSkillId(skill.id);
                    setSelectedTagIds((skill.tags || []).map((tag) => tag.id));
                  }}
                >
                  {t('skillHubPage.editTags')}
                </button>
                <button
                  type="button"
                  className="app-button-secondary"
                  disabled={actionLoading === `unpublish-${skill.id}`}
                  onClick={() => void handleUnpublish(skill.id)}
                >
                  {t('skillHubPage.unpublish')}
                </button>
              </>
            ) : null}
            {canDelete ? (
              <button
                type="button"
                className="rounded-lg border border-red-200 bg-red-50 px-4 py-2 text-sm font-medium text-red-700"
                disabled={actionLoading === `delete-${skill.id}`}
                onClick={() => void handleDelete(skill.id)}
              >
                {t('skillHubPage.delete')}
              </button>
            ) : null}
          </div>
        </div>
      </div>
    );
  };

  return (
    <UserLayout title={t('skillHubPage.title')}>
      <div className="space-y-6">
        <section className="app-panel px-6 py-6">
          <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-[#b09d93]">Skill Hub</p>
          <h1 className="mt-2 text-[2rem] font-semibold tracking-[-0.03em] text-[#1d1713]">{t('skillHubPage.title')}</h1>
          <p className="mt-2 max-w-3xl text-sm text-[#6f6158]">{t('skillHubPage.subtitle')}</p>
        </section>

        {error ? <div className="rounded-[18px] border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div> : null}
        {notice ? <div className="rounded-[18px] border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">{notice}</div> : null}

        <section className="app-panel px-6 py-6">
          <div className="flex flex-wrap gap-2">
            {(['catalog', 'mine', ...(user?.role === 'admin' ? ['admin'] as const : [])] as HubTab[]).map((item) => (
              <button
                key={item}
                type="button"
                className={tab === item ? 'app-button-primary' : 'app-button-secondary'}
                onClick={() => setTab(item)}
              >
                {t(`skillHubPage.tabs.${item}`)}
              </button>
            ))}
          </div>

          {tab === 'catalog' ? (
            <div className="mt-5 flex flex-wrap gap-3">
              <input
                className="app-input min-w-[240px]"
                placeholder={t('skillHubPage.searchPlaceholder')}
                value={search}
                onChange={(event) => setSearch(event.target.value)}
              />
              <select className="app-input min-w-[180px]" value={selectedTag} onChange={(event) => setSelectedTag(event.target.value)}>
                <option value="">{t('skillHubPage.allTags')}</option>
                {visibleTags.filter((tag) => !tag.admin_only).map((tag) => (
                  <option key={tag.id} value={tag.tag_key}>{tagLabel(tag)}</option>
                ))}
              </select>
              <button type="button" className="app-button-secondary" onClick={() => void loadCatalog()}>{t('common.refresh')}</button>
            </div>
          ) : null}

          {tab === 'mine' ? (
            <div className="mt-5 flex flex-col gap-3 lg:flex-row lg:items-center">
              <label
                htmlFor="skill-hub-upload"
                className="flex w-full cursor-pointer items-center gap-3 rounded-2xl border border-[#dfd6cf] bg-white px-4 py-3 text-sm text-[#3f3a36] transition hover:border-[#cfc3ba] hover:bg-[#fcfaf8]"
              >
                <span className="rounded-xl border border-[#d8d0ca] bg-[#f6f3f0] px-3 py-2 text-sm font-medium text-[#2f2a27]">
                  {t('openClawResourcesPage.skillActions.chooseFile')}
                </span>
                <span className="min-w-0 truncate text-[#6d655f]">
                  {uploadFiles.length > 0
                    ? uploadFiles.map((file) => file.name).join(', ')
                    : t('openClawResourcesPage.noFileSelected')}
                </span>
                <input
                  id="skill-hub-upload"
                  type="file"
                  multiple
                  accept=".zip,application/zip,application/x-zip-compressed"
                  onChange={(event) => setUploadFiles(Array.from(event.target.files || []))}
                  className="hidden"
                />
              </label>
              <button
                type="button"
                className="app-button-primary whitespace-nowrap disabled:cursor-not-allowed disabled:opacity-50"
                disabled={uploadFiles.length === 0 || actionLoading === 'upload'}
                onClick={() => void handleUpload()}
              >
                {t('skillHubPage.upload')}
              </button>
            </div>
          ) : null}

          <div className="mt-5 space-y-3">
            {loading ? <div className="text-sm text-[#7a6d66]">{t('common.loading')}</div> : null}
            {!loading && currentSkills.length === 0 ? (
              <div className="rounded-[22px] border border-dashed border-[#e7d9d1] bg-[#fffaf7] px-5 py-6 text-sm text-[#7a6d66]">
                {t('skillHubPage.noSkills')}
              </div>
            ) : null}
            {!loading ? currentSkills.map((skill) => renderSkillCard(skill, { showOwner: tab !== 'mine', adminView: tab === 'admin' })) : null}
          </div>
        </section>
      </div>

      {publishSkillId !== null ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4">
          <div className="w-full max-w-lg rounded-[24px] bg-white p-6 shadow-xl">
            <h2 className="text-lg font-semibold text-[#1d1713]">{t('skillHubPage.publish')}</h2>
            <p className="mt-2 text-sm text-[#6f6158]">{t('skillHubPage.selectTags')}</p>
            <div className="mt-4 flex flex-wrap gap-2">
              {visibleTags.map((tag) => (
                <label key={tag.id} className="flex items-center gap-2 rounded-full border border-[#ead8cf] px-3 py-1 text-sm">
                  <input type="checkbox" checked={selectedTagIds.includes(tag.id)} onChange={() => toggleTag(tag.id)} />
                  {tagLabel(tag)}
                </label>
              ))}
            </div>
            <div className="mt-6 flex justify-end gap-2">
              <button type="button" className="app-button-secondary" onClick={() => setPublishSkillId(null)}>{t('common.cancel')}</button>
              <button type="button" className="app-button-primary" onClick={() => void handlePublish()}>{t('skillHubPage.publish')}</button>
            </div>
          </div>
        </div>
      ) : null}

      {editTagsSkillId !== null ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4">
          <div className="w-full max-w-lg rounded-[24px] bg-white p-6 shadow-xl">
            <h2 className="text-lg font-semibold text-[#1d1713]">{t('skillHubPage.editTags')}</h2>
            <p className="mt-2 text-sm text-[#6f6158]">{t('skillHubPage.selectTags')}</p>
            <div className="mt-4 flex flex-wrap gap-2">
              {visibleTags.map((tag) => (
                <label key={tag.id} className="flex items-center gap-2 rounded-full border border-[#ead8cf] px-3 py-1 text-sm">
                  <input type="checkbox" checked={selectedTagIds.includes(tag.id)} onChange={() => toggleTag(tag.id)} />
                  {tagLabel(tag)}
                </label>
              ))}
            </div>
            <div className="mt-6 flex justify-end gap-2">
              <button type="button" className="app-button-secondary" onClick={() => { setEditTagsSkillId(null); setSelectedTagIds([]); }}>{t('common.cancel')}</button>
              <button type="button" className="app-button-primary" disabled={actionLoading === `edit-tags-${editTagsSkillId}`} onClick={() => void handleUpdateTags()}>{t('common.save')}</button>
            </div>
          </div>
        </div>
      ) : null}

      {installSkillId !== null ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4">
          <div className="w-full max-w-lg rounded-[24px] bg-white p-6 shadow-xl">
            <h2 className="text-lg font-semibold text-[#1d1713]">{t('skillHubPage.install')}</h2>
            <select className="app-input mt-4 w-full" value={selectedInstanceId} onChange={(event) => setSelectedInstanceId(event.target.value ? Number(event.target.value) : '')}>
              <option value="">{t('skillHubPage.selectInstance')}</option>
              {instances.map((instance) => (
                <option key={instance.id} value={instance.id}>{instance.name}</option>
              ))}
            </select>
            <div className="mt-6 flex justify-end gap-2">
              <button type="button" className="app-button-secondary" onClick={() => setInstallSkillId(null)}>{t('common.cancel')}</button>
              <button type="button" className="app-button-primary" onClick={() => void handleInstall()}>{t('skillHubPage.install')}</button>
            </div>
          </div>
        </div>
      ) : null}

      <SkillImportConflictDialog
        open={importDialogOpen}
        items={importPreviewItems}
        loading={actionLoading === 'upload'}
        onConfirm={(decisions) => void handleImportConflictConfirm(decisions)}
        onCancel={handleImportConflictCancel}
      />
    </UserLayout>
  );
};

export default SkillHubPage;
