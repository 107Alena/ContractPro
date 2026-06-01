// Sidebar widget (§8.3 high-architecture): левый rail с навигацией.
// Figma 85:2 (этап 4.4 App Shell): логотип-плашка, WorkspaceSwitcher (org),
// группы МЕНЮ/СИСТЕМА, активный/неактивный nav-стиль, UserProfile + logout внизу.
// Desktop: collapsed (~72px) / expanded (240px) через Zustand UI-store.
// Mobile: drawer-overlay (Radix Dialog как sheet).
//
// RBAC: admin-пункты (Политики/Чек-листы) пред-фильтруются role-based
// `can(role, permission)` — ORG_ADMIN видит, остальные нет (§5.6).
// Audit скрыт в v1 (§18 п.5).
import * as Dialog from '@radix-ui/react-dialog';
import { useEffect } from 'react';
import { Link, useLocation } from 'react-router-dom';

import { can, useSession } from '@/shared/auth';
import { useLayoutStore, useMobileDrawerOpen, useSidebarCollapsed } from '@/shared/layout';
import { cn } from '@/shared/lib/cn';
import { SimpleTooltip } from '@/shared/ui/tooltip';

import { BrandLogoIcon, ChevronDownIcon, ChevronLeftIcon, CloseIcon } from './icons';
import { isNavItemActive, NAV_ITEMS, type NavGroup, type NavItem } from './nav-items';
import { UserProfile } from './user-profile';

const GROUP_LABEL: Record<NavGroup, string> = {
  menu: 'МЕНЮ',
  system: 'СИСТЕМА',
};

interface NavItemLinkProps {
  item: NavItem;
  collapsed: boolean;
  onNavigate?: (() => void) | undefined;
}

function NavItemLink({ item, collapsed, onNavigate }: NavItemLinkProps): JSX.Element {
  const { pathname } = useLocation();
  const active = isNavItemActive(item, pathname);
  const Icon = item.icon;

  const link = (
    <Link
      to={item.to}
      onClick={onNavigate}
      aria-current={active ? 'page' : undefined}
      aria-label={collapsed ? item.label : undefined}
      data-testid={`nav-${item.key}`}
      className={cn(
        'group flex items-center gap-2.5 rounded-[8px] transition-colors duration-150',
        'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2',
        collapsed ? 'h-10 w-10 justify-center' : 'px-2.5 py-[9px]',
        active ? 'bg-brand-50' : 'hover:bg-bg-muted',
      )}
    >
      <Icon
        className={cn('size-5 shrink-0', active ? 'text-brand-500' : 'text-fg-subtle')}
        aria-hidden="true"
      />
      <span
        className={cn(
          'truncate text-14',
          collapsed && 'sr-only',
          active ? 'font-semibold text-brand-600' : 'font-medium text-fg-muted',
        )}
      >
        {item.label}
      </span>
    </Link>
  );

  if (!collapsed) return link;

  // В collapsed-rail оборачиваем в Tooltip — hover/focus раскрывает label.
  // `sr-only` label остаётся в DOM для screen-reader (WCAG SC 2.4.4).
  return (
    <SimpleTooltip content={item.label} side="right" align="center" size="sm">
      {link}
    </SimpleTooltip>
  );
}

interface NavSectionProps {
  group: NavGroup;
  items: NavItem[];
  collapsed: boolean;
  onNavigate?: (() => void) | undefined;
}

