import { Outlet } from 'react-router-dom';

import { Breadcrumbs } from '@/widgets/breadcrumbs';
import { SidebarNavigation } from '@/widgets/sidebar-navigation';
import { Topbar } from '@/widgets/topbar';

/**
 * Корневой layout-route для всех аутентифицированных страниц приложения (§8.3).
 * FE-TASK-032: SidebarNavigation как sticky-колонка слева (desktop rail + mobile drawer).
 * FE-TASK-033: Topbar (sticky top, несёт sticky-баннер offline §9.3) и Breadcrumbs
 * (под Topbar, перед Outlet) — встроены в main-колонку.
 */
export function AppLayout(): JSX.Element {
  return (
    <div className="min-h-screen bg-bg text-fg flex">
      <SidebarNavigation />
      <div className="flex flex-1 flex-col min-w-0">
        <Topbar />
        <Breadcrumbs />
        <main className="flex-1 min-w-0">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
