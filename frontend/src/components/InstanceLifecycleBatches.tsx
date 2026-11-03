import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import api from "../services/api";
import ConfirmDialog from "./ConfirmDialog";

type Batch = { batch_id: string; action: "restart" | "reset"; status: string; created_at: string; policy_version?: number; retry_round?: number };
type Operation = { operation_id?: string; instance_id?: number; status?: string; error_message?: string; started_at?: string; finished_at?: string };
type Detail = { batch: Batch; counts: Record<string, number>; total: number; page: number; page_size: number; limits?: {per_action: number; global: number; per_pod: number}; items: { item: { source_id: number; source_name: string; state: string }; operation?: Operation; attempts?: Operation[] }[] };
const labels: Record<string, string> = { running: "执行中", paused: "已暂停", cancelling: "取消中（等待进行中的操作完成）", cancelled: "已取消", completed: "已完成", completed_with_errors: "已结束，有失败或待清理项", pending: "排队中", active: "操作中", succeeded: "成功", failed: "失败", warning: "成功，旧实例待清理" };
const unfinished = (b: Batch) => ["running", "paused", "cancelling"].includes(b.status);

// Progress is fetched from the server; navigation and failed HTTP requests do
// not create/retry operations. The submission key survives ambiguous responses.
export default function InstanceLifecycleBatches({ selectedIds, onChanged, selectionControl, selectionBusy = false }: { selectedIds: number[]; onChanged: () => void; selectionControl?: ReactNode; selectionBusy?: boolean }) {
  const [batches, setBatches] = useState<Batch[]>([]);
  const [selected, setSelected] = useState("");
  const [detail, setDetail] = useState<Detail | null>(null);
  const [page, setPage] = useState(1);
  const [error, setError] = useState("");
  const [confirm, setConfirm] = useState<"restart" | "reset" | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [historyExpanded, setHistoryExpanded] = useState(false);
  const request = useRef<{ signature: string; key: string } | null>(null);
  const changed = useRef(onChanged);
  useEffect(() => { changed.current = onChanged; }, [onChanged]);
  const previous = useRef("");
  const refresh = useCallback(async () => {
    const result = await api.get("/instances/batch/lifecycle");
    const list = result.data.data as Batch[];
    setBatches(list);
    const id = selected || list.find(unfinished)?.batch_id || list[0]?.batch_id;
    if (!id) return;
    const response = await api.get(`/instances/batch/lifecycle/${id}`, { params: { page } });
    const next = response.data.data as Detail;
    setDetail(next);
    const fingerprint = JSON.stringify([id, next.counts]);
    if (previous.current && previous.current !== fingerprint) changed.current();
    previous.current = fingerprint;
    setError("");
  }, [selected, page]);
  useEffect(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try { await refresh(); } catch { if (!stopped) setError("暂时无法查询进度，后台任务不会因此停止；请勿重复提交。"); }
      if (!stopped) timer = setTimeout(() => void poll(), 3000);
    };
    const focus = () => { if (!stopped) void refresh().catch(() => {}); };
    void poll();
    window.addEventListener("focus", focus);
    return () => { stopped = true; clearTimeout(timer); window.removeEventListener("focus", focus); };
  }, [refresh]);
  const submit = async () => {
    if (!confirm || !selectedIds.length || selectedIds.length > 500 || submitting || selectionBusy) return;
    const signature = JSON.stringify([confirm, [...selectedIds].sort((a, b) => a - b)]);
    if (request.current?.signature !== signature) request.current = { signature, key: crypto.randomUUID() };
    setSubmitting(true);
    try {
      const response = await api.post("/instances/batch/lifecycle", { instance_ids: selectedIds, action: confirm, request_id: request.current.key, confirm_data_deletion: confirm === "reset" });
      setSelected(response.data.data.batch_id);
      setHistoryExpanded(true);
      setPage(1);
      setConfirm(null);
      request.current = null;
      await refresh();
    } catch (e: unknown) {
      const message = (e as { response?: { data?: { message?: string } } }).response?.data?.message;
      setError(message || "提交结果暂未确认。请先查看任务列表；再次确认将使用相同请求编号，避免重复执行。");
    } finally { setSubmitting(false); }
  };
  const control = async (action: string) => {
    if (!detail) return;
    try { await api.post(`/instances/batch/lifecycle/${detail.batch.batch_id}/${action}`); await refresh(); }
    catch { setError("操作未确认，请刷新查询任务状态。"); }
  };
  return <div className="mb-4 rounded-xl border border-slate-200 bg-white p-3">
    <div className="flex flex-wrap items-center gap-2">
      <button className="app-button-secondary text-blue-700" disabled={!selectedIds.length || selectedIds.length > 500 || submitting || selectionBusy} onClick={() => setConfirm("restart")}>批量重启{selectedIds.length ? `（${selectedIds.length}）` : ""}</button>
      <button className="app-button-secondary border-red-200 bg-red-50 text-red-600" disabled={!selectedIds.length || selectedIds.length > 500 || submitting || selectionBusy} onClick={() => setConfirm("reset")}>批量重置{selectedIds.length ? `（${selectedIds.length}）` : ""}</button>
      <span className="text-xs text-slate-500">仅处理已勾选 Lite。切换页面不影响执行。</span>
      {selectionControl}
      <span role="status" className="text-sm font-medium text-slate-700">已选中 {selectedIds.length} 个实例（含跨页选择）</span>
      {!!batches.length && <button type="button" className="ml-auto text-sm text-blue-700" aria-expanded={historyExpanded} aria-controls="instance-batch-history" onClick={() => setHistoryExpanded(value => !value)}>
        {historyExpanded ? "收起任务记录" : "展开任务记录"}{batches.some(unfinished) ? "（有未完成任务）" : ""}
      </button>}
    </div>
    {error && <p role="alert" className="mt-2 text-sm text-red-600">{error}</p>}
    {!!batches.length && <div id="instance-batch-history" hidden={!historyExpanded} className="mt-3 text-sm">
      <select aria-label="批任务" className="max-w-full rounded border p-1" value={selected || detail?.batch.batch_id || ""} onChange={e => { setSelected(e.target.value); setPage(1); }}>
        {batches.map(b => <option key={b.batch_id} value={b.batch_id}>{b.action === "reset" ? "重置" : "重启"} · {new Date(b.created_at).toLocaleString()} · {labels[b.status] || b.status}</option>)}
      </select>
      {detail && <>
        <p className="my-2">{labels[detail.batch.status]} · 共 {detail.total} 个 · {Object.entries(detail.counts).map(([state, count]) => `${labels[state] || state} ${count}`).join(" / ")}</p>
        <p className="mb-2 text-xs text-slate-500">{detail.batch.policy_version === 2 ? (detail.batch.retry_round ? "失败补试轮（最多一次）" : "首轮执行") : "旧版任务"}。本任务执行中 {detail.counts.active || 0} 个（含等待就绪、切换和清理），排队 {detail.counts.pending || 0} 个。本类最多同时执行 {detail.limits?.per_action ?? 5} 个，重启与重置合计最多 {detail.limits?.global ?? 10} 个；{detail.batch.policy_version === 2 ? `同 Pod 最多 ${detail.limits?.per_pod ?? 5} 个，完成一个补一个。失败项首轮结束后安全补试一次。` : "旧任务仍按原有同 Pod 互斥执行。"}</p>
        {detail.batch.status === "cancelling" && <p role="status">已停止后续派发和自动补试，等待 {detail.counts.active || 0} 个在途操作安全完成；不会强杀正在重启的实例。</p>}
        {unfinished(detail.batch) && <div className="mb-2 flex gap-2">
          {detail.batch.status !== "cancelling" && <button className="app-button-secondary" onClick={() => void control(detail.batch.status === "paused" ? "resume" : "pause")}>{detail.batch.status === "paused" ? "继续剩余任务" : "暂停派发"}</button>}
          {detail.batch.status !== "cancelling" && <button className="app-button-secondary" onClick={() => void control("cancel")}>停止任务（在途操作继续完成）</button>}
        </div>}
        <details><summary className="cursor-pointer">查看逐项结果（重置成功后显示新实例 ID）</summary>
          {detail.items.map(({ item, operation, attempts }) => <div key={item.source_id} className="border-b py-1">#{item.source_id} {item.source_name} · {labels[item.state] || item.state}{operation?.instance_id && operation.instance_id !== item.source_id ? ` → #${operation.instance_id}` : ""}{operation?.error_message ? ` · ${operation.error_message}` : ""}{!!attempts?.length && <details><summary>查看两次尝试记录</summary>{attempts.map((attempt, index) => <p key={attempt.operation_id}>第 {index + 1} 次 · {labels[attempt.status || ""] || attempt.status} {attempt.started_at ? new Date(attempt.started_at).toLocaleString() : ""} {attempt.error_message}</p>)}</details>}</div>)}
          <div className="mt-2 flex gap-3"><button disabled={page <= 1} onClick={() => setPage(p => p - 1)}>上一页</button><span>{page} / {Math.max(1, Math.ceil(detail.total / detail.page_size))}</span><button disabled={page * detail.page_size >= detail.total} onClick={() => setPage(p => p + 1)}>下一页</button></div>
        </details>
      </>}
    </div>}
    <ConfirmDialog open={confirm !== null} cancelLabel="取消" destructive={confirm === "reset"} title={confirm === "reset" ? "确认批量重置" : "确认批量重启"} message={`本次共选中 ${selectedIds.length} 个 Lite 实例（包含其他页面的选择）。${confirm === "reset" ? "重置" : "重启"}最多同时执行 5 个，两类操作合计最多 10 个，同 Pod 最多 5 个，所有批任务共享额度，完成一个补一个。首轮结束后安全补试失败项一次；停止任务只停止后续派发，正在执行的操作会安全完成。${confirm === "reset" ? "重置会删除旧实例的全部数据、对话和配置，无法撤销。新实例未就绪时不会删除旧实例；成功后旧数据不会保留。" : "重启保留已保存的工作区和对话；当前连接会中断，请先保存尚未落盘的内容。"}`} confirmLabel={confirm === "reset" ? "确认删除数据并重置" : "确认重启"} onConfirm={() => void submit()} onCancel={() => !submitting && setConfirm(null)} loading={submitting} />
  </div>;
}
