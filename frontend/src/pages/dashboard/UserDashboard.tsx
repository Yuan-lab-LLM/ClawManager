import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useAuth } from '../../contexts/AuthContext';
import { useI18n } from '../../contexts/I18nContext';
import UserLayout from '../../components/UserLayout';
import { userService } from '../../services/userService';
import { instanceService } from '../../services/instanceService';
import type { UserQuota } from '../../types/user';
import type { Instance } from '../../types/instance';

const UserDashboard: React.FC = () => {
  const { user } = useAuth();
  const { t } = useI18n();
  const [quota, setQuota] = useState<UserQuota | null>(null);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    loadData();
  }, []);

  const loadData = async () => {
    if (!user) return;
    try {
      const [quotaData, instancesData] = await Promise.all([
        userService.getUserQuota(user.id),
        instanceService.getInstances(1, 100)
      ]);
      setQuota(quotaData);
      setInstances(instancesData.instances);
    } catch (err) {
      console.error('Failed to load data:', err);
    } finally {
      setLoading(false);
    }
  };

  const runningCount = instances.filter(i => i.status === 'running').length;
  const totalStorage = instances.reduce((sum, i) => sum + i.disk_gb, 0);
  const recentInstances = [...instances].sort((a, b) => {
    return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
  });

  return (
    <UserLayout title={t('nav.userDashboard')}>
      <div className="space-y-8">
        {/* Stats Cards */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
          <Link to="/instances" className="app-panel transition-all hover:-translate-y-0.5 hover:shadow-[0_30px_80px_-52px_rgba(72,44,24,0.62)]">
            <div className="px-4 py-5 sm:p-6">
              <dt className="truncate text-sm font-medium text-[#8f8681]">
                {t('userDashboard.myInstances')}
              </dt>
              <dd className="mt-1 text-3xl font-semibold text-[#171212]">
                {instances.length}
              </dd>
            </div>
          </Link>

          <Link to="/instances" className="app-panel transition-all hover:-translate-y-0.5 hover:shadow-[0_30px_80px_-52px_rgba(72,44,24,0.62)]">
            <div className="px-4 py-5 sm:p-6">
              <dt className="truncate text-sm font-medium text-[#8f5b4b]">
                {t('userDashboard.running')}
              </dt>
              <dd className="mt-1 text-3xl font-semibold text-[#dc2626]">
                {runningCount}
              </dd>
            </div>
          </Link>

          <div className="app-panel">
            <div className="px-4 py-5 sm:p-6">
              <dt className="truncate text-sm font-medium text-[#8f8681]">
                {t('userDashboard.storageUsed')}
              </dt>
              <dd className="mt-1 text-3xl font-semibold text-[#171212]">
                {totalStorage} GB
              </dd>
            </div>
          </div>
        </div>

        {/* My Quota */}
        <div>
          <h2 className="text-lg font-medium text-gray-900 mb-4">{t('userDashboard.myResourceQuota')}</h2>
          <div className="app-panel p-6">
            {loading ? (
              <div className="text-center py-4">{t('userDashboard.loadingQuota')}</div>
            ) : quota ? (
              <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
                <div className="text-center">
                    <div className="text-2xl font-bold text-[#dc2626]">
                    {instances.length} / {quota.max_instances}
                  </div>
                  <div className="text-sm text-gray-500">{t('userDashboard.instances')}</div>
                </div>
                <div className="text-center">
                    <div className="text-2xl font-bold text-[#dc2626]">
                    {quota.max_cpu_cores}
                  </div>
                  <div className="text-sm text-gray-500">{t('userDashboard.maxCpuCores')}</div>
                </div>
                <div className="text-center">
                    <div className="text-2xl font-bold text-[#dc2626]">
                    {quota.max_memory_gb} GB
                  </div>
                  <div className="text-sm text-gray-500">{t('userDashboard.maxMemory')}</div>
                </div>
                <div className="text-center">
                    <div className="text-2xl font-bold text-[#dc2626]">
                    {quota.max_storage_gb} GB
                  </div>
                  <div className="text-sm text-gray-500">{t('userDashboard.maxStorage')}</div>
                </div>
                <div className="text-center">
                    <div className="text-2xl font-bold text-[#dc2626]">
                    {quota.max_gpu_count}
                  </div>
                  <div className="text-sm text-gray-500">{t('userDashboard.maxGpus')}</div>
                </div>
              </div>
            ) : (
              <div className="text-center text-gray-500">{t('userDashboard.quotaUnavailable')}</div>
            )}
          </div>
        </div>

        {/* Recent Instances */}
        {instances.length > 0 && (
          <div>
            <div className="flex justify-between items-center mb-4">
              <h2 className="text-lg font-medium text-gray-900">{t('userDashboard.recentInstances')}</h2>
              <Link to="/instances" className="text-sm font-medium text-[#dc2626] hover:text-[#b91c1c]">
                {t('userDashboard.viewAll')} →
              </Link>
            </div>
            <div className="app-panel">
              <ul className="divide-y divide-[#f1e7e1]">
                {recentInstances.slice(0, 5).map((instance) => (
                  <li key={instance.id} className="px-4 py-4 hover:bg-[#fff8f5]">
                    <Link to={`/instances/${instance.id}`} className="flex items-center justify-between">
                      <div>
                        <h3 className="text-sm font-medium text-[#dc2626]">{instance.name}</h3>
                        <p className="text-sm text-gray-500">
                          {instance.type} • {instance.cpu_cores} CPU • {instance.memory_gb} GB RAM
                        </p>
                      </div>
                      <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
                        instance.status === 'running' ? 'bg-green-100 text-green-800' :
                        instance.status === 'stopped' ? 'bg-gray-100 text-gray-800' :
                        'bg-yellow-100 text-yellow-800'
                      }`}>
                        {t(`status.${instance.status}`)}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            </div>
          </div>
        )}

        {/* Quick Actions */}
        <div>
          <h2 className="text-lg font-medium text-gray-900 mb-4">{t('userDashboard.quickActions')}</h2>
          <div className="flex flex-wrap gap-4">
            <Link to="/instances/new" className="app-button-primary text-base">
              <svg className="w-5 h-5 mr-2" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
              </svg>
              {t('userDashboard.createNewInstance')}
            </Link>
            <Link to="/instances" className="app-button-secondary text-base">
              <svg className="w-5 h-5 mr-2" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 10h16M4 14h16M4 18h16" />
              </svg>
              {t('userDashboard.viewAllInstances')}
            </Link>
          </div>
        </div>

        {/* Empty State */}
        {instances.length === 0 && (
          <div className="app-panel p-12 text-center">
            <svg 
              className="mx-auto h-16 w-16 text-gray-300" 
              fill="none" 
              viewBox="0 0 24 24" 
              stroke="currentColor"
            >
              <path 
                strokeLinecap="round" 
                strokeLinejoin="round" 
                strokeWidth={1.5}
                d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01" 
              />
            </svg>
            <h3 className="mt-4 text-lg font-medium text-gray-900">
              {t('userDashboard.noInstancesYet')}
            </h3>
            <p className="mt-2 text-gray-500 max-w-sm mx-auto">
              {t('userDashboard.noInstancesSubtitle')}
            </p>
            <div className="mt-6">
              <Link to="/instances/new" className="app-button-primary text-base">
                <svg className="w-5 h-5 mr-2" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
                </svg>
                {t('userDashboard.createInstance')}
              </Link>
            </div>
          </div>
        )}
      </div>
    </UserLayout>
  );
};

export default UserDashboard;



