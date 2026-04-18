import { Outlet } from 'react-router-dom';

import { RequireRole } from '@/shared/auth';

/**
 * Layout-обёртка для всей admin-секции (§5.6 Pattern A, §6.1).
 * Один <RequireRole> покрывает все вложенные admin-маршруты:
 * - /admin/policies
 * - /admin/checklists
 * Не-аутентифицированный → /login; не-ORG_ADMIN → /403.
 */
export function AdminLayout(): JSX.Element {
  return (
    <RequireRole roles={['ORG_ADMIN']}>
      <Outlet />
    </RequireRole>
  );
}
