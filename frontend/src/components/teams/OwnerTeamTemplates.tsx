import { useContext, useEffect, useId, useRef, useState } from 'react';
import { Check, MoreHorizontal, NotebookPen, PencilLine, Plus, RefreshCw, Sparkles, Trash2, Users } from 'lucide-react';
import { OwnerTeamOperationError, ownerTeamService, waitForOwnerTeamOperation, withOwnerTeamIntent, type TeamOperation } from '../../services/ownerTeamService';
import type { CustomTeamTemplate } from '../../types/customTeamTemplate';
import TeamBadge, { TeamIconPicker } from './TeamBadge';
import { OwnerTeamDialog, useOwnerTeamDraftGuard } from './OwnerTeamDialog';
import { OwnerTeamFeedbackContext, OwnerTeamNotifications, useOwnerTeamFeedback } from './OwnerTeamNotifications';

type Draft = { name: string; intent: string; count: number; revision?: number };
const initialDraft = (template: CustomTeamTemplate | null): Draft => ({ name: template?.name || '', intent: template?.intent || '', count: template?.resolved_member_count || 3, revision: template?.revision });
const errorMessage = (error: unknown) => error instanceof Error ? error.message : '操作失败，请重试';

export function OwnerTeamTemplates({ owner, canGenerate, onOperation }: { owner: string; canGenerate: boolean; onOperation: (op: TeamOperation) => void }) {
  const parent = useContext(OwnerTeamFeedbackContext);
  const props = { owner, canGenerate, onOperation };
  return parent ? <OwnerTeamTemplatesContent {...props}/> : <OwnerTeamNotifications key={owner} owner={owner}><OwnerTeamTemplatesContent {...props}/></OwnerTeamNotifications>;
}

