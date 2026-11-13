import React, { useEffect, useRef } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { ChevronDown, ChevronLeft, ChevronRight, Home, LogOut, Menu, Shield, User, X } from 'lucide-react';
import { useAuth } from '../contexts/AuthContext';
import { useI18n } from '../contexts/I18nContext';
import { useUiStore } from '../stores/uiStore';
import AppVersion from './AppVersion';
import FilterSelect from './FilterSelect';

export interface ManagementHeaderProps {
  context: React.ReactNode;
  current: React.ReactNode;
  actions?: React.ReactNode;
}
export default function ManagementHeader({ context, current, actions }: ManagementHeaderProps) {
  const { t } = useI18n();
  return <header className="app-topbar cm-management-header">{context}<nav aria-label={t('shell.currentPage')} className="cm-current-page"><span aria-current="page">{current}</span></nav><div className="cm-header-actions">{actions}</div></header>;
}

interface HeaderProps {
  title: string;
  titleAccessory?: React.ReactNode;
  admin?: boolean;
  onOpenNavigation: () => void;
  navigationTrigger: React.RefObject<HTMLButtonElement | null>;
}

function ShellHeader({ title, titleAccessory, admin, onOpenNavigation, navigationTrigger }: HeaderProps) {
  const { user, logout } = useAuth();
  const { t, locale, setLocale, localeOptions } = useI18n();
  const navigate = useNavigate();
  const contextLabel = t(admin ? 'nav.adminDashboard' : 'nav.userDashboard');
  const account = useRef<HTMLDetailsElement>(null);
  const accountTrigger = useRef<HTMLElement>(null);
  useEffect(() => {
    const close = (event: PointerEvent | KeyboardEvent) => {
      if (!account.current?.open || event.defaultPrevented) return;
      if (event instanceof KeyboardEvent && event.key === 'Escape') {
        account.current.open = false;
        accountTrigger.current?.focus();
      } else if (event instanceof PointerEvent && !account.current.contains(event.target as Node)) account.current.open = false;
    };
    document.addEventListener('pointerdown', close);
    document.addEventListener('keydown', close);
    return () => { document.removeEventListener('pointerdown', close); document.removeEventListener('keydown', close); };
  }, []);
  const handleLogout = async () => {
    try { await logout(); } catch { /* The login page retains revocation errors. */ }
    finally { navigate('/login', { replace: true }); }
  };
  return <ManagementHeader context={<>{title !== contextLabel && <Link className="cm-header-context" to={admin ? '/admin' : '/dashboard'}>{admin ? <Shield size={15} /> : <Home size={15} />}<span>{contextLabel}</span><ChevronRight size={13} /></Link>}<button ref={navigationTrigger} className="cm-icon-button cm-mobile-nav-trigger" aria-label={t('shell.openNavigation')} aria-haspopup="dialog" onClick={onOpenNavigation}><Menu size={17} /></button></>} current={title} actions={<>{titleAccessory}
      <details ref={account} className="cm-account">
        <summary ref={accountTrigger} aria-label={t('shell.account')}><User size={16} /><span className="cm-account-name">{user?.username}</span><ChevronDown size={13} /></summary>
        <div className="cm-management-popup cm-account-popup">
          <div className="cm-account-identity">{user?.username}<span>{user?.role}</span></div>
          <div className="cm-shell-language"><span>{t('common.language')}</span>
            <FilterSelect label={t('common.language')} value={locale} options={localeOptions} onChange={value => setLocale(value as typeof locale)} />
          </div>
          {(admin || user?.role === 'admin') && <Link to={admin ? '/dashboard' : '/admin'} onClick={() => { if (account.current) account.current.open = false; }}>{t(admin ? 'nav.backToUserDashboard' : 'nav.adminDashboard')}</Link>}
          <button type="button" onClick={handleLogout}><LogOut size={14} />{t('common.logout')}</button>
        </div>
      </details>
    </>} />;
}

interface ShellProps {
  children: React.ReactNode;
  title: string;
  titleAccessory?: React.ReactNode;
  admin?: boolean;
  fillHeight?: boolean;
  scrollableMain?: boolean;
  navigation: React.ReactNode;
  mobileNavigation: React.ReactNode;
}

export function ManagementShell({ children, title, titleAccessory, admin, fillHeight, scrollableMain, navigation, mobileNavigation }: ShellProps) {
  const { t } = useI18n();
  const location = useLocation();
  const navigate = useNavigate();
  const { sidebarCollapsed, toggleSidebarCollapsed } = useUiStore();
  const drawer = useRef<HTMLDialogElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const closeDrawer = () => { drawer.current?.close(); trigger.current?.focus(); };
  useEffect(() => {
    if (!location.state?.cmNavigationFocus) return;
    trigger.current?.focus();
    const state = { ...location.state };
    delete state.cmNavigationFocus;
    navigate(`${location.pathname}${location.search}${location.hash}`, { replace: true, state });
  }, [location, navigate]);
  useEffect(() => {
    const breakpoint = matchMedia('(min-width: 768px)');
    const close = () => { if (breakpoint.matches && drawer.current?.open) drawer.current.close(); };
    breakpoint.addEventListener('change', close);
    return () => breakpoint.removeEventListener('change', close);
  }, []);
  const brand = <Link to={admin ? '/admin' : '/dashboard'} className="cm-shell-brand"><img src="/lobster_logo.png" alt="ClawManager logo" /><span>{t('app.name')}</span></Link>;
  return <div className={`app-shell cm-management-shell ${sidebarCollapsed ? 'cm-sidebar-collapsed' : ''} ${fillHeight ? 'cm-shell-fill' : ''}`}>
    <aside className="cm-management-sidebar">
      <div className="cm-sidebar-inner">{brand}<nav aria-label={t('shell.mainNavigation')} className="cm-shell-navigation">{navigation}</nav><div className="cm-shell-version"><AppVersion /></div></div>
      <button type="button" onClick={toggleSidebarCollapsed} aria-label={t(sidebarCollapsed ? 'common.expandSidebar' : 'common.collapseSidebar')} className="cm-sidebar-edge">{sidebarCollapsed ? <ChevronRight size={14} /> : <ChevronLeft size={14} />}</button>
    </aside>
    <div className="cm-shell-content">
      <ShellHeader title={title} titleAccessory={titleAccessory} admin={admin} navigationTrigger={trigger} onOpenNavigation={() => drawer.current?.showModal()} />
      <main className={`cm-shell-main ${scrollableMain ? 'cm-main-scrollable' : ''}`}>{children}</main>
    </div>
    <dialog ref={drawer} aria-label={t('shell.mainNavigation')} className="cm-navigation-drawer" onClickCapture={event => {
        const link = (event.target as HTMLElement).closest('a');
        if (!link || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        event.preventDefault();
        closeDrawer();
        navigate(link.getAttribute('href')!, { state: { ...location.state, cmNavigationFocus: true } });
      }} onCancel={event => { event.preventDefault(); closeDrawer(); }} onClick={event => { if (event.target === event.currentTarget) closeDrawer(); }}>
      <div className="cm-drawer-heading">{brand}<button type="button" className="cm-icon-button" aria-label={t('shell.closeNavigation')} onClick={closeDrawer}><X size={18} /></button></div>
      <nav aria-label={t('shell.mainNavigation')} className="cm-shell-navigation">{mobileNavigation}</nav>
    </dialog>
  </div>;
}
