import React, { useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import {
  ArrowLeft,
  Bot,
  ChevronDown,
  ChevronRight,
  Gauge,
  Home,
  LogOut,
  Monitor,
  Server,
  Settings,
  Shield,
  Users,
} from 'lucide-react';
import { useAuth } from '../contexts/AuthContext';
import { useI18n } from '../contexts/I18nContext';
import LanguageSwitcher from './LanguageSwitcher';
import { CATEGORIES } from '../pages/admin/protection/_data';

interface AdminLayoutProps {
  children: React.ReactNode;
  title?: string;
}

interface NavItem {
  path: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  matchPaths?: string[];
  exact?: boolean;
  hasPopup?: boolean;
}

const shellContainerClass = 'w-full px-3 sm:px-4 lg:px-5 2xl:px-6';
const appLogoSrc = '/lobster_logo.png';

const AdminLayout: React.FC<AdminLayoutProps> = ({ children, title = '' }) => {
  const location = useLocation();
  const { user, logout } = useAuth();
  const { t } = useI18n();
  const [profileExpanded, setProfileExpanded] = useState(false);
  const [popoverItemPath, setPopoverItemPath] = useState<string | null>(null);

  // 安全防护 popup 用 7 类别（移除总览 + events，跟主入口和侧边其他 nav 重复）
  const PROTECTION_POPUP_CATS = CATEGORIES.filter((c) => c.id !== 'overview' && c.id !== 'events');

  const navItems: NavItem[] = [
    { path: '/admin', label: t('nav.adminDashboard'), icon: Home, exact: true },
    { path: '/admin/users', label: t('nav.users'), icon: Users },
    { path: '/admin/instances', label: t('nav.instances'), icon: Monitor },
    { path: '/admin/runtime-pods', label: t('nav.runtime'), icon: Server },
    {
      path: '/admin/secplane',
      label: t('nav.secplane'),
      icon: Shield,
      matchPaths: [
        '/admin/secplane/runtime',
        '/admin/secplane/events',
        '/admin/secplane/cat-trust',
        '/admin/secplane/cat-identity',
        '/admin/secplane/cat-isolate',
        '/admin/secplane/cat-govern',
        '/admin/secplane/cat-policy',
        '/admin/secplane/cat-comm',
        '/admin/security',
        '/admin/assets',
        '/admin/skills',
      ],
      hasPopup: true,
    },
    {
      path: '/admin/ai-gateway',
      label: t('nav.aiGateway'),
      icon: Bot,
      matchPaths: ['/admin/models', '/admin/ai-audit', '/admin/costs', '/admin/risk-rules', '/admin/session-usage'],
    },
    { path: '/admin/settings', label: t('nav.settings'), icon: Settings },
  ];

  const isActive = (item: NavItem) => {
    if (item.exact) {
      return location.pathname === item.path;
    }
    const candidates = [item.path, ...(item.matchPaths ?? [])];
    return candidates.some((path) => location.pathname === path || location.pathname.startsWith(`${path}/`));
  };

  const handleLogout = () => {
    logout();
    window.location.href = '/login';
  };

  const renderNavItem = (item: NavItem) => {
    const Icon = item.icon;
    return (
      <Link
        key={item.path}
        to={item.path}
        className={`app-nav-link ${isActive(item) ? 'app-nav-link-active' : ''}`}
      >
        <Icon className="h-4 w-4 shrink-0" />
        <span className="truncate">{item.label}</span>
      </Link>
    );
  };

  const renderNavItemWithPopup = (item: NavItem) => {
    const Icon = item.icon;
    if (!item.hasPopup) {
      return (
        <div key={item.path} className="relative">
          <Link
            to={item.path}
            className={`app-nav-link ${isActive(item) ? 'app-nav-link-active' : ''}`}
          >
            <Icon className="h-4 w-4 shrink-0" />
            <span className="truncate">{item.label}</span>
          </Link>
        </div>
      );
    }
    return (
      <div
        key={item.path}
        className="relative"
        onMouseEnter={() => setPopoverItemPath(item.path)}
        onMouseLeave={() => setPopoverItemPath(null)}
      >
        <Link
          to={item.path}
          className={`app-nav-link ${isActive(item) ? 'app-nav-link-active' : ''}`}
        >
          <Icon className="h-4 w-4 shrink-0" />
          <span className="truncate">{item.label}</span>
          <ChevronRight className="ml-auto h-3.5 w-3.5 shrink-0 opacity-60" />
        </Link>
        {popoverItemPath === item.path && (
          <div className="absolute left-full top-0 z-40 ml-3 w-64 rounded-md border border-slate-200 bg-white p-2 shadow-lg">
            <div className="px-3 pb-2 pt-1 text-xs font-semibold uppercase tracking-wider text-slate-500">
              {t('adminLayout.protectionCats')}
            </div>
            {PROTECTION_POPUP_CATS.map((cat) => (
              <Link
                key={cat.id}
                to={cat.path}
                className={`flex items-center gap-2 rounded-md px-3 py-2 text-sm transition ${
                  cat.disabled
                    ? 'cursor-not-allowed opacity-50'
                    : 'text-slate-950 hover:bg-slate-100'
                }`}
                onClick={() => setPopoverItemPath(null)}
              >
                <span
                  className="inline-block h-1.5 w-1.5 shrink-0 rounded-full"
                  style={{ background: cat.color }}
                />
                <span className="flex-1 truncate font-medium">
                  {cat.labelKey ? t(cat.labelKey) : cat.label}
                </span>
                {cat.count !== undefined && !cat.disabled && (
                  <span className="text-[10px] text-slate-500">
                    {t('adminLayout.sceneCount', { count: cat.count })}
                  </span>
                )}
                {cat.disabled && (
                  <span className="badge badge-slate text-[10px]">{t('adminLayout.planned')}</span>
                )}
              </Link>
            ))}
          </div>
        )}
      </div>
    );
  };

  return (
    <div className="app-shell">
      <div className="md:hidden">
        <header className="app-topbar">
          <div className={shellContainerClass}>
            <div className="flex min-h-16 items-center justify-between gap-4 py-3">
              <Link to="/admin" className="flex items-center gap-2 text-slate-950">
                <img
                  src={appLogoSrc}
                  alt="ClawManager logo"
                  className="h-9 w-9 object-contain"
                />
                <span className="text-lg font-semibold">{t('app.name')}</span>
              </Link>

              <div className="flex items-center gap-2">
                <LanguageSwitcher />
                <button
                  onClick={handleLogout}
                  className="cm-icon-button"
                  title={t('common.logout')}
                  type="button"
                >
                  <LogOut className="h-4 w-4" />
                </button>
              </div>
            </div>
          </div>

          <nav className="border-t border-slate-200 px-2 py-2">
            <div className="grid gap-1">{navItems.map(renderNavItem)}</div>
          </nav>
        </header>

        <div className="app-subheader">
          <div className={`${shellContainerClass} py-4`}>
            <h1 className="text-xl font-semibold text-slate-950">{title}</h1>
          </div>
        </div>

        <main className={`${shellContainerClass} py-6`}>{children}</main>
      </div>

      <div className="hidden min-h-screen md:flex">
        <aside className="w-64 shrink-0 border-r border-slate-200 bg-white">
          <div className="sticky top-0 flex h-screen flex-col">
            <div className="flex h-20 items-center border-b border-slate-200 px-5">
              <Link to="/admin" className="flex items-center gap-3 text-slate-950">
                <img
                  src={appLogoSrc}
                  alt="ClawManager logo"
                  className="h-9 w-9 object-contain"
                />
                <div>
                  <div className="flex items-center gap-1 text-xs font-medium text-slate-500">
                    <Gauge className="h-3.5 w-3.5" />
                    {t('adminLayout.admin')}
                  </div>
                  <div className="text-lg font-semibold leading-tight">{t('app.name')}</div>
                </div>
              </Link>
            </div>

            <nav className="flex-1 overflow-y-auto p-3">
              <div className="space-y-1">{navItems.map(renderNavItemWithPopup)}</div>
            </nav>

            <div className="border-t border-slate-200 p-3">
              <button
                type="button"
                onClick={() => setProfileExpanded((current) => !current)}
                className="flex w-full items-center gap-3 rounded-md px-3 py-2 text-left hover:bg-slate-100"
              >
                <div className="flex h-9 w-9 items-center justify-center rounded-md bg-slate-900 text-sm font-semibold text-white">
                  {(user?.username?.[0] || 'A').toUpperCase()}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium text-slate-950">{user?.username}</div>
                  <div className="text-xs text-slate-500">{user?.role || 'admin'}</div>
                </div>
                <ChevronDown
                  className={`h-4 w-4 text-slate-500 transition-transform ${
                    profileExpanded ? 'rotate-180' : ''
                  }`}
                />
              </button>

              {profileExpanded && (
                <div className="mt-2 space-y-2 rounded-md border border-slate-200 bg-slate-50 p-2">
                  <LanguageSwitcher />
                  <Link to="/dashboard" className="app-button-secondary w-full">
                    <ArrowLeft className="h-4 w-4" />
                    {t('nav.backToUserDashboard')}
                  </Link>
                  <button onClick={handleLogout} className="app-button-primary w-full" type="button">
                    <LogOut className="h-4 w-4" />
                    {t('common.logout')}
                  </button>
                </div>
              )}
            </div>
          </div>
        </aside>

        <div className="flex min-w-0 flex-1 flex-col">
          <div className="app-subheader">
            <div className={`${shellContainerClass} flex h-20 items-center`}>
              <h1 className="text-2xl font-semibold text-slate-950">{title}</h1>
            </div>
          </div>

          <main className={`${shellContainerClass} flex-1 py-6`}>{children}</main>
        </div>
      </div>
    </div>
  );
};

export default AdminLayout;
