// <RequireRole> — route-guard для /admin/*, /audit/* (§5.6 Pattern A).
// Не-аутентифицированный → /login; роль вне whitelist → /403.
// Архитектура: Frontend/architecture/high-architecture.md §5.6 / §6.1 / §20.3.
import type { ReactNode } from 'react';
import { Navigate } from 'react-router-dom';

import { type UserRole, useSession } from './session-store';

export interface RequireRoleProps {
  roles: readonly UserRole[];
  children: ReactNode;
}

export function RequireRole({ roles, children }: RequireRoleProps): ReactNode {
  const role = useSession((s) => s.user?.role);
  if (!role) return <Navigate to="/login" replace />;
  if (!roles.includes(role)) return <Navigate to="/403" replace />;
  return children;
}
