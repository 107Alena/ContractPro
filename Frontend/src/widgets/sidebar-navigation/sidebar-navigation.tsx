// Sidebar widget (§8.3 high-architecture): основной левый rail с навигацией.
// Desktop: collapsed (~72px) / expanded (~248px) rail через Zustand UI-store.
// Mobile: drawer-overlay поверх контента (Radix Dialog как sheet).
//
// RBAC: пункты /admin/* оборачиваются в <Can I="admin.policies" | "admin.checklists">
// — ORG_ADMIN видит, остальные нет. Backend — источник истины (§5.6).
// Audit скрыт в v1 (§18 п.5) — не присутствует в NAV_ITEMS.
//
// Закрытие mobile drawer при переходе: через useEffect на location.pathname —
// надёжнее, чем вешать onClick на каждый NavLink (можно забыть нигде).
import * as Dialog from '@radix-ui/react-dialog';
import { useEffect } from 'react';
import { NavLink, useLocation } from 'react-router-dom';

import { Can, useRole } from '@/shared/auth';
import { useLayoutStore, useMobileDrawerOpen, useSidebarCollapsed } from '@/shared/layout';
import { cn } from '@/shared/lib/cn';
import { SimpleTooltip } from '@/shared/ui/tooltip';

import { BrandLogoIcon, ChevronLeftIcon, CloseIcon } from './icons';
import { NAV_ITEMS, type NavItem } from './nav-items';

const GROUP_LABELS: Record<NavItem['group'], string | null> = {
  primary: null,
  secondary: null,
  admin: 'Администрирование',
};

const GROUP_ORDER: readonly NavItem['group'][] = ['primary', 'secondary', 'admin'] as const;

function groupItems(items: readonly NavItem[]): Record<NavItem['group'], NavItem[]> {
  return {
    primary: items.filter((i) => i.group === 'primary'),
    secondary: items.filter((i) => i.group === 'secondary'),
    admin: items.filter((i) => i.group === 'admin'),
  };
}

interface NavItemLinkProps {
  item: NavItem;
  collapsed: boolean;
  onNavigate?: (() => void) | undefined;
}

function NavItemLink({ item, collapsed, onNavigate }: NavItemLinkProps): JSX.Element {
  const Icon = item.icon;
  const link = (
    <NavLink
      to={item.to}
      end={item.end ?? false}
      onClick={onNavigate}
      aria-label={collapsed ? item.label : undefined}
      className={({ isActive }) =>
        cn(
          'group flex items-center gap-3 rounded-md text-sm font-medium',
          'transition-colors duration-150',
          'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2',
          collapsed ? 'h-10 w-10 justify-center' : 'h-10 px-3',
          isActive ? 'bg-brand-50 text-brand-600' : 'text-fg-muted hover:bg-bg-muted hover:text-fg',
        )
      }
      data-testid={`nav-${item.key}`}
    >
      <Icon className={cn('shrink-0', collapsed ? 'h-5 w-5' : 'h-5 w-5')} />
      <span className={cn('truncate', collapsed && 'sr-only')}>{item.label}</span>
    </NavLink>
  );

  if (!collapsed) return link;

  // В collapsed-rail оборачиваем в Tooltip — hover/focus раскрывает label.
  // `sr-only` label остаётся в DOM для screen-reader (WCAG SC 2.4.4 Link Purpose).
  return (
    <SimpleTooltip content={item.label} side="right" align="center" size="sm">
      {link}
    </SimpleTooltip>
  );
}

interface NavSectionProps {
  items: NavItem[];
  group: NavItem['group'];
  collapsed: boolean;
  onNavigate?: (() => void) | undefined;
}

