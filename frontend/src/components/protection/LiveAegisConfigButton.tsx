import React, { useCallback, useEffect, useState } from 'react';
import { secplaneService, type LiveAegisConfig } from '../../services/secplaneService';
import { instanceService } from '../../services/instanceService';

// 按钮 + modal — 拉取某个 openclaw 实例 pod 内 ClawAegisEx 实时 user_config.json
// 通过 GET /secplane/instances/:id/aegis/live-config
//   → backend 优先读 secplane_instance_runtime_config 表（每次 dispatch 成功都 upsert）
//   → 找不到时回退到 agent 上报的 skill_blob 解 zip（plugin auto-discover 安装路径下后者 404）
//   → 比 effective-config（DB 里 last-dispatched 那份）更接近 pod 真实状态

type InstanceLite = { id: number; name: string; status?: string };

const LiveAegisConfigButton: React.FC = () => {
  const [open, setOpen] = useState(false);
  const [instances, setInstances] = useState<InstanceLite[]>([]);
  const [picked, setPicked] = useState<number | null>(null);
  const [data, setData] = useState<LiveAegisConfig | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadInstances = useCallback(async () => {
    try {
      const list = await instanceService.getInstances(1, 1000);
      const items = (list?.instances ?? []) as Array<{ id: number; name: string; status?: string; type?: string }>;
      const openclawList = items
        .filter((i) => (i.type ?? 'openclaw') === 'openclaw')
        .map((i) => ({ id: i.id, name: i.name, status: i.status }));
      setInstances(openclawList);
      if (openclawList.length > 0 && picked === null) {
        const running = openclawList.find((i) => i.status === 'running');
        setPicked(running ? running.id : openclawList[0].id);
      }
    } catch (e) {
      const err = e as { message?: string };
      setError('加载实例列表失败：' + (err.message ?? '未知'));
    }
  }, [picked]);

  const fetchLive = useCallback(async (id: number) => {
    setLoading(true);
    setError(null);
    setData(null);
    try {
      const cfg = await secplaneService.getLiveAegisConfig(id);
      setData(cfg);
    } catch (e) {
      const err = e as { response?: { data?: { error?: string } }; message?: string };
      setError(err.response?.data?.error ?? err.message ?? '未知错误');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (open) loadInstances();
  }, [open, loadInstances]);

  const onOpen = () => setOpen(true);
  const onClose = () => {
    setOpen(false);
    setData(null);
    setError(null);
  };

  return (
    <>
      <button className="btn-secondary" onClick={onOpen}>
        <svg width="16" height="16" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
        </svg>
        Pod 实时 Aegis 配置
      </button>
      {open && (
        <div
          style={{
            position: 'fixed', inset: 0, zIndex: 1000, background: 'rgba(0,0,0,0.45)',
            display: 'flex', alignItems: 'flex-start', justifyContent: 'center',
            padding: '60px 20px', overflowY: 'auto',
          }}
          onClick={onClose}
        >
          <div
            style={{
              background: 'white', borderRadius: 24, boxShadow: '0 24px 48px rgba(0,0,0,0.18)',
              maxWidth: 780, width: '100%', maxHeight: '85vh', display: 'flex', flexDirection: 'column',
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <div style={{ padding: '20px 24px', borderBottom: '1px solid #eadfd8', display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 16 }}>
              <div>
                <h3 style={{ fontSize: '1.05rem', fontWeight: 700, color: '#171212', margin: 0 }}>
                  Pod 实时 ClawAegisEx user_config
                </h3>
                <p className="muted-strong" style={{ fontSize: 11, marginTop: 4, marginBottom: 0 }}>
                  从 agent 最近一次上传的 skill_blob 解出 — 比 last-dispatched 更接近 pod 真实状态
                </p>
              </div>
              <button
                onClick={onClose}
                style={{ background: 'transparent', border: 'none', fontSize: 22, cursor: 'pointer', color: '#7a4a30', lineHeight: 1, padding: '0 4px' }}
                aria-label="关闭"
              >×</button>
            </div>

            <div style={{ padding: '16px 24px', borderBottom: '1px solid #eadfd8', display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
              <span className="text-xs muted-strong">实例：</span>
              <select
                className="input"
                value={picked ?? ''}
                onChange={(e) => setPicked(e.target.value ? Number(e.target.value) : null)}
                style={{ minWidth: 280 }}
              >
                <option value="">— 选择 openclaw 实例 —</option>
                {instances.map((i) => (
                  <option key={i.id} value={i.id}>
                    [{i.id}] {i.name}{i.status ? ` (${i.status})` : ''}
                  </option>
                ))}
              </select>
              <button
                className="btn-primary btn-sm"
                disabled={loading || picked === null}
                onClick={() => picked !== null && fetchLive(picked)}
              >
                {loading ? '拉取中…' : '拉取实时配置'}
              </button>
            </div>

            <div style={{ padding: '20px 24px', overflowY: 'auto', flex: 1 }}>
              {error && (
                <div className="alert alert-danger">
                  <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 9v2m0 4h.01" />
                  </svg>
                  {error}
                </div>
              )}
              {!error && !data && !loading && (
                <div className="muted text-sm">选实例 → 点"拉取实时配置"。后端优先读 <code>secplane_instance_runtime_config</code> 表（每次 dispatch 成功都 upsert），找不到时回退到 agent 上报的 skill_blob 解 zip。</div>
              )}
              {data && (
                <>
                  <div style={{ display: 'grid', gridTemplateColumns: '110px 1fr', gap: '6px 12px', marginBottom: 12, fontSize: 12 }}>
                    <span className="muted-strong">Skill</span>
                    <span><code className="text-xs">{data.skill_name ?? 'clawaegisex'}</code>{data.skill_id ? <> (id={data.skill_id})</> : null}</span>
                    <span className="muted-strong">来源</span>
                    <span>
                      <code className="text-xs">{data.provenance}</code>
                      {data.source && <> · <code className="text-xs muted">{data.source}</code></>}
                    </span>
                    {data.revision && (
                      <>
                        <span className="muted-strong">Revision</span>
                        <code className="text-xs muted">{data.revision}</code>
                      </>
                    )}
                    {data.config_sha256 && (
                      <>
                        <span className="muted-strong">Config sha256</span>
                        <code className="text-xs muted">{data.config_sha256.slice(0, 24)}…</code>
                      </>
                    )}
                    {data.blob_content_hash && (
                      <>
                        <span className="muted-strong">Blob hash</span>
                        <code className="text-xs muted">{data.blob_content_hash.slice(0, 24)}…</code>
                      </>
                    )}
                    {data.source_file && (
                      <>
                        <span className="muted-strong">Source file</span>
                        <code className="text-xs muted">{data.source_file}</code>
                      </>
                    )}
                    {data.command_id && (
                      <>
                        <span className="muted-strong">Command ID</span>
                        <code className="text-xs">#{data.command_id}</code>
                      </>
                    )}
                    {data.dispatched_at && (
                      <>
                        <span className="muted-strong">Dispatched at</span>
                        <span className="text-xs muted">{data.dispatched_at}</span>
                      </>
                    )}
                    <span className="muted-strong">Fetched at</span>
                    <span className="text-xs muted">{data.fetched_at}</span>
                  </div>
                  <div className="divider" />
                  <pre
                    style={{
                      background: '#1c1611', color: '#fde8d6', padding: '14px 16px', borderRadius: 10,
                      fontFamily: "ui-monospace, 'SF Mono', Menlo, Consolas, monospace",
                      fontSize: 12, lineHeight: 1.55, overflowX: 'auto', margin: 0,
                    }}
                  >
                    {JSON.stringify(data.user_config, null, 2)}
                  </pre>
                </>
              )}
            </div>

            <div style={{ padding: '14px 24px', borderTop: '1px solid #eadfd8', display: 'flex', justifyContent: 'flex-end' }}>
              <button className="btn-secondary btn-sm" onClick={onClose}>关闭</button>
            </div>
          </div>
        </div>
      )}
    </>
  );
};

export default LiveAegisConfigButton;
