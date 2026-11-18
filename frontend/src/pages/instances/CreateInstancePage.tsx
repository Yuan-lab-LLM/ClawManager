import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { isAxiosError } from 'axios';
import { ChevronDown, ChevronRight, FileUp, Minus, Monitor, Plus, X } from 'lucide-react';
import UserLayout from '../../components/UserLayout';
import FilterSelect from '../../components/FilterSelect';
import OpenClawConfigPlanSection, { type OpenClawInjectionMode } from '../../components/OpenClawConfigPlanSection';
import { useI18n } from '../../contexts/I18nContext';
import { useAuthStore } from '../../stores/authStore';
import { instanceService } from '../../services/instanceService';
import { skillHubService } from '../../services/skillHubService';
import { systemSettingsService } from '../../services/systemSettingsService';
import { userService } from '../../services/userService';
import { DEFAULT_LITE_DISK_GB, MINIMUM_PRO_DISK_GB, PRESET_CONFIGS } from '../../types/instance';
import type { CreateInstanceRequest, DesktopStreamProfile, Instance, InstanceMode } from '../../types/instance';
import type { OpenClawConfigCompilePreview } from '../../types/openclawConfig';
import { createAgentTypes, createNames, defaultCreateImage, maskCreateEnvironment, parseCreateEnvironment, supportsCreateImport, supportsCreateSkills, validCreateNames, type CreationResult, type CreationSnapshot } from '../../lib/instanceCreation';
import { rememberInstanceTarget, rememberLastInstance } from '../../lib/instanceNavigation';
import './instance-create.css';

function requestMessage(error: unknown, fallback: string) {
  if (isAxiosError(error)) {
    const message = error.response?.data?.error;
    if (typeof message === 'string') return message;
  }
  return error instanceof Error ? error.message : fallback;
}
function createdIdFromError(error: unknown): number | undefined {
  if (!isAxiosError(error)) return;
  const id = Number(error.response?.headers['x-clawmanager-created-instance']);
  return Number.isSafeInteger(id) && id > 0 ? id : undefined;
}
async function allQuotaInstances(signal: AbortSignal) {
  const instances: Instance[] = [];
  for (let page = 1; ; page += 1) {
    const data = await instanceService.getInstances(page, 100, undefined, signal);
    instances.push(...data.instances);
    if (instances.length >= data.total) return instances;
    if (!data.instances.length) throw new Error('Incomplete instance inventory');
  }
}


async function waitForCreatedInstance(instanceId: number, active: () => boolean) {
  const deadline = Date.now() + 5 * 60 * 1000;
  while (active() && Date.now() < deadline) {
    const instance = await instanceService.getInstance(instanceId);
    if (!active()) return false;
    if (instance.status === 'running') return true;
    if (['error', 'deleting', 'stopped'].includes(instance.status)) throw new Error(`${instance.name}: ${instance.status}`);
    await new Promise(resolve => window.setTimeout(resolve, 1500));
  }
  return false;
}

