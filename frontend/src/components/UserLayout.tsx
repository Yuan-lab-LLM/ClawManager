import React from 'react';
import { Link, useLocation } from 'react-router-dom';
import { Home, Monitor, Package, Settings, SlidersHorizontal, Users } from 'lucide-react';
import { useI18n } from '../contexts/I18nContext';
import { useUiStore } from '../stores/uiStore';
import { ManagementShell } from './ManagementHeader';

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

const UserLayout: React.FC<UserLayoutProps> = ({
  children,
  title,
  titleAccessory,
  fillHeight = false,
  scrollableMain = false,
}) => {
  const location = useLocation();
  const { t } = useI18n();
  const { sidebarCollapsed } = useUiStore();

  const navItems: UserNavItem[] = [
    { path: '/dashboard', label: t('nav.userDashboard'), icon: Home, exact: true },
    { path: '/instances', label: t('nav.myInstances'), icon: Monitor },
    { path: '/teams', label: t('nav.teams'), icon: Users },
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

  const renderNavItem = (item: UserNavItem, collapsed = false) => {
    const Icon = item.icon;
    return (
      <Link
        key={item.path}
        to={item.path}
        aria-label={item.label}
        aria-current={isActive(item) ? 'page' : undefined}
        title={collapsed ? item.label : undefined}
        className={`app-nav-link ${collapsed ? 'justify-center' : ''} ${
          isActive(item) ? 'app-nav-link-active' : ''
        }`}
      >
        <Icon className="h-4 w-4 shrink-0" />
        {!collapsed && <span className="truncate">{item.label}</span>}
      </Link>
    );
  };

  return <ManagementShell title={title || navItems.find(isActive)?.label || t('nav.userDashboard')} titleAccessory={titleAccessory} fillHeight={fillHeight} scrollableMain={scrollableMain} navigation={navItems.map(item => renderNavItem(item, sidebarCollapsed))} mobileNavigation={navItems.map(item => renderNavItem(item))}>{children}</ManagementShell>;
};
export default UserLayout;