function NavSection({ group, items, collapsed, onNavigate }: NavSectionProps): JSX.Element | null {
  if (items.length === 0) return null;
  return (
    <div className="flex flex-col gap-1" role="group" aria-label={GROUP_LABEL[group]}>
      {collapsed ? (
        <div className="mx-auto my-2 h-px w-6 bg-border-subtle" aria-hidden="true" />
      ) : (
        <div className="px-2.5 pb-1 pt-2 text-11 font-semibold uppercase tracking-[1px] text-fg-disabled">
          {GROUP_LABEL[group]}
        </div>
      )}
      <ul className="flex flex-col gap-1">
        {items.map((item) => (
          <li key={item.key} className={cn(collapsed && 'flex justify-center')}>
            <NavItemLink item={item} collapsed={collapsed} onNavigate={onNavigate} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function WorkspaceSwitcher({
  collapsed,
  orgName,
  onNavigate,
}: {
  collapsed: boolean;
  orgName: string;
  onNavigate?: (() => void) | undefined;
}): JSX.Element {
  const initial =
    orgName
      .replace(/[«»"']/g, '')
      .trim()
      .charAt(0)
      .toUpperCase() || 'О';
  const badge = (
    <span
      aria-hidden="true"
      className="grid size-5 shrink-0 place-items-center rounded-[4px] bg-brand-50 text-11 font-semibold text-brand-600"
    >
      {initial}
    </span>
  );

  if (collapsed) {
    return (
      <Link
        to="/settings"
        onClick={onNavigate}
        aria-label={`Организация: ${orgName}`}
        className="mx-auto flex size-10 items-center justify-center rounded-[8px] hover:bg-bg-muted focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2"
      >
        {badge}
      </Link>
    );
  }

  return (
    <Link
      to="/settings"
      onClick={onNavigate}
      aria-label={`Организация: ${orgName}`}
      className="flex items-center gap-2 rounded-[8px] border border-border-subtle bg-bg-muted px-2.5 py-2 transition-colors hover:border-border focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2"
    >
      {badge}
      <span className="min-w-0 flex-1 truncate text-13 font-medium text-fg-strong">{orgName}</span>
      <ChevronDownIcon className="size-4 shrink-0 text-fg-subtle" aria-hidden="true" />
    </Link>
  );
}

interface SidebarContentProps {
  collapsed: boolean;
  onToggleCollapse?: (() => void) | undefined;
  onNavigate?: (() => void) | undefined;
  onClose?: (() => void) | undefined;
  variant: 'desktop' | 'mobile';
}

function SidebarContent({
  collapsed,
  onToggleCollapse,
  onNavigate,
  onClose,
  variant,
}: SidebarContentProps): JSX.Element {
  const user = useSession((s) => s.user);
  const orgName = user?.organization_name;
  const role = user?.role;

  // Пред-фильтрация по RBAC (role-based, как <Can>) — без пустых <li> для
  // скрытых admin-пунктов.
  const visible = NAV_ITEMS.filter((i) => !i.permission || can(role, i.permission));
  const menuItems = visible.filter((i) => i.group === 'menu');
  const systemItems = visible.filter((i) => i.group === 'system');

  return (
    <div className={cn('flex h-full flex-col', collapsed ? 'px-2 py-5' : 'px-4 py-5')}>
      {/* Logo */}
      <div className="flex items-center gap-2.5 px-2">
        <Link
          to="/dashboard"
          onClick={onNavigate}
          aria-label="ContractPro — на главную"
          className="flex min-w-0 items-center gap-2.5 rounded-[8px] focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2"
        >
          <BrandLogoIcon className="size-7 shrink-0 text-brand-500" />
          {!collapsed && <span className="truncate text-18 font-bold text-fg">ContractPro</span>}
        </Link>
        {variant === 'mobile' && onClose && (
          <button
            type="button"
            onClick={onClose}
            className="ml-auto inline-flex size-9 items-center justify-center rounded-[8px] text-fg-muted hover:bg-bg-muted focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2"
            aria-label="Закрыть меню"
          >
            <CloseIcon />
          </button>
        )}
      </div>

      {/* WorkspaceSwitcher */}
      {orgName && (
        <div className="mt-4">
          <WorkspaceSwitcher collapsed={collapsed} orgName={orgName} onNavigate={onNavigate} />
        </div>
      )}

      {/* Навигация: МЕНЮ сверху, СИСТЕМА прижата вниз */}
      <nav aria-label="Основная навигация" className="mt-5 flex flex-1 flex-col overflow-y-auto">
        <NavSection group="menu" items={menuItems} collapsed={collapsed} onNavigate={onNavigate} />
        <div className="flex-1" aria-hidden="true" />
        <NavSection
          group="system"
          items={systemItems}
          collapsed={collapsed}
          onNavigate={onNavigate}
        />
      </nav>

      {/* Divider + профиль пользователя */}
      <div className="mt-2 h-px w-full bg-border-subtle" aria-hidden="true" />
      <div className="mt-2">
        <UserProfile collapsed={collapsed} />
      </div>

      {/* Collapse-toggle (desktop) */}
      {variant === 'desktop' && onToggleCollapse && (
        <div className={cn('mt-1', collapsed && 'flex justify-center')}>
          <button
            type="button"
            onClick={onToggleCollapse}
            aria-label={collapsed ? 'Развернуть боковую панель' : 'Свернуть боковую панель'}
            aria-expanded={!collapsed}
            aria-controls="sidebar-navigation-aside"
            data-testid="sidebar-collapse-toggle"
            className={cn(
              'inline-flex items-center gap-2 rounded-[8px] text-13 font-medium text-fg-subtle',
              'transition-colors duration-150 hover:bg-bg-muted hover:text-fg',
              'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2',
              collapsed ? 'size-10 justify-center' : 'h-9 w-full px-2.5',
            )}
          >
            <ChevronLeftIcon
              className={cn('size-4 transition-transform', collapsed && 'rotate-180')}
            />
            {!collapsed && <span>Свернуть</span>}
          </button>
        </div>
      )}
    </div>
  );
}

export interface SidebarNavigationProps {
  /** Тест-оверрайд для Storybook/тестов: форсирует collapsed-состояние, игнорируя store. */
  forceCollapsed?: boolean;
}

export function SidebarNavigation({ forceCollapsed }: SidebarNavigationProps = {}): JSX.Element {
  const storeCollapsed = useSidebarCollapsed();
  const collapsed = forceCollapsed ?? storeCollapsed;
  const mobileOpen = useMobileDrawerOpen();
  const toggleSidebar = useLayoutStore((s) => s.toggleSidebar);
  const setMobileDrawerOpen = useLayoutStore((s) => s.setMobileDrawerOpen);
  const closeMobileDrawer = useLayoutStore((s) => s.closeMobileDrawer);
  const location = useLocation();

  // Автозакрытие drawer при смене роута.
  useEffect(() => {
    if (mobileOpen) closeMobileDrawer();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.pathname]);

  return (
    <>
      <aside
        id="sidebar-navigation-aside"
        data-testid="sidebar-desktop"
        data-collapsed={collapsed || undefined}
        aria-label="Навигация приложения"
        className={cn(
          'hidden md:flex h-screen sticky top-0 shrink-0 flex-col border-r border-border-subtle bg-bg',
          'transition-[width] duration-200',
          collapsed ? 'w-[72px]' : 'w-60',
        )}
      >
        <SidebarContent collapsed={collapsed} onToggleCollapse={toggleSidebar} variant="desktop" />
      </aside>

      <Dialog.Root open={mobileOpen} onOpenChange={setMobileDrawerOpen}>
        <Dialog.Portal>
          <Dialog.Overlay
            className={cn(
              'fixed inset-0 z-modal bg-fg/40 md:hidden',
              'motion-safe:transition-opacity motion-safe:duration-150',
              'data-[state=closed]:motion-safe:opacity-0',
            )}
          />
          <Dialog.Content
            aria-label="Навигация приложения"
            data-testid="sidebar-mobile"
            className={cn(
              'fixed inset-y-0 left-0 z-modal flex w-72 max-w-[85vw] flex-col bg-bg shadow-lg outline-none md:hidden',
              'motion-safe:transition motion-safe:duration-200',
              'data-[state=closed]:motion-safe:-translate-x-full',
            )}
          >
            <Dialog.Title className="sr-only">Основная навигация</Dialog.Title>
            <Dialog.Description className="sr-only">
              Список разделов приложения ContractPro
            </Dialog.Description>
            <SidebarContent
              collapsed={false}
              variant="mobile"
              onClose={closeMobileDrawer}
              onNavigate={closeMobileDrawer}
            />
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </>
  );
}
