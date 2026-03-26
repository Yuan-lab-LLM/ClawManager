import React, { useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { useAuth } from '../contexts/AuthContext';
import { useI18n } from '../contexts/I18nContext';
import LanguageSwitcher from './LanguageSwitcher';

interface UserLayoutProps {
  children: React.ReactNode;
  title?: string;
}

interface UserNavItem {
  path: string;
  label: string;
  icon: string;
  exact?: boolean;
}

const UserLayout: React.FC<UserLayoutProps> = ({ children, title }) => {
  const location = useLocation();
  const navigate = useNavigate();
  const { user, logout } = useAuth();
  const { t } = useI18n();
  const shellContainerClass = 'mx-auto w-full max-w-[1800px] px-4 sm:px-6 lg:px-8 xl:px-10 2xl:px-12';
  const [profileExpanded, setProfileExpanded] = useState(false);

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

  const navItems: UserNavItem[] = [
    { path: '/dashboard', label: t('nav.userDashboard'), icon: 'M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6', exact: true },
    { path: '/instances', label: t('nav.myInstances'), icon: 'M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01' },
    { path: '/settings', label: t('nav.settings'), icon: 'M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z' },
  ];

  const visibleNavItems = navItems;

  return (
    <div className="app-shell">
      <div className="md:hidden">
        <header className="app-topbar">
          <div className={shellContainerClass}>
            <div className="flex min-h-16 items-center justify-between gap-4 py-3">
              <Link
                to="/dashboard"
                className="flex items-center text-[#171212] transition-colors hover:text-[#dc2626]"
              >
                <img
                  src="/lobster_transparent.png"
                  alt="ClawManager logo"
                  className="mr-2 h-10 w-10 object-contain"
                />
                <span className="font-bold text-xl">{t('app.name')}</span>
              </Link>

              <div className="flex items-center gap-3">
                <LanguageSwitcher />
                <button
                  onClick={handleLogout}
                  className="text-[#696363] transition-colors hover:text-[#dc2626]"
                  title={t('common.logout')}
                >
                  <svg
                    className="h-5 w-5"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth={2}
                      d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1"
                    />
                  </svg>
                </button>
              </div>
            </div>
          </div>

          <div className="border-t border-[#eee5df]">
            <div className="px-2 py-3 space-y-1">
              {visibleNavItems.map((item) => (
                <Link
                  key={item.path}
                  to={item.path}
                  className={`app-nav-link text-base ${
                    isActive(item)
                      ? 'app-nav-link-active'
                      : ''
                  }`}
                >
                  <svg
                    className="mr-3 h-5 w-5"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth={2}
                      d={item.icon}
                    />
                  </svg>
                  {item.label}
                </Link>
              ))}
            </div>
          </div>
        </header>

        {title && (
          <div className="app-subheader">
            <div className={`${shellContainerClass} py-4`}>
              <h1 className="text-2xl font-bold text-[#171212]">{title}</h1>
            </div>
          </div>
        )}

        <main className={`${shellContainerClass} py-8`}>
          {children}
        </main>
      </div>

      <div className="hidden min-h-screen md:flex">
        <aside className="w-[248px] shrink-0 border-r border-[#eadfd8] bg-[linear-gradient(180deg,rgba(255,255,255,0.94)_0%,rgba(255,248,244,0.92)_100%)] shadow-[18px_0_50px_-44px_rgba(72,44,24,0.45)]">
          <div className="sticky top-0 flex h-screen flex-col">
            <div className="flex h-[104px] items-center border-b border-[#efe2da] px-5">
              <Link
                to="/dashboard"
                className="flex items-center text-[#171212] transition-colors hover:text-[#dc2626]"
              >
                <img
                  src="/lobster_transparent.png"
                  alt="ClawManager logo"
                  className="mr-3 h-9 w-9 object-contain"
                />
                <div>
                  <div className="text-[10px] font-semibold uppercase tracking-[0.22em] text-[#b46c50]">
                    User
                  </div>
                  <div className="mt-0.5 text-[1.45rem] font-bold leading-none">{t('app.name')}</div>
                </div>
              </Link>
            </div>

            <nav className="flex-1 overflow-y-auto px-3 pb-6">
              <div className="px-3 pb-3 text-xs font-semibold uppercase tracking-[0.18em] text-[#b46c50]">
                Navigation
              </div>
              <div className="space-y-1.5">
                {visibleNavItems.map((item) => (
                  <Link
                    key={item.path}
                    to={item.path}
                    className={`flex items-center rounded-2xl px-4 py-3 text-sm font-medium transition-all duration-200 ${
                      isActive(item)
                        ? 'bg-[#fff1eb] text-red-600 shadow-[inset_0_0_0_1px_rgba(243,199,183,0.8),0_16px_30px_-24px_rgba(220,38,38,0.45)]'
                        : 'text-[#6e6763] hover:bg-[rgba(247,236,230,0.82)] hover:text-[#171212]'
                    }`}
                  >
                    <svg
                      className="mr-3 h-5 w-5 shrink-0"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                    >
                      <path
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        strokeWidth={2}
                        d={item.icon}
                      />
                    </svg>
                    <span>{item.label}</span>
                  </Link>
                ))}
              </div>
            </nav>

            <div className="border-t border-[#efe2da] px-4 py-5">
              <div className="rounded-[26px] border border-[#eadfd8] bg-[rgba(255,250,247,0.95)] p-4 shadow-[0_18px_42px_-34px_rgba(72,44,24,0.42)]">
                <button
                  type="button"
                  onClick={() => setProfileExpanded((current) => !current)}
                  className="flex w-full items-center gap-3 rounded-2xl text-left transition-colors hover:bg-[rgba(255,243,237,0.8)] focus:outline-none"
                >
                  <div className="flex h-11 w-11 items-center justify-center rounded-2xl bg-[linear-gradient(135deg,#ef6b4a_0%,#dc2626_100%)] text-base font-bold text-white shadow-[0_16px_28px_-22px_rgba(220,38,38,0.65)]">
                    {(user?.username?.[0] || 'U').toUpperCase()}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-semibold text-[#171212]">{user?.username}</div>
                  </div>
                  <svg
                    className={`h-5 w-5 shrink-0 text-[#8f8681] transition-transform duration-200 ${profileExpanded ? 'rotate-180' : ''}`}
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                  >
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                  </svg>
                </button>

                {profileExpanded && (
                  <div className="mt-4 grid grid-cols-2 gap-2">
                    <div className="col-span-2 rounded-2xl border border-[#eadfd8] bg-white/92 px-3 py-3 shadow-[0_12px_28px_-24px_rgba(72,44,24,0.35)]">
                      <LanguageSwitcher />
                    </div>

                    {user?.role === 'admin' && (
                      <Link
                        to="/admin"
                        className="col-span-2 flex items-center justify-center rounded-2xl border border-[#eadfd8] bg-white/92 px-4 py-3 text-sm font-semibold text-[#5f5957] shadow-[0_14px_28px_-24px_rgba(72,44,24,0.45)] transition-all duration-200 hover:border-[#ef6b4a] hover:bg-[rgba(255,248,245,0.95)] hover:text-[#171212]"
                      >
                        <svg
                          className="mr-2 h-5 w-5"
                          fill="none"
                          viewBox="0 0 24 24"
                          stroke="currentColor"
                        >
                          <path
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            strokeWidth={2}
                            d="M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6"
                          />
                        </svg>
                        {t('nav.adminDashboard')}
                      </Link>
                    )}

                    <button
                      onClick={handleLogout}
                      className="col-span-2 flex w-full items-center justify-center rounded-2xl border border-transparent px-4 py-3 text-sm font-semibold text-white shadow-[0_18px_32px_-24px_rgba(220,38,38,0.6)] transition-all duration-200 hover:translate-y-[-1px] hover:shadow-[0_24px_36px_-24px_rgba(220,38,38,0.75)]"
                      style={{ background: 'linear-gradient(135deg, #ef6b4a 0%, #dc2626 100%)' }}
                    >
                      <svg
                        className="mr-2 h-5 w-5"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                      >
                        <path
                          strokeLinecap="round"
                          strokeLinejoin="round"
                          strokeWidth={2}
                          d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1"
                        />
                      </svg>
                      {t('common.logout')}
                    </button>
                  </div>
                )}
              </div>
            </div>
          </div>
        </aside>

        <div className="flex min-w-0 flex-1 flex-col">
          {title && (
            <div className="app-subheader">
              <div className={`${shellContainerClass} flex h-[104px] items-center`}>
                <div>
                  <div className="text-[11px] font-semibold uppercase tracking-[0.22em] text-[#b46c50]">
                    Workspace
                  </div>
                  <h1 className="mt-1 text-[2rem] font-bold tracking-[-0.04em] text-[#171212]">{title}</h1>
                </div>
              </div>
            </div>
          )}

          <main className={`${shellContainerClass} flex-1 py-8`}>
            {children}
          </main>
        </div>
      </div>
    </div>
  );
};

export default UserLayout;
