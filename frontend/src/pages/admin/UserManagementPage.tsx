import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import AdminLayout from '../../components/AdminLayout';
import { useI18n } from '../../contexts/I18nContext';
import { enterpriseAuthService } from '../../services/enterpriseAuthService';
import type { EnterpriseAuthStatus } from '../../services/enterpriseAuthService';
import { userService } from '../../services/userService';
import type { CreateUserRequest, ImportUsersResponse, LDAPImportRequest, LDAPImportUser } from '../../services/userService';
import type { User, UserQuota } from '../../types/user';
import { localizeEnterpriseAuthIssue } from '../../utils/enterpriseAuthErrors';

const USERS_PAGE_SIZE = 20;
type UserFilter = 'all' | 'local' | 'ldap' | 'inactive' | 'admin';
type EnterpriseAuthTone = 'success' | 'warning' | 'error' | 'neutral';
type CreateUserField = 'username' | 'email' | 'role';
type ImportQuotaValues = Omit<LDAPImportRequest, 'role'>;

const DEFAULT_USER_IMPORT_QUOTA: ImportQuotaValues = {
  max_instances: 10,
  max_cpu_cores: 40,
  max_memory_gb: 100,
  max_storage_gb: 500,
  max_gpu_count: 2,
};

const DEFAULT_ADMIN_IMPORT_QUOTA: ImportQuotaValues = {
  max_instances: 100,
  max_cpu_cores: 200,
  max_memory_gb: 1000,
  max_storage_gb: 5000,
  max_gpu_count: 10,
};

function quotaValuesEqual(left: ImportQuotaValues, right: ImportQuotaValues) {
  return left.max_instances === right.max_instances &&
    left.max_cpu_cores === right.max_cpu_cores &&
    left.max_memory_gb === right.max_memory_gb &&
    left.max_storage_gb === right.max_storage_gb &&
    left.max_gpu_count === right.max_gpu_count;
}

function quotaForImportRole(role: 'admin' | 'user', requested: ImportQuotaValues, roleSyncEnabled = false) {
  return role === 'admin' && (roleSyncEnabled || quotaValuesEqual(requested, DEFAULT_USER_IMPORT_QUOTA))
    ? DEFAULT_ADMIN_IMPORT_QUOTA
    : requested;
}

const getEnterpriseAuthToneClasses = (tone: EnterpriseAuthTone) => {
  switch (tone) {
    case 'success':
      return 'border-green-200 bg-green-50 text-green-800';
    case 'error':
      return 'border-red-200 bg-red-50 text-red-800';
    case 'warning':
      return 'border-amber-200 bg-amber-50 text-amber-800';
    case 'neutral':
    default:
      return 'border-gray-200 bg-white text-gray-600';
  }
};

const getEnterpriseAuthDotClasses = (tone: EnterpriseAuthTone) => {
  switch (tone) {
    case 'success':
      return 'bg-green-500';
    case 'error':
      return 'bg-red-500';
    case 'warning':
      return 'bg-amber-500';
    case 'neutral':
    default:
      return 'bg-gray-400';
  }
};