export default function CreateInstancePage() {
  const userId = useAuthStore(auth => auth.isAuthenticated ? auth.user?.id : undefined);
  return <InstanceCreator key={userId ?? 'anonymous'} userId={userId} />;
}
function InstanceCreator({ userId }: { userId?: number }) {
  const { t } = useI18n();
  const images = useQuery({ queryKey: ['instance-create-images', userId], queryFn: systemSettingsService.getImageSettings, enabled: !!userId, retry: false, staleTime: 60000 });
  if (!images.data) return <UserLayout title={t('instances.createInstance')}><div role={images.error ? 'alert' : 'status'}>{images.error ? t('instanceCreate.imagesFailed') : t('common.loading')}{images.error && <button className="app-button-secondary" type="button" onClick={() => void images.refetch()}>{t('common.retry')}</button>}</div></UserLayout>;
  const initialMode = createAgentTypes.some(type => defaultCreateImage(images.data!, type.id, 'lite')) ? 'lite' : 'pro';
  const initialType = createAgentTypes.find(type => defaultCreateImage(images.data!, type.id, initialMode))?.id ?? '';
  return <InstanceCreateForm userId={userId} initialType={initialType} initialMode={initialMode} />;
}
function InstanceCreateForm({ userId, initialType, initialMode }: { userId?: number; initialType: string; initialMode: InstanceMode }) {
  const { t } = useI18n();
  const c = (key: string, values?: Record<string, string | number>) => t(`instanceCreate.${key}`, values);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [search] = useSearchParams();
  const [mode, setMode] = useState<InstanceMode>(initialMode);
  const [chosenType, setChosenType] = useState(initialType);
  const [batch, setBatch] = useState(initialMode === 'lite' && search.get('batch') === '1');
  const [name, setName] = useState('');
  const [count, setCount] = useState(3);
  const [startIndex, setStartIndex] = useState(1);
  const [owner, setOwner] = useState('');
  const [description, setDescription] = useState('');
  const [environment, setEnvironment] = useState('');
  const [skillSearch, setSkillSearch] = useState('');
  const [skillIds, setSkillIds] = useState<number[]>([]);
  const [resources, setResources] = useState({ ...PRESET_CONFIGS.medium, gpu_enabled: false, gpu_count: 0, storage_class: '' });
  const [desktopProfile, setDesktopProfile] = useState<DesktopStreamProfile>('standard');
  const [archive, setArchive] = useState<File | null>(null);
  const archiveInput = useRef<HTMLInputElement>(null);
  const importButton = useRef<HTMLButtonElement>(null);
  const advanced = useRef<HTMLDetailsElement>(null);
  const [planMode, setPlanMode] = useState<OpenClawInjectionMode>('none');
  const [bundleId, setBundleId] = useState<number>();
  const [resourceIds, setResourceIds] = useState<number[]>([]);
  const [planMounted, setPlanMounted] = useState(false);
  const [planPreview, setPlanPreview] = useState<OpenClawConfigCompilePreview | null>(null);
  const [planState, setPlanState] = useState<{ loading: boolean; error: string | null }>({ loading: false, error: null });
  const [error, setError] = useState('');
  const [progress, setProgress] = useState('');
  const [busy, setBusy] = useState(false);
  const [results, setResults] = useState<CreationResult[]>([]);
  const submitting = useRef(false);
  const mounted = useRef(true);
  const snapshot = useRef<CreationSnapshot | null>(null);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const current = () => mounted.current && useAuthStore.getState().isAuthenticated && useAuthStore.getState().user?.id === userId;
  const imagesQuery = useQuery({ queryKey: ['instance-create-images', userId], queryFn: systemSettingsService.getImageSettings, enabled: !!userId, retry: false, staleTime: 60000 });
  const quotaQuery = useQuery({ queryKey: ['instance-create-quota', userId], queryFn: () => userService.getUserQuota(userId!), enabled: !!userId, retry: false });
  const usageQuery = useQuery({ queryKey: ['instance-create-usage', userId], queryFn: ({ signal }) => allQuotaInstances(signal), enabled: !!userId, retry: false });
  const skillsQuery = useQuery({ queryKey: ['instance-create-skills', userId], queryFn: skillHubService.listAttachable, enabled: !!userId, retry: false });
  const images = imagesQuery.data ?? [];
  const types = createAgentTypes.filter(type => defaultCreateImage(images, type.id, mode));
  const type = types.find(item => item.id === chosenType);
  const image = type ? defaultCreateImage(images, type.id, mode) : undefined;
  const pro = mode === 'pro';
  const canImport = !!type && supportsCreateImport(type.id);
  const canSelectSkills = !!type && supportsCreateSkills(type.id);
  const names = createNames(name, batch, count, startIndex);
  const namesValid = validCreateNames(names);
  const env = parseCreateEnvironment(environment);
  const envError = env.error ? c(env.error.duplicate ? 'envDuplicate' : 'envInvalid', { line: env.error.line, name: env.error.duplicate ?? '' }) : '';
  const skills = (skillsQuery.data ?? []).filter(skill => skill.status === 'active' && ['none', 'low'].includes(skill.risk_level));
  const selectedSkills = canSelectSkills ? skills.filter(skill => skillIds.includes(skill.id)) : [];
  const visibleSkills = skills.filter(skill => `${skill.name} ${skill.description ?? ''}`.toLowerCase().includes(skillSearch.toLowerCase()));
  const quota = quotaQuery.data;
  const inventory = usageQuery.data ?? [];
  const dedicated = inventory.filter(instance => instance.instance_mode === 'pro' || (!instance.instance_mode && instance.runtime_type !== 'gateway'));
  const requested = batch ? count : 1;
  const quotaExceeded = !!quota && (inventory.length + requested > quota.max_instances || (pro && (
    dedicated.reduce((sum, instance) => sum + instance.cpu_cores, 0) + resources.cpu_cores > quota.max_cpu_cores ||
    dedicated.reduce((sum, instance) => sum + instance.memory_gb, 0) + resources.memory_gb > quota.max_memory_gb ||
    dedicated.reduce((sum, instance) => sum + instance.disk_gb, 0) + resources.disk_gb > quota.max_storage_gb ||
    dedicated.reduce((sum, instance) => sum + (instance.gpu_enabled ? instance.gpu_count : 0), 0) + (resources.gpu_enabled ? resources.gpu_count : 0) > quota.max_gpu_count
  )));
  const resourcesValid = !pro || (Number.isFinite(resources.cpu_cores) && resources.cpu_cores >= .1 && resources.cpu_cores <= 32 && Number.isInteger(resources.memory_gb) && resources.memory_gb >= 1 && resources.memory_gb <= 128 && Number.isInteger(resources.disk_gb) && resources.disk_gb >= MINIMUM_PRO_DISK_GB && resources.disk_gb <= 1000 && (!resources.gpu_enabled || Number.isInteger(resources.gpu_count) && resources.gpu_count >= 1 && resources.gpu_count <= 4));
  const planInvalid = pro && canImport && ((planMode === 'bundle' && !bundleId) || (['bundle', 'manual'].includes(planMode) && (planState.loading || !!planState.error)) || (planMode === 'archive' && !archive));
  const disabled = busy || (pro && batch) || results.length > 0 || !type || !image || !namesValid || !!env.error || !resourcesValid || imagesQuery.isPending || quotaQuery.isPending || usageQuery.isPending || !!quotaQuery.error || !!usageQuery.error || !quota || quotaExceeded || planInvalid;
  const locked = busy || results.length > 0;
  const onPlanPreview = useCallback((preview: OpenClawConfigCompilePreview | null, state: { loading: boolean; error: string | null }) => { setPlanPreview(preview); setPlanState(state); }, []);

  const resetPlan = () => { setPlanMode('none'); setBundleId(undefined); setResourceIds([]); setPlanPreview(null); setPlanState({ loading: false, error: null }); setArchive(null); if (archiveInput.current) archiveInput.current.value = ''; };
  const changeType = (next: string) => { setChosenType(next); resetPlan(); if (!supportsCreateSkills(next)) setSkillIds([]); };
  const changeMode = (next: InstanceMode) => { setMode(next); if (next === 'pro') setBatch(false); resetPlan(); };
  const changeArchive = (file: File | null) => { setArchive(file); if (file) { setPlanMode('archive'); setBundleId(undefined); setResourceIds([]); setPlanPreview(null); setPlanState({ loading: false, error: null }); } else if (planMode === 'archive') setPlanMode('none'); };
  const invalidateInventory = () => { void queryClient.invalidateQueries({ predicate: query => ['instance-switcher', 'instances', 'instance-create-usage', 'instance-create-quota', 'workspace-dashboard'].includes(String(query.queryKey[0])) }); };
  function finishCreation(rows: CreationResult[]) {
    invalidateInventory();
    if (rows.length && rows.every(row => row.status === 'created' && row.instanceId)) {
      rememberLastInstance(userId!, rows[0].instanceId!);
      navigate('/instances', { replace: true });
    } else setResults(rows);
  }

  async function importConfiguration(row: CreationResult, submitted: CreationSnapshot): Promise<CreationResult> {
    if (!submitted.file || !row.instanceId || !supportsCreateImport(submitted.payload.type)) return row;
    try {
      if (current()) setProgress(c('importWaiting'));
      const ready = await waitForCreatedInstance(row.instanceId, current);
      if (!current()) return row;
      if (!ready) throw new Error(c('importTimeout'));
      setProgress(c('importing'));
      if (submitted.payload.type === 'hermes') await instanceService.importHermesWorkspace(row.instanceId, submitted.file);
      else await instanceService.importOpenClawWorkspace(row.instanceId, submitted.file);
      return { ...row, status: 'created', error: undefined, importPending: false };
    } catch (cause) { return { ...row, status: 'failed', importPending: true, error: `${c('importFailed')} ${requestMessage(cause, c('importTimeout'))}` }; }
  }
  async function createOne(submitted: CreationSnapshot, instanceName: string): Promise<CreationResult> {
    try {
      const instance = await instanceService.createInstance({ ...submitted.payload, name: instanceName });
      if (!instance?.id) throw new Error(c('unknownResult'));
      if (current()) { rememberInstanceTarget(userId!, instance); }
      return { name: instanceName, instanceId: instance.id, status: 'created' };
    } catch (cause) { return { name: instanceName, status: 'failed', instanceId: createdIdFromError(cause), retryBlocked: !createdIdFromError(cause) && (!isAxiosError(cause) || !cause.response || cause.response.status >= 500), error: requestMessage(cause, t('instances.createFailed')) }; }
  }
  async function finishImports(rows: CreationResult[], submitted: CreationSnapshot) {
    if (!submitted.file) return rows;
    const updated = [...rows]; let next = 0;
    await Promise.all(Array.from({ length: Math.min(3, rows.length) }, async () => {
      while (current() && next < rows.length) {
        const index = next++;
        if (!rows[index].instanceId || rows[index].status !== 'created') continue;
        updated[index] = await importConfiguration(rows[index], submitted);
        if (current()) setResults([...updated]);
      }
    }));
    return updated;
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting.current || disabled || !type || !image) return;
    submitting.current = true; setBusy(true); setError(''); setProgress(t('instances.creatingNow'));
    const payload: CreateInstanceRequest = {
      name: names[0], owner: owner.trim() || undefined, description: description.trim() || undefined,
      type: type.id, mode, instance_mode: mode, runtime_type: pro ? 'desktop' : 'gateway',
      cpu_cores: pro ? resources.cpu_cores : PRESET_CONFIGS.small.cpu_cores,
      memory_gb: pro ? resources.memory_gb : PRESET_CONFIGS.small.memory_gb,
      disk_gb: pro ? resources.disk_gb : DEFAULT_LITE_DISK_GB,
      gpu_enabled: pro && resources.gpu_enabled, gpu_count: pro && resources.gpu_enabled ? resources.gpu_count : 0,
      os_type: type.id === 'custom' ? 'ubuntu' : type.id, os_version: 'latest', image_registry: image.image,
      storage_class: pro ? resources.storage_class : '', desktop_stream_profile: pro ? desktopProfile : undefined,
      environment_overrides: Object.keys(env.values).length ? env.values : undefined,
      skill_ids: canSelectSkills ? selectedSkills.map(skill => skill.id) : undefined,
      openclaw_config_plan: pro && canImport && planMode === 'bundle' && bundleId ? { mode: 'bundle', bundle_id: bundleId } : pro && canImport && planMode === 'manual' && resourceIds.length ? { mode: 'manual', resource_ids: resourceIds } : undefined,
    };
    const submitted: CreationSnapshot = { payload, file: canImport ? archive : null };
    snapshot.current = submitted;
    try {
      let rows: CreationResult[];
      if (batch) {
        const response = await instanceService.batchCreateLiteInstances({ name_prefix: name.trim(), count, start_index: startIndex, template: payload });
        if (!current()) return;
        const returned = Array.isArray(response.results) ? response.results : [];
        rows = names.map(expected => {
          const matches = returned.filter(result => result.name === expected);
          const known = matches.find(result => result.instance?.id) ?? matches[0];
          if (matches.length !== 1 || !known || !['created','failed'].includes(known.status) || known.status === 'created' && !known.instance?.id) return { name: expected, instanceId: known?.instance?.id, status: 'failed', retryBlocked: true, error: c('unknownResult') };
          return { name: expected, status: known.status, instanceId: known.instance?.id, error: known.error };
        });
        if (returned.length !== names.length || rows.some(row => row.retryBlocked)) setError(c('unknownResult'));
      } else rows = [await createOne(submitted, names[0])];
      if (!current()) return;
      setResults(rows);
      rows = await finishImports(rows, submitted);
      if (!current()) return;
      finishCreation(rows);
    } catch (cause) { if (current()) {
      setError(requestMessage(cause, t('instances.createFailed')));
      if (batch && (!isAxiosError(cause) || !cause.response || cause.response.status >= 500)) setResults(names.map(value => ({name:value,status:'failed',retryBlocked:true,error:c('unknownResult')})));
    } }
    finally { if (current()) { setBusy(false); setProgress(''); } submitting.current = false; }
  }
  async function retryResult(index: number, importOnly: boolean) {
    const submitted = snapshot.current, row = results[index];
    if (!submitted || !row || submitting.current || (row.instanceId && !importOnly) || row.retryBlocked) return;
    submitting.current = true; setBusy(true); setError('');
    try {
      let updated = importOnly ? await importConfiguration(row, submitted) : await createOne(submitted, row.name);
      if (!importOnly && updated.status === 'created') updated = await importConfiguration(updated, submitted);
      if (current()) finishCreation(results.map((item, itemIndex) => itemIndex === index ? updated : item));
    } finally { if (current()) { setBusy(false); setProgress(''); } submitting.current = false; }
  }
  function another() { setResults([]); snapshot.current = null; setError(''); if (batch) setStartIndex(value => value + count); else setName(''); }
  const agentIcon = type?.icon ? <img src={type.icon} alt="" width={32} height={32} /> : <Monitor size={32} aria-hidden="true" />;

  return <UserLayout title={t('instances.createInstance')}><form className="cm-create" onSubmit={event => void submit(event)} noValidate>
    <div className="cm-create-grid"><fieldset className="cm-create-form app-panel" disabled={locked}>
      <section className="cm-create-section"><h2>{c('chooseAgent')}</h2>
        {imagesQuery.isPending ? <p role="status">{t('common.loading')}</p> : imagesQuery.error ? <div role="alert">{c('imagesFailed')} <button className="app-button-secondary" type="button" onClick={() => void imagesQuery.refetch()}>{t('common.retry')}</button></div> : <div className="cm-create-types" role="radiogroup" aria-label={c('chooseAgent')} onKeyDown={event => {
          if (!['ArrowLeft','ArrowRight','ArrowUp','ArrowDown','Home','End'].includes(event.key)) return;
          const buttons = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('[role=radio]')]; const index = buttons.indexOf(document.activeElement as HTMLButtonElement); if (index < 0 || !buttons.length) return;
          event.preventDefault(); const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : (index + (['ArrowRight','ArrowDown'].includes(event.key) ? 1 : -1) + buttons.length) % buttons.length; buttons[next].focus(); buttons[next].click();
        }}>{types.map(option => <button type="button" role="radio" aria-checked={type?.id === option.id} aria-label={option.label} className="cm-create-type" key={option.id} onClick={() => changeType(option.id)}>{option.icon ? <img src={option.icon} alt="" width={32} height={32} /> : <Monitor size={32} aria-hidden="true" />}<span>{option.id === 'custom' ? defaultCreateImage(images, option.id, mode)?.display_name || option.label : option.label}</span></button>)}</div>}
        {!imagesQuery.isPending && !imagesQuery.error && !types.length && <p role="status">{c('noTypes')}</p>}
        <div className="cm-create-mode-row"><span>{c('mode')}</span><div className="cm-create-modes" role="radiogroup" aria-label={c('mode')} onKeyDown={event => { if (['ArrowLeft','ArrowRight'].includes(event.key)) { event.preventDefault(); const button = event.currentTarget.querySelector<HTMLButtonElement>(`[aria-checked="false"]:not(:disabled)`); button?.click(); button?.focus(); } }}>
          {(['lite','pro'] as const).map(value => <button key={value} type="button" role="radio" aria-checked={mode === value} disabled={!createAgentTypes.some(agent => defaultCreateImage(images, agent.id, value))} onClick={() => changeMode(value)}>{value === 'lite' ? 'Lite' : 'Pro'}</button>)}
        </div><span className="cm-create-hint">{c(pro ? 'proHelp' : 'liteHelp')}</span></div>
        <div className="cm-create-mode-row"><span id="cm-create-batch-label">{c('batchToggle')}</span><button className="cm-create-switch" type="button" role="switch" aria-labelledby="cm-create-batch-label" aria-checked={batch} disabled={pro} onClick={() => setBatch(value => !value)}><span /></button>{(pro || batch) && <span className="cm-create-hint">{pro ? c('batchProOnly') : c('batchHelp')}</span>}</div>
      </section>
      <section className="cm-create-section"><h2>{c('info')}</h2><div className={`cm-create-naming ${batch ? 'is-batch' : ''}`}>
        <label className="cm-create-field"><span>{batch ? c('namePrefix') : t('instances.instanceName')}</span><input className="app-input" name="name" value={name} maxLength={batch ? 40 : 50} required placeholder={c('namePlaceholder')} onChange={event => setName(event.target.value)} aria-describedby={name && !namesValid ? 'cm-create-name-error' : undefined} /></label>
        {batch && <><div className="cm-create-field"><label htmlFor="cm-create-count">{c('count')}</label><span className="cm-create-stepper"><button type="button" aria-label={c('decreaseCount')} onClick={() => setCount(value => Math.max(2, value - 1))}><Minus size={14} aria-hidden="true" /></button><input id="cm-create-count" name="count" type="number" min={2} max={100} value={Number.isNaN(count) ? '' : count} onChange={event => setCount(event.target.value === '' ? NaN : Number(event.target.value))} /><button type="button" aria-label={c('increaseCount')} onClick={() => setCount(value => Math.min(100, value + 1))}><Plus size={14} aria-hidden="true" /></button></span></div><label className="cm-create-field"><span>{c('startIndex')}</span><input className="app-input" name="start_index" type="number" min={1} max={9999} value={Number.isNaN(startIndex) ? '' : startIndex} onChange={event => setStartIndex(Number(event.target.value))} /></label></>}
      </div>{name && !namesValid && <p id="cm-create-name-error" className="cm-create-error">{c('namesInvalid')}</p>}
      <div className="cm-create-info"><label className="cm-create-field"><span>{c('owner')} <span className="cm-create-hint">{c('optional')}</span></span><input className="app-input" name="owner" value={owner} maxLength={128} placeholder={c('ownerPlaceholder')} onChange={event => setOwner(event.target.value)} /></label><label className="cm-create-field"><span>{t('instances.descriptionOptional')}</span><input className="app-input" name="description" value={description} onChange={event => setDescription(event.target.value)} placeholder={t('instances.descriptionPlaceholder')} /></label></div></section>
      {pro && <section className="cm-create-section cm-create-reveal"><h2>{t('instances.resourceConfiguration')}</h2><div className="cm-create-resources">{([
        {key:'cpu_cores',label:t('common.cpu'),unit:'vCPU',min:.1,max:32,step:.1},
        {key:'memory_gb',label:t('instances.memoryLabel'),unit:'GB',min:1,max:128,step:1},
        {key:'disk_gb',label:t('instances.storageLabel'),unit:'GB',min:10,max:1000,step:1},
      ] as const).map(resource => <div className="cm-create-field" key={resource.key}><label htmlFor={`create-${resource.key}`}>{resource.label}</label><div className="cm-create-unit"><input id={`create-${resource.key}`} className="app-input" type="number" min={resource.min} max={resource.max} step={resource.step} value={Number.isNaN(resources[resource.key]) ? '' : resources[resource.key]} onChange={event => setResources(value => ({ ...value, [resource.key]: event.target.value === '' ? NaN : Number(event.target.value) }))} /><span>{resource.unit}</span></div><input type="range" min={resource.min} max={resource.max} step={resource.step} value={Number.isFinite(resources[resource.key]) ? resources[resource.key] : resource.min} aria-label={`${resource.label} ${t('instances.stepConfig')}`} onChange={event => setResources(value => ({ ...value, [resource.key]: Number(event.target.value) }))} /></div>)}</div>
        <details className="cm-create-resource-options"><summary>{c('resourceOptions')}</summary><div className="cm-create-info"><label className="cm-create-field"><span>{t('instances.storageClass')}</span><input className="app-input" value={resources.storage_class} onChange={event => setResources(value => ({...value,storage_class:event.target.value}))} /></label><div className="cm-create-field"><span>{t('instances.desktopStreamProfile')}</span><FilterSelect label={t('instances.desktopStreamProfile')} value={desktopProfile} options={(['low','standard','high'] as const).map(value => ({value,label:t(`instances.desktopStream${value[0].toUpperCase()}${value.slice(1)}`)}))} onChange={value => setDesktopProfile(value as DesktopStreamProfile)} /></div></div>
        <label className="cm-create-gpu"><input type="checkbox" checked={resources.gpu_enabled} onChange={event => setResources(value => ({...value,gpu_enabled:event.target.checked,gpu_count:event.target.checked ? Math.max(1,value.gpu_count) : 0}))} />{t('instances.enableGpu')}</label>{resources.gpu_enabled && <label className="cm-create-field"><span>{t('instances.gpuLabel')}</span><input className="app-input" type="number" min={1} max={4} value={resources.gpu_count} onChange={event => setResources(value => ({...value,gpu_count:Number(event.target.value)}))} /></label>}</details>
      </section>}
      {pro && canImport && <details className="cm-create-section cm-create-plan" onToggle={event => { if (event.currentTarget.open) setPlanMounted(true); }}><summary><h2>{c('initial')}</h2><ChevronDown size={16} aria-hidden="true" /></summary>{planMounted && <OpenClawConfigPlanSection embedded hideHeader mode={planMode} bundleId={bundleId} resourceIds={resourceIds} onModeChange={next => { setPlanMode(next); setPlanPreview(null); if (next !== 'archive') { setArchive(null); if (archiveInput.current) archiveInput.current.value = ''; } else { if (advanced.current) advanced.current.open = true; archiveInput.current?.click(); } }} onSelectionChange={({bundleId:nextBundle,resourceIds:nextResources}) => {setBundleId(nextBundle);setResourceIds(nextResources);}} onPreviewChange={onPlanPreview} />}</details>}
      <details className="cm-create-section cm-create-advanced" ref={advanced}><summary><h2><span>{c('advanced')}</span><small>{c('advancedHint')}</small></h2><ChevronRight size={16} aria-hidden="true" /></summary><div className="cm-create-advanced-body">
        <div className="cm-create-import"><input hidden ref={archiveInput} type="file" accept=".tar.gz,.tgz,.gz" aria-label={c('import')} onChange={event => changeArchive(event.target.files?.[0] ?? null)} /><button ref={importButton} type="button" className="app-button-secondary" disabled={!canImport} onClick={() => archiveInput.current?.click()}><FileUp size={17} aria-hidden="true" />{c(archive ? 'replaceFile' : 'import')}</button>{archive && <span className="cm-create-file"><span title={archive.name}>{archive.name}</span><button className="cm-icon-button" type="button" aria-label={c('removeFile')} onClick={() => { changeArchive(null); if (archiveInput.current) archiveInput.current.value = ''; importButton.current?.focus(); }}><X size={15} aria-hidden="true" /></button></span>}{!canImport && <span className="cm-create-hint">{c('importUnavailable')}</span>}</div>
        <label className="cm-create-field"><span>{t('instances.environmentVariables')} <span className="cm-create-hint">{c('optional')}</span></span><textarea className="app-input" name="environment" rows={4} value={environment} spellCheck={false} placeholder={c('envPlaceholder')} aria-invalid={!!env.error} aria-describedby="cm-create-env-help cm-create-env-error" onChange={event => setEnvironment(event.target.value)} /></label><p id="cm-create-env-help" className="cm-create-hint">{c('envHelp')}</p>{envError && <p id="cm-create-env-error" className="cm-create-error" role="alert">{envError}</p>}
        {canSelectSkills && <section className="cm-create-skills" aria-label={c('skills')}><div className="cm-create-subheading"><h3>{c('skills')} <span className="cm-create-hint">{c('optional')}</span></h3><span className="cm-create-hint">{c('skillSelected',{count:selectedSkills.length})}</span></div><input className="app-input" type="search" aria-label={c('searchSkills')} placeholder={c('searchSkills')} value={skillSearch} onChange={event => setSkillSearch(event.target.value)} />
          {skillsQuery.isPending ? <p role="status">{t('common.loading')}</p> : skillsQuery.error ? <div role="alert">{c('skillUnavailable')} <button type="button" className="app-button-secondary" onClick={() => void skillsQuery.refetch()}>{t('common.retry')}</button></div> : <div className="cm-create-skill-options">{visibleSkills.length ? visibleSkills.map(skill => <label className="cm-create-skill" key={skill.id}><input type="checkbox" checked={skillIds.includes(skill.id)} onChange={event => setSkillIds(value => event.target.checked ? [...value,skill.id] : value.filter(id => id !== skill.id))} /><span><strong>{skill.name}</strong>{skill.description && <small>{skill.description}</small>}</span></label>) : <p>{c(skills.length ? 'noMatches' : 'noSkills')}</p>}</div>}
          <Link to="/skill-hub" target="_blank" rel="noopener noreferrer">{c('manageSkills')}</Link>
        </section>}
      </div></details>
    </fieldset>
    <aside className="app-panel cm-create-summary" aria-label={c('preview')}><div className="cm-create-subheading"><h2>{c('preview')}</h2><span className="cm-create-badge">{c('instanceCount',{count:Number.isFinite(requested) ? requested : 0})}</span></div>
      <div className="cm-create-identity">{agentIcon}<strong>{type?.label ?? '—'}</strong><span>{pro ? 'Pro' : 'Lite'}</span></div>
      <div className="cm-create-names">{names.slice(0,6).map(value => <div className="cm-create-name" key={value}>{agentIcon}<span title={value}>{value}</span></div>)}{names.length > 6 && <p className="cm-create-hint">{c('moreNames',{count:names.length-6})}</p>}</div>
      <dl className="cm-create-facts">{pro && <><div><dt>{t('common.cpu')}</dt><dd>{resources.cpu_cores} vCPU</dd></div><div><dt>{t('instances.memoryLabel')}</dt><dd>{resources.memory_gb} GB</dd></div><div><dt>{t('instances.storageLabel')}</dt><dd>{resources.disk_gb} GB</dd></div></>}<div><dt>{c('owner')}</dt><dd title={owner.trim()}>{owner.trim() || c('notProvided')}</dd></div>{pro && canImport && <div><dt>{c('initial')}</dt><dd>{planPreview?.bundle?.name || c('defaults')}</dd></div>}</dl>
      {Object.keys(env.values).length > 0 && <section className="cm-create-summary-section"><div className="cm-create-subheading"><h3>{t('instances.environmentVariables')}</h3><span className="cm-create-hint">{c('envCount',{count:Object.keys(env.values).length})}</span></div><div className="cm-create-env-scroll"><table aria-label={c('environmentPreview')}><colgroup><col style={{width:'40%'}} /><col style={{width:'60%'}} /></colgroup><thead><tr><th scope="col">Key</th><th scope="col">Value</th></tr></thead><tbody>{Object.entries(env.values).map(([key,value]) => <tr key={key}><th scope="row" title={key}>{key}</th><td title={maskCreateEnvironment(key,value)}>{maskCreateEnvironment(key,value) || c('emptyValue')}</td></tr>)}</tbody></table></div></section>}
      {selectedSkills.length > 0 && <section className="cm-create-summary-section"><div className="cm-create-subheading"><h3>{c('skills')}</h3><span className="cm-create-hint">{c('skillSelected',{count:selectedSkills.length})}</span></div><ul className="cm-create-selected-skills">{selectedSkills.map(skill => <li key={skill.id}><span>{skill.name}</span><button className="cm-icon-button" type="button" disabled={locked} aria-label={c('removeSkill',{name:skill.name})} onClick={event => { setSkillIds(value => value.filter(id => id !== skill.id)); const list = event.currentTarget.closest('ul'); const nextButton = [...(list?.querySelectorAll<HTMLButtonElement>('button') ?? [])].find(button => button !== event.currentTarget); if (nextButton) nextButton.focus(); else advanced.current?.querySelector('summary')?.focus(); }}><X size={14} aria-hidden="true" /></button></li>)}</ul></section>}
      {(quotaQuery.isPending || usageQuery.isPending) && <p role="status" className="cm-create-hint">{t('instances.checkingQuota')}</p>}
      {(quotaQuery.error || usageQuery.error) && <div role="alert" className="cm-create-error">{c('quotaFailed')} <button className="app-button-secondary" type="button" onClick={() => {void quotaQuery.refetch();void usageQuery.refetch();}}>{t('common.retry')}</button></div>}
      {quotaExceeded && !results.length && <p role="alert" className="cm-create-error">{c('quotaExceeded')}</p>}
      {quota && !results.length && <p className="cm-create-hint cm-create-quota">{c('quotaSlots',{used:inventory.length+(Number.isFinite(requested) ? requested : 0),max:quota.max_instances})}</p>}
      {planInvalid && planState.error && <p role="alert" className="cm-create-error">{planState.error}</p>}
      {error && <p role="alert" className="cm-create-error">{error}</p>}
      <div className="cm-create-actions"><button className="app-button-primary" type="submit" disabled={disabled}><Plus size={18} aria-hidden="true" />{busy ? t('instances.creatingNow') : results.length ? c('results') : batch ? c('createBatch',{count:Number.isFinite(count) ? count : 0}) : t('instances.createInstance')}</button><Link to="/instances" className="app-button-secondary">{t('common.cancel')}</Link>{results.length > 0 && <button className="app-button-secondary" type="button" disabled={busy} onClick={another}>{c('another')}</button>}</div>
      <p className="cm-create-hint" role="status" aria-live="polite">{progress}</p>
    </aside></div>
    {results.length > 0 && <section className="app-panel cm-create-results" aria-label={c('results')}><h2>{c('results')}</h2><p role="status">{c('batchSummary',{created:results.filter(row => !!row.instanceId).length,failed:results.filter(row => row.status === 'failed').length})}</p><ul>{results.map((row,index) => <li key={row.name}><span className="cm-create-result-name">{row.instanceId ? <Link to={`/instances/${row.instanceId}`}>{row.name}</Link> : row.name}</span><span className={row.status === 'failed' ? 'cm-create-error' : 'cm-create-success'}>{c(row.status === 'created' ? 'created' : 'failed')}</span>{row.error && <p>{row.error}</p>}{row.instanceId ? <Link to={`/instances/${row.instanceId}`} className="app-button-secondary">{c('viewInstance')}</Link> : row.retryBlocked ? <Link to="/instances" className="app-button-secondary">{t('instances.listTitle')}</Link> : <button className="app-button-secondary" type="button" disabled={busy} onClick={() => void retryResult(index,false)}>{c('retryCreate')}</button>}{row.importPending && <button className="app-button-secondary" type="button" disabled={busy} onClick={() => void retryResult(index,true)}>{c('importRetry')}</button>}</li>)}</ul></section>}
  </form></UserLayout>;
}
