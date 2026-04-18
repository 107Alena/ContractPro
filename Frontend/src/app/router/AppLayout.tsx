import { Outlet } from 'react-router-dom';

import { SidebarNavigation } from '@/widgets/sidebar-navigation';

/**
 * Корневой layout-route для всех аутентифицированных страниц приложения (§8.3).
 * FE-TASK-032: встраивает SidebarNavigation (desktop rail + mobile drawer).
 * Topbar и Breadcrumbs (FE-TASK-033) встраиваются в стэк выше <Outlet />
 * следующей итерацией: sidebar становится sticky-колонкой слева, а топ-бар
 * — внутри main-секции сверху контента.
 */
export function AppLayout(): JSX.Element {
  return (
    <div className="min-h-screen bg-bg text-fg flex">
      <SidebarNavigation />
      <div className="flex flex-1 flex-col min-w-0">
        <main className="flex-1 min-w-0">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