const UserManagementPage: React.FC = () => {
  const { t } = useI18n();
  const [users, setUsers] = useState<User[]>([]);
  const [userFilter, setUserFilter] = useState<UserFilter>('all');
  const [page, setPage] = useState(1);
  const [totalUsers, setTotalUsers] = useState(0);
  const [loading, setLoading] = useState(true);
  const [enterpriseAuthStatus, setEnterpriseAuthStatus] = useState<EnterpriseAuthStatus | null>(null);
  const [enterpriseAuthSyncRole, setEnterpriseAuthSyncRole] = useState(false);
  const [enterpriseAuthLoading, setEnterpriseAuthLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [createUserError, setCreateUserError] = useState<string | null>(null);
  const [createUserFieldErrors, setCreateUserFieldErrors] = useState<Partial<Record<CreateUserField, string>>>({});
  const [selectedUser, setSelectedUser] = useState<User | null>(null);
  const [showQuotaModal, setShowQuotaModal] = useState(false);
  const [showRoleModal, setShowRoleModal] = useState(false);
  const [showAddModal, setShowAddModal] = useState(false);
  const [showImportModal, setShowImportModal] = useState(false);
  const [showDisableModal, setShowDisableModal] = useState(false);
  const [userToDisable, setUserToDisable] = useState<User | null>(null);
  const [showDeleteModal, setShowDeleteModal] = useState(false);
  const [userToDelete, setUserToDelete] = useState<User | null>(null);
  const [showRestoreModal, setShowRestoreModal] = useState(false);
  const [userToRestore, setUserToRestore] = useState<User | null>(null);
  const [quota, setQuota] = useState<UserQuota | null>(null);
  const [importFile, setImportFile] = useState<File | null>(null);
  const [importing, setImporting] = useState(false);
  const [importResult, setImportResult] = useState<ImportUsersResponse | null>(null);
  const [importMode, setImportMode] = useState<'csv' | 'ldap'>('csv');
  const [ldapPreview, setLdapPreview] = useState<LDAPImportUser[]>([]);
  const [ldapPreviewLoading, setLdapPreviewLoading] = useState(false);
  const [ldapPreviewQuery, setLdapPreviewQuery] = useState('');
  const [ldapPreviewLimit, setLdapPreviewLimit] = useState(100);
  const [selectedLDAPExternalIDs, setSelectedLDAPExternalIDs] = useState<string[]>([]);
  const [ldapImportConfig, setLdapImportConfig] = useState<LDAPImportRequest>({
    role: 'user', ...DEFAULT_USER_IMPORT_QUOTA,
  });
  const importInputRef = useRef<HTMLInputElement | null>(null);
  const [newUser, setNewUser] = useState<CreateUserRequest>({
    username: '',
    email: '',
    password: '',
    role: 'user',
  });

  const loadUsers = useCallback(async (targetPage: number) => {
    try {
      setLoading(true);
      setError(null);
      const data = await userService.getUsers(targetPage, USERS_PAGE_SIZE);
      const total = data.total || 0;
      const maxPage = Math.max(1, Math.ceil(total / USERS_PAGE_SIZE));

      if (targetPage > maxPage) {
        setUsers([]);
        setTotalUsers(total);
        setPage(maxPage);
        return;
      }

      setUsers(data.users || []);
      setTotalUsers(total);
      setPage(data.page || targetPage);
    } catch (err: unknown) {
      setError(getRequestErrorMessage(err, t('userManagementPage.loadFailed')));
      setUsers([]);
      setTotalUsers(0);
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void loadUsers(page);
  }, [loadUsers, page]);

  useEffect(() => {
    let cancelled = false;

    const loadEnterpriseAuthStatus = async () => {
      try {
        setEnterpriseAuthLoading(true);
        const config = await enterpriseAuthService.getConfig();
        if (!cancelled) {
          setEnterpriseAuthStatus(config.status);
          setEnterpriseAuthSyncRole(config.sync_role);
        }
      } catch {
        if (!cancelled) {
          setEnterpriseAuthStatus(null);
          setEnterpriseAuthSyncRole(false);
        }
      } finally {
        if (!cancelled) {
          setEnterpriseAuthLoading(false);
        }
      }
    };

    void loadEnterpriseAuthStatus();
    return () => {
      cancelled = true;
    };
  }, []);

  const totalPages = Math.max(1, Math.ceil(totalUsers / USERS_PAGE_SIZE));
  const showingFrom = totalUsers === 0 ? 0 : (page - 1) * USERS_PAGE_SIZE + 1;
  const showingTo = Math.min(page * USERS_PAGE_SIZE, totalUsers);
  const filteredUsers = useMemo(() => {
    switch (userFilter) {
      case 'local':
        return users.filter((user) => (user.auth_provider || 'local') === 'local');
      case 'ldap':
        return users.filter((user) => user.auth_provider === 'ldap');
      case 'inactive':
        return users.filter((user) => !user.is_active);
      case 'admin':
        return users.filter((user) => user.role === 'admin');
      case 'all':
      default:
        return users;
    }
  }, [userFilter, users]);
  const userFilterOptions: Array<{ value: UserFilter; label: string }> = [
    { value: 'all', label: t('userManagementPage.filterAll') },
    { value: 'local', label: t('userManagementPage.filterLocal') },
    { value: 'ldap', label: t('userManagementPage.filterLdap') },
    { value: 'inactive', label: t('userManagementPage.filterInactive') },
    { value: 'admin', label: t('userManagementPage.filterAdmin') },
  ];
  const enterpriseAuthSummary = useMemo(() => {
    if (enterpriseAuthLoading) {
      return {
        tone: 'neutral' as EnterpriseAuthTone,
        label: t('userManagementPage.enterpriseCheckingTitle'),
      };
    }
    if (!enterpriseAuthStatus) {
      return {
        tone: 'warning' as EnterpriseAuthTone,
        label: t('userManagementPage.enterpriseStatusUnavailableTitle'),
      };
    }
    if (!enterpriseAuthStatus.enabled) {
      return {
        tone: 'warning' as EnterpriseAuthTone,
        label: t('userManagementPage.enterpriseDisabledTitle'),
      };
    }
    if (!enterpriseAuthStatus.configured || Object.values(enterpriseAuthStatus.checks || {}).includes('failed')) {
      return {
        tone: 'error' as EnterpriseAuthTone,
        label: t('userManagementPage.enterpriseUnhealthyTitle'),
      };
    }
    if ((enterpriseAuthStatus.warnings || []).length > 0) {
      return {
        tone: 'warning' as EnterpriseAuthTone,
        label: t('userManagementPage.enterpriseWarningTitle'),
      };
    }
    return {
      tone: 'success' as EnterpriseAuthTone,
      label: t('userManagementPage.enterpriseConnectedTitle'),
    };
  }, [enterpriseAuthLoading, enterpriseAuthStatus, t]);
  const enterpriseAuthNotice = useMemo(() => {
    if (enterpriseAuthLoading) {
      return null;
    }
    if (!enterpriseAuthStatus) {
      return {
        tone: 'warning',
        message: t('userManagementPage.enterpriseStatusUnavailableMessage'),
      };
    }
    if (!enterpriseAuthStatus.enabled) {
      return null;
    }
    if (!enterpriseAuthStatus.configured || Object.values(enterpriseAuthStatus.checks || {}).includes('failed')) {
      return {
        tone: 'error',
        message: t('userManagementPage.enterpriseUnhealthyMessage', {
          error: localizeEnterpriseAuthIssue(
            enterpriseAuthStatus.error,
            t,
          ) || t('userManagementPage.enterpriseUnknownError'),
        }),
      };
    }
    if ((enterpriseAuthStatus.warnings || []).length > 0) {
      return {
        tone: 'warning',
        message: t('userManagementPage.enterpriseWarningMessage'),
      };
    }
    return null;
  }, [enterpriseAuthLoading, enterpriseAuthStatus, t]);
  const importableLDAPUsers = useMemo(
    () => ldapPreview.filter((user) => user.status === 'ready' || user.status === 'pending_alias'),
    [ldapPreview],
  );
  const selectedLDAPSet = useMemo(() => new Set(selectedLDAPExternalIDs), [selectedLDAPExternalIDs]);
  const selectedLDAPImportableCount = useMemo(
    () => importableLDAPUsers.filter((user) => selectedLDAPSet.has(user.external_id)).length,
    [importableLDAPUsers, selectedLDAPSet],
  );
  const allImportableLDAPSelected = importableLDAPUsers.length > 0 &&
    selectedLDAPImportableCount === importableLDAPUsers.length;

  const handleDeleteClick = (user: User) => {
    setUserToDelete(user);
    setShowDeleteModal(true);
  };

  const handleConfirmDelete = async () => {
    if (!userToDelete) return;

    try {
      await userService.deleteUser(userToDelete.id);
      const nextTotal = Math.max(0, totalUsers - 1);
      const nextPage = Math.min(page, Math.max(1, Math.ceil(nextTotal / USERS_PAGE_SIZE)));
      setShowDeleteModal(false);
      setUserToDelete(null);
      await loadUsers(nextPage);
    } catch (err: unknown) {
      setError(getRequestErrorMessage(err, t('userManagementPage.deleteFailed')));
    }
  };

  const handleCancelDelete = () => {
    setShowDeleteModal(false);
    setUserToDelete(null);
  };

  const handleDisableClick = (user: User) => {
    setUserToDisable(user);
    setShowDisableModal(true);
  };

  const handleConfirmDisable = async () => {
    if (!userToDisable) return;

    try {
      await userService.disableUser(userToDisable.id);
      setShowDisableModal(false);
      setUserToDisable(null);
      await loadUsers(page);
    } catch (err: unknown) {
      setError(getRequestErrorMessage(err, t('userManagementPage.updateStatusFailed')));
    }
  };

  const handleCancelDisable = () => {
    setShowDisableModal(false);
    setUserToDisable(null);
  };

  const handleRestoreClick = (user: User) => {
    setUserToRestore(user);
    setShowRestoreModal(true);
  };

  const handleConfirmRestore = async () => {
    if (!userToRestore) return;

    try {
      await userService.restoreUser(userToRestore.id);
      setShowRestoreModal(false);
      setUserToRestore(null);
      await loadUsers(page);
    } catch (err: unknown) {
      setError(getRequestErrorMessage(err, t('userManagementPage.restoreFailed')));
    }
  };

  const handleCancelRestore = () => {
    setShowRestoreModal(false);
    setUserToRestore(null);
  };

  const handleEditQuota = async (user: User) => {
    try {
      const userQuota = await userService.getUserQuota(user.id);
      setQuota(userQuota);
      setSelectedUser(user);
      setShowQuotaModal(true);
    } catch (err: unknown) {
      setError(getRequestErrorMessage(err, t('userManagementPage.loadQuotaFailed')));
    }
  };

  const handleSaveQuota = async () => {
    if (!selectedUser || !quota) return;

    try {
      await userService.updateQuota(selectedUser.id, {
        max_instances: quota.max_instances,
        max_cpu_cores: quota.max_cpu_cores,
        max_memory_gb: quota.max_memory_gb,
        max_storage_gb: quota.max_storage_gb,
        max_gpu_count: quota.max_gpu_count,
      });
      setShowQuotaModal(false);
      setSelectedUser(null);
      setQuota(null);
    } catch (err: unknown) {
      setError(getRequestErrorMessage(err, t('userManagementPage.updateQuotaFailed')));
    }
  };

  const handleEditRole = (user: User) => {
    setSelectedUser(user);
    setShowRoleModal(true);
  };

  const handleSaveRole = async (newRole: 'admin' | 'user') => {
    if (!selectedUser) return;

    try {
      await userService.updateRole(selectedUser.id, { role: newRole });
      setUsers((current) => current.map((user) => (
        user.id === selectedUser.id ? { ...user, role: newRole } : user
      )));
      setShowRoleModal(false);
      setSelectedUser(null);
    } catch (err: unknown) {
      setError(getRequestErrorMessage(err, t('userManagementPage.updateRoleFailed')));
    }
  };

  const handleAddUser = async () => {
    setCreateUserError(null);
    setCreateUserFieldErrors({});
    try {
      await userService.createUser(newUser);
      setShowAddModal(false);
      setCreateUserError(null);
      setCreateUserFieldErrors({});
      setNewUser({ username: '', email: '', password: '', role: 'user' });
      await loadUsers(page);
    } catch (err: unknown) {
      const createError = getCreateUserError(err, t);
      if (createError.field) {
        setCreateUserFieldErrors({ [createError.field]: createError.message });
        setCreateUserError(null);
      } else {
        setCreateUserFieldErrors({});
        setCreateUserError(createError.message);
      }
    }
  };

  const handleImportUsers = async () => {
    if (!importFile) {
      setError(t('userManagementPage.selectCsv'));
      return;
    }

    try {
      setImporting(true);
      setError(null);
      const result = await userService.importUsers(importFile);
      setImportResult(result);
      setShowImportModal(false);
      setImportFile(null);
      await loadUsers(1);
    } catch (err: unknown) {
      setError(getRequestErrorMessage(err, t('userManagementPage.importFailed')));
    } finally {
      setImporting(false);
    }
  };

  const handlePreviewLDAPUsers = async () => {
    try {
      setLdapPreviewLoading(true);
      setError(null);
      const result = await userService.previewLDAPUsers({
        query: ldapPreviewQuery.trim() || undefined,
        limit: ldapPreviewLimit || undefined,
      });
      setLdapPreview(result.users || []);
      setSelectedLDAPExternalIDs((current) => {
        const available = new Set((result.users || [])
          .filter((user) => user.status === 'ready' || user.status === 'pending_alias')
          .map((user) => user.external_id));
        return current.filter((externalID) => available.has(externalID));
      });
    } catch (err: unknown) {
      setError(localizeEnterpriseAuthIssue(
        getRequestErrorMessage(err, t('userManagementPage.ldapPreviewFailed')),
        t,
      ));
    } finally {
      setLdapPreviewLoading(false);
    }
  };

  const handleImportLDAPUsers = async () => {
    if (selectedLDAPImportableCount === 0) return;
    try {
      setImporting(true);
      setError(null);
      const result = await userService.importLDAPUsers({
        ...ldapImportConfig,
        query: ldapPreviewQuery.trim() || undefined,
        limit: ldapPreviewLimit || undefined,
        external_ids: selectedLDAPExternalIDs,
      });
      setImportResult(result);
      setShowImportModal(false);
      setLdapPreview([]);
      setSelectedLDAPExternalIDs([]);
      await loadUsers(1);
    } catch (err: unknown) {
      setError(localizeEnterpriseAuthIssue(
        getRequestErrorMessage(err, t('userManagementPage.importFailed')),
        t,
      ));
    } finally {
      setImporting(false);
    }
  };

  const toggleLDAPSelection = (externalID: string, checked: boolean) => {
    setSelectedLDAPExternalIDs((current) => {
      if (checked) {
        return current.includes(externalID) ? current : [...current, externalID];
      }
      return current.filter((item) => item !== externalID);
    });
  };

  const toggleAllImportableLDAPSelection = (checked: boolean) => {
    setSelectedLDAPExternalIDs(checked ? importableLDAPUsers.map((user) => user.external_id) : []);
  };

  const handleDownloadTemplate = () => {
    const template = [
      [
        'Username',
        'Email',
        'Role',
        'Auth Provider',
        'External ID',
        'Password (optional)',
        'Max Instances',
        'Max CPU Cores',
        'Max Memory (GB)',
        'Max Storage (GB)',
        'Max GPU Count (optional)',
      ],
      ['alice', 'alice@example.com', 'user', 'ldap', 'uid=alice,ou=users,dc=example,dc=com', '', '10', '40', '100', '500', '2'],
      ['bob', '', 'admin', 'local', '', 'admin123', '20', '80', '200', '1000', '4'],
    ]
      .map((row) => row.map((value) => /[",\n]/.test(value) ? `"${value.replace(/"/g, '""')}"` : value).join(','))
      .join('\n');

    const blob = new Blob([template], { type: 'text/csv;charset=utf-8;' });
    const url = window.URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = 'clawmanager-user-import-template.csv';
    link.click();
    window.URL.revokeObjectURL(url);
  };

  const handleModalBackgroundClick = (e: React.MouseEvent, closeFn: () => void) => {
    if (e.target === e.currentTarget) {
      closeFn();
    }
  };

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-lg">{t('userManagementPage.loading')}</div>
      </div>
    );
  }

  return (
    <AdminLayout title={t('admin.userManagement')}>
      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div
          className={`inline-flex w-fit items-center gap-2 rounded-md border px-3 py-2 text-sm font-medium ${getEnterpriseAuthToneClasses(enterpriseAuthSummary.tone)}`}
        >
          <span className={`h-2 w-2 rounded-full ${getEnterpriseAuthDotClasses(enterpriseAuthSummary.tone)}`} />
          <span>{enterpriseAuthSummary.label}</span>
        </div>
        <div className="flex justify-end gap-3">
          <button onClick={() => { setImportMode('csv'); setShowImportModal(true); }} className="app-button-secondary">
            {t('userManagementPage.importUsers')}
          </button>
          <button
            onClick={() => {
              setShowAddModal(true);
              setCreateUserError(null);
              setCreateUserFieldErrors({});
            }}
            className="app-button-primary"
          >
            {t('admin.addUser')}
          </button>
        </div>
      </div>

      <div className="space-y-4">
        {enterpriseAuthNotice && (
          <div className={`rounded-xl border px-4 py-3 text-sm ${getEnterpriseAuthToneClasses(enterpriseAuthNotice.tone as EnterpriseAuthTone)}`}>
            {enterpriseAuthNotice.message}
          </div>
        )}

        {error && (
          <div className="mb-4 rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-red-700">
            {error}
            <button
              onClick={() => setError(null)}
              className="float-right text-red-500 hover:text-red-700"
            >
              ×
            </button>
          </div>
        )}

        {importResult && (
          <div className="app-panel mb-4 px-4 py-4 text-sm text-[#5f5957]">
            <div className="flex items-start justify-between gap-4">
              <div>
                <div className="font-medium text-[#171212]">
                  {t('userManagementPage.importCompleted', {
                    created: importResult.created_count,
                    failed: importResult.failed_count,
                  })}
                  {typeof importResult.skipped_count === 'number' && (
                    <span className="ml-2 text-amber-700">
                      {t('userManagementPage.importSkipped', { skipped: importResult.skipped_count })}
                    </span>
                  )}
                </div>
                <div className="mt-1 text-[#8f5b4b]">
                  {t('userManagementPage.expectedColumns')} <code>Username,Email,Role,Auth Provider,External ID,Password (optional),Max Instances,Max CPU Cores,Max Memory (GB),Max Storage (GB),Max GPU Count (optional)</code>
                </div>
              </div>
              <button
                onClick={() => setImportResult(null)}
                className="text-[#8f5b4b] hover:text-[#171212]"
              >
                ×
              </button>
            </div>
            {importResult.errors.length > 0 && (
              <div className="mt-3 max-h-48 overflow-y-auto rounded-lg border border-[#eadfd8] bg-white p-3">
                <ul className="space-y-2">
                  {importResult.errors.map((item, index) => (
                    <li key={`${item.line}-${index}`} className="text-sm text-[#5f5957]">
                      {t('userManagementPage.lineError', {
                        line: item.line,
                        username: item.username ? ` (${item.username})` : '',
                      })}: {localizeEnterpriseAuthIssue(item.error, t)}
                    </li>
                  ))}
                </ul>
              </div>
            )}
            {importResult.created_users.length > 0 && (
              <div className="mt-3 max-h-56 overflow-y-auto rounded-lg border border-[#eadfd8] bg-white p-3">
                <div className="mb-2 text-sm font-medium text-[#171212]">{t('userManagementPage.createdAccounts')}</div>
                <ul className="space-y-2">
                  {importResult.created_users.map((item, index) => (
                    <li key={`${item.username}-${index}`} className="rounded-md bg-[#fff8f5] px-3 py-2 text-sm text-[#5f5957]">
                      <div><span className="font-medium text-[#171212]">{item.username}</span>{item.login_alias ? ` · ${item.login_alias}` : ''} · {item.role} · {item.auth_provider || 'local'}</div>
                      <div>{t('auth.email')}: {item.email}</div>
                      {item.warning_codes?.map((warningCode) => (
                        <div key={warningCode} className="text-amber-700">
                          {warningCode === 'ldap_password_ignored' ? t('userManagementPage.ldapPasswordIgnored') : warningCode}
                        </div>
                      ))}
                      <div>
                        {t('userManagementPage.quota')}: {item.max_instances} / {item.max_cpu_cores} CPU / {item.max_memory_gb} GB / {item.max_storage_gb} GB / {item.max_gpu_count} GPU
                      </div>
                      {item.initial_password && (
                        <div>{t('userManagementPage.initialPassword')}: <code>{item.initial_password}</code></div>
                      )}
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        )}

        <div className="app-panel">
          <div className="flex flex-wrap gap-2 border-b border-gray-200 px-6 py-4">
            {userFilterOptions.map((option) => (
              <button
                key={option.value}
                type="button"
                onClick={() => setUserFilter(option.value)}
                className={`rounded-md px-3 py-1.5 text-sm font-medium ${
                  userFilter === option.value
                    ? 'bg-indigo-600 text-white'
                    : 'bg-gray-100 text-gray-700 hover:bg-gray-200'
                }`}
              >
                {option.label}
              </button>
            ))}
          </div>
          {users.length === 0 ? (
            <div className="px-6 py-12 text-center text-sm text-gray-500">
              {t('userManagementPage.noUsers')}
            </div>
          ) : filteredUsers.length === 0 ? (
            <div className="px-6 py-12 text-center text-sm text-gray-500">
              {t('userManagementPage.noFilteredUsers')}
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="min-w-full divide-y divide-gray-200">
                <thead className="bg-gray-50">
                  <tr>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                      {t('auth.username')}
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                      {t('auth.email')}
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                      {t('admin.role')}
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                      {t('userManagementPage.authProvider')}
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                      {t('common.status')}
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                      {t('common.createdAt')}
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                      Last Login
                    </th>
                    <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">
                      {t('aiAuditPage.action')}
                    </th>
                  </tr>
                </thead>
                <tbody className="bg-white divide-y divide-gray-200">
                  {filteredUsers.map((user) => (
                    <tr key={user.id}>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <div className="text-sm font-medium text-gray-900">{user.username}</div>
                        {user.login_alias && <div className="text-xs text-gray-500">{user.login_alias}</div>}
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <div className="text-sm text-gray-500">{user.email}</div>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span className={`px-2 inline-flex text-xs leading-5 font-semibold rounded-full ${
                          user.role === 'admin'
                            ? 'bg-purple-100 text-purple-800'
                            : 'bg-green-100 text-green-800'
                        }`}>
                          {user.role === 'admin' ? t('common.admin') : t('common.user')}
                        </span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span className={`px-2 inline-flex text-xs leading-5 font-semibold rounded-full ${
                          user.auth_provider === 'ldap'
                            ? 'bg-blue-100 text-blue-800'
                            : 'bg-gray-100 text-gray-800'
                        }`}>
                          {user.auth_provider === 'ldap' ? t('userManagementPage.authProviderLdap') : t('userManagementPage.authProviderLocal')}
                        </span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span className={`px-2 inline-flex text-xs leading-5 font-semibold rounded-full ${
                          user.is_active
                            ? 'bg-green-100 text-green-800'
                            : 'bg-red-100 text-red-800'
                        }`}>
                          {user.is_active ? t('modelManagementPage.active') : t('modelManagementPage.inactive')}
                        </span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                        {new Date(user.created_at).toLocaleDateString()}
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                        {user.last_login ? new Date(user.last_login).toLocaleString() : '-'}
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                        <button
                          onClick={() => handleEditQuota(user)}
                          className="text-indigo-600 hover:text-indigo-900 mr-4"
                        >
                          {t('userManagementPage.quota')}
                        </button>
                        <button
                          onClick={() => handleEditRole(user)}
                          className="text-indigo-600 hover:text-indigo-900 mr-4"
                        >
                          {t('admin.role')}
                        </button>
                        {user.auth_provider === 'ldap' && (
                          user.is_active ? (
                            <button
                              onClick={() => handleDisableClick(user)}
                              className="mr-4 text-orange-600 hover:text-orange-900"
                            >
                              {t('common.disable')}
                            </button>
                          ) : (
                            <button
                              onClick={() => handleRestoreClick(user)}
                              className="mr-4 text-green-600 hover:text-green-900"
                            >
                              {t('common.restore')}
                            </button>
                          )
                        )}
                        <button
                          onClick={() => handleDeleteClick(user)}
                          className="text-red-600 hover:text-red-900"
                        >
                          {t('common.delete')}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {totalUsers > 0 && (
            <div className="flex flex-col gap-3 border-t border-gray-200 px-6 py-4 text-sm text-gray-500 sm:flex-row sm:items-center sm:justify-between">
              <div>
                {t('userManagementPage.showingUsers', {
                  from: showingFrom,
                  to: showingTo,
                  total: totalUsers,
                })}
              </div>
              <div className="flex items-center gap-3">
                <button
                  type="button"
                  onClick={() => setPage((current) => Math.max(1, current - 1))}
                  disabled={page <= 1}
                  className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {t('admin.prev')}
                </button>
                <span>
                  {t('admin.pageSummary', { page, total: totalPages })}
                </span>
                <button
                  type="button"
                  onClick={() => setPage((current) => Math.min(totalPages, current + 1))}
                  disabled={page >= totalPages}
                  className="app-button-secondary disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {t('admin.nextPage')}
                </button>
              </div>
            </div>
          )}
        </div>
      </div>

      {showImportModal && (
        <div
          className="fixed inset-0 h-full w-full overflow-y-auto bg-gray-600 bg-opacity-50"
          onClick={(e) => handleModalBackgroundClick(e, () => setShowImportModal(false))}
        >
          <div className="relative top-12 mx-auto w-[min(42rem,calc(100%-2rem))] rounded-md border bg-white p-5 shadow-lg">
            <h3 className="mb-4 text-lg font-medium text-gray-900">
              {t('userManagementPage.importUsers')}
            </h3>
            <div className="mb-4 flex gap-2 border-b border-gray-200">
              <button type="button" onClick={() => setImportMode('csv')} className={`border-b-2 px-3 py-2 text-sm font-medium ${importMode === 'csv' ? 'border-red-500 text-red-600' : 'border-transparent text-gray-500'}`}>{t('userManagementPage.csvImport')}</button>
              <button type="button" disabled={!enterpriseAuthStatus?.enabled || !enterpriseAuthStatus.configured || Object.values(enterpriseAuthStatus.checks || {}).includes('failed')} onClick={() => { setImportMode('ldap'); if (ldapPreview.length === 0) void handlePreviewLDAPUsers(); }} className={`border-b-2 px-3 py-2 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-40 ${importMode === 'ldap' ? 'border-red-500 text-red-600' : 'border-transparent text-gray-500'}`}>{t('userManagementPage.ldapImport')}</button>
            </div>
            {importMode === 'csv' ? (
              <div className="space-y-4">
                <div className="rounded-lg border border-[#eadfd8] bg-[#fff8f5] p-3 text-sm text-[#5f5957]">
                  <div className="font-medium text-[#171212]">{t('userManagementPage.supportedFormat')}</div>
                  <div className="mt-1">{t('userManagementPage.csvHeaders')}</div>
                  <code className="mt-2 block rounded bg-white px-2 py-1 text-xs">Username,Email,Role,Auth Provider,External ID,Password (optional),Max Instances,Max CPU Cores,Max Memory (GB),Max Storage (GB),Max GPU Count (optional)</code>
                  <div className="mt-2 text-xs text-[#8f5b4b]">{t('userManagementPage.csvHelp')}</div>
                  <button type="button" onClick={handleDownloadTemplate} className="mt-3 inline-flex items-center rounded-md border border-[#eadfd8] bg-white px-3 py-2 text-sm font-medium text-[#5f5957] hover:bg-[#fff2ea]">{t('userManagementPage.downloadTemplate')}</button>
                </div>
                <input ref={importInputRef} type="file" accept=".csv" onChange={(e) => setImportFile(e.target.files?.[0] || null)} className="hidden" />
                <div className="flex items-center gap-3"><button type="button" onClick={() => importInputRef.current?.click()} className="rounded-md bg-[#ef4444] px-4 py-2 text-sm font-medium text-white hover:bg-[#dc2626]">{t('userManagementPage.chooseFile')}</button><span className="text-sm text-gray-500">{importFile ? importFile.name : t('userManagementPage.noFileSelected')}</span></div>
              </div>
            ) : (
              <div className="space-y-4">
                {enterpriseAuthSyncRole && (
                  <div className="rounded-lg border border-blue-100 bg-blue-50 p-3 text-sm text-blue-800">
                    {t('userManagementPage.ldapSyncRoleNotice')}
                  </div>
                )}
                {ldapPreviewLoading ? <div className="py-8 text-center text-sm text-gray-500">{t('userManagementPage.ldapLoading')}</div> : (
                  <>
                    <div className="grid grid-cols-1 gap-3 rounded-lg border border-gray-200 bg-gray-50 p-3 sm:grid-cols-[1fr_8rem_auto] sm:items-end">
                      <label className="text-sm text-gray-700">
                        {t('userManagementPage.ldapSearch')}
                        <input
                          type="search"
                          value={ldapPreviewQuery}
                          onChange={(event) => setLdapPreviewQuery(event.target.value)}
                          onKeyDown={(event) => {
                            if (event.key === 'Enter') void handlePreviewLDAPUsers();
                          }}
                          className="mt-1 block w-full rounded border px-2 py-2"
                          placeholder={t('userManagementPage.ldapSearchPlaceholder')}
                        />
                      </label>
                      <label className="text-sm text-gray-700">
                        {t('userManagementPage.ldapPreviewLimit')}
                        <input
                          type="number"
                          min="1"
                          value={ldapPreviewLimit}
                          onChange={(event) => setLdapPreviewLimit(Number(event.target.value) || 100)}
                          className="mt-1 block w-full rounded border px-2 py-2"
                        />
                      </label>
                      <button
                        type="button"
                        onClick={() => void handlePreviewLDAPUsers()}
                        className="rounded-md border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-100"
                      >
                        {t('userManagementPage.ldapRefreshPreview')}
                      </button>
                    </div>
                    <div className="grid grid-cols-4 gap-2 text-center text-sm"><div className="rounded bg-gray-50 p-2"><b>{ldapPreview.length}</b><div>{t('userManagementPage.ldapTotal')}</div></div><div className="rounded bg-green-50 p-2"><b>{importableLDAPUsers.length}</b><div>{t('userManagementPage.ldapReady')}</div></div><div className="rounded bg-blue-50 p-2"><b>{selectedLDAPImportableCount}</b><div>{t('userManagementPage.ldapSelected')}</div></div><div className="rounded bg-amber-50 p-2"><b>{ldapPreview.length - importableLDAPUsers.length}</b><div>{t('userManagementPage.ldapSkipped')}</div></div></div>
                    <div className="max-h-48 overflow-auto rounded border border-gray-200">
                      <table className="min-w-full text-left text-sm">
                        <thead className="bg-gray-50">
                          <tr>
                            <th className="w-10 px-3 py-2">
                              <input
                                type="checkbox"
                                checked={allImportableLDAPSelected}
                                disabled={importableLDAPUsers.length === 0}
                                onChange={(event) => toggleAllImportableLDAPSelection(event.target.checked)}
                              />
                            </th>
                            <th className="px-3 py-2">{t('auth.username')}</th>
                            <th className="px-3 py-2">{t('auth.email')}</th>
                            <th className="px-3 py-2">{t('userManagementPage.ldapRole')}</th>
                            <th className="px-3 py-2">{t('userManagementPage.quota')}</th>
                            <th className="px-3 py-2">{t('common.status')}</th>
                          </tr>
                        </thead>
                        <tbody>
                          {ldapPreview.map((user) => {
                            const role = (enterpriseAuthSyncRole ? user.role : ldapImportConfig.role) === 'admin' ? 'admin' : 'user';
                            const previewQuota = quotaForImportRole(role, ldapImportConfig, enterpriseAuthSyncRole);
                            const importable = user.status === 'ready' || user.status === 'pending_alias';
                            return (
                              <tr key={user.external_id} className="border-t">
                                <td className="px-3 py-2">
                                  <input
                                    type="checkbox"
                                    checked={selectedLDAPSet.has(user.external_id)}
                                    disabled={!importable}
                                    onChange={(event) => toggleLDAPSelection(user.external_id, event.target.checked)}
                                  />
                                </td>
                                <td className="px-3 py-2">{user.username || '-'}</td>
                                <td className="px-3 py-2">{user.email || '-'}</td>
                                <td className="px-3 py-2">{role === 'admin' ? t('common.admin') : t('common.user')}</td>
                                <td className="px-3 py-2 text-xs text-gray-600">{previewQuota.max_instances} / {previewQuota.max_cpu_cores} CPU / {previewQuota.max_memory_gb} GB / {previewQuota.max_storage_gb} GB / {previewQuota.max_gpu_count} GPU</td>
                                <td className="px-3 py-2">{user.status === 'ready' ? t('userManagementPage.ldapReady') : user.status === 'pending_alias' ? t('userManagementPage.ldapPendingAlias') : user.status === 'exists' ? t('userManagementPage.ldapExists') : (localizeEnterpriseAuthIssue(user.error, t) || t('userManagementPage.ldapInvalid'))}</td>
                              </tr>
                            );
                          })}
                        </tbody>
                      </table>
                    </div>
                    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
                      {([['role', t('admin.role')], ['max_instances', t('userManagementPage.maxInstances')], ['max_cpu_cores', t('userManagementPage.maxCpuCores')], ['max_memory_gb', t('userManagementPage.maxMemoryGb')], ['max_storage_gb', t('userManagementPage.maxStorageGb')], ['max_gpu_count', t('userManagementPage.maxGpuCount')]] as const).map(([key, label]) => (
                        key === 'role' && enterpriseAuthSyncRole ? null : (
                          <label key={key} className="text-sm text-gray-700">
                            {label}
                            {key === 'role' ? (
                              <select value={ldapImportConfig.role} onChange={(e) => setLdapImportConfig({ ...ldapImportConfig, role: e.target.value as 'admin' | 'user' })} className="mt-1 block w-full rounded border px-2 py-2">
                                <option value="user">{t('common.user')}</option>
                                <option value="admin">{t('common.admin')}</option>
                              </select>
                            ) : (
                              <input type="number" min="0" value={ldapImportConfig[key]} onChange={(e) => setLdapImportConfig({ ...ldapImportConfig, [key]: Number(e.target.value) })} className="mt-1 block w-full rounded border px-2 py-2" />
                            )}
                          </label>
                        )
                      ))}
                    </div>
                  </>
                )}
              </div>
            )}
            <div className="mt-4 flex justify-end space-x-2">
              <button
                onClick={() => {
                  setShowImportModal(false);
                  setImportFile(null);
                  setLdapPreview([]);
                  setSelectedLDAPExternalIDs([]);
                }}
                className="rounded-md bg-gray-300 px-4 py-2 text-gray-700 hover:bg-gray-400"
              >
                {t('common.cancel')}
              </button>
              <button
                onClick={importMode === 'csv' ? handleImportUsers : handleImportLDAPUsers}
                disabled={importing || (importMode === 'csv' ? !importFile : selectedLDAPImportableCount === 0)}
                className="rounded-md bg-[#ef4444] px-4 py-2 text-white hover:bg-[#dc2626] disabled:cursor-not-allowed disabled:opacity-50"
              >
                {importing ? t('userManagementPage.importing') : t('userManagementPage.startImport')}
              </button>
            </div>
          </div>
        </div>
      )}

      {showAddModal && (
        <div
          className="fixed inset-0 bg-gray-600 bg-opacity-50 overflow-y-auto h-full w-full"
          onClick={(e) => handleModalBackgroundClick(e, () => {
            setShowAddModal(false);
            setCreateUserError(null);
            setCreateUserFieldErrors({});
          })}
        >
          <div className="relative top-20 mx-auto p-5 border w-96 shadow-lg rounded-md bg-white">
            <h3 className="text-lg font-medium text-gray-900 mb-4">
              {t('userManagementPage.addNewUser')}
            </h3>
            {createUserError && (
              <div className="mb-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
                {createUserError}
              </div>
            )}
            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-gray-700">
                  {t('auth.username')}
                </label>
                <input
                  type="text"
                  value={newUser.username}
                  onChange={(e) => {
                    setNewUser({ ...newUser, username: e.target.value });
                    setCreateUserFieldErrors((current) => ({ ...current, username: undefined }));
                    setCreateUserError(null);
                  }}
                  aria-invalid={Boolean(createUserFieldErrors.username)}
                  aria-describedby={createUserFieldErrors.username ? 'create-user-username-error' : undefined}
                  className={`mt-1 block w-full rounded-md border px-3 py-2 ${createUserFieldErrors.username ? 'border-red-400' : 'border-gray-300'}`}
                  placeholder={t('auth.usernamePlaceholder')}
                />
                {createUserFieldErrors.username && (
                  <p id="create-user-username-error" className="mt-1 text-sm text-red-600" role="alert">
                    {createUserFieldErrors.username}
                  </p>
                )}
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700">
                  {t('auth.email')}
                </label>
                <input
                  type="email"
                  value={newUser.email}
                  onChange={(e) => {
                    setNewUser({ ...newUser, email: e.target.value });
                    setCreateUserFieldErrors((current) => ({ ...current, email: undefined }));
                    setCreateUserError(null);
                  }}
                  aria-invalid={Boolean(createUserFieldErrors.email)}
                  aria-describedby={createUserFieldErrors.email ? 'create-user-email-error' : undefined}
                  className={`mt-1 block w-full rounded-md border px-3 py-2 ${createUserFieldErrors.email ? 'border-red-400' : 'border-gray-300'}`}
                  placeholder={t('auth.enterEmail')}
                />
                {createUserFieldErrors.email && (
                  <p id="create-user-email-error" className="mt-1 text-sm text-red-600" role="alert">
                    {createUserFieldErrors.email}
                  </p>
                )}
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700">
                  {t('admin.role')}
                </label>
                <select
                  value={newUser.role}
                  onChange={(e) => {
                    setNewUser({ ...newUser, role: e.target.value as 'admin' | 'user' });
                    setCreateUserFieldErrors((current) => ({ ...current, role: undefined }));
                    setCreateUserError(null);
                  }}
                  aria-invalid={Boolean(createUserFieldErrors.role)}
                  aria-describedby={createUserFieldErrors.role ? 'create-user-role-error' : undefined}
                  className={`mt-1 block w-full rounded-md border px-3 py-2 ${createUserFieldErrors.role ? 'border-red-400' : 'border-gray-300'}`}
                >
                  <option value="user">{t('common.user')}</option>
                  <option value="admin">{t('common.admin')}</option>
                </select>
                {createUserFieldErrors.role && (
                  <p id="create-user-role-error" className="mt-1 text-sm text-red-600" role="alert">
                    {createUserFieldErrors.role}
                  </p>
                )}
              </div>
              <div className="rounded-md border border-[#eadfd8] bg-[#fff8f5] px-3 py-2 text-sm text-[#5f5957]">
                {t('userManagementPage.initialPassword')}: <span className="font-medium">{newUser.role === 'admin' ? 'admin123' : 'user123'}</span>
              </div>
            </div>
            <div className="mt-4 flex justify-end space-x-2">
              <button
                onClick={() => {
                  setShowAddModal(false);
                  setCreateUserError(null);
                  setCreateUserFieldErrors({});
                }}
                className="px-4 py-2 bg-gray-300 text-gray-700 rounded-md hover:bg-gray-400"
              >
                {t('common.cancel')}
              </button>
              <button
                onClick={handleAddUser}
                className="px-4 py-2 bg-indigo-600 text-white rounded-md hover:bg-indigo-700"
              >
                {t('common.create')}
              </button>
            </div>
          </div>
        </div>
      )}

      {showQuotaModal && quota && (
        <div
          className="fixed inset-0 bg-gray-600 bg-opacity-50 overflow-y-auto h-full w-full"
          onClick={(e) => handleModalBackgroundClick(e, () => setShowQuotaModal(false))}
        >
          <div className="relative top-20 mx-auto p-5 border w-96 shadow-lg rounded-md bg-white">
            <h3 className="text-lg font-medium text-gray-900 mb-4">
              {t('userManagementPage.editQuotaFor', { username: selectedUser?.username || '' })}
            </h3>
            <div className="space-y-4">
              <FieldNumber label={t('userManagementPage.maxInstances')} value={quota.max_instances} onChange={(value) => setQuota({ ...quota, max_instances: value })} />
              <FieldNumber label={t('userManagementPage.maxCpuCores')} value={quota.max_cpu_cores} onChange={(value) => setQuota({ ...quota, max_cpu_cores: value })} />
              <FieldNumber label={t('userManagementPage.maxMemoryGb')} value={quota.max_memory_gb} onChange={(value) => setQuota({ ...quota, max_memory_gb: value })} />
              <FieldNumber label={t('userManagementPage.maxStorageGb')} value={quota.max_storage_gb} onChange={(value) => setQuota({ ...quota, max_storage_gb: value })} />
              <FieldNumber label={t('userManagementPage.maxGpuCount')} value={quota.max_gpu_count} onChange={(value) => setQuota({ ...quota, max_gpu_count: value })} />
            </div>
            <div className="mt-4 flex justify-end space-x-2">
              <button
                onClick={() => setShowQuotaModal(false)}
                className="px-4 py-2 bg-gray-300 text-gray-700 rounded-md hover:bg-gray-400"
              >
                {t('common.cancel')}
              </button>
              <button
                onClick={handleSaveQuota}
                className="px-4 py-2 bg-indigo-600 text-white rounded-md hover:bg-indigo-700"
              >
                {t('common.save')}
              </button>
            </div>
          </div>
        </div>
      )}

      {showRoleModal && (
        <div
          className="fixed inset-0 bg-gray-600 bg-opacity-50 overflow-y-auto h-full w-full"
          onClick={(e) => handleModalBackgroundClick(e, () => setShowRoleModal(false))}
        >
          <div className="relative top-20 mx-auto p-5 border w-80 shadow-lg rounded-md bg-white">
            <h3 className="text-lg font-medium text-gray-900 mb-4">
              {t('userManagementPage.changeRoleFor', { username: selectedUser?.username || '' })}
            </h3>
            <div className="space-y-2">
              <button
                onClick={() => handleSaveRole('user')}
                className={`w-full px-4 py-2 rounded-md ${
                  selectedUser?.role === 'user'
                    ? 'bg-green-600 text-white'
                    : 'bg-gray-200 text-gray-700 hover:bg-gray-300'
                }`}
              >
                {t('common.user')}
              </button>
              <button
                onClick={() => handleSaveRole('admin')}
                className={`w-full px-4 py-2 rounded-md ${
                  selectedUser?.role === 'admin'
                    ? 'bg-purple-600 text-white'
                    : 'bg-gray-200 text-gray-700 hover:bg-gray-300'
                }`}
              >
                {t('common.admin')}
              </button>
            </div>
            <div className="mt-4 flex justify-end">
              <button
                onClick={() => setShowRoleModal(false)}
                className="px-4 py-2 bg-gray-300 text-gray-700 rounded-md hover:bg-gray-400"
              >
                {t('common.cancel')}
              </button>
            </div>
          </div>
        </div>
      )}

      {showDisableModal && userToDisable && (
        <div
          className="fixed inset-0 bg-gray-600 bg-opacity-50 overflow-y-auto h-full w-full"
          onClick={(e) => handleModalBackgroundClick(e, handleCancelDisable)}
        >
          <div className="relative top-20 mx-auto p-5 border w-96 shadow-lg rounded-md bg-white">
            <h3 className="text-lg font-medium text-gray-900 mb-4">
              {t('userManagementPage.confirmDisableTitle')}
            </h3>
            <p className="text-gray-600 mb-4">
              {t('userManagementPage.confirmDisableLdapMessage', { username: userToDisable.username })}
            </p>
            <div className="mt-4 flex justify-end space-x-2">
              <button
                onClick={handleCancelDisable}
                className="px-4 py-2 bg-gray-300 text-gray-700 rounded-md hover:bg-gray-400"
              >
                {t('common.cancel')}
              </button>
              <button
                onClick={handleConfirmDisable}
                className="px-4 py-2 bg-orange-600 text-white rounded-md hover:bg-orange-700"
              >
                {t('common.disable')}
              </button>
            </div>
          </div>
        </div>
      )}

      {showDeleteModal && userToDelete && (
        <div
          className="fixed inset-0 bg-gray-600 bg-opacity-50 overflow-y-auto h-full w-full"
          onClick={(e) => handleModalBackgroundClick(e, handleCancelDelete)}
        >
          <div className="relative top-20 mx-auto p-5 border w-96 shadow-lg rounded-md bg-white">
            <h3 className="text-lg font-medium text-gray-900 mb-4">
              {t('userManagementPage.confirmDeleteTitle')}
            </h3>
            <p className="text-gray-600 mb-4">
              {userToDelete.auth_provider === 'ldap'
                ? t('userManagementPage.confirmDeleteLdapMessage', { username: userToDelete.username })
                : t('userManagementPage.confirmDeleteMessage', { username: userToDelete.username })}
            </p>
            <div className="mt-4 flex justify-end space-x-2">
              <button
                onClick={handleCancelDelete}
                className="px-4 py-2 bg-gray-300 text-gray-700 rounded-md hover:bg-gray-400"
              >
                {t('common.cancel')}
              </button>
              <button
                onClick={handleConfirmDelete}
                className="px-4 py-2 bg-red-600 text-white rounded-md hover:bg-red-700"
              >
                {t('common.delete')}
              </button>
            </div>
          </div>
        </div>
      )}

      {showRestoreModal && userToRestore && (
        <div
          className="fixed inset-0 bg-gray-600 bg-opacity-50 overflow-y-auto h-full w-full"
          onClick={(e) => handleModalBackgroundClick(e, handleCancelRestore)}
        >
          <div className="relative top-20 mx-auto p-5 border w-96 shadow-lg rounded-md bg-white">
            <h3 className="text-lg font-medium text-gray-900 mb-4">
              {t('userManagementPage.confirmRestoreTitle')}
            </h3>
            <p className="text-gray-600 mb-4">
              {t('userManagementPage.confirmRestoreLdapMessage', { username: userToRestore.username })}
            </p>
            <div className="mt-4 flex justify-end space-x-2">
              <button
                onClick={handleCancelRestore}
                className="px-4 py-2 bg-gray-300 text-gray-700 rounded-md hover:bg-gray-400"
              >
                {t('common.cancel')}
              </button>
              <button
                onClick={handleConfirmRestore}
                className="px-4 py-2 bg-green-600 text-white rounded-md hover:bg-green-700"
              >
                {t('common.restore')}
              </button>
            </div>
          </div>
        </div>
      )}
    </AdminLayout>
  );
};

function FieldNumber({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number;
  onChange: (value: number) => void;
}) {
  return (
    <div>
      <label className="block text-sm font-medium text-gray-700">{label}</label>
      <input
        type="number"
        value={value}
        onChange={(e) => onChange(parseInt(e.target.value, 10) || 0)}
        className="mt-1 block w-full px-3 py-2 border border-gray-300 rounded-md"
      />
    </div>
  );
}

function getRequestErrorMessage(err: unknown, fallback: string) {
  if (typeof err === 'object' && err !== null && 'response' in err) {
    const response = (err as { response?: { data?: { error?: unknown } } }).response;
    if (typeof response?.data?.error === 'string') {
      return response.data.error;
    }
  }

  return fallback;
}

function getCreateUserError(
  err: unknown,
  translate: (key: string) => string,
): { message: string; field?: CreateUserField } {
  const rawMessage = getRequestErrorMessage(err, '').trim();
  const knownErrors: Record<string, { key: string; field?: CreateUserField }> = {
    'Username is required': { key: 'userManagementPage.createUsernameRequired', field: 'username' },
    'Username must be at least 3 characters': { key: 'userManagementPage.createUsernameMinLength', field: 'username' },
    'Username must be at most 32 characters': { key: 'userManagementPage.createUsernameMaxLength', field: 'username' },
    'Username must be alphanumeric': { key: 'userManagementPage.createUsernameAlphanumeric', field: 'username' },
    'Email is required': { key: 'userManagementPage.createEmailRequired', field: 'email' },
    'Email must be a valid email': { key: 'userManagementPage.createEmailInvalid', field: 'email' },
    'Role is required': { key: 'userManagementPage.createRoleRequired', field: 'role' },
    'Role is invalid': { key: 'userManagementPage.createRoleInvalid', field: 'role' },
    'username already exists': { key: 'userManagementPage.createUsernameExists' },
    'email already exists': { key: 'userManagementPage.createEmailExists' },
    'local usernames cannot start with ldap_': { key: 'userManagementPage.createReservedUsername', field: 'username' },
    'LDAP users must be imported from LDAP': { key: 'userManagementPage.createLdapNotAllowed' },
  };
  const knownError = knownErrors[rawMessage];

  return knownError
    ? { message: translate(knownError.key), field: knownError.field }
    : { message: translate('userManagementPage.createFailed') };
}

export default UserManagementPage;
