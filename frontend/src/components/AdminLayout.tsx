import React, { useEffect, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import {
  Archive,
  Bot,
  ChevronRight,
  Home,
  Monitor,
  Network,
  Server,
  Settings,
  Shield,
  Users,
} from 'lucide-react';
import { useI18n } from '../contexts/I18nContext';
import { useUiStore } from '../stores/uiStore';
import { ManagementShell } from './ManagementHeader';
import { CATEGORIES } from '../pages/admin/protection/_data';

interface AdminLayoutProps {
  children: React.ReactNode;
  title?: string;
  fullHeight?: boolean;
}

interface NavItem {
  path: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  matchPaths?: string[];
  exact?: boolean;
  hasPopup?: boolean;
}

const AdminLayout: React.FC<AdminLayoutProps> = ({ children, title = '', fullHeight = false }) => {
  const location = useLocation();
  const { t } = useI18n();
  const { sidebarCollapsed } = useUiStore();
  const [popoverItemPath, setPopoverItemPath] = useState<string | null>(null);
  const [popoverPosition, setPopoverPosition] = useState({ left: 0, top: 0, maxHeight: 0 });
  const [popoverAnchor, setPopoverAnchor] = useState<{ element: HTMLElement; top: number } | null>(null);
  useEffect(() => {
    const close = () => setPopoverItemPath(null);
    const scroll = (event: Event) => {
      if (!(event.target instanceof HTMLElement) || !event.target.matches('.cm-management-sidebar .cm-shell-navigation')) return;
      const anchor = popoverAnchor;
      if (anchor && anchor.element.getBoundingClientRect().top !== anchor.top) close();
    };
    window.addEventListener('resize', close);
    document.addEventListener('scroll', scroll, true);
    return () => { window.removeEventListener('resize', close); document.removeEventListener('scroll', scroll, true); };
  }, [popoverAnchor]);
  const openPopover = (element: HTMLElement, path: string) => {
    const rect = element.getBoundingClientRect();
    setPopoverAnchor({ element, top: rect.top });
    const top = Math.max(12, Math.min(rect.top, innerHeight - 24));
    setPopoverPosition({ left: rect.right - 1, top, maxHeight: Math.max(0, innerHeight - top - 12) });
    setPopoverItemPath(path);
  };

  // 安全防护 popup 用 7 类别（移除总览 + events，跟主入口和侧边其他 nav 重复）
  const PROTECTION_POPUP_CATS = CATEGORIES.filter((c) => c.id !== 'overview' && c.id !== 'events');

  const navItems: NavItem[] = [
    { path: '/admin', label: t('nav.adminDashboard'), icon: Home, exact: true },
    { path: '/admin/backups', label: t('nav.backups'), icon: Archive },
    { path: '/admin/users', label: t('nav.users'), icon: Users },
    { path: '/admin/instances', label: t('nav.instances'), icon: Monitor },
    { path: '/admin/runtime-pods', label: t('nav.runtime'), icon: Server },
    { path: '/admin/northbound', label: t('nav.northbound'), icon: Network },
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

  const renderNavItem = (item: NavItem) => {
    const Icon = item.icon;
    return (
      <Link
        key={item.path}
        to={item.path}
        aria-label={item.label}
        aria-current={isActive(item) ? 'page' : undefined}
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
            aria-label={item.label}
            aria-current={isActive(item) ? 'page' : undefined}
            title={sidebarCollapsed ? item.label : undefined}
            className={`app-nav-link ${sidebarCollapsed ? 'justify-center' : ''} ${
              isActive(item) ? 'app-nav-link-active' : ''
            }`}
          >
            <Icon className="h-4 w-4 shrink-0" />
            {!sidebarCollapsed && <span className="truncate">{item.label}</span>}
          </Link>
        </div>
      );
    }
    return (
      <div
        key={item.path}
        className="relative"
        onFocus={event => openPopover(event.currentTarget, item.path)}
        onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setPopoverItemPath(null); }}
        onKeyDown={event => { if (event.key === 'Escape') { event.currentTarget.querySelector('a')?.focus(); setPopoverItemPath(null); event.stopPropagation(); } }}
        onMouseEnter={event => openPopover(event.currentTarget, item.path)}
        onMouseLeave={event => { if (!event.currentTarget.contains(document.activeElement)) setPopoverItemPath(null); }}
      >
        <Link
          to={item.path}
        aria-label={item.label}
        aria-current={isActive(item) ? 'page' : undefined}
          title={sidebarCollapsed ? item.label : undefined}
          className={`app-nav-link ${sidebarCollapsed ? 'justify-center' : ''} ${
            isActive(item) ? 'app-nav-link-active' : ''
          }`}
        >
          <Icon className="h-4 w-4 shrink-0" />
          {!sidebarCollapsed && <span className="truncate">{item.label}</span>}
          {!sidebarCollapsed && (
            <ChevronRight className="ml-auto h-3.5 w-3.5 shrink-0 opacity-60" />
          )}
        </Link>
        {popoverItemPath === item.path && (
          <div className="cm-management-popup cm-sidebar-protection-popup" style={popoverPosition}>
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

  return <ManagementShell admin title={title || navItems.find(isActive)?.label || t('nav.adminDashboard')} fillHeight={fullHeight} scrollableMain={fullHeight} navigation={navItems.map(renderNavItemWithPopup)} mobileNavigation={navItems.map(renderNavItem)}>{children}</ManagementShell>;
};
export default AdminLayout;
