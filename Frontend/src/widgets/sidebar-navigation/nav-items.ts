// Декларативная таблица пунктов навигации Sidebar (§8.3 high-architecture).
// Причина declarative const + filter (а не render-prop <Can>-per-item): меньше
// ре-рендеров, типобезопасность, тестируемость без React. <Can>-guard'ы
// задумывались для разнородных секций, а не для простых list-фильтраций.
//
// Порядок пунктов = порядок в Figma (fileKey Lxhk7jQyXL3iuoTpiOHxcb):
// primary → secondary → admin. Audit намеренно не включён (v1.1, §18 п.5).
import type { ComponentType, SVGProps } from 'react';

import type { Permission } from '@/shared/auth';

import {
  ChecklistIcon,
  ContractsIcon,
  DashboardIcon,
  PoliciesIcon,
  ReportsIcon,
  SettingsIcon,
} from './icons';

export type NavGroup = 'primary' | 'secondary' | 'admin';

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
   * Для react-router NavLink `end`-prop. По умолчанию `false` (default NavLink
   * behavior): подсветка активна при любом вложенном пути. Dashboard/Settings
   * — роуты-листы, поэтому `end: true` чтобы не подсвечивались на вложенных.
   */
  end?: boolean;
}

export const NAV_ITEMS: readonly NavItem[] = [
  {
    key: 'dashboard',
    label: 'Главная',
    to: '/dashboard',
    icon: DashboardIcon,
    group: 'primary',
    end: true,
  },
  { key: 'contracts', label: 'Контракты', to: '/contracts', icon: ContractsIcon, group: 'primary' },
  { key: 'reports', label: 'Отчёты', to: '/reports', icon: ReportsIcon, group: 'primary' },
  {
    key: 'settings',
    label: 'Настройки',
    to: '/settings',
    icon: SettingsIcon,
    group: 'secondary',
    end: true,
  },
  {
    key: 'admin-policies',
    label: 'Политики',
    to: '/admin/policies',
    icon: PoliciesIcon,
    group: 'admin',
    permission: 'admin.policies',
  },
  {
    key: 'admin-checklists',
    label: 'Чек-листы',
    to: '/admin/checklists',
    icon: ChecklistIcon,
    group: 'admin',
    permission: 'admin.checklists',
  },
] as const;
