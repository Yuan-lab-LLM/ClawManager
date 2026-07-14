import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import AdminLayout from '../../../../components/AdminLayout';

// 容器隔离 (scenario K) — 对齐 specs/001-clawmanager-hardening/prototypes/scenario-k-container.html
// 单页结构：Hero + 容器内文件/进程防护表 + 容器逃逸监控 + 容器防护日志
// 原型态：本地 state 模拟交互，未接 host-side bridge

// ===========================
// 共享 UI（与 HostHardeningPage 风格一致；为保持页面隔离，本文件内复制一份小工具集）
// ===========================

type ToastKind = 'info' | 'success' | 'warning';
type ToastState = { message: string; kind: ToastKind } | null;

const Toast: React.FC<{ toast: ToastState; onClose: () => void }> = ({ toast, onClose }) => {
  React.useEffect(() => {
    if (!toast) return;
    const t = setTimeout(onClose, 2000);
    return () => clearTimeout(t);
  }, [toast, onClose]);
  if (!toast) return null;
  const bg = toast.kind === 'success' ? '#dcfce7' : toast.kind === 'warning' ? '#fef3c7' : '#dbeafe';
  const fg = toast.kind === 'success' ? '#166534' : toast.kind === 'warning' ? '#92400e' : '#1e40af';
  return (
    <div
      style={{
        position: 'fixed',
        bottom: 24,
        right: 24,
        zIndex: 100,
        padding: '10px 16px',
        background: bg,
        color: fg,
        borderRadius: 10,
        fontSize: 13,
        boxShadow: '0 8px 24px rgba(0,0,0,0.12)',
      }}
    >
      {toast.message}
    </div>
  );
};

const Modal: React.FC<{
  open: boolean;
  title: string;
  eyebrow?: string;
  onClose: () => void;
  children: React.ReactNode;
  footer: React.ReactNode;
  maxWidth?: number;
}> = ({ open, title, eyebrow, onClose, children, footer, maxWidth = 720 }) => {
  if (!open) return null;
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-[rgba(44,30,22,0.48)] px-4"
      onClick={onClose}
    >
      <div
        className="panel"
        style={{ maxWidth, width: '100%', maxHeight: '90vh', overflow: 'auto' }}
        onClick={(e) => e.stopPropagation()}
      >
        {eyebrow && <div className="eyebrow text-[10px] mb-1">{eyebrow}</div>}
        <h3 className="section-title-lg mb-4">{title}</h3>
        <div className="mb-5">{children}</div>
        <div className="flex justify-end gap-2">{footer}</div>
      </div>
    </div>
  );
};

const Toggle: React.FC<{ on: boolean; onChange: (v: boolean) => void }> = ({ on, onChange }) => (
  <div className={`toggle${on ? ' toggle-on' : ''}`} onClick={() => onChange(!on)}>
    <div className="toggle-thumb" />
  </div>
);

// ===========================
// 数据：容器列表 + 每个容器的规则
// ===========================

type Container = { name: string; id: string; image: string; created: string };
const CONTAINERS: Container[] = [
  { name: 'openclaw-prod-east-12', id: 'abc123def456', image: 'openclaw/runtime:v2.5', created: '2026-05-20 10:30' },
  { name: 'openclaw-finance-svc', id: 'def456abc789', image: 'openclaw/finance:v1.5', created: '2026-05-21 14:22' },
  { name: 'openclaw-ops-bot-3', id: '789abc012def', image: 'openclaw/ops:v3.1', created: '2026-05-19 09:15' },
  { name: 'openclaw-mcp-router', id: '012def345abc', image: 'openclaw/mcp:v2.2', created: '2026-05-22 16:08' },
  { name: 'openclaw-staging-7', id: '345abc678def', image: 'openclaw/staging:v0.9', created: '2026-05-23 11:40' },
];

type FileRule = { path: string; trust: string; r: boolean; w: boolean; x: boolean; d: boolean };
type ProcRule = { path: string; type: '进程保护' | '进程黑名单' };

