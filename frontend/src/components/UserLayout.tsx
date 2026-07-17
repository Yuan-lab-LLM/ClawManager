import React, { useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import {
  ChevronDown,
  Home,
  LogOut,
  Monitor,
  Package,
  Settings,
  SlidersHorizontal,
  Users,
} from 'lucide-react';
import { useAuth } from '../contexts/AuthContext';
import { useI18n } from '../contexts/I18nContext';
import LanguageSwitcher from './LanguageSwitcher';

interface UserLayoutProps {
  children: React.ReactNode;
  title?: string;
  titleAccessory?: React.ReactNode;
  fillHeight?: boolean;
  scrollableMain?: boolean;
}

interface UserNavItem {
  path: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  exact?: boolean;
}

const shellContainerClass = 'w-full px-3 sm:px-4 lg:px-5 2xl:px-6';
const appLogoSrc = '/lobster_logo.png';

const UserLayout: React.FC<UserLayoutProps> = ({
  children,
  title,
  titleAccessory,
  fillHeight = false,
  scrollableMain = false,
}) => {
  const location = useLocation();
  const navigate = useNavigate();
  const { user, logout } = useAuth();
  const { t } = useI18n();
  const [profileExpanded, setProfileExpanded] = useState(false);

  const navItems: UserNavItem[] = [
    { path: '/dashboard', label: t('nav.userDashboard'), icon: Home, exact: true },
    { path: '/instances', label: t('nav.myInstances'), icon: Monitor },
    { path: '/teams', label: 'Teams', icon: Users },
    { path: '/openclaw-configs', label: t('nav.openClawConfigs'), icon: SlidersHorizontal },
    { path: '/skill-hub', label: t('nav.skillHub'), icon: Package },
    { path: '/settings', label: t('nav.settings'), icon: Settings },
  ];

  const isActive = (item: UserNavItem) => {
    if (item.exact) {
      return location.pathname === item.path;
    }
    return location.pathname === item.path || location.pathname.startsWith(`${item.path}/`);
  };

  const handleLogout = () => {
    logout();
    navigate('/login');
  };

  const renderNavItem = (item: UserNavItem) => {
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

  return (
    <div className="app-shell">
      <div className="md:hidden">
        <header className="app-topbar">
          <div className={shellContainerClass}>
            <div className="flex min-h-16 items-center justify-between gap-4 py-3">
              <Link to="/dashboard" className="flex items-center gap-2 text-slate-950">
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

        {title && (
          <div className="app-subheader">
            <div className={`${shellContainerClass} flex flex-col gap-3 py-4`}>
              <h1 className="text-xl font-semibold text-slate-950">{title}</h1>
              {titleAccessory}
            </div>
          </div>
        )}

        <main className={`${shellContainerClass} py-6`}>{children}</main>
      </div>

      <div className={`hidden md:flex ${fillHeight ? "h-screen overflow-hidden" : "min-h-screen"}`}>
        <aside className="w-64 shrink-0 border-r border-slate-200 bg-white">
          <div className="sticky top-0 flex h-screen flex-col">
            <div className="flex h-20 items-center border-b border-slate-200 px-5">
              <Link to="/dashboard" className="flex items-center gap-3 text-slate-950">
                <img
                  src={appLogoSrc}
                  alt="ClawManager logo"
                  className="h-9 w-9 object-contain"
                />
                <div>
                  <div className="text-xs font-medium text-slate-500">User</div>
                  <div className="text-lg font-semibold leading-tight">{t('app.name')}</div>
                </div>
              </Link>
            </div>

            <nav className="flex-1 overflow-y-auto p-3">
              <div className="space-y-1">{navItems.map(renderNavItem)}</div>
            </nav>

            <div className="border-t border-slate-200 p-3">
              <button
                type="button"
                onClick={() => setProfileExpanded((current) => !current)}
                className="flex w-full items-center gap-3 rounded-md px-3 py-2 text-left hover:bg-slate-100"
              >
                <div className="flex h-9 w-9 items-center justify-center rounded-md bg-slate-900 text-sm font-semibold text-white">
                  {(user?.username?.[0] || 'U').toUpperCase()}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium text-slate-950">{user?.username}</div>
                  <div className="text-xs text-slate-500">{user?.role || 'user'}</div>
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
                  {user?.role === 'admin' && (
                    <Link to="/admin" className="app-button-secondary w-full">
                      {t('nav.adminDashboard')}
                    </Link>
                  )}
                  <button onClick={handleLogout} className="app-button-primary w-full" type="button">
                    <LogOut className="h-4 w-4" />
                    {t('common.logout')}
                  </button>
                </div>
              )}
            </div>
          </div>
        </aside>

        <div className={`flex min-w-0 flex-1 flex-col ${fillHeight ? "min-h-0 overflow-hidden" : ""}`}>
          {title && (
            <div className="app-subheader">
              <div className={`${shellContainerClass} flex min-h-20 items-center justify-between gap-6 py-4`}>
                <div className="min-w-0">
                  <div className="text-[11px] font-semibold uppercase tracking-[0.22em] text-[#b46c50]">
                    Workspace
                  </div>
                  <h1 className="mt-1 truncate text-2xl font-semibold text-slate-950">{title}</h1>
                </div>
                {titleAccessory && (
                  <div className="flex shrink-0 justify-end">
                    {titleAccessory}
                  </div>
                )}
              </div>
            </div>
          )}

          <main
            className={`${shellContainerClass} flex-1 py-6 ${
              fillHeight
                ? `flex min-h-0 flex-col ${scrollableMain ? "overflow-y-auto" : "overflow-hidden"}`
                : ""
            }`}
          >
            {children}
          </main>
        </div>
      </div>
    </div>
  );
};

export default UserLayout;