function NavSection({ items, group, collapsed, onNavigate }: NavSectionProps): JSX.Element | null {
  if (items.length === 0) return null;

  const heading = GROUP_LABELS[group];
  return (
    <div className="flex flex-col gap-1" role="group" aria-label={heading ?? undefined}>
      {heading && !collapsed && (
        <div className="px-3 pt-3 pb-1 text-xs font-semibold uppercase tracking-wide text-fg-muted">
          {heading}
        </div>
      )}
      {heading && collapsed && <div className="my-2 h-px bg-border" aria-hidden="true" />}
      <ul className="flex flex-col gap-1">
        {items.map((item) => (
          <li key={item.key} className={cn(collapsed && 'flex justify-center')}>
            {item.permission ? (
              <Can I={item.permission}>
                <NavItemLink item={item} collapsed={collapsed} onNavigate={onNavigate} />
              </Can>
            ) : (
              <NavItemLink item={item} collapsed={collapsed} onNavigate={onNavigate} />
            )}
          </li>
        ))}
      </ul>
    </div>
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
  const groups = groupItems(NAV_ITEMS);
  // Подмена пустых групп: primary всегда непуста, admin отфильтруется через
  // <Can> на рендере. Для layout (divider между secondary и admin) важен именно
  // заявленный порядок.
  const role = useRole();
  const isAdmin = role === 'ORG_ADMIN';

  return (
    <>
      <div
        className={cn(
          'flex items-center gap-2 border-b border-border',
          collapsed ? 'h-16 justify-center px-2' : 'h-16 px-4',
        )}
      >
        <NavLink
          to="/dashboard"
          className="flex items-center gap-2 text-fg focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2 rounded-md"
          onClick={onNavigate}
          aria-label="ContractPro — на главную"
        >
          <BrandLogoIcon className="text-brand-500 shrink-0" />
          {!collapsed && (
            <span className="font-semibold tracking-tight text-base">ContractPro</span>
          )}
        </NavLink>
        {variant === 'mobile' && onClose && (
          <button
            type="button"
            onClick={onClose}
            className="ml-auto inline-flex h-9 w-9 items-center justify-center rounded-md text-fg-muted hover:bg-bg-muted focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2"
            aria-label="Закрыть меню"
          >
            <CloseIcon />
          </button>
        )}
      </div>
      <nav
        aria-label="Основная навигация"
        className={cn('flex-1 overflow-y-auto', collapsed ? 'px-2 py-3' : 'px-3 py-3')}
      >
        <div className="flex flex-col gap-2">
          {GROUP_ORDER.map((group) => {
            // Оптимизация: для non-ORG_ADMIN не рендерим admin-секцию вообще,
            // избегая пустого divider + heading + ul[] при обёртке <Can>.
            if (group === 'admin' && !isAdmin) return null;
            return (
              <NavSection
                key={group}
                group={group}
                items={groups[group]}
                collapsed={collapsed}
                onNavigate={onNavigate}
              />
            );
          })}
        </div>
      </nav>
      {variant === 'desktop' && onToggleCollapse && (
        <div
          className={cn(
            'mt-auto border-t border-border',
            collapsed ? 'flex justify-center px-2 py-3' : 'px-3 py-3',
          )}
        >
          <button
            type="button"
            onClick={onToggleCollapse}
            aria-label={collapsed ? 'Развернуть боковую панель' : 'Свернуть боковую панель'}
            aria-expanded={!collapsed}
            aria-controls="sidebar-navigation-aside"
            className={cn(
              'inline-flex items-center gap-2 rounded-md text-sm font-medium text-fg-muted',
              'transition-colors duration-150 hover:bg-bg-muted hover:text-fg',
              'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2',
              collapsed ? 'h-10 w-10 justify-center' : 'h-10 w-full px-3',
            )}
            data-testid="sidebar-collapse-toggle"
          >
            <ChevronLeftIcon
              className={cn('h-4 w-4 transition-transform', collapsed && 'rotate-180')}
            />
            {!collapsed && <span>Свернуть</span>}
          </button>
        </div>
      )}
    </>
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

  // Автозакрытие drawer при смене роута — защищает от случая, когда навигация
  // произошла не по NavLink (например, через useNavigate() в action-обработчике).
  useEffect(() => {
    if (mobileOpen) closeMobileDrawer();
    // closeMobileDrawer стабилен (Zustand setter), но линтер требует явную зависимость.
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
          'hidden md:flex h-screen sticky top-0 shrink-0 flex-col border-r border-border bg-bg',
          'transition-[width] duration-200',
          collapsed ? 'w-[72px]' : 'w-[248px]',
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