const INITIAL_RULES: Record<string, { files: FileRule[]; processes: ProcRule[] }> = {
  'openclaw-prod-east-12': {
    files: [
      { path: '/etc/openclaw/config.yaml', trust: '/opt/openclaw/server', r: true, w: false, x: true, d: false },
      { path: '/var/lib/openclaw/data', trust: '/opt/openclaw/server', r: true, w: true, x: false, d: false },
      { path: '/var/log/openclaw/*', trust: '/opt/openclaw/server, /usr/sbin/rsyslogd', r: true, w: true, x: false, d: true },
    ],
    processes: [{ path: '/opt/openclaw/server', type: '进程保护' }],
  },
  'openclaw-finance-svc': {
    files: [
      { path: '/etc/finance/secret.key', trust: '/opt/openclaw/finance-svc', r: true, w: false, x: false, d: false },
      { path: '/var/lib/finance/db', trust: '/opt/openclaw/finance-svc', r: true, w: true, x: false, d: false },
    ],
    processes: [],
  },
  'openclaw-ops-bot-3': {
    files: [
      { path: '/etc/clawmanager', trust: '/opt/clawmanager/server', r: true, w: true, x: false, d: false },
      { path: '/opt/clawmanager/config/*', trust: '/opt/clawmanager/server', r: true, w: true, x: false, d: false },
      { path: '/var/lib/mysql/clawmanager', trust: '/usr/sbin/mysqld', r: true, w: true, x: false, d: false },
      { path: '/var/log/clawmanager/*', trust: '/opt/clawmanager/server', r: true, w: true, x: false, d: true },
      { path: '/etc/nginx/nginx.conf', trust: '/usr/sbin/nginx', r: true, w: false, x: false, d: false },
    ],
    processes: [
      { path: '/usr/sbin/auditd', type: '进程保护' },
      { path: '/usr/bin/nc', type: '进程黑名单' },
    ],
  },
  'openclaw-mcp-router': {
    files: [{ path: '/etc/mcp/routes.yaml', trust: '/opt/openclaw/mcp-router', r: true, w: false, x: true, d: false }],
    processes: [{ path: '/opt/openclaw/mcp-router', type: '进程保护' }],
  },
  'openclaw-staging-7': { files: [], processes: [] },
};

const INITIAL_MOUNT_WHITELIST = ['/var/run/openclaw/sockets', '/var/log/openclaw'];

const CONTAINER_LOG: Array<[string, string, string, string, string, string, string]> = [
  ['2026-05-24 10:23:14', 'openclaw-prod-east-12', 'root', '/opt/openclaw/server (pid 8421)', '/etc/openclaw/config.yaml', '写入', '已阻断'],
  ['2026-05-24 09:55:02', 'openclaw-ops-bot-3', 'admin', '/bin/sh (pid 12044)', '/opt/clawmanager/config/secret.yaml', '写入', '已阻断'],
  ['2026-05-24 09:30:48', 'openclaw-mcp-router', 'root', '/usr/bin/curl (pid 3142)', '/etc/mcp/routes.yaml', '写入', '已阻断'],
  ['2026-05-24 08:12:33', 'openclaw-finance-svc', 'app', '/opt/openclaw/finance-svc (pid 1024)', '/var/lib/finance/db', '删除', '已阻断'],
  ['2026-05-24 07:48:15', 'openclaw-prod-east-12', 'root', '/opt/openclaw/server (pid 8421)', '/var/log/openclaw/access.log', '写入', '放行'],
  ['2026-05-24 06:21:09', 'openclaw-ops-bot-3', 'root', '/usr/bin/nc (pid 7811)', '—', '启动', '已阻断'],
  ['2026-05-24 04:11:52', 'openclaw-prod-east-12', 'root', 'docker (pid 2210)', '/host/var/run/docker.sock', 'mount', '已阻断'],
];

// ===========================
// 主页
// ===========================

