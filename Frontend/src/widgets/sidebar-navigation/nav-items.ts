// Декларативная таблица пунктов навигации Sidebar (§8.3 high-architecture).
// Причина declarative const + filter (а не render-prop <Can>-per-item): меньше
// ре-рендеров, типобезопасность, тестируемость без React.
//
// Группы и порядок = Figma 85:2 (fileKey Lxhk7jQyXL3iuoTpiOHxcb):
//   menu   → «МЕНЮ»: Главная, Проверка договора, Документы, Отчёты
//   system → «СИСТЕМА»: Настройки, Политики, Чек-листы (admin — под RBAC)
// «Сравнение версий» (Figma) опущено: нет standalone-роута (только per-договор
// /contracts/:id/compare). «Организация» из Figma реализована гранулярно как
// admin-пункты Политики/Чек-листы (реальные роуты + RBAC). Audit — v1.1 (§18 п.5).
import type { ComponentType, SVGProps } from 'react';

import type { Permission } from '@/shared/auth';

import {
  ChecklistIcon,
  ContractsIcon,
  DashboardIcon,
  NewCheckIcon,
  PoliciesIcon,
  ReportsIcon,
  SettingsIcon,
} from './icons';

export type NavGroup = 'menu' | 'system';

export interface NavItem {
  /** Стабильный ключ для React key + data-testid. */
  key: string;
  label: string;
  to: string;
  icon: ComponentType<SVGProps<SVGSVGElement>>;
  group: NavGroup;
  /**
   * Permission-ключ из shared/auth/rbac. `undefined` = пункт доступен всем
   * аутентифицированным ролям.
   */
  permission?: Permission;
  /**
   * Кастомный матчер активного состояния по pathname. Если не задан — точное
   * совпадение при `end`, иначе префиксное (`/to` или `/to/...`).
   */
  isActive?: (pathname: string) => boolean;
  /** Точное совпадение (для роутов-листов вроде /dashboard, /settings). */
  end?: boolean;
}

export const NAV_ITEMS: readonly NavItem[] = [
  {
    key: 'dashboard',
    label: 'Главная',
    to: '/dashboard',
    icon: DashboardIcon,
    group: 'menu',
    end: true,
  },
  {
    key: 'new-check',
    label: 'Проверка договора',
    to: '/contracts/new',
    icon: NewCheckIcon,
    group: 'menu',
    end: true,
  },
  {
    key: 'contracts',
    label: 'Документы',
    to: '/contracts',
    icon: ContractsIcon,
    group: 'menu',
    // Активна на /contracts и /contracts/:id, но НЕ на /contracts/new
    // (там активна «Проверка договора»).
    isActive: (p) => p === '/contracts' || (p.startsWith('/contracts/') && p !== '/contracts/new'),
  },
  { key: 'reports', label: 'Отчёты', to: '/reports', icon: ReportsIcon, group: 'menu' },
  {
    key: 'settings',
    label: 'Настройки',
    to: '/settings',
    icon: SettingsIcon,
    group: 'system',
    end: true,
  },
  {
    key: 'admin-policies',
    label: 'Политики',
    to: '/admin/policies',
    icon: PoliciesIcon,
    group: 'system',
    permission: 'admin.policies',
  },
  {
    key: 'admin-checklists',
    label: 'Чек-листы',
    to: '/admin/checklists',
    icon: ChecklistIcon,
    group: 'system',
    permission: 'admin.checklists',
  },
] as const;

/** Активен ли пункт при данном pathname (учитывает isActive / end / префикс). */
export function isNavItemActive(item: NavItem, pathname: string): boolean {
  if (item.isActive) return item.isActive(pathname);
  if (item.end) return pathname === item.to;
  return pathname === item.to || pathname.startsWith(`${item.to}/`);
}