function OwnerTeamTemplatesContent({ owner, canGenerate, onOperation }: { owner: string; canGenerate: boolean; onOperation: (op: TeamOperation) => void }) {
  const feedback = useOwnerTeamFeedback(), memberTabID = useId();
  const [memberID, setMemberID] = useState(''), [detailTab, setDetailTab] = useState<'delivery' | 'collaboration'>('delivery'), [pane, setPane] = useState<'info' | 'members'>('members');
  const actionID = useRef<string>(crypto.randomUUID()), operationID = useRef<string | undefined>(undefined), attemptCount = useRef<number | undefined>(undefined), terminal = useRef(false), operationSucceeded = useRef(false);
  const [templates, setTemplates] = useState<CustomTeamTemplate[]>([]), [selected, setSelected] = useState<CustomTeamTemplate | null>(null);
  const [draft, setDraft] = useState<Draft>(initialDraft(null));
  const [loading, setLoading] = useState(true), [busy, setBusy] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState('');
  const [adjustMember, setAdjustMember] = useState<string | null>(null), [instruction, setInstruction] = useState('');
  const form = useRef<HTMLFormElement>(null);
  const controller = useRef<AbortController | null>(null), lock = useRef(false), sequence = useRef(0);
  const dirty = draft.name !== (selected?.name || '') || draft.intent !== (selected?.intent || '') || draft.count !== (selected?.resolved_member_count || 3);
  useOwnerTeamDraftGuard(dirty || (!!adjustMember && !!instruction) || busy);
  const key = (id: number | null) => `owner-team-template-draft:${owner}:${id ?? 'new'}`;
  const choose = (template: CustomTeamTemplate | null) => {
    feedback.dismiss(actionID.current);
    setMemberID('');
    let next = initialDraft(template);
    try {
      const saved = JSON.parse(sessionStorage.getItem(key(template?.id ?? null)) || 'null');
      if (saved && typeof saved.name === 'string' && typeof saved.intent === 'string' && Number.isInteger(saved.count) && saved.count >= 2 && saved.count <= 6) next = { ...next, ...saved };
      sessionStorage.setItem(`owner-team-template-selected:${owner}`, String(template?.id ?? 'new'));
    } catch { /* Storage unavailable: keep the visible form usable. */ }
    setSelected(template); setDraft(next); setError(''); setNotice('');
  };
  useEffect(() => {
    if (loading) return;
    try { sessionStorage.setItem(key(selected?.id ?? null), JSON.stringify(draft)); } catch { /* Refresh guard still protects the visible draft. */ }
  }, [owner, selected?.id, draft, loading]);
  useEffect(() => {
    const active = new AbortController(); controller.current = active;
    void ownerTeamService.templates(active.signal).then(result => {
      if (active.signal.aborted) return;
      setTemplates(result.templates);
      let remembered: string | null = null; try { remembered = sessionStorage.getItem(`owner-team-template-selected:${owner}`); } catch { /* optional */ }
      choose(remembered === 'new' ? null : result.templates.find(t => String(t.id) === remembered) || result.templates[0] || null);
    }).catch(error => { if (!active.signal.aborted) setError(errorMessage(error)); }).finally(() => { if (!active.signal.aborted) setLoading(false); });
    return () => active.abort();
  // The component is keyed by owner; initial loading must not replace edits.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [owner]);
  const refresh = async () => {
    const request = ++sequence.current;
    const result = await ownerTeamService.templates(controller.current?.signal);
    if (controller.current?.signal.aborted || request !== sequence.current) return result.templates;
    setTemplates(result.templates); return result.templates;
  };
  const run = async (action: () => Promise<void>) => {
    if (lock.current) return;
    lock.current = true; feedback.dismiss(actionID.current); actionID.current = crypto.randomUUID(); operationID.current = undefined; attemptCount.current = undefined; terminal.current = false; operationSucceeded.current = false; setBusy(true); setError(''); setNotice('');
    try { await action(); } catch (error) { if (operationSucceeded.current) { actionID.current = `refresh:${operationID.current}`; terminal.current = false; } if (error instanceof OwnerTeamOperationError) { attemptCount.current = error.operation.attempt_count; terminal.current = true; } if (!controller.current?.signal.aborted) setError(errorMessage(error)); }
    finally { lock.current = false; if (!controller.current?.signal.aborted) setBusy(false); }
  };
  const accept = (template: CustomTeamTemplate, replaceDraft: boolean) => {
    if (controller.current?.signal.aborted) return;
    setSelected(template);
    setDraft(previous => replaceDraft ? initialDraft(template) : { ...previous, revision: template.revision });
    try { sessionStorage.setItem(`owner-team-template-selected:${owner}`, String(template.id)); } catch { /* optional */ }
  };
  const generate = async (action: string, data: object) => {
    const template = selected;
    const op = await withOwnerTeamIntent(owner, `template:${template?.id || 'new'}:${action}`, data, (payload, requestKey) => template ? ownerTeamService.templateAction(template.id, action, payload, requestKey) : ownerTeamService.generateTemplate(payload as { intent: string; member_count?: number }, requestKey));
    if (controller.current?.signal.aborted) return;
    actionID.current = op.operation_id; operationID.current = op.operation_id; attemptCount.current = op.attempt_count;
    feedback.notify({ id: op.operation_id, operationID: op.operation_id, attemptCount: op.attempt_count, terminal: false, kind: 'pending', title: '正在处理模板', message: '可以继续查看内容，进度保留在操作记录中。' });
    onOperation(op);
    const done = await waitForOwnerTeamOperation(op.operation_id, controller.current?.signal);
    attemptCount.current = done.attempt_count; terminal.current = true; operationSucceeded.current = true;
    feedback.notify({ id: op.operation_id, operationID: op.operation_id, attemptCount: done.attempt_count, terminal: true, kind: 'success', title: '操作已完成', message: '后台已确认完成，正在更新模板显示。' });
    await refresh();
    if (done.template_id) {
      const updated = await ownerTeamService.template(done.template_id, controller.current?.signal);
      // Selection is locked during a mutation; unmount/owner changes are fenced.
      if (!controller.current?.signal.aborted) { accept(updated, true); setNotice('模板已更新，已创建的团队保持不变。'); }
    }
  };
  const reloadVersion = () => void run(async () => {
    if (!selected) return;
    const latest = await ownerTeamService.template(selected.id, controller.current?.signal);
    accept(latest, false); await refresh(); setNotice('已读取最新版本，保留了你的输入。请核对内容后再保存或修订。');
  });
  useEffect(() => {
    if (error) feedback.notify({ id: actionID.current, operationID: operationID.current, attemptCount: attemptCount.current, terminal: terminal.current || !operationID.current, kind: 'error', title: operationSucceeded.current ? '操作已完成，模板暂未刷新' : '本次操作未完成', message: error, actions: selected ? [{ label: '读取最新版本并保留输入', onClick: reloadVersion }] : undefined });
    else if (notice) feedback.notify({ id: actionID.current, operationID: operationID.current, attemptCount: attemptCount.current, terminal: terminal.current || !operationID.current, kind: 'success', title: '操作已完成', message: notice });
  // Notifications capture the failed action's template, and choosing another
  // template dismisses that callback; drafts and operation history remain intact.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [error, notice, feedback.notify]);
  const submitTemplate = () => {
    if (busy || loading || !form.current) return;
    if (!form.current.checkValidity()) {
      setPane('info');
      requestAnimationFrame(() => { if (!controller.current?.signal.aborted) form.current?.reportValidity(); });
      return;
    }
    form.current.requestSubmit();
  };
  const member = selected?.spec.members.find(m => m.memberId === memberID) || selected?.spec.members[0];
  const memberFields = (title: string, items?: string[]) => items?.length ? <section className="ot-member-field"><h5>{title}</h5><ul>{items.map((item, index) => <li key={index}>{item}</li>)}</ul></section> : null;
  return <section className="ot-template-area" aria-busy={loading} aria-label="团队模板管理">
    <header className="ot-template-toolbar"><div><strong>我的模板</strong><span className="ot-muted">{templates.length} 个</span></div><div className="ot-actions">
      <button type="button" className="ot-secondary" disabled={busy || loading} onClick={() => void run(async () => { await refresh(); setNotice('模板列表已刷新，编辑内容已保留。'); })}><RefreshCw size={15}/>刷新模板</button>
      <button type="button" className="ot-secondary" disabled={busy || loading} onClick={() => choose(null)}><Plus size={15}/>自定义团队模板</button>
    </div></header>
    <div className="ot-template-manager">
      <aside className="ot-template-list" aria-label="模板列表">{loading && <p role="status" className="ot-muted">正在读取模板…</p>}{templates.map(t => <button type="button" disabled={busy} className="ot-master-item" aria-pressed={selected?.id === t.id} key={t.id} onClick={() => choose(t)}><strong>{t.name}</strong><span>{t.resolved_member_count} 人 · v{t.revision}</span></button>)}{!loading && !templates.length && <p className="ot-muted">还没有自定义模板，请描述目标生成。</p>}</aside>
      <section className="ot-template-editor ot-compact-editor" hidden={loading} aria-label="模板编辑区" data-pane={pane}>
        <div className="ot-template-title"><h3>{selected ? selected.name : '生成自定义团队模板'}</h3>{selected && <span className="ot-tag">自定义模板 · v{selected.revision}</span>}</div>
        <div className="ot-compact-tabs" role="tablist" aria-label="模板编辑内容"><button type="button" role="tab" aria-selected={pane === 'info'} onClick={() => setPane('info')}>团队信息</button><button type="button" role="tab" aria-selected={pane === 'members'} onClick={() => setPane('members')}>成员分工</button></div>
        <div className="ot-editor-columns">
          <section className="ot-template-information" aria-label="团队信息"><h4><NotebookPen size={15}/>团队信息</h4><form ref={form} id="ot-template-form" onSubmit={event => { event.preventDefault(); void run(() => generate(selected ? 'revise' : '', { intent: draft.intent, member_count: draft.count, ...(selected ? { name: draft.name, expected_revision: draft.revision ?? selected.revision } : {}) })); }}>
            {selected && <label>模板名称<input value={draft.name} maxLength={100} required disabled={busy} onChange={e => setDraft(v => ({ ...v, name: e.target.value }))}/></label>}
            <label>成员数量<input type="number" min={2} max={6} required disabled={busy} value={draft.count} onChange={e => setDraft(v => ({ ...v, count: Number(e.target.value) }))}/></label>
            <label>团队目标与分工<textarea aria-label="团队目标与分工" required maxLength={4000} rows={4} disabled={busy} value={draft.intent} onChange={e => setDraft(v => ({ ...v, intent: e.target.value }))} placeholder="描述希望团队完成的目标、成员分工与交付要求…"/></label>
          </form><p className="ot-muted">修订仅改变模板，已创建的团队保持不变。</p></section>
          <section className="ot-template-members" aria-label="成员与分工"><div className="ot-section-title"><h4><Users size={15}/>成员与分工</h4><span>{selected?.spec.members.length || 0} 名成员</span></div>
            {!!selected && draft.count !== selected.spec.members.length && <p className="ot-member-count-note">当前模板 {selected.spec.members.length} 人；本次修订期望 {draft.count || '—'} 人，生成成功后更新。</p>}
            {member && selected ? <>
              <div className="ot-member-selector" role="tablist" aria-label="选择模板成员">{selected.spec.members.map((m, index) => <button className="ot-member-tab" type="button" role="tab" key={m.memberId} id={`${memberTabID}-${index}`} aria-controls={`${memberTabID}-detail`} aria-selected={m.memberId === member.memberId} onClick={() => setMemberID(m.memberId)}><TeamBadge iconKey={m.isLeader ? 'leader' : m.iconOverride || m.autoIconKey || m.iconKey || 'worker'} size="chat"/><span><strong title={m.displayName}>{m.displayName}</strong><small title={m.isLeader ? 'Leader' : m.role || 'Worker'}>{m.isLeader ? 'Leader' : m.role || 'Worker'}</small></span></button>)}</div>
              <label className="ot-mobile-member-select">成员<select aria-label="当前模板成员" value={member.memberId} onChange={event => setMemberID(event.target.value)}>{selected.spec.members.map(m => <option key={m.memberId} value={m.memberId}>{m.displayName} · {m.isLeader ? 'Leader' : m.role || 'Worker'}</option>)}</select><small>共 {selected.spec.members.length} 人</small></label>
              <div className="ot-member-detail" id={`${memberTabID}-detail`} role="tabpanel" aria-label={`${member.displayName}详情`}>
                <div className="ot-member-detail-tabs" role="tablist" aria-label="成员信息"><button type="button" role="tab" aria-selected={detailTab === 'delivery'} onClick={() => setDetailTab('delivery')}>职责与交付</button><button type="button" role="tab" aria-selected={detailTab === 'collaboration'} onClick={() => setDetailTab('collaboration')}>协作与边界</button></div>
                <div className="ot-member-detail-scroll" aria-label="成员职责详情"><p className="ot-member-summary">{member.summary}</p><div className="ot-member-detail-grid">{detailTab === 'delivery' ? <><div><h5>职责与交付</h5><p>{member.mission}</p>{memberFields('主要职责', member.responsibilities)}</div><div>{memberFields('交付物', member.deliverables)}{memberFields('验收标准', member.acceptanceCriteria)}</div></> : <><div>{memberFields('所需输入', member.expectedInputs)}{memberFields('协作说明', member.collaborationNotes)}</div><div>{memberFields('工作边界', member.boundaries)}{memberFields('能力标签', member.capabilityTags)}</div></>}</div></div>
                <footer className="ot-member-detail-actions"><button type="button" className="ot-secondary" disabled={!canGenerate || busy} onClick={() => { setAdjustMember(member.memberId); setInstruction(''); }}><PencilLine size={14}/>调整职责</button><button type="button" className="ot-secondary" disabled={!canGenerate || busy} onClick={() => void run(() => generate(`members/${encodeURIComponent(member.memberId)}/regenerate`, { expected_revision: draft.revision ?? selected.revision }))}><RefreshCw size={14}/>重新生成成员</button><TeamIconPicker label={`${member.displayName}图标`} value={member.iconOverride} autoKey={member.isLeader ? 'leader' : member.autoIconKey || member.iconKey || 'worker'} disabled={busy || member.isLeader} onChange={value => void run(async () => { const t = await ownerTeamService.templateIcon(selected.id, member.memberId, { icon_override: value, expected_revision: draft.revision ?? selected.revision }); accept(t, false); await refresh(); setNotice('成员图标已保存。'); })}/></footer>
              </div>
            </> : <div className="ot-empty">生成模板后，在此查看和调整成员分工。</div>}
          </section>
        </div>
      </section>
    </div>
    <footer className="ot-template-footer"><span className="ot-muted">{dirty ? '有尚未保存的修改' : '修改仅应用于模板'}</span><div className="ot-actions">
      {selected && <><button type="button" className="ot-secondary" disabled={busy || !draft.name.trim()} onClick={() => void run(async () => { const t = await ownerTeamService.updateTemplate(selected.id, draft.name, draft.revision ?? selected.revision); accept(t, false); await refresh(); setNotice('模板名称已保存。'); })}><Check size={15}/>仅保存名称</button>
      <details className="ot-more" onKeyDown={event => { if (event.key === 'Escape') { event.currentTarget.open = false; event.currentTarget.querySelector('summary')?.focus(); } }}><summary className="ot-secondary" aria-label="更多模板操作"><MoreHorizontal size={16}/>更多</summary><div className="ot-more-menu"><button type="button" disabled={!canGenerate || busy} onClick={() => void run(() => generate('regenerate', { expected_revision: draft.revision ?? selected.revision }))}><RefreshCw size={15}/>重新生成模板</button><button type="button" className="ot-danger-text" disabled={busy} onClick={() => { if (window.confirm('删除模板不会改变已创建的团队，确认删除？')) void run(async () => { await ownerTeamService.deleteTemplate(selected.id); try { sessionStorage.removeItem(key(selected.id)); } catch { /* optional */ } await refresh(); choose(null); setNotice('模板已删除，已有团队保持不变。'); }); }}><Trash2 size={15}/>删除模板</button></div></details></>}
      <button type="button" onClick={submitTemplate} className="ot-primary" disabled={!canGenerate || busy || loading}><Sparkles size={15}/>{busy ? '处理中…' : selected ? '按说明修订模板' : '生成模板'}</button>
    </div></footer>
    {adjustMember && <OwnerTeamDialog title="调整成员职责" busy={busy} dirty={!!instruction} onClose={() => { setAdjustMember(null); setInstruction(''); }}><form onSubmit={event => { event.preventDefault(); void run(async () => { await generate(`members/${encodeURIComponent(adjustMember)}/adjust`, { instruction, expected_revision: draft.revision ?? selected!.revision }); if (!controller.current?.signal.aborted) { setAdjustMember(null); setInstruction(''); } }); }}><div className="ot-form-scroll"><label>调整说明<textarea required maxLength={4000} rows={5} disabled={busy} value={instruction} onChange={e => setInstruction(e.target.value)} placeholder="描述希望补充或调整的职责…"/></label>{error && <p role="alert" className="ot-error">{error}</p>}</div><footer><button type="button" className="ot-secondary" disabled={busy} onClick={() => { if (!instruction || window.confirm('放弃本次职责调整说明？')) { setAdjustMember(null); setInstruction(''); } }}>取消</button><button className="ot-primary" disabled={busy || !instruction.trim()}><Sparkles size={15}/>应用调整</button></footer></form></OwnerTeamDialog>}
  </section>;
}