const ContainerPage: React.FC = () => {
  const [masterOn, setMasterOn] = useState(true);
  const [defenseMode, setDefenseMode] = useState<'block' | 'monitor'>('block');
  const [mountWhitelist, setMountWhitelist] = useState<string[]>(INITIAL_MOUNT_WHITELIST);
  const [forbidPrivilegedContainer, setForbidPrivilegedContainer] = useState(true);
  const [rules, setRules] = useState<Record<string, { files: FileRule[]; processes: ProcRule[] }>>(INITIAL_RULES);
  const [toast, setToast] = useState<ToastState>(null);
  const fireToast = (message: string, kind: ToastKind = 'info') => setToast({ message, kind });

  type ModalState =
    | null
    | { kind: 'mount-add' }
    | { kind: 'file-rules-manager'; container: string }
    | { kind: 'proc-rules-manager'; container: string }
    | { kind: 'file-rule-add'; container: string }
    | { kind: 'proc-rule-add'; container: string };
  const [modal, setModal] = useState<ModalState>(null);
  const closeModal = () => setModal(null);

  // 容器规则数 helper
  const containerRuleCount = (name: string) => {
    const r = rules[name] || { files: [], processes: [] };
    return { files: r.files.length, processes: r.processes.length };
  };

  // 统计：受保护容器（至少 1 条规则的）
  const protectedContainers = CONTAINERS.filter((c) => {
    const { files, processes } = containerRuleCount(c.name);
    return files > 0 || processes > 0;
  }).length;
  const totalRules = CONTAINERS.reduce(
    (acc, c) => {
      const r = rules[c.name] || { files: [], processes: [] };
      return { files: acc.files + r.files.length, processes: acc.processes + r.processes.length };
    },
    { files: 0, processes: 0 },
  );

  return (
    <AdminLayout>
      <div className="secp-scope space-y-6">
        <div className="crumb">
          <Link to="/admin/secplane">安全防护</Link>
          <span>/</span>
          <Link to="/admin/secplane/cat-isolate">环境隔离与安全增强</Link>
          <span>/</span>
          <span className="crumb-current">容器隔离</span>
        </div>

        {/* Hero */}
        <div className="panel">
          <div className="flex items-start justify-between gap-6 mb-5">
            <div className="hero-block flex-1">
              <div className="h-eyebrow">容器层加固 · 防逃逸 + 文件/进程防护</div>
              <h2 className="h-title">容器隔离</h2>
              <p className="h-subtitle">为运行中的容器配置文件 / 进程防护规则，并通过 mount 白名单与特权容器创建限制阻断逃逸攻击。</p>
            </div>
            <div className="flex flex-col items-end gap-3 shrink-0">
              <div className="flex items-center gap-2">
                <span className="text-xs muted-strong">容器隔离总开关</span>
                <Toggle on={masterOn} onChange={(v) => { setMasterOn(v); fireToast('容器隔离已切换', 'info'); }} />
                <button className="btn-primary btn-sm" onClick={() => fireToast('配置已保存', 'success')}>保存并应用</button>
              </div>
              <div className="flex items-center gap-3">
                <span className="text-xs muted">防御模式</span>
                <label className="flex items-center gap-1.5 cursor-pointer text-sm">
                  <input type="radio" name="kMode" checked={defenseMode === 'block'} onChange={() => setDefenseMode('block')} style={{ accentColor: '#dc2626' }} />
                  拦截模式
                </label>
                <label className="flex items-center gap-1.5 cursor-pointer text-sm">
                  <input type="radio" name="kMode" checked={defenseMode === 'monitor'} onChange={() => setDefenseMode('monitor')} style={{ accentColor: '#dc2626' }} />
                  监控模式
                </label>
              </div>
            </div>
          </div>
          <div className="grid grid-cols-4 gap-3">
            <div className="stat-card">
              <div className="stat-card-label">受保护容器</div>
              <div className="stat-card-value">
                {protectedContainers}
                <span className="text-base muted-strong">/{CONTAINERS.length}</span>
              </div>
              <div className="stat-card-sub muted-strong">{CONTAINERS.length - protectedContainers} 个容器暂未配置规则</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">防护规则总数</div>
              <div className="stat-card-value">{totalRules.files + totalRules.processes}</div>
              <div className="stat-card-sub muted-strong">文件 {totalRules.files} + 进程 {totalRules.processes}</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">24h 防护拦截</div>
              <div className="stat-card-value tone-red">6</div>
              <div className="stat-card-sub muted-strong">文件 4 · 进程 1 · mount 1</div>
            </div>
            <div className="stat-card">
              <div className="stat-card-label">逃逸尝试拦截</div>
              <div className="stat-card-value tone-red">3</div>
              <div className="stat-card-sub muted-strong">mount 2 · 特权容器 1</div>
            </div>
          </div>
        </div>

        {/* 容器内文件与进程防护 */}
        <div className="panel">
          <div className="mb-4">
            <div className="eyebrow">容器维度防护 · 文件 + 进程</div>
            <h3 className="section-title-lg mt-1">容器内文件与进程防护</h3>
          </div>
          <div className="text-xs muted mb-3">
            基于容器列表为每个运行中的容器配置文件/进程级防护规则。总开关与防御模式由页面顶部统一控制。
          </div>
          <table className="tbl">
            <thead>
              <tr>
                <th>容器名称</th>
                <th style={{ width: 140 }}>容器ID</th>
                <th>容器镜像</th>
                <th style={{ width: 140 }}>创建时间</th>
                <th style={{ width: 140, textAlign: 'center' }}>文件防护</th>
                <th style={{ width: 140, textAlign: 'center' }}>进程防护</th>
              </tr>
            </thead>
            <tbody>
              {CONTAINERS.map((c) => {
                const counts = containerRuleCount(c.name);
                return (
                  <tr key={c.id}>
                    <td><code className="font-mono text-xs font-bold text-[#171212]">{c.name}</code></td>
                    <td><code className="font-mono text-xs muted-strong">{c.id}</code></td>
                    <td><code className="font-mono text-xs muted">{c.image}</code></td>
                    <td><span className="text-xs muted-strong">{c.created}</span></td>
                    <td style={{ textAlign: 'center' }}>
                      <span
                        className={`badge badge-${counts.files > 0 ? 'green' : 'slate'} text-[10px]`}
                        style={{ marginRight: 6 }}
                      >
                        {counts.files} 条
                      </span>
                      <button
                        className="text-xs"
                        style={{ color: '#dc2626', fontWeight: 500 }}
                        onClick={() => setModal({ kind: 'file-rules-manager', container: c.name })}
                      >
                        管理
                      </button>
                    </td>
                    <td style={{ textAlign: 'center' }}>
                      <span
                        className={`badge badge-${counts.processes > 0 ? 'green' : 'slate'} text-[10px]`}
                        style={{ marginRight: 6 }}
                      >
                        {counts.processes} 条
                      </span>
                      <button
                        className="text-xs"
                        style={{ color: '#dc2626', fontWeight: 500 }}
                        onClick={() => setModal({ kind: 'proc-rules-manager', container: c.name })}
                      >
                        管理
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        {/* 容器逃逸监控 */}
        <div className="panel">
          <div className="mb-4">
            <div className="eyebrow">容器逃逸监控</div>
            <h3 className="section-title-lg mt-1">容器防逃逸</h3>
          </div>

          {/* mount 白名单 */}
          <div className="mb-5">
            <div className="flex items-center justify-between mb-3">
              <span className="font-semibold text-[#171212] text-sm">📁 mount 挂载白名单</span>
              <button className="btn-primary btn-sm" onClick={() => setModal({ kind: 'mount-add' })}>
                + 添加
              </button>
            </div>
            <table className="tbl">
              <thead>
                <tr>
                  <th>允许挂载目录</th>
                  <th style={{ width: 100 }}>操作</th>
                </tr>
              </thead>
              <tbody>
                {mountWhitelist.map((p, i) => (
                  <tr key={i}>
                    <td><code className="text-sm font-mono text-[#171212]">{p}</code></td>
                    <td>
                      <button
                        className="text-xs text-[#dc2626] font-semibold hover:underline"
                        onClick={() => {
                          setMountWhitelist(mountWhitelist.filter((_, idx) => idx !== i));
                          fireToast('已删除挂载白名单', 'success');
                        }}
                      >
                        删除
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            <div className="text-xs muted mt-2">未在白名单内的目录被 mount 进容器时将被拦截/告警。</div>
          </div>

          <div className="divider" />

          {/* 禁止在容器内创建特权容器 */}
          <div className="flex items-center justify-between mt-4">
            <div className="flex items-center gap-2">
              <span style={{ fontSize: 18 }}>🔒</span>
              <span className="font-semibold text-[#171212] text-sm">禁止在容器内创建特权容器</span>
              <span className="text-xs muted">阻止容器使用 --privileged 创建嵌套特权容器，规避逃逸常用手法</span>
            </div>
            <Toggle
              on={forbidPrivilegedContainer}
              onChange={(v) => {
                setForbidPrivilegedContainer(v);
                fireToast('特权容器创建限制已切换', 'info');
              }}
            />
          </div>
        </div>

        {/* 容器防护日志 */}
        <div className="panel">
          <div className="flex items-center justify-between mb-4">
            <div>
              <div className="eyebrow">安全日志 · 容器防护</div>
              <h3 className="section-title-lg mt-1">容器防护日志</h3>
            </div>
            <button className="btn-secondary btn-sm" onClick={() => fireToast('已刷新日志', 'info')}>刷新</button>
          </div>
          <table className="tbl">
            <thead>
              <tr>
                <th style={{ width: 140 }}>时间</th>
                <th style={{ width: 180 }}>容器</th>
                <th style={{ width: 100 }}>进程用户</th>
                <th>进程</th>
                <th>保护对象</th>
                <th style={{ width: 80 }}>动作</th>
                <th style={{ width: 100 }}>处理结果</th>
              </tr>
            </thead>
            <tbody>
              {CONTAINER_LOG.map(([t, c, user, proc, obj, act, result], i) => {
                const tone = result === '已阻断' ? 'red' : result === '放行' ? 'green' : 'slate';
                return (
                  <tr key={i}>
                    <td><span className="text-xs muted-strong font-mono">{t}</span></td>
                    <td><code className="font-mono text-xs font-bold text-[#171212]">{c}</code></td>
                    <td><span className="text-xs">{user}</span></td>
                    <td><code className="text-xs font-mono text-[#171212]">{proc}</code></td>
                    <td><code className="text-xs font-mono">{obj}</code></td>
                    <td><span className="badge badge-slate text-[10px]">{act}</span></td>
                    <td><span className={`badge badge-${tone}`}>{result}</span></td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        {/* ===== Modals ===== */}
        {modal?.kind === 'mount-add' && (
          <MountAddModal
            onCancel={closeModal}
            onConfirm={(lines) => {
              setMountWhitelist([...mountWhitelist, ...lines].slice(0, 20));
              closeModal();
              fireToast('已添加 mount 白名单', 'success');
            }}
          />
        )}

        {modal?.kind === 'file-rules-manager' && (
          <FileRulesManagerModal
            container={modal.container}
            rules={rules[modal.container]?.files || []}
            onClose={closeModal}
            onDelete={(i) => {
              const next = { ...rules };
              const cur = next[modal.container] || { files: [], processes: [] };
              cur.files = cur.files.filter((_, idx) => idx !== i);
              next[modal.container] = cur;
              setRules(next);
              fireToast('已删除规则', 'success');
            }}
            onAddClick={() => setModal({ kind: 'file-rule-add', container: modal.container })}
          />
        )}

        {modal?.kind === 'proc-rules-manager' && (
          <ProcRulesManagerModal
            container={modal.container}
            rules={rules[modal.container]?.processes || []}
            onClose={closeModal}
            onDelete={(i) => {
              const next = { ...rules };
              const cur = next[modal.container] || { files: [], processes: [] };
              cur.processes = cur.processes.filter((_, idx) => idx !== i);
              next[modal.container] = cur;
              setRules(next);
              fireToast('已删除规则', 'success');
            }}
            onAddClick={() => setModal({ kind: 'proc-rule-add', container: modal.container })}
          />
        )}

        {modal?.kind === 'file-rule-add' && (
          <FileRuleAddModal
            container={modal.container}
            onCancel={() => setModal({ kind: 'file-rules-manager', container: modal.container })}
            onConfirm={(rule) => {
              const next = { ...rules };
              const cur = next[modal.container] || { files: [], processes: [] };
              cur.files = [...cur.files, rule];
              next[modal.container] = cur;
              setRules(next);
              setModal({ kind: 'file-rules-manager', container: modal.container });
              fireToast(`已为 ${modal.container} 添加文件防护规则`, 'success');
            }}
          />
        )}

        {modal?.kind === 'proc-rule-add' && (
          <ProcRuleAddModal
            container={modal.container}
            onCancel={() => setModal({ kind: 'proc-rules-manager', container: modal.container })}
            onConfirm={(rule) => {
              const next = { ...rules };
              const cur = next[modal.container] || { files: [], processes: [] };
              cur.processes = [...cur.processes, rule];
              next[modal.container] = cur;
              setRules(next);
              setModal({ kind: 'proc-rules-manager', container: modal.container });
              fireToast(`已为 ${modal.container} 添加进程防护规则`, 'success');
            }}
          />
        )}

        <Toast toast={toast} onClose={() => setToast(null)} />
      </div>
    </AdminLayout>
  );
};

// ===========================
// Modals
// ===========================

const MountAddModal: React.FC<{ onCancel: () => void; onConfirm: (lines: string[]) => void }> = ({
  onCancel,
  onConfirm,
}) => {
  const [text, setText] = useState('');
  const lines = text.split('\n').map((l) => l.trim()).filter(Boolean);
  return (
    <Modal
      open
      eyebrow="新建"
      title="添加 mount 挂载白名单"
      onClose={onCancel}
      footer={
        <>
          <button className="btn-secondary" onClick={onCancel}>取消</button>
          <button className="btn-primary" disabled={lines.length === 0} onClick={() => onConfirm(lines)}>
            确定
          </button>
        </>
      }
    >
      <textarea
        className="input"
        rows={8}
        style={{
          width: '100%',
          fontFamily: 'ui-monospace,SFMono-Regular,Menlo,monospace',
          fontSize: 12,
          lineHeight: 1.6,
          resize: 'vertical',
        }}
        placeholder="支持输入单条/多条挂载目录，每行填写一条，最多支持 20 条"
        value={text}
        onChange={(e) => setText(e.target.value)}
      />
      <div className="text-xs muted mt-2">
        填写允许被容器 mount 的宿主机绝对路径。未列入白名单的 mount 行为将根据当前防御模式被拦截或告警。
      </div>
    </Modal>
  );
};

const FileRulesManagerModal: React.FC<{
  container: string;
  rules: FileRule[];
  onClose: () => void;
  onDelete: (i: number) => void;
  onAddClick: () => void;
}> = ({ container, rules, onClose, onDelete, onAddClick }) => (
  <Modal
    open
    eyebrow="规则管理"
    title={`文件防护规则 · ${container}`}
    onClose={onClose}
    footer={<button className="btn-secondary" onClick={onClose}>关闭</button>}
  >
    <div className="flex items-center justify-between mb-3">
      <div>
        <div className="eyebrow text-[10px]">容器</div>
        <code className="font-mono text-sm font-bold text-[#171212]">{container}</code>
      </div>
      <button className="btn-primary btn-sm" onClick={onAddClick}>+ 添加规则</button>
    </div>
    {rules.length === 0 ? (
      <div
        className="text-center py-10"
        style={{ background: '#fdfaf7', border: '1px dashed #eadfd8', borderRadius: 10 }}
      >
        <div className="text-sm muted">暂无文件防护规则</div>
        <div className="text-xs muted mt-1">点击右上角 "+ 添加规则" 为该容器创建第一条规则</div>
      </div>
    ) : (
      <table className="tbl" style={{ margin: 0 }}>
        <thead>
          <tr>
            <th>保护文件/目录</th>
            <th style={{ width: 160 }}>信任进程</th>
            <th style={{ width: 50, textAlign: 'center' }}>读</th>
            <th style={{ width: 50, textAlign: 'center' }}>写</th>
            <th style={{ width: 50, textAlign: 'center' }}>执行</th>
            <th style={{ width: 50, textAlign: 'center' }}>删除</th>
            <th style={{ width: 60 }}>操作</th>
          </tr>
        </thead>
        <tbody>
          {rules.map((rule, i) => (
            <tr key={i}>
              <td><code className="text-xs font-mono text-[#171212]">{rule.path}</code></td>
              <td><code className="text-xs font-mono muted-strong">{rule.trust}</code></td>
              {(['r', 'w', 'x', 'd'] as const).map((k) => (
                <td key={k} style={{ textAlign: 'center' }}>
                  <span className={`badge badge-${rule[k] ? 'green' : 'slate'} text-[10px]`}>
                    {rule[k] ? '✓' : '—'}
                  </span>
                </td>
              ))}
              <td>
                <button
                  className="text-xs text-[#dc2626] font-semibold hover:underline"
                  onClick={() => onDelete(i)}
                >
                  删除
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    )}
  </Modal>
);

const ProcRulesManagerModal: React.FC<{
  container: string;
  rules: ProcRule[];
  onClose: () => void;
  onDelete: (i: number) => void;
  onAddClick: () => void;
}> = ({ container, rules, onClose, onDelete, onAddClick }) => (
  <Modal
    open
    eyebrow="规则管理"
    title={`进程防护规则 · ${container}`}
    onClose={onClose}
    footer={<button className="btn-secondary" onClick={onClose}>关闭</button>}
    maxWidth={560}
  >
    <div className="flex items-center justify-between mb-3">
      <div>
        <div className="eyebrow text-[10px]">容器</div>
        <code className="font-mono text-sm font-bold text-[#171212]">{container}</code>
      </div>
      <button className="btn-primary btn-sm" onClick={onAddClick}>+ 添加规则</button>
    </div>
    {rules.length === 0 ? (
      <div
        className="text-center py-10"
        style={{ background: '#fdfaf7', border: '1px dashed #eadfd8', borderRadius: 10 }}
      >
        <div className="text-sm muted">暂无进程防护规则</div>
        <div className="text-xs muted mt-1">点击右上角 "+ 添加规则" 为该容器创建第一条规则</div>
      </div>
    ) : (
      <table className="tbl" style={{ margin: 0 }}>
        <thead>
          <tr>
            <th>进程路径</th>
            <th style={{ width: 130 }}>规则类型</th>
            <th style={{ width: 60 }}>操作</th>
          </tr>
        </thead>
        <tbody>
          {rules.map((rule, i) => (
            <tr key={i}>
              <td><code className="text-xs font-mono text-[#171212]">{rule.path}</code></td>
              <td>
                <span className={`badge badge-${rule.type === '进程保护' ? 'red' : 'orange'}`}>{rule.type}</span>
              </td>
              <td>
                <button
                  className="text-xs text-[#dc2626] font-semibold hover:underline"
                  onClick={() => onDelete(i)}
                >
                  删除
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    )}
  </Modal>
);

const FileRuleAddModal: React.FC<{
  container: string;
  onCancel: () => void;
  onConfirm: (rule: FileRule) => void;
}> = ({ container, onCancel, onConfirm }) => {
  const [path, setPath] = useState('');
  const [trust, setTrust] = useState('');
  const [r, setR] = useState(false);
  const [w, setW] = useState(false);
  const [x, setX] = useState(false);
  const [d, setD] = useState(false);
  const canSubmit = path.startsWith('/');
  return (
    <Modal
      open
      eyebrow="新建"
      title={`添加文件防护规则 · ${container}`}
      onClose={onCancel}
      footer={
        <>
          <button className="btn-secondary" onClick={onCancel}>取消</button>
          <button
            className="btn-primary"
            disabled={!canSubmit}
            onClick={() => onConfirm({ path, trust: trust || '-', r, w, x, d })}
          >
            确定
          </button>
        </>
      }
    >
      <div className="space-y-4">
        <div>
          <div className="eyebrow text-[10px] mb-1">容器</div>
          <input
            className="input"
            value={container}
            readOnly
            style={{ background: '#fdf6f1', color: '#7a4a30', fontFamily: 'ui-monospace,SFMono-Regular,Menlo,monospace' }}
          />
        </div>
        <div>
          <div className="eyebrow text-[10px] mb-1"><span style={{ color: '#dc2626' }}>*</span> 保护文件/目录</div>
          <input className="input" placeholder="请输入绝对路径（容器内路径）" value={path} onChange={(e) => setPath(e.target.value)} />
        </div>
        <div>
          <div className="eyebrow text-[10px] mb-1">信任进程</div>
          <textarea
            className="input"
            rows={4}
            style={{ fontFamily: 'ui-monospace,SFMono-Regular,Menlo,monospace', fontSize: 12, lineHeight: 1.6, resize: 'vertical' }}
            placeholder="支持输入单条/多条信任进程，每行填写一条，最多支持 20 条；支持输入目录，表示此目录下进程均信任"
            value={trust}
            onChange={(e) => setTrust(e.target.value)}
          />
        </div>
        <div>
          <div className="eyebrow text-[10px] mb-2">权限</div>
          <div className="flex items-center gap-5">
            <label className="flex items-center gap-1.5 cursor-pointer text-sm">
              <input type="checkbox" style={{ accentColor: '#dc2626' }} checked={r} onChange={(e) => setR(e.target.checked)} /> 读
            </label>
            <label className="flex items-center gap-1.5 cursor-pointer text-sm">
              <input type="checkbox" style={{ accentColor: '#dc2626' }} checked={w} onChange={(e) => setW(e.target.checked)} /> 写
            </label>
            <label className="flex items-center gap-1.5 cursor-pointer text-sm">
              <input type="checkbox" style={{ accentColor: '#dc2626' }} checked={x} onChange={(e) => setX(e.target.checked)} /> 执行
            </label>
            <label className="flex items-center gap-1.5 cursor-pointer text-sm">
              <input type="checkbox" style={{ accentColor: '#dc2626' }} checked={d} onChange={(e) => setD(e.target.checked)} /> 删除
            </label>
          </div>
        </div>
      </div>
    </Modal>
  );
};

const ProcRuleAddModal: React.FC<{
  container: string;
  onCancel: () => void;
  onConfirm: (rule: ProcRule) => void;
}> = ({ container, onCancel, onConfirm }) => {
  const [type, setType] = useState<ProcRule['type']>('进程保护');
  const [path, setPath] = useState('');
  const lines = path.split('\n').map((l) => l.trim()).filter(Boolean);
  return (
    <Modal
      open
      eyebrow="新建"
      title={`添加进程防护规则 · ${container}`}
      onClose={onCancel}
      footer={
        <>
          <button className="btn-secondary" onClick={onCancel}>取消</button>
          <button
            className="btn-primary"
            disabled={lines.length === 0}
            onClick={() => onConfirm({ path: lines[0], type })}
          >
            确定
          </button>
        </>
      }
    >
      <div className="space-y-4">
        <div>
          <div className="eyebrow text-[10px] mb-1">容器</div>
          <input
            className="input"
            value={container}
            readOnly
            style={{ background: '#fdf6f1', color: '#7a4a30', fontFamily: 'ui-monospace,SFMono-Regular,Menlo,monospace' }}
          />
        </div>
        <div>
          <div className="eyebrow text-[10px] mb-1"><span style={{ color: '#dc2626' }}>*</span> 规则类型</div>
          <select className="input" value={type} onChange={(e) => setType(e.target.value as ProcRule['type'])}>
            <option value="进程保护">进程保护</option>
            <option value="进程黑名单">进程黑名单</option>
          </select>
        </div>
        <div>
          <div className="eyebrow text-[10px] mb-1"><span style={{ color: '#dc2626' }}>*</span> 进程路径</div>
          <textarea
            className="input"
            rows={5}
            style={{ fontFamily: 'ui-monospace,SFMono-Regular,Menlo,monospace', fontSize: 12, lineHeight: 1.6, resize: 'vertical' }}
            placeholder="支持输入单条/多条进程路径（容器内路径），每行填写一条，最多支持 20 条"
            value={path}
            onChange={(e) => setPath(e.target.value)}
          />
        </div>
      </div>
    </Modal>
  );
};

export default ContainerPage;
