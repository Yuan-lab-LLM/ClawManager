import React, { useEffect, useMemo, useState } from 'react';
import AdminLayout from '../../components/AdminLayout';
import { useI18n } from '../../contexts/I18nContext';
import {
  adminService,
  type AdminSkillRecord,
  type SecurityScanConfig,
  type SecurityScanJob,
} from '../../services/adminService';

const AdminSkillsPage: React.FC = () => {
  const { t } = useI18n();
  const [skills, setSkills] = useState<AdminSkillRecord[]>([]);
  const [jobs, setJobs] = useState<SecurityScanJob[]>([]);
  const [selectedJobId, setSelectedJobId] = useState<number | null>(null);
  const [selectedJob, setSelectedJob] = useState<SecurityScanJob | null>(null);
  const [config, setConfig] = useState<SecurityScanConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [savingConfig, setSavingConfig] = useState(false);
  const [startingScan, setStartingScan] = useState<'' | 'quick' | 'deep'>('');
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState('');
  const [assetPage, setAssetPage] = useState(1);
  const [selectedAssetId, setSelectedAssetId] = useState<number | null>(null);

  const pageSize = 10;

  const loadAll = async (mode: 'initial' | 'refresh' = 'initial') => {
    try {
      if (mode === 'initial') {
        setLoading(true);
      } else {
        setRefreshing(true);
      }
      setError(null);
      const [skillItems, jobItems, configItem] = await Promise.all([
        adminService.listSkills(),
        adminService.listSecurityScanJobs(20),
        adminService.getSecurityConfig(),
      ]);
      setSkills(skillItems);
      setAssetPage(1);
      setSelectedAssetId((current) => {
        if (current && skillItems.some((item) => item.id === current)) {
          return current;
        }
        return skillItems.find((item) => item.risk_level === 'high')?.id ?? skillItems[0]?.id ?? null;
      });
      setJobs(jobItems);
      setConfig(configItem);
      const activeJobId = selectedJobId ?? jobItems[0]?.id ?? null;
      setSelectedJobId(activeJobId);
      if (activeJobId) {
        const detail = await adminService.getSecurityScanJob(activeJobId);
        setSelectedJob(detail);
      } else {
        setSelectedJob(null);
      }
    } catch (err: any) {
      setError(err.response?.data?.error || '加载安全中心失败。');
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  };

  useEffect(() => {
    void loadAll();
  }, []);

  useEffect(() => {
    setAssetPage(1);
  }, [search]);

  useEffect(() => {
    const hasRunningJob = jobs.some((job) => job.status === 'queued' || job.status === 'running');
    if (!hasRunningJob) {
      return;
    }
    const timer = window.setInterval(() => {
      void loadAll('refresh');
    }, 3000);
    return () => window.clearInterval(timer);
  }, [jobs, selectedJobId]);

  const filteredSkills = useMemo(() => {
    const keyword = search.trim().toLowerCase();
    if (!keyword) {
      return skills;
    }
    return skills.filter((item) =>
      [item.name, item.skill_key, item.source_type, item.risk_level, item.scan_status, String(item.user_id)].some((value) =>
        value.toLowerCase().includes(keyword),
      ),
    );
  }, [search, skills]);

  const summary = useMemo(() => ({
    total: skills.length,
    uploaded: skills.filter((item) => item.source_type === 'uploaded').length,
    discovered: skills.filter((item) => item.source_type !== 'uploaded').length,
    highRisk: skills.filter((item) => item.risk_level === 'high').length,
  }), [skills]);

  const totalAssetPages = Math.max(1, Math.ceil(filteredSkills.length / pageSize));
  const currentAssetPage = Math.min(assetPage, totalAssetPages);
  const paginatedSkills = useMemo(() => {
    const start = (currentAssetPage - 1) * pageSize;
    return filteredSkills.slice(start, start + pageSize);
  }, [currentAssetPage, filteredSkills]);
  const selectedAsset = useMemo(
    () => filteredSkills.find((item) => item.id === selectedAssetId) ?? skills.find((item) => item.id === selectedAssetId) ?? null,
    [filteredSkills, selectedAssetId, skills],
  );

  const handleStartScan = async (scanMode: 'quick' | 'deep') => {
    try {
      setStartingScan(scanMode);
      setError(null);
      const job = await adminService.startSecurityScan({ asset_type: 'skill', scan_mode: scanMode });
      setSelectedJobId(job.id);
      await loadAll('refresh');
    } catch (err: any) {
      setError(err.response?.data?.error || `启动${scanMode === 'deep' ? '深度' : '快速'}扫描失败。`);
    } finally {
      setStartingScan('');
    }
  };

  const handleSaveConfig = async () => {
    if (!config) {
      return;
    }
    try {
      setSavingConfig(true);
      setError(null);
      const saved = await adminService.saveSecurityConfig(config);
      setConfig(saved);
    } catch (err: any) {
      setError(err.response?.data?.error || '保存扫描配置失败。');
    } finally {
      setSavingConfig(false);
    }
  };

  const handleSelectJob = async (jobId: number) => {
    try {
      setSelectedJobId(jobId);
      const detail = await adminService.getSecurityScanJob(jobId);
      setSelectedJob(detail);
    } catch (err: any) {
      setError(err.response?.data?.error || '加载扫描报告失败。');
    }
  };

  return (
    <AdminLayout title="安全中心">
      <div className="space-y-6">
        <section className="rounded-[28px] border border-[#dbe4f0] bg-[rgba(255,250,247,0.96)] p-6 shadow-[0_20px_50px_-36px_rgba(30,64,175,0.42)]">
          <div className="flex flex-col gap-4 xl:flex-row xl:items-end xl:justify-between">
            <div>
              <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[#2563eb]">Security Center</p>
              <h1 className="mt-2 text-3xl font-semibold text-[#171212]">安全中心</h1>
              <p className="mt-2 max-w-3xl text-sm leading-6 text-[#6f6661]">
                类杀毒软件式的运行时安全中心，支持快速扫描、深度扫描、实时进度、扫描报告和规则配置。当前首个资产类型为 skills。
              </p>
            </div>
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <StatCard label="资产总数" value={String(summary.total)} />
              <StatCard label="上传资产" value={String(summary.uploaded)} />
              <StatCard label="运行时发现" value={String(summary.discovered)} />
              <StatCard label="高风险" value={String(summary.highRisk)} />
            </div>
          </div>
        </section>

        {error ? (
          <div className="rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
            {error}
          </div>
        ) : null}

        <section className="grid grid-cols-1 gap-6 xl:grid-cols-[1.1fr_0.9fr]">
          <div className="rounded-[28px] border border-[#dbe4f0] bg-white p-6 shadow-[0_24px_60px_-42px_rgba(30,64,175,0.35)]">
            <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
              <div>
                <h2 className="text-lg font-semibold text-[#171212]">扫描控制台</h2>
                <p className="mt-1 text-sm text-[#8f8681]">手动发起 Quick Scan 或 Deep Scan，并查看当前任务进度。</p>
              </div>
              <div className="flex flex-wrap gap-3">
                <button
                  type="button"
                  onClick={() => void loadAll('refresh')}
                  disabled={loading || refreshing}
                  className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {refreshing ? '刷新中...' : '刷新'}
                </button>
                <button
                  type="button"
                  onClick={() => void handleStartScan('quick')}
                  disabled={loading || startingScan !== ''}
                  className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {startingScan === 'quick' ? '启动中...' : '快速扫描'}
                </button>
                <button
                  type="button"
                  onClick={() => void handleStartScan('deep')}
                  disabled={loading || startingScan !== ''}
                  className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {startingScan === 'deep' ? '启动中...' : '深度扫描'}
                </button>
              </div>
            </div>

            <div className="mt-6 grid grid-cols-1 gap-4 lg:grid-cols-3">
              <div className="rounded-2xl border border-[#efe1d8] bg-[#f8fbff] p-4">
                <div className="text-xs font-semibold uppercase tracking-[0.16em] text-[#2563eb]">默认模式</div>
                <div className="mt-2 text-lg font-semibold text-[#171212]">{config?.default_mode === 'deep' ? '深度扫描' : config?.default_mode === 'quick' ? '快速扫描' : '--'}</div>
              </div>
              <div className="rounded-2xl border border-[#efe1d8] bg-[#f8fbff] p-4">
                <div className="text-xs font-semibold uppercase tracking-[0.16em] text-[#2563eb]">快速扫描超时</div>
                <div className="mt-2 text-lg font-semibold text-[#171212]">{config?.quick_timeout_seconds ?? '--'}s</div>
              </div>
              <div className="rounded-2xl border border-[#efe1d8] bg-[#f8fbff] p-4">
                <div className="text-xs font-semibold uppercase tracking-[0.16em] text-[#2563eb]">深度扫描超时</div>
                <div className="mt-2 text-lg font-semibold text-[#171212]">{config?.deep_timeout_seconds ?? '--'}s</div>
              </div>
            </div>

            <div className="mt-6 rounded-2xl border border-[#efe1d8] bg-[#f8fbff] p-5">
              <div className="text-sm font-semibold text-[#171212]">最近任务</div>
              <div className="mt-4 space-y-3">
                {loading ? (
                  <div className="text-sm text-[#8f8681]">{t('common.loading')}</div>
                ) : jobs.length === 0 ? (
                  <div className="text-sm text-[#8f8681]">还没有扫描任务。</div>
                ) : (
                  jobs.map((job) => (
                    <button
                      key={job.id}
                      type="button"
                      onClick={() => void handleSelectJob(job.id)}
                      className={`w-full rounded-2xl border px-4 py-4 text-left transition ${
                        selectedJobId === job.id ? 'border-[#93c5fd] bg-[#dbeafe]' : 'border-[#dbe4f0] bg-white hover:border-[#dcc6b8]'
                      }`}
                    >
                      <div className="flex items-start justify-between gap-3">
                        <div>
                          <div className="font-medium text-[#171212]">
                            {job.scan_mode === 'deep' ? '深度扫描' : '快速扫描'} #{job.id}
                          </div>
                          <div className="mt-1 text-xs text-[#8f8681]">
                            {assetTypeLabel(job.asset_type)} · 已完成 {job.completed_items}/{job.total_items}
                            {job.current_item_name ? ` · 当前 ${job.current_item_name}` : ''}
                          </div>
                        </div>
                        <Badge tone={job.status === 'completed' ? 'green' : job.status === 'failed' ? 'red' : 'orange'}>
                          {jobStatusLabel(job.status)}
                        </Badge>
                      </div>
                      <div className="mt-3 h-2 overflow-hidden rounded-full bg-[#f3e7df]">
                        <div className="h-full rounded-full bg-[#2563eb]" style={{ width: `${job.progress_pct}%` }} />
                      </div>
                    </button>
                  ))
                )}
              </div>
            </div>
          </div>

          <div className="rounded-[28px] border border-[#dbe4f0] bg-white p-6 shadow-[0_24px_60px_-42px_rgba(30,64,175,0.35)]">
            <div className="flex items-center justify-between gap-4">
              <div>
                <h2 className="text-lg font-semibold text-[#171212]">扫描规则配置</h2>
                <p className="mt-1 text-sm text-[#8f8681]">配置快速扫描和深度扫描的 analyzer 与超时。后台扫描强制走 skill-scanner。</p>
              </div>
              <button
                type="button"
                onClick={handleSaveConfig}
                disabled={!config || savingConfig}
                className="app-button-primary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {savingConfig ? '保存中...' : '保存配置'}
              </button>
            </div>

            {config ? (
              <div className="mt-6 space-y-5">
                <div>
                  <label className="block text-sm font-medium text-gray-700">默认模式</label>
                  <select
                    value={config.default_mode}
                    onChange={(event) => setConfig((current) => current ? { ...current, default_mode: event.target.value } : current)}
                    className="app-input mt-1 w-full"
                  >
                    <option value="quick">快速扫描</option>
                    <option value="deep">深度扫描</option>
                  </select>
                </div>
                <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                  <div>
                    <label className="block text-sm font-medium text-gray-700">快速扫描超时（秒）</label>
                    <input
                      type="number"
                      min={5}
                      value={config.quick_timeout_seconds}
                      onChange={(event) => setConfig((current) => current ? { ...current, quick_timeout_seconds: Number(event.target.value) || 30 } : current)}
                      className="app-input mt-1 w-full"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-gray-700">深度扫描超时（秒）</label>
                    <input
                      type="number"
                      min={10}
                      value={config.deep_timeout_seconds}
                      onChange={(event) => setConfig((current) => current ? { ...current, deep_timeout_seconds: Number(event.target.value) || 120 } : current)}
                      className="app-input mt-1 w-full"
                    />
                  </div>
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700">快速扫描 Analyzers</label>
                  <input
                    value={(config.quick_analyzers ?? []).join(', ')}
                    onChange={(event) => setConfig((current) => current ? { ...current, quick_analyzers: splitAnalyzers(event.target.value) } : current)}
                    className="app-input mt-1 w-full"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700">深度扫描 Analyzers</label>
                  <input
                    value={(config.deep_analyzers ?? []).join(', ')}
                    onChange={(event) => setConfig((current) => current ? { ...current, deep_analyzers: splitAnalyzers(event.target.value) } : current)}
                    className="app-input mt-1 w-full"
                  />
                </div>
                <div className="rounded-2xl border border-[#efe1d8] bg-[#f8fbff] px-4 py-3 text-sm text-[#6f6661]">
                  当前策略：仅允许使用外部 <span className="font-semibold text-[#171212]">skill-scanner</span>。
                  若扫描服务不可用或返回错误，任务会直接失败，不会回退到内置启发式扫描。
                </div>
              </div>
            ) : (
              <div className="mt-6 text-sm text-[#8f8681]">{t('common.loading')}</div>
            )}
          </div>
        </section>

        <section className="grid grid-cols-1 gap-6 xl:grid-cols-[1.1fr_0.9fr]">
          <div className="rounded-[28px] border border-[#dbe4f0] bg-white p-6 shadow-[0_24px_60px_-42px_rgba(30,64,175,0.35)]">
            <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <div>
                <h2 className="text-lg font-semibold text-[#171212]">资产列表</h2>
                <p className="mt-1 text-sm text-[#8f8681]">
                  {loading ? '正在加载资产...' : `当前显示第 ${currentAssetPage} / ${totalAssetPages} 页，共 ${filteredSkills.length} / ${skills.length} 个资产`}
                </p>
              </div>
              <input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder="按名称、key、用户、来源、风险、扫描状态搜索"
                className="app-input w-full max-w-sm"
              />
            </div>

            <div className="mt-4 overflow-x-auto">
              {loading ? (
                <div className="py-12 text-center text-[#8f8681]">{t('common.loading')}</div>
              ) : filteredSkills.length === 0 ? (
                <div className="py-12 text-center text-[#8f8681]">没有找到匹配的资产。</div>
              ) : (
                <>
                  <table className="min-w-full divide-y divide-[#f3e7df] text-sm">
                    <thead className="bg-[#f8fbff] text-left text-xs font-semibold uppercase tracking-[0.14em] text-[#8f8681]">
                      <tr>
                        <th className="px-4 py-3">资产</th>
                        <th className="px-4 py-3">用户</th>
                        <th className="px-4 py-3">来源</th>
                        <th className="px-4 py-3">扫描状态</th>
                        <th className="px-4 py-3">风险</th>
                        <th className="px-4 py-3">最近扫描</th>
                        <th className="px-4 py-3">实例数</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-[#f5ebe5]">
                      {paginatedSkills.map((item) => (
                        <tr
                          key={item.id}
                          onClick={() => setSelectedAssetId(item.id)}
                          className={`cursor-pointer transition hover:bg-[#f8fbff] ${selectedAssetId === item.id ? 'bg-[#fff6f2]' : ''}`}
                        >
                          <td className="px-4 py-3">
                            <div className="font-medium text-[#171212]">{item.name}</div>
                            <div className="mt-1 text-xs text-[#8f8681]">{item.skill_key}</div>
                            {item.risk_level === 'high' && item.risk_reason ? (
                              <div className="mt-2 text-xs leading-5 text-[#b42318]">
                                原因：{item.risk_reason}
                              </div>
                            ) : null}
                          </td>
                          <td className="px-4 py-3 text-[#5f5957]">#{item.user_id}</td>
                          <td className="px-4 py-3"><Badge tone={item.source_type === 'uploaded' ? 'amber' : 'slate'}>{sourceLabel(item.source_type)}</Badge></td>
                          <td className="px-4 py-3"><Badge tone={scanStatusTone(item.scan_status)}>{scanStatusLabel(item.scan_status)}</Badge></td>
                          <td className="px-4 py-3"><Badge tone={riskTone(item.risk_level)}>{riskLabel(item.risk_level)}</Badge></td>
                          <td className="px-4 py-3 text-[#5f5957]">{item.last_scanned_at ? formatDateTime(item.last_scanned_at) : '未扫描'}</td>
                          <td className="px-4 py-3 text-[#5f5957]">{item.instance_count}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                  <div className="mt-4 flex flex-col gap-3 border-t border-[#f3e7df] pt-4 sm:flex-row sm:items-center sm:justify-between">
                    <div className="text-sm text-[#8f8681]">
                      每页 {pageSize} 条，本页显示 {(currentAssetPage - 1) * pageSize + 1}-{Math.min(currentAssetPage * pageSize, filteredSkills.length)}
                    </div>
                    <div className="flex items-center gap-2">
                      <button
                        type="button"
                        onClick={() => setAssetPage((current) => Math.max(1, current - 1))}
                        disabled={currentAssetPage <= 1}
                        className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        上一页
                      </button>
                      <div className="min-w-[88px] text-center text-sm font-medium text-[#5f5957]">
                        {currentAssetPage} / {totalAssetPages}
                      </div>
                      <button
                        type="button"
                        onClick={() => setAssetPage((current) => Math.min(totalAssetPages, current + 1))}
                        disabled={currentAssetPage >= totalAssetPages}
                        className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        下一页
                      </button>
                    </div>
                  </div>
                  {selectedAsset ? (
                    <div className="mt-5 rounded-2xl border border-[#efe1d8] bg-[#f8fbff] p-5">
                      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                        <div>
                          <div className="text-sm font-semibold text-[#171212]">资产详情</div>
                          <div className="mt-2 text-lg font-semibold text-[#171212]">{selectedAsset.name}</div>
                          <div className="mt-1 text-xs text-[#8f8681]">{selectedAsset.skill_key}</div>
                        </div>
                        <div className="flex flex-wrap gap-2">
                          <Badge tone={riskTone(selectedAsset.risk_level)}>{riskLabel(selectedAsset.risk_level)}</Badge>
                          <Badge tone={scanStatusTone(selectedAsset.scan_status)}>{scanStatusLabel(selectedAsset.scan_status)}</Badge>
                        </div>
                      </div>
                      <div className="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2">
                        <div className="rounded-2xl border border-[#f1e2d9] bg-white p-4">
                          <div className="text-xs font-semibold uppercase tracking-[0.16em] text-[#2563eb]">判定原因</div>
                          <div className="mt-3 text-sm leading-6 text-[#6f6661]">
                            {selectedAsset.risk_reason || '当前没有摘要原因，可查看下方具体依据。'}
                          </div>
                        </div>
                        <div className="rounded-2xl border border-[#f1e2d9] bg-white p-4">
                          <div className="text-xs font-semibold uppercase tracking-[0.16em] text-[#2563eb]">处置建议</div>
                          <div className="mt-3 text-sm leading-6 text-[#6f6661]">
                            {selectedAsset.risk_level === 'high'
                              ? '立即禁用该资产在实例中的继续使用，并根据命中项移除高风险文件或功能后重新扫描。'
                              : selectedAsset.risk_level === 'medium'
                                ? '限制继续注入到新实例，并按规则建议整改后复扫。'
                                : selectedAsset.risk_level === 'low'
                                  ? '保留告警，建议按下方规则逐项修复。'
                                  : '当前未发现需要立即处置的风险。'}
                          </div>
                        </div>
                      </div>
                      <div className="mt-4">
                        <div className="text-sm font-medium text-[#171212]">判定依据</div>
                        <div className="mt-3 space-y-3">
                          {(selectedAsset.top_findings ?? []).length === 0 ? (
                            <div className="rounded-2xl border border-dashed border-[#dbe4f0] bg-white px-4 py-5 text-sm text-[#8f8681]">
                              当前没有可展示的 findings。
                            </div>
                          ) : (
                            (selectedAsset.top_findings ?? []).map((finding, index) => (
                              <div key={`${selectedAsset.id}-${finding.rule_id}-${index}`} className="rounded-2xl border border-[#f1d9d9] bg-white px-4 py-4">
                                <div className="flex flex-wrap items-center gap-2">
                                  <Badge tone={severityTone(finding.severity)}>{severityLabel(finding.severity)}</Badge>
                                  <span className="text-[11px] font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{finding.analyzer}</span>
                                  <span className="text-[11px] text-[#8f8681]">{finding.rule_id}</span>
                                </div>
                                <div className="mt-2 text-sm font-medium text-[#171212]">{finding.title}</div>
                                <div className="mt-1 text-sm leading-6 text-[#6f6661]">{finding.description}</div>
                                {finding.file_path ? (
                                  <div className="mt-2 text-xs text-[#8f8681]">
                                    依据：{finding.file_path}{finding.line_number ? `:${finding.line_number}` : ''}
                                  </div>
                                ) : null}
                                {finding.snippet ? (
                                  <pre className="mt-2 overflow-x-auto rounded-lg bg-[#f8fbff] px-3 py-2 text-xs text-[#7a4b34]">{finding.snippet}</pre>
                                ) : null}
                                {finding.remediation ? (
                                  <div className="mt-2 text-xs leading-5 text-[#8f8681]">建议：{finding.remediation}</div>
                                ) : null}
                              </div>
                            ))
                          )}
                        </div>
                      </div>
                    </div>
                  ) : null}
                </>
              )}
            </div>
          </div>

          <div className="rounded-[28px] border border-[#dbe4f0] bg-white p-6 shadow-[0_24px_60px_-42px_rgba(30,64,175,0.35)]">
            <div>
              <h2 className="text-lg font-semibold text-[#171212]">扫描报告</h2>
              <p className="mt-1 text-sm text-[#8f8681]">每次扫描都会生成独立报告，支持查看风险分布、命中摘要和逐项结果。</p>
            </div>

            {!selectedJob ? (
              <div className="mt-6 rounded-2xl border border-dashed border-[#dbe4f0] px-4 py-10 text-center text-sm text-[#8f8681]">
                选择一个扫描任务查看报告。
              </div>
            ) : (
              <div className="mt-6 space-y-5">
                <div className="rounded-2xl border border-[#efe1d8] bg-[#f8fbff] p-4">
                  <div className="flex items-center justify-between gap-3">
                    <div className="font-medium text-[#171212]">
                      {selectedJob.scan_mode === 'deep' ? '深度扫描' : '快速扫描'} #{selectedJob.id}
                    </div>
                    <Badge tone={selectedJob.status === 'completed' ? 'green' : selectedJob.status === 'failed' ? 'red' : 'orange'}>
                      {jobStatusLabel(selectedJob.status)}
                    </Badge>
                  </div>
                  <div className="mt-3 h-2 overflow-hidden rounded-full bg-[#f3e7df]">
                    <div className="h-full rounded-full bg-[#2563eb]" style={{ width: `${selectedJob.progress_pct}%` }} />
                  </div>
                  <div className="mt-3 text-xs text-[#8f8681]">
                    已完成 {selectedJob.completed_items}/{selectedJob.total_items}
                    {selectedJob.current_item_name ? ` · 当前 ${selectedJob.current_item_name}` : ''}
                  </div>
                  <div className="mt-2 text-xs text-[#8f8681]">
                    {selectedJob.started_at ? `开始于 ${formatDateTime(selectedJob.started_at)}` : '等待开始'}
                    {selectedJob.finished_at ? ` · 完成于 ${formatDateTime(selectedJob.finished_at)}` : ''}
                  </div>
                </div>

                {selectedJob.report ? (
                  <>
                    <div className="grid grid-cols-2 gap-3">
                      {Object.entries(selectedJob.report.risk_counts ?? {}).map(([level, count]) => (
                        <StatCard key={level} label={riskLabel(level)} value={String(count)} />
                      ))}
                    </div>
                    <div className="rounded-2xl border border-[#efe1d8] bg-[#f8fbff] p-4">
                      <div className="text-sm font-medium text-[#171212]">Analyzer 视图</div>
                      <p className="mt-1 text-xs leading-5 text-[#8f8681]">
                        配置项表示本次请求给 skill-scanner 的 analyzer；可用项来自 scanner 当前 /health；实际命中项来自本次扫描结果里的 findings。
                        如果某个 analyzer 执行了但没有产生 findings，当前 skill-scanner 不会单独返回执行痕迹。
                      </p>
                      <div className="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-3">
                        <AnalyzerGroup title="本次配置" items={selectedJob.report.configured_analyzers ?? []} />
                        <AnalyzerGroup title="当前可用" items={selectedJob.report.available_analyzers ?? []} />
                        <AnalyzerGroup title="实际命中" items={selectedJob.report.triggered_analyzers ?? []} emptyLabel="本次没有 analyzer 产出 findings" />
                      </div>
                    </div>
                    <div>
                      <div className="text-sm font-medium text-[#171212]">命中摘要</div>
                      <div className="mt-3 space-y-3">
                        {(selectedJob.report.findings_summary ?? []).length === 0 ? (
                          <div className="text-sm text-[#8f8681]">当前没有命中摘要。</div>
                        ) : (
                          (selectedJob.report.findings_summary ?? []).slice(0, 8).map((entry, index) => (
                            <div key={`${entry.asset_name}-${index}`} className="rounded-2xl border border-[#efe1d8] bg-[#f8fbff] px-4 py-3">
                              <div className="font-medium text-[#171212]">{entry.asset_name}</div>
                              <div className="mt-1 text-sm text-[#6f6661]">{entry.summary}</div>
                            </div>
                          ))
                        )}
                      </div>
                    </div>
                    <div>
                      <div className="text-sm font-medium text-[#171212]">逐项结果</div>
                      <div className="mt-3 max-h-[420px] space-y-3 overflow-y-auto pr-1">
                        {(selectedJob.report.items ?? []).map((item) => (
                          <div key={item.id} className="rounded-2xl border border-[#efe1d8] bg-[#f8fbff] px-4 py-3">
                            <div className="flex items-start justify-between gap-3">
                              <div>
                                <div className="font-medium text-[#171212]">{item.asset_name}</div>
                                <div className="mt-1 text-xs text-[#8f8681]">
                                  {item.cached_result ? '复用缓存结果' : '实时扫描'} · {jobStatusLabel(item.status)}
                                </div>
                                {item.triggered_analyzers && item.triggered_analyzers.length > 0 ? (
                                  <div className="mt-2 flex flex-wrap gap-2">
                                    {item.triggered_analyzers.map((analyzer) => (
                                      <span
                                        key={`${item.id}-${analyzer}`}
                                        className="inline-flex rounded-full border border-[#d9e0e7] bg-[#f6f8fb] px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.12em] text-[#556070]"
                                      >
                                        {analyzer}
                                      </span>
                                    ))}
                                  </div>
                                ) : null}
                                {item.summary ? <div className="mt-2 text-sm text-[#6f6661]">{item.summary}</div> : null}
                                {item.findings && item.findings.length > 0 ? (
                                  <div className="mt-3 space-y-2">
                                    {item.findings.map((finding, findingIndex) => (
                                      <div key={`${item.id}-${finding.rule_id}-${findingIndex}`} className="rounded-xl border border-[#f1d9d9] bg-white px-3 py-3">
                                        <div className="flex flex-wrap items-center gap-2">
                                          <Badge tone={severityTone(finding.severity)}>{severityLabel(finding.severity)}</Badge>
                                          <span className="text-[11px] font-semibold uppercase tracking-[0.12em] text-[#8f8681]">{finding.analyzer}</span>
                                          <span className="text-[11px] text-[#8f8681]">{finding.rule_id}</span>
                                        </div>
                                        <div className="mt-2 text-sm font-medium text-[#171212]">{finding.title}</div>
                                        <div className="mt-1 text-sm leading-6 text-[#6f6661]">{finding.description}</div>
                                        {finding.file_path ? (
                                          <div className="mt-2 text-xs text-[#8f8681]">
                                            依据：{finding.file_path}{finding.line_number ? `:${finding.line_number}` : ''}
                                          </div>
                                        ) : null}
                                        {finding.snippet ? (
                                          <pre className="mt-2 overflow-x-auto rounded-lg bg-[#f8fbff] px-3 py-2 text-xs text-[#7a4b34]">{finding.snippet}</pre>
                                        ) : null}
                                        {finding.remediation ? (
                                          <div className="mt-2 text-xs leading-5 text-[#8f8681]">建议：{finding.remediation}</div>
                                        ) : null}
                                      </div>
                                    ))}
                                  </div>
                                ) : null}
                                {item.error_message ? <div className="mt-2 text-sm text-red-700">{item.error_message}</div> : null}
                              </div>
                              <Badge tone={riskTone(item.risk_level || 'unknown')}>{riskLabel(item.risk_level || item.status)}</Badge>
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>
                  </>
                ) : (
                  <div className="text-sm text-[#8f8681]">报告还在生成中。</div>
                )}
              </div>
            )}
          </div>
        </section>
      </div>
    </AdminLayout>
  );
};

function AnalyzerGroup({ title, items, emptyLabel }: { title: string; items: string[]; emptyLabel?: string }) {
  const safeItems = Array.isArray(items) ? items : [];
  return (
    <div className="rounded-2xl border border-[#f1e2d9] bg-white p-4">
      <div className="text-xs font-semibold uppercase tracking-[0.16em] text-[#2563eb]">{title}</div>
      {safeItems.length === 0 ? (
        <div className="mt-3 text-sm text-[#8f8681]">{emptyLabel || '暂无'}</div>
      ) : (
        <div className="mt-3 flex flex-wrap gap-2">
          {safeItems.map((item) => (
            <span
              key={`${title}-${item}`}
              className="inline-flex rounded-full border border-[#d9e0e7] bg-[#f6f8fb] px-3 py-1 text-xs font-semibold uppercase tracking-[0.12em] text-[#556070]"
            >
              {item}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-2xl border border-[#f1e2d9] bg-white px-4 py-3">
      <div className="text-[11px] font-semibold uppercase tracking-[0.16em] text-[#2563eb]">{label}</div>
      <div className="mt-2 text-2xl font-semibold text-[#171212]">{value}</div>
    </div>
  );
}

function Badge({
  children,
  tone,
}: {
  children: React.ReactNode;
  tone: 'green' | 'yellow' | 'orange' | 'red' | 'amber' | 'slate';
}) {
  const toneClass =
    tone === 'green'
      ? 'border-[#bde8ca] bg-[#edfdf2] text-[#177245]'
      : tone === 'yellow'
        ? 'border-[#f5df9f] bg-[#fff8dd] text-[#9a6a00]'
        : tone === 'orange'
          ? 'border-[#f7c8a4] bg-[#fff1e6] text-[#b45309]'
          : tone === 'red'
            ? 'border-[#f2c2c2] bg-[#fff0f0] text-[#b42318]'
            : tone === 'amber'
              ? 'border-[#f1d9c7] bg-[#eff6ff] text-[#2563eb]'
              : 'border-[#d9e0e7] bg-[#f6f8fb] text-[#556070]';

  return (
    <span className={`inline-flex rounded-full border px-3 py-1 text-xs font-semibold uppercase tracking-[0.12em] ${toneClass}`}>
      {children}
    </span>
  );
}

function splitAnalyzers(value: string): string[] {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean);
}

function assetTypeLabel(value: string): string {
  if (value === 'skill') {
    return '技能';
  }
  return value;
}

function sourceLabel(value: string): string {
  if (value === 'uploaded') {
    return '用户上传';
  }
  if (value === 'discovered') {
    return '实例发现';
  }
  return value;
}

function riskLabel(value: string): string {
  switch (value) {
    case 'high':
      return '高风险';
    case 'medium':
      return '中风险';
    case 'low':
      return '低风险';
    case 'none':
      return 'SAFE';
    case 'completed':
      return '已完成';
    case 'running':
      return '扫描中';
    case 'failed':
      return '失败';
    case 'pending':
      return '等待中';
    case 'queued':
      return '排队中';
    default:
      return value;
  }
}

function jobStatusLabel(value: string): string {
  switch (value) {
    case 'queued':
      return '排队中';
    case 'pending':
      return '等待中';
    case 'running':
      return '扫描中';
    case 'completed':
      return '已完成';
    case 'failed':
      return '失败';
    default:
      return value;
  }
}

function formatDateTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString('zh-CN', {
    hour12: false,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

function riskTone(value: string): 'green' | 'yellow' | 'orange' | 'red' | 'amber' | 'slate' {
  switch (value) {
    case 'high':
      return 'red';
    case 'medium':
      return 'orange';
    case 'low':
      return 'yellow';
    case 'none':
      return 'green';
    case 'uploaded':
      return 'amber';
    default:
      return 'slate';
  }
}

function severityTone(value: string): 'green' | 'yellow' | 'orange' | 'red' | 'amber' | 'slate' {
  switch (value.toUpperCase()) {
    case 'CRITICAL':
    case 'HIGH':
      return 'red';
    case 'MEDIUM':
    case 'MODERATE':
      return 'orange';
    case 'LOW':
    case 'WARNING':
      return 'yellow';
    case 'INFO':
    case 'SAFE':
      return 'green';
    default:
      return 'slate';
  }
}

function severityLabel(value: string): string {
  switch (value.toUpperCase()) {
    case 'CRITICAL':
      return 'CRITICAL';
    case 'HIGH':
      return 'HIGH';
    case 'MEDIUM':
    case 'MODERATE':
      return 'MEDIUM';
    case 'LOW':
    case 'WARNING':
      return 'LOW';
    case 'INFO':
      return 'INFO';
    default:
      return value;
  }
}

function scanStatusLabel(value: string): string {
  switch (value) {
    case 'completed':
      return '已完成';
    case 'pending':
      return '待扫描';
    case 'failed':
      return '扫描失败';
    case 'running':
      return '扫描中';
    default:
      return value || '未知';
  }
}

function scanStatusTone(value: string): 'green' | 'yellow' | 'orange' | 'red' | 'amber' | 'slate' {
  switch (value) {
    case 'completed':
      return 'green';
    case 'pending':
      return 'amber';
    case 'running':
      return 'orange';
    case 'failed':
      return 'red';
    default:
      return 'slate';
  }
}

export default AdminSkillsPage;
