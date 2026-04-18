import { Outlet } from 'react-router-dom';

/**
 * Корневой layout-route для всех аутентифицированных страниц приложения.
 * В FE-TASK-031 — минимальная shell с <Outlet />. FE-TASK-032 заменит на
 * композицию SidebarNavigation + Topbar + Breadcrumbs (widgets/sidebar-navigation,
 * widgets/topbar). LandingPage / LoginPage / error-pages находятся вне layout —
 * у них собственные оформления.
 */
export function AppLayout(): JSX.Element {
  return (
    <div className="min-h-screen bg-bg text-fg">
      <Outlet />
    </div>
  );
}
